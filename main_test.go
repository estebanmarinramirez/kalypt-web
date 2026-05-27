package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestContactRejectsInvalidEmail(t *testing.T) {
	app := newApp()
	form := url.Values{
		"name":    {"Ada"},
		"email":   {"not-an-email"},
		"message": {"Please send fund information."},
	}
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "valid email") {
		t.Fatalf("expected email validation message, got %q", rec.Body.String())
	}
}

func TestPagesIncludeSecurityHeaders(t *testing.T) {
	app := newApp()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	app.ServeHTTP(rec, req)

	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
		"X-Frame-Options":        "DENY",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Fatalf("%s expected %q, got %q", header, want, got)
		}
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Fatalf("expected CSP default-src, got %q", got)
	}
}

func TestPagesAllowHeadRequests(t *testing.T) {
	app := newApp()
	req := httptest.NewRequest(http.MethodHead, "/", nil)
	rec := httptest.NewRecorder()

	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestContactAcceptsValidInquiry(t *testing.T) {
	var captured map[string]string
	var emailed inquiry
	handler := newTestApp(t, "https://project.supabase.co", "eyJlegacy-service-role")
	site := handler.(*app)
	site.notifier = notifierFunc(func(_ *http.Request, item inquiry) error {
		emailed = item
		return nil
	})
	site.supabase.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/rest/v1/inquiries" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("apikey") != "eyJlegacy-service-role" {
			t.Fatalf("missing Supabase apikey header")
		}
		if r.Header.Get("Authorization") != "Bearer eyJlegacy-service-role" {
			t.Fatalf("missing Supabase authorization header")
		}
		if r.Header.Get("Prefer") != "return=minimal" {
			t.Fatalf("unexpected Prefer header %q", r.Header.Get("Prefer"))
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusCreated,
			Body:       io.NopCloser(bytes.NewBufferString("{}")),
			Header:     make(http.Header),
		}, nil
	})}

	form := url.Values{
		"name":    {"Ada Lovelace"},
		"email":   {"ada@example.com"},
		"market":  {"securities"},
		"message": {"I would like to understand your research process."},
	}
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "received") {
		t.Fatalf("expected success response, got %q", rec.Body.String())
	}
	if captured["name"] != "Ada Lovelace" || captured["email"] != "ada@example.com" || captured["focus"] != "securities" {
		t.Fatalf("unexpected Supabase payload: %#v", captured)
	}
	if emailed.Name != "Ada Lovelace" || emailed.Email != "ada@example.com" || emailed.Focus != "securities" {
		t.Fatalf("unexpected email payload: %#v", emailed)
	}
}

func TestContactRejectsOversizedBody(t *testing.T) {
	handler := newTestApp(t, "https://project.supabase.co", "test-secret")
	form := url.Values{
		"name":    {"Ada Lovelace"},
		"email":   {"ada@example.com"},
		"market":  {"careers"},
		"message": {strings.Repeat("x", int(maxContactBodyBytes)+1)},
	}
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSupabaseConfigRejectsInvalidURLAndTable(t *testing.T) {
	t.Setenv("SUPABASE_URL", "http://example.com")
	t.Setenv("SUPABASE_SECRET_KEY", "secret")
	client := newSupabaseClientFromEnv()
	if err := client.insertInquiry(contextWithTimeout(t), inquiry{Name: "Ada", Email: "ada@example.com", Message: "hello world hello"}); err == nil {
		t.Fatal("expected invalid Supabase URL to be rejected")
	}

	t.Setenv("SUPABASE_URL", "https://project.supabase.co")
	t.Setenv("SUPABASE_INQUIRIES_TABLE", "../weird")
	client = newSupabaseClientFromEnv()
	if err := client.insertInquiry(contextWithTimeout(t), inquiry{Name: "Ada", Email: "ada@example.com", Message: "hello world hello"}); err == nil {
		t.Fatal("expected invalid Supabase table to be rejected")
	}
}

func TestSupabaseClientNormalizesRESTEndpointURL(t *testing.T) {
	t.Setenv("SUPABASE_URL", "https://project.supabase.co/rest/v1")
	t.Setenv("SUPABASE_SECRET_KEY", "secret")
	t.Setenv("SUPABASE_INQUIRIES_TABLE", "inquiries")

	client := newSupabaseClientFromEnv()

	if client.url != "https://project.supabase.co" {
		t.Fatalf("expected normalized project URL, got %q", client.url)
	}
}

func TestSupabaseClientAcceptsServiceRoleKeyAlias(t *testing.T) {
	t.Setenv("SUPABASE_URL", "https://project.supabase.co")
	t.Setenv("SUPABASE_SECRET_KEY", "")
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "service-role-secret")

	client := newSupabaseClientFromEnv()

	if client.secretKey != "service-role-secret" {
		t.Fatalf("expected service role alias to populate secret key")
	}
}

