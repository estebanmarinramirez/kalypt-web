package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
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

func TestContactAcceptsValidInquiry(t *testing.T) {
	var captured map[string]string
	var emailed inquiry
	handler := newTestApp(t, "https://project.supabase.co", "test-secret")
	site := handler.(*app)
	site.notifier = notifierFunc(func(_ *http.Request, item inquiry) error {
		emailed = item
		return nil
	})
	site.supabase.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/rest/v1/inquiries" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("apikey") != "test-secret" {
			t.Fatalf("missing Supabase apikey header")
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" {
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
