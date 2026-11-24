package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"kiro-go/config"
	"kiro-go/telemetry"
)

// memSessionStore is an in-memory telemetry.AdminSessionStore.
type memSessionStore struct {
	mu   sync.Mutex
	rows map[string]telemetry.AdminSession
}

func (m *memSessionStore) SaveAdminSession(_ context.Context, s *telemetry.AdminSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[s.IDHash] = *s
	return nil
}
func (m *memSessionStore) LoadAdminSession(_ context.Context, id string) (*telemetry.AdminSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.rows[id]; ok {
		return &s, nil
	}
	return nil, nil
}
func (m *memSessionStore) TouchAdminSession(context.Context, string, time.Time) error { return nil }
func (m *memSessionStore) DeleteAdminSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, id)
	return nil
}
func (m *memSessionStore) DeleteAllAdminSessions(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = map[string]telemetry.AdminSession{}
	return nil
}
func (m *memSessionStore) PurgeAdminSessions(context.Context, time.Time) error { return nil }

func resetSessions(t *testing.T) {
	t.Helper()
	mustInitConfig(t)
	config.SetPassword("correct-horse-battery")
	adminAuthLimiter = &adminLimiter{failures: map[string]*adminFailure{}}
	adminSessions = &sessionManager{sessions: map[string]*adminSession{}}
}

func login(t *testing.T, h *Handler, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/admin/api/session", strings.NewReader(body))
	r.RemoteAddr = "192.0.2.10:1000"
	r.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func sessionCookieFrom(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == adminSessionCookie {
			return c
		}
	}
	t.Fatalf("no session cookie in %v", w.Header())
	return nil
}

