package proxy

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
)

func TestAdminAuthLocksOutAfterRepeatedFailures(t *testing.T) {
	mustInitConfig(t)
	config.SetPassword("correct-horse-battery")
	adminAuthLimiter = &adminLimiter{failures: map[string]*adminFailure{}}
	h := &Handler{}

	try := func(pw string) int {
		r := httptest.NewRequest("GET", "/admin/api/status", nil)
		r.RemoteAddr = "203.0.113.7:5555"
		r.Header.Set("X-Admin-Password", pw)
		w := httptest.NewRecorder()
		h.checkAdminAuth(w, r)
		return w.Code
	}
	for i := 0; i < adminMaxFailures; i++ {
		if code := try("wrong"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: got %d want 401", i, code)
		}
	}
	if code := try("correct-horse-battery"); code != http.StatusTooManyRequests {
		t.Fatalf("locked client must be refused even with the right password, got %d", code)
	}
	// Another client is unaffected.
	r := httptest.NewRequest("GET", "/admin/api/status", nil)
	r.RemoteAddr = "198.51.100.1:1"
	r.Header.Set("X-Admin-Password", "correct-horse-battery")
	if !h.checkAdminAuth(httptest.NewRecorder(), r) {
		t.Fatal("other clients must still be able to log in")
	}
}

func TestAdminAuthIgnoresCookie(t *testing.T) {
	mustInitConfig(t)
	config.SetPassword("correct-horse-battery")
	adminAuthLimiter = &adminLimiter{failures: map[string]*adminFailure{}}
	r := httptest.NewRequest("GET", "/admin/api/status", nil)
	r.AddCookie(&http.Cookie{Name: "admin_password", Value: "correct-horse-battery"})
	if (&Handler{}).checkAdminAuth(httptest.NewRecorder(), r) {
		t.Fatal("cookie auth must not be accepted (CSRF)")
	}
}

func TestAdminRoutesGetSecurityHeadersAndNoCORS(t *testing.T) {
	mustInitConfig(t)
	h := &Handler{}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/admin/api/status", nil))
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("admin routes must not send CORS headers")
	}
	if w.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("admin routes must forbid framing")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("OPTIONS", "/v1/chat/completions", nil))
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("public API keeps CORS for browser clients")
	}
}

func TestServeWebFileRefusesDirectoriesAndTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "web", "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "web", "app.js"), []byte("ok"), 0o644)
	os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("secret"), 0o644)
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(dir)

	get := func(p string) (int, string) {
		w := httptest.NewRecorder()
		serveWebFile(w, httptest.NewRequest("GET", "/admin/x", nil), p)
		return w.Code, w.Body.String()
	}
	if code, body := get("app.js"); code != 200 || body != "ok" {
		t.Fatalf("file: %d %q", code, body)
	}
	if code, _ := get("vendor/"); code != 404 {
		t.Fatalf("directory listing must be refused, got %d", code)
	}
	if code, body := get("../secret.txt"); code != 404 || strings.Contains(body, "secret") {
		t.Fatalf("traversal must be refused, got %d %q", code, body)
	}
}

func TestSafeRegionRejectsHostInjection(t *testing.T) {
	for in, want := range map[string]string{
		"eu-central-1":      "eu-central-1",
		"us-gov-west-1":     "us-gov-west-1",
		"evil.example/x#":   "us-east-1",
		"us-east-1.evil.io": "us-east-1",
		"":                  "us-east-1",
	} {
		if got := config.SafeRegion(in); got != want {
			t.Fatalf("SafeRegion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStoredResponsesAreEvictedOverLimit(t *testing.T) {
	mustInitConfig(t)
	t.Setenv("KIRO_MAX_STORED_RESPONSES", "3")
	dir := responsesDir()
	os.MkdirAll(dir, 0o755)
	now := time.Now()
	for i := 0; i < 5; i++ {
		p := filepath.Join(dir, "resp_"+string(rune('a'+i))+".json")
		os.WriteFile(p, []byte("{}"), 0o600)
		os.Chtimes(p, now.Add(time.Duration(i)*time.Minute), now.Add(time.Duration(i)*time.Minute))
	}
	purgeExpiredResponses(responsesDefaultTTL)
	left, _ := os.ReadDir(dir)
	if len(left) != 3 {
		t.Fatalf("want 3 files left, got %d", len(left))
	}
	for _, e := range left {
		if e.Name() == "resp_a.json" || e.Name() == "resp_b.json" {
			t.Fatalf("oldest files should be evicted first, still have %s", e.Name())
		}
	}
}