func TestSupabaseClientNormalizesPastedKey(t *testing.T) {
	t.Setenv("SUPABASE_URL", "https://project.supabase.co")
	t.Setenv("SUPABASE_SECRET_KEY", "\"Bearer service-role-secret\"")

	client := newSupabaseClientFromEnv()

	if client.secretKey != "service-role-secret" {
		t.Fatalf("expected normalized key, got %q", client.secretKey)
	}
}

func TestSupabaseInsertDoesNotSendSecretKeyAsBearerToken(t *testing.T) {
	client := &supabaseClient{
		url:       "https://project.supabase.co",
		secretKey: "sb_secret_test",
		table:     "inquiries",
	}
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("apikey"); got != "sb_secret_test" {
			t.Fatalf("expected apikey header, got %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("did not expect Authorization header for opaque secret key, got %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusCreated,
			Body:       io.NopCloser(bytes.NewBufferString("{}")),
			Header:     make(http.Header),
		}, nil
	})}

	err := client.insertInquiry(contextWithTimeout(t), inquiry{
		Name:    "Ada",
		Email:   "ada@example.com",
		Message: "hello world hello",
	})

	if err != nil {
		t.Fatalf("insert inquiry: %v", err)
	}
}

func TestContactAcceptsBrowserFormDataSubmission(t *testing.T) {
	var captured map[string]string
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{
		"name":    "Ada Lovelace",
		"email":   "ada@example.com",
		"market":  "careers",
		"message": "I would like to discuss research roles.",
	} {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write multipart field: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	handler := newTestApp(t, "https://project.supabase.co", "test-secret")
	site := handler.(*app)
	site.notifier = notifierFunc(func(_ *http.Request, _ inquiry) error {
		return nil
	})
	site.supabase.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusCreated,
			Body:       io.NopCloser(bytes.NewBufferString("{}")),
			Header:     make(http.Header),
		}, nil
	})}

	req := httptest.NewRequest(http.MethodPost, "/contact", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if captured["name"] != "Ada Lovelace" {
		t.Fatalf("expected multipart name to be captured, got %#v", captured)
	}
}

func TestContactReturnsServerErrorWhenSupabaseIsMissing(t *testing.T) {
	app := newTestApp(t, "", "")
	form := url.Values{
		"name":    {"Ada Lovelace"},
		"email":   {"ada@example.com"},
		"market":  {"careers"},
		"message": {"I would like to understand your research process."},
	}
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "temporarily unavailable") {
		t.Fatalf("expected configuration error response, got %q", rec.Body.String())
	}
}

func TestContactReturnsServerErrorWhenEmailIsMissing(t *testing.T) {
	handler := newTestApp(t, "https://project.supabase.co", "test-secret")
	site := handler.(*app)
	site.supabase.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusCreated,
			Body:       io.NopCloser(bytes.NewBufferString("{}")),
			Header:     make(http.Header),
		}, nil
	})}
	site.notifier = newSMTPNotifierFromEnv()

	form := url.Values{
		"name":    {"Ada Lovelace"},
		"email":   {"ada@example.com"},
		"market":  {"careers"},
		"message": {"I would like to understand your research process."},
	}
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "notification") {
		t.Fatalf("expected notification error response, got %q", rec.Body.String())
	}
}

func TestSMTPNotifierDefaultsToCorrectRecipient(t *testing.T) {
	t.Setenv("INQUIRY_EMAIL_TO", "")
	notifier := newSMTPNotifierFromEnv()

	if notifier.to != "estebanmarinramirez@icloud.com" {
		t.Fatalf("unexpected default recipient %q", notifier.to)
	}
}

func TestLegalPagesRender(t *testing.T) {
	app := newApp()
	for _, path := range []string{"/privacy", "/terms", "/cookies", "/gdpr"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		app.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s expected 200, got %d", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Kalypt") {
			t.Fatalf("%s expected Kalypt content", path)
		}
	}
}

func TestHomeRendersCareersContent(t *testing.T) {
	app := newApp()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Research Scientist", "Research Infrastructure Programmer", "Research that earns its latency"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected home page to contain %q", want)
		}
	}
}

func newTestApp(t *testing.T, supabaseURL, supabaseSecret string) http.Handler {
	t.Helper()
	t.Setenv("SUPABASE_URL", supabaseURL)
	t.Setenv("SUPABASE_SECRET_KEY", supabaseSecret)
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "")
	t.Setenv("SUPABASE_INQUIRIES_TABLE", "")
	t.Setenv("SMTP_HOST", "")
	t.Setenv("SMTP_PORT", "")
	t.Setenv("SMTP_USERNAME", "")
	t.Setenv("SMTP_PASSWORD", "")
	t.Setenv("SMTP_FROM", "")
	t.Setenv("INQUIRY_EMAIL_TO", "")
	t.Setenv("GOCACHE", os.Getenv("GOCACHE"))
	return newApp()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

type notifierFunc func(*http.Request, inquiry) error

func (fn notifierFunc) sendInquiry(r *http.Request, item inquiry) error {
	return fn(r, item)
}

func contextWithTimeout(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}
