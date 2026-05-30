package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxContactBodyBytes int64 = 1 << 20

var supabaseTablePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,62}$`)

type pageData struct {
	Title string
	Path  string
}

type app struct {
	mux       *http.ServeMux
	templates *template.Template
	supabase  *supabaseClient
	notifier  inquiryNotifier
}

type inquiry struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	Focus      string `json:"focus,omitempty"`
	Message    string `json:"message"`
	UserAgent  string `json:"user_agent,omitempty"`
	RemoteAddr string `json:"remote_addr,omitempty"`
}

type supabaseClient struct {
	url        string
	secretKey  string
	table      string
	httpClient *http.Client
}

type inquiryNotifier interface {
	sendInquiry(*http.Request, inquiry) error
}

type smtpNotifier struct {
	host     string
	port     int
	username string
	password string
	from     string
	to       string
}

type resendNotifier struct {
	apiURL     string
	apiKey     string
	from       string
	to         string
	httpClient *http.Client
}

func main() {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           newApp(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("kalypt listening on port %s", port)
	log.Fatal(srv.ListenAndServe())
}

func newApp() http.Handler {
	tmpl := template.Must(template.ParseGlob("templates/*.html"))
	a := &app{
		mux:       http.NewServeMux(),
		templates: tmpl,
		supabase:  newSupabaseClientFromEnv(),
		notifier:  newInquiryNotifierFromEnv(),
	}
	a.routes()
	return a
}

func (a *app) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	a.mux.ServeHTTP(w, r)
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; connect-src 'self'; img-src 'self' data:; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
}

func (a *app) routes() {
	static := http.FileServer(http.Dir("static"))
	a.mux.Handle("/static/", http.StripPrefix("/static/", static))
	a.mux.HandleFunc("/", a.page("home", "Kalypt"))
	a.mux.HandleFunc("/preview", a.page("preview", "Kalypt - Ma Preview"))
	a.mux.HandleFunc("/privacy", a.page("privacy", "Privacy"))
	a.mux.HandleFunc("/terms", a.page("terms", "Terms"))
	a.mux.HandleFunc("/cookies", a.page("cookies", "Cookies"))
	a.mux.HandleFunc("/gdpr", a.page("gdpr", "GDPR"))
	a.mux.HandleFunc("/contact", a.contact)
}

func (a *app) page(name, title string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if name == "home" && r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		err := a.templates.ExecuteTemplate(w, name+".html", pageData{Title: title, Path: r.URL.Path})
		if err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	}
}

func (a *app) contact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxContactBodyBytes)
	if err := parseContactForm(r); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeJSON(w, http.StatusRequestEntityTooLarge, "Inquiry is too large.")
			return
		}
		writeJSON(w, http.StatusBadRequest, "Invalid form submission.")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.TrimSpace(r.FormValue("email"))
	message := strings.TrimSpace(r.FormValue("message"))
	focus := strings.TrimSpace(r.FormValue("market"))

	switch {
	case len(name) < 2:
		writeJSON(w, http.StatusBadRequest, "Please enter your name.")
	case !validEmail(email):
		writeJSON(w, http.StatusBadRequest, "Please enter a valid email address.")
	case len(message) < 12:
		writeJSON(w, http.StatusBadRequest, "Please include a short message.")
	default:
		err := a.supabase.insertInquiry(r.Context(), inquiry{
			Name:       name,
			Email:      email,
			Focus:      focus,
			Message:    message,
			UserAgent:  r.UserAgent(),
			RemoteAddr: r.RemoteAddr,
		})
		if err != nil {
			log.Printf("supabase inquiry insert failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, "Inquiry storage is temporarily unavailable.")
			return
		}
		notification := inquiry{
			Name:       name,
			Email:      email,
			Focus:      focus,
			Message:    message,
			UserAgent:  r.UserAgent(),
			RemoteAddr: r.RemoteAddr,
		}
		go a.sendInquiryNotification(r, notification)
		log.Printf("inquiry received focus=%q message_chars=%d", focus, len(message))
		writeJSON(w, http.StatusOK, "Inquiry received. We will respond if there is a fit.")
	}
}

func (a *app) sendInquiryNotification(r *http.Request, item inquiry) {
	if err := a.notifier.sendInquiry(r, item); err != nil {
		log.Printf("inquiry email notification failed: %v", err)
	}
}

func parseContactForm(r *http.Request) error {
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		return r.ParseMultipartForm(1 << 20)
	}
	return r.ParseForm()
}

func newSupabaseClientFromEnv() *supabaseClient {
	table := strings.TrimSpace(os.Getenv("SUPABASE_INQUIRIES_TABLE"))
	if table == "" {
		table = "inquiries"
	}
	secretKey := strings.TrimSpace(os.Getenv("SUPABASE_SECRET_KEY"))
	if secretKey == "" {
		secretKey = strings.TrimSpace(os.Getenv("SUPABASE_SERVICE_ROLE_KEY"))
	}
	return &supabaseClient{
		url:        normalizeSupabaseURL(os.Getenv("SUPABASE_URL")),
		secretKey:  normalizeSupabaseKey(secretKey),
		table:      table,
		httpClient: &http.Client{Timeout: 6 * time.Second},
	}
}

func normalizeSupabaseURL(raw string) string {
	value := strings.TrimRight(strings.TrimSpace(raw), "/")
	value = strings.TrimSuffix(value, "/rest/v1")
	return strings.TrimRight(value, "/")
}

func normalizeSupabaseKey(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.Trim(value, `"'`)
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "Bearer ")
	return strings.TrimSpace(value)
}

