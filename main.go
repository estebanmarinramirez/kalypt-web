package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"
)

type pageData struct {
	Title string
	Path  string
}

type app struct {
	mux       *http.ServeMux
	templates *template.Template
	supabase  *supabaseClient
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
	}
	a.routes()
	return a
}

func (a *app) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mux.ServeHTTP(w, r)
}

func (a *app) routes() {
	static := http.FileServer(http.Dir("static"))
	a.mux.Handle("/static/", http.StripPrefix("/static/", static))
	a.mux.HandleFunc("/", a.page("home", "Kalypt"))
	a.mux.HandleFunc("/privacy", a.page("privacy", "Privacy"))
	a.mux.HandleFunc("/terms", a.page("terms", "Terms"))
	a.mux.HandleFunc("/cookies", a.page("cookies", "Cookies"))
	a.mux.HandleFunc("/gdpr", a.page("gdpr", "GDPR"))
	a.mux.HandleFunc("/contact", a.contact)
}

func (a *app) page(name, title string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
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
	if err := r.ParseForm(); err != nil {
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
		log.Printf("inquiry received name=%q email=%q focus=%q message_chars=%d", name, email, focus, len(message))
		writeJSON(w, http.StatusOK, "Inquiry received. We will respond if there is a fit.")
	}
}

func newSupabaseClientFromEnv() *supabaseClient {
	table := strings.TrimSpace(os.Getenv("SUPABASE_INQUIRIES_TABLE"))
	if table == "" {
		table = "inquiries"
	}
	return &supabaseClient{
		url:        strings.TrimRight(strings.TrimSpace(os.Getenv("SUPABASE_URL")), "/"),
		secretKey:  strings.TrimSpace(os.Getenv("SUPABASE_SECRET_KEY")),
		table:      table,
		httpClient: &http.Client{Timeout: 6 * time.Second},
	}
}

func (c *supabaseClient) insertInquiry(ctx context.Context, item inquiry) error {
	if c == nil || c.url == "" || c.secretKey == "" {
		return errors.New("supabase configuration missing")
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
	req.Header.Set("Authorization", "Bearer "+c.secretKey)
	req.Header.Set("Prefer", "return=minimal")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send supabase request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("supabase returned status %d", resp.StatusCode)
	}
	return nil
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