func adminCall(h *Handler, method, path string, c *http.Cookie, hdr map[string]string, body string) *httptest.ResponseRecorder {
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	r := httptest.NewRequest(method, path, rd)
	r.RemoteAddr = "192.0.2.10:1000"
	if c != nil {
		r.AddCookie(c)
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAdminSessionLoginCookieAndLogout(t *testing.T) {
	resetSessions(t)
	h := &Handler{}

	if w := login(t, h, `{"password":"wrong"}`, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", w.Code)
	}
	w := login(t, h, `{"password":"correct-horse-battery"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	c := sessionCookieFrom(t, w)
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/admin" || c.Secure || c.MaxAge != 0 {
		t.Fatalf("cookie attributes: %+v", c)
	}
	if len(c.Value) < 43 {
		t.Fatalf("session id too short: %q", c.Value)
	}

	// whoami
	w = adminCall(h, "GET", "/admin/api/session", c, nil, "")
	var who map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &who)
	if w.Code != 200 || who["authenticated"] != true || who["remember"] != false {
		t.Fatalf("whoami: %d %s", w.Code, w.Body.String())
	}
	exp := int64(who["expiresAt"].(float64))
	if d := time.Until(time.Unix(exp, 0)); d < 6*24*time.Hour || d > 7*24*time.Hour+time.Minute {
		t.Fatalf("absolute expiry %v", d)
	}

	// Authenticated GET via cookie.
	if w := adminCall(h, "GET", "/admin/api/version", c, nil, ""); w.Code != 200 {
		t.Fatalf("GET with cookie: %d", w.Code)
	}
	// State-changing call without JSON or X-Requested-With: CSRF refusal.
	if w := adminCall(h, "DELETE", "/admin/api/logs", c, nil, ""); w.Code != http.StatusForbidden {
		t.Fatalf("CSRF check: %d", w.Code)
	}
	if w := adminCall(h, "DELETE", "/admin/api/logs", c, map[string]string{"X-Requested-With": "kiro-console"}, ""); w.Code != 200 {
		t.Fatalf("DELETE with header: %d %s", w.Code, w.Body.String())
	}

	// Logout ends the session and clears the cookie.
	w = adminCall(h, "DELETE", "/admin/api/session", c, map[string]string{"X-Requested-With": "kiro-console"}, "")
	if w.Code != 200 || sessionCookieFrom(t, w).MaxAge >= 0 {
		t.Fatalf("logout: %d %v", w.Code, w.Header())
	}
	if w := adminCall(h, "GET", "/admin/api/version", c, nil, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", w.Code)
	}
}

func TestAdminSessionHeaderAuthStillWorks(t *testing.T) {
	resetSessions(t)
	h := &Handler{}
	if w := adminCall(h, "GET", "/admin/api/version", nil, map[string]string{"X-Admin-Password": "correct-horse-battery"}, ""); w.Code != 200 {
		t.Fatalf("header auth: %d", w.Code)
	}
	w := adminCall(h, "GET", "/admin/api/session", nil, map[string]string{"X-Admin-Password": "correct-horse-battery"}, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"via":"header"`) {
		t.Fatalf("whoami via header: %d %s", w.Code, w.Body.String())
	}
	// Requests without any credentials don't count as failed logins.
	for i := 0; i < adminMaxFailures+2; i++ {
		adminCall(h, "GET", "/admin/api/version", nil, nil, "")
	}
	if w := login(t, h, `{"password":"correct-horse-battery"}`, nil); w.Code != 200 {
		t.Fatalf("anonymous requests locked the client out: %d", w.Code)
	}
}

func TestAdminSessionRememberAndSecure(t *testing.T) {
	resetSessions(t)
	h := &Handler{}
	w := login(t, h, `{"password":"correct-horse-battery","remember":true}`, map[string]string{"X-Forwarded-Proto": "https"})
	c := sessionCookieFrom(t, w)
	if !c.Secure {
		t.Fatal("Secure not set behind https proxy")
	}
	if d := time.Duration(c.MaxAge) * time.Second; d < 29*24*time.Hour || d > 30*24*time.Hour {
		t.Fatalf("remember cookie max-age %v", d)
	}
}

func TestAdminSessionLoginNeedsJSON(t *testing.T) {
	resetSessions(t)
	h := &Handler{}
	r := httptest.NewRequest(http.MethodPost, "/admin/api/session", strings.NewReader(`password=correct-horse-battery`))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("form login: %d", w.Code)
	}
}

func TestAdminSessionRateLimited(t *testing.T) {
	resetSessions(t)
	h := &Handler{}
	for i := 0; i < adminMaxFailures; i++ {
		login(t, h, `{"password":"nope"}`, nil)
	}
	if w := login(t, h, `{"password":"correct-horse-battery"}`, nil); w.Code != http.StatusTooManyRequests {
		t.Fatalf("lockout: %d", w.Code)
	}
}

func TestAdminSessionTimeouts(t *testing.T) {
	resetSessions(t)
	now := time.Now()
	id, _, _ := adminSessions.create(false, "", "", now)
	if adminSessions.validate(id, now.Add(11*time.Hour)) == nil {
		t.Fatal("session expired before idle timeout")
	}
	// Idle timeout counts from the last use.
	if adminSessions.validate(id, now.Add(22*time.Hour)) == nil {
		t.Fatal("use did not extend the idle timeout")
	}
	if adminSessions.validate(id, now.Add(22*time.Hour+sessionIdleTimeout+time.Second)) != nil {
		t.Fatal("idle session still valid")
	}
	// Absolute timeout even with constant use.
	id, _, _ = adminSessions.create(false, "", "", now)
	for d := time.Hour; d < sessionAbsoluteTimeout; d += 6 * time.Hour {
		adminSessions.validate(id, now.Add(d))
	}
	if adminSessions.validate(id, now.Add(sessionAbsoluteTimeout+time.Second)) != nil {
		t.Fatal("session outlived its absolute timeout")
	}
}

func TestAdminPasswordChangeEndsSessions(t *testing.T) {
	resetSessions(t)
	store := &memSessionStore{rows: map[string]telemetry.AdminSession{}}
	adminSessions.store = store
	h := &Handler{}
	c := sessionCookieFrom(t, login(t, h, `{"password":"correct-horse-battery"}`, nil))
	if len(store.rows) != 1 {
		t.Fatalf("session not persisted: %d", len(store.rows))
	}
	for _, v := range store.rows {
		if strings.Contains(v.IDHash, c.Value) || v.IDHash == c.Value {
			t.Fatal("raw session id stored")
		}
	}
	w := adminCall(h, "POST", "/admin/api/settings", c, map[string]string{"Content-Type": "application/json"}, `{"password":"another-good-password"}`)
	if w.Code != 200 {
		t.Fatalf("change password: %d %s", w.Code, w.Body.String())
	}
	if w := adminCall(h, "GET", "/admin/api/version", c, nil, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("session survived password change: %d", w.Code)
	}
	if len(store.rows) != 0 {
		t.Fatal("persisted sessions not revoked")
	}

	// A password changed behind our back (e.g. ADMIN_PASSWORD on restart)
	// also invalidates sessions.
	c = sessionCookieFrom(t, login(t, h, `{"password":"another-good-password"}`, nil))
	config.SetPassword("third-password-123")
	if w := adminCall(h, "GET", "/admin/api/version", c, nil, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("session survived out-of-band password change: %d", w.Code)
	}
}

func TestAdminSessionSurvivesRestartViaStore(t *testing.T) {
	resetSessions(t)
	store := &memSessionStore{rows: map[string]telemetry.AdminSession{}}
	adminSessions.store = store
	h := &Handler{}
	c := sessionCookieFrom(t, login(t, h, `{"password":"correct-horse-battery"}`, nil))
	// "Restart": memory is empty, the store still has the hashed session.
	adminSessions = &sessionManager{sessions: map[string]*adminSession{}, store: store}
	if w := adminCall(h, "GET", "/admin/api/version", c, nil, ""); w.Code != 200 {
		t.Fatalf("session lost across restart: %d", w.Code)
	}
}