func isLegacyJWTKey(key string) bool {
	return strings.HasPrefix(key, "eyJ")
}

func (c *supabaseClient) insertInquiry(ctx context.Context, item inquiry) error {
	if c == nil || c.url == "" || c.secretKey == "" {
		return errors.New("supabase configuration missing")
	}
	if err := validateSupabaseConfig(c.url, c.table); err != nil {
		return err
	}
	body, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("encode inquiry: %w", err)
	}
	endpoint := c.url + "/rest/v1/" + c.table
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build supabase request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", c.secretKey)
	if isLegacyJWTKey(c.secretKey) {
		req.Header.Set("Authorization", "Bearer "+c.secretKey)
	}
	req.Header.Set("Prefer", "return=minimal")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send supabase request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("supabase returned status %d: %s", resp.StatusCode, limitedResponseBody(resp.Body))
	}
	return nil
}

func limitedResponseBody(body io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(body, 512))
	if err != nil {
		return "unable to read response body"
	}
	return strings.TrimSpace(string(data))
}

func validateSupabaseConfig(rawURL, table string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("supabase URL invalid: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || !strings.HasSuffix(parsed.Host, ".supabase.co") {
		return errors.New("supabase URL must be an https supabase.co project URL")
	}
	if !supabaseTablePattern.MatchString(table) {
		return errors.New("supabase table name invalid")
	}
	return nil
}

func newInquiryNotifierFromEnv() inquiryNotifier {
	if strings.TrimSpace(os.Getenv("RESEND_API_KEY")) != "" {
		return newResendNotifierFromEnv()
	}
	return newSMTPNotifierFromEnv()
}

func newResendNotifierFromEnv() *resendNotifier {
	to := strings.TrimSpace(os.Getenv("INQUIRY_EMAIL_TO"))
	if to == "" {
		to = "estebanmarinramirez@icloud.com"
	}
	apiURL := strings.TrimSpace(os.Getenv("RESEND_API_URL"))
	if apiURL == "" {
		apiURL = "https://api.resend.com/emails"
	}
	return &resendNotifier{
		apiURL:     apiURL,
		apiKey:     strings.TrimSpace(os.Getenv("RESEND_API_KEY")),
		from:       strings.TrimSpace(os.Getenv("RESEND_FROM")),
		to:         to,
		httpClient: &http.Client{Timeout: 6 * time.Second},
	}
}

func (n *resendNotifier) sendInquiry(r *http.Request, item inquiry) error {
	if n == nil || n.apiURL == "" || n.apiKey == "" || n.from == "" || n.to == "" {
		return errors.New("resend configuration missing")
	}
	if !validEmail(n.to) {
		return errors.New("inquiry recipient email invalid")
	}
	subject, body := inquiryEmailContent(item)
	payload := map[string]any{
		"from":    n.from,
		"to":      []string{n.to},
		"subject": subject,
		"text":    body,
	}
	if validEmail(item.Email) {
		payload["reply_to"] = item.Email
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode resend request: %w", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, n.apiURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+n.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send resend request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("resend returned status %d: %s", resp.StatusCode, limitedResponseBody(resp.Body))
	}
	return nil
}

func newSMTPNotifierFromEnv() *smtpNotifier {
	port := 587
	if raw := strings.TrimSpace(os.Getenv("SMTP_PORT")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			port = parsed
		}
	}
	to := strings.TrimSpace(os.Getenv("INQUIRY_EMAIL_TO"))
	if to == "" {
		to = "estebanmarinramirez@icloud.com"
	}
	return &smtpNotifier{
		host:     strings.TrimSpace(os.Getenv("SMTP_HOST")),
		port:     port,
		username: strings.TrimSpace(os.Getenv("SMTP_USERNAME")),
		password: strings.TrimSpace(os.Getenv("SMTP_PASSWORD")),
		from:     strings.TrimSpace(os.Getenv("SMTP_FROM")),
		to:       to,
	}
}

func (n *smtpNotifier) sendInquiry(r *http.Request, item inquiry) error {
	if n == nil || n.host == "" || n.username == "" || n.password == "" || n.from == "" || n.to == "" {
		return errors.New("smtp configuration missing")
	}
	if !validEmail(n.to) {
		return errors.New("inquiry recipient email invalid")
	}
	if !validEmail(n.from) {
		return errors.New("smtp from email invalid")
	}
	subject, body := inquiryEmailContent(item)
	msg := strings.Join([]string{
		"From: " + n.from,
		"To: " + n.to,
		"Reply-To: " + cleanHeader(item.Email),
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")
	addr := n.host + ":" + strconv.Itoa(n.port)
	auth := smtp.PlainAuth("", n.username, n.password, n.host)
	return sendMailWithTimeout(addr, auth, n.from, []string{n.to}, []byte(msg), 4*time.Second)
}

func inquiryEmailContent(item inquiry) (string, string) {
	subject := "New Kalypt inquiry: " + cleanHeader(item.Focus)
	if strings.TrimSpace(item.Focus) == "" {
		subject = "New Kalypt inquiry"
	}
	body := fmt.Sprintf(
		"New Kalypt inquiry\n\nName: %s\nEmail: %s\nFocus: %s\nMessage:\n%s\n\nUser-Agent: %s\nRemote: %s\n",
		item.Name,
		item.Email,
		item.Focus,
		item.Message,
		item.UserAgent,
		item.RemoteAddr,
	)
	return subject, body
}

func sendMailWithTimeout(addr string, auth smtp.Auth, from string, to []string, msg []byte, timeout time.Duration) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		config := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
		if err := client.StartTLS(config); err != nil {
			return err
		}
	}
	if auth != nil {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(auth); err != nil {
				return err
			}
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, addr := range to {
		if err := client.Rcpt(addr); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(msg); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func cleanHeader(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func validEmail(value string) bool {
	addr, err := mail.ParseAddress(value)
	return err == nil && addr.Address == value && strings.Contains(addr.Address, ".")
}

func writeJSON(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}
