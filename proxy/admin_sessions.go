package proxy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/logger"
	"github.com/springmusk026/Kiro-Gateway/telemetry"
)

// Console sessions. POST /admin/api/session exchanges the admin password for
// a random 256-bit session id in an HttpOnly, SameSite=Strict cookie scoped
// to /admin; DELETE signs out and GET reports the session. The
// X-Admin-Password header keeps working for scripts.
//
// Sessions live in memory and, with the database backend, in
// kiro_admin_sessions (hashed ids only) so restarts don't sign everyone out.
// A session is bound to the admin password it was created with: changing the
// password ends every session.

const (
	adminSessionCookie = "kiro_admin_session"

	sessionIdleTimeout     = 12 * time.Hour
	sessionAbsoluteTimeout = 7 * 24 * time.Hour
	// "Remember me" sessions survive a week without use, 30 days at most.
	rememberIdleTimeout     = 7 * 24 * time.Hour
	rememberAbsoluteTimeout = 30 * 24 * time.Hour

	// touchPersistEvery limits last-seen writes to the database.
	touchPersistEvery = 5 * time.Minute
	maxMemorySessions = 10000
)

type adminSession struct {
	telemetry.AdminSession
	persistedSeen time.Time
}

func (s *adminSession) idleTimeout() time.Duration {
	if s.Remember {
		return rememberIdleTimeout
	}
	return sessionIdleTimeout
}

// idleExpiry is when the session ends if unused.
func (s *adminSession) idleExpiry() time.Time {
	t := s.LastSeen.Add(s.idleTimeout())
	if t.After(s.ExpiresAt) {
		return s.ExpiresAt
	}
	return t
}

type sessionManager struct {
	mu       sync.Mutex
	sessions map[string]*adminSession // by id hash
	store    telemetry.AdminSessionStore
}

var adminSessions = &sessionManager{sessions: map[string]*adminSession{}}

// UseAdminSessionStore persists console sessions (database backend).
func UseAdminSessionStore(store telemetry.AdminSessionStore) {
	adminSessions.mu.Lock()
	adminSessions.store = store
	adminSessions.mu.Unlock()
	if store != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = store.PurgeAdminSessions(ctx, time.Now())
		}()
	}
}

func hashSessionID(id string) string {
	sum := sha256.Sum256([]byte("kiro-admin-session:" + id))
	return hex.EncodeToString(sum[:])
}

// passwordCheck binds a session to the admin password without storing
// anything that reveals it.
func passwordCheck(id, password string) string {
	sum := sha256.Sum256([]byte("kiro-admin-session-password:" + id + "\x00" + password))
	return hex.EncodeToString(sum[:])
}

func (m *sessionManager) storeCtx() (telemetry.AdminSessionStore, context.Context, context.CancelFunc) {
	m.mu.Lock()
	st := m.store
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	return st, ctx, cancel
}

// create starts a session and returns its raw id.
func (m *sessionManager) create(remember bool, ip, ua string, now time.Time) (string, *adminSession, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	id := base64.RawURLEncoding.EncodeToString(buf)
	abs := sessionAbsoluteTimeout
	if remember {
		abs = rememberAbsoluteTimeout
	}
	if len(ua) > 256 {
		ua = ua[:256]
	}
	s := &adminSession{AdminSession: telemetry.AdminSession{
		IDHash:        hashSessionID(id),
		PasswordCheck: passwordCheck(id, config.GetPassword()),
		Remember:      remember,
		CreatedAt:     now,
		LastSeen:      now,
		ExpiresAt:     now.Add(abs),
		IP:            ip,
		UserAgent:     ua,
	}, persistedSeen: now}
	m.mu.Lock()
	if len(m.sessions) >= maxMemorySessions {
		m.pruneLocked(now)
	}
	m.sessions[s.IDHash] = s
	m.mu.Unlock()
	if st, ctx, cancel := m.storeCtx(); st != nil {
		defer cancel()
		if err := st.SaveAdminSession(ctx, &s.AdminSession); err != nil {
			logger.Warnf("Admin session: could not persist session: %v", err)
		}
	} else {
		cancel()
	}
	return id, s, nil
}

func (m *sessionManager) pruneLocked(now time.Time) {
	for k, s := range m.sessions {
		if !now.Before(s.idleExpiry()) {
			delete(m.sessions, k)
		}
	}
}

// validate returns the live session for a raw id, refreshing its last-seen
// time, or nil.
func (m *sessionManager) validate(id string, now time.Time) *adminSession {
	if id == "" || len(id) > 128 {
		return nil
	}
	key := hashSessionID(id)
	m.mu.Lock()
	s := m.sessions[key]
	m.mu.Unlock()
	if s == nil {
		st, ctx, cancel := m.storeCtx()
		if st != nil {
			stored, err := st.LoadAdminSession(ctx, key)
			if err != nil {
				logger.Warnf("Admin session: lookup failed: %v", err)
			}
			if stored != nil {
				s = &adminSession{AdminSession: *stored, persistedSeen: stored.LastSeen}
				m.mu.Lock()
				m.sessions[key] = s
				m.mu.Unlock()
			}
		}
		cancel()
		if s == nil {
			return nil
		}
	}
	m.mu.Lock()
	expired := !now.Before(s.idleExpiry())
	pwOK := subtle.ConstantTimeCompare([]byte(s.PasswordCheck), []byte(passwordCheck(id, config.GetPassword()))) == 1
	if expired || !pwOK {
		delete(m.sessions, key)
		m.mu.Unlock()
		m.forget(key)
		return nil
	}
	s.LastSeen = now
	persist := now.Sub(s.persistedSeen) >= touchPersistEvery
	if persist {
		s.persistedSeen = now
	}
	cp := *s
	m.mu.Unlock()
	if persist {
		go func() {
			if st, ctx, cancel := m.storeCtx(); st != nil {
				defer cancel()
				_ = st.TouchAdminSession(ctx, key, now)
			} else {
				cancel()
			}
		}()
	}
	return &cp
}

// forget deletes a session from the store (memory already handled).
func (m *sessionManager) forget(key string) {
	st, ctx, cancel := m.storeCtx()
	defer cancel()
	if st != nil {
		if err := st.DeleteAdminSession(ctx, key); err != nil {
			logger.Warnf("Admin session: delete failed: %v", err)
		}
	}
}

func (m *sessionManager) revoke(id string) {
	if id == "" {
		return
	}
	key := hashSessionID(id)
	m.mu.Lock()
	delete(m.sessions, key)
	m.mu.Unlock()
	m.forget(key)
}

// revokeAll ends every session (admin password changed).
func (m *sessionManager) revokeAll() {
	m.mu.Lock()
	m.sessions = map[string]*adminSession{}
	m.mu.Unlock()
	st, ctx, cancel := m.storeCtx()
	defer cancel()
	if st != nil {
		if err := st.DeleteAllAdminSessions(ctx); err != nil {
			logger.Warnf("Admin session: revoking sessions failed: %v", err)
		}
	}
}

// RevokeAdminSessions ends every console session.
func RevokeAdminSessions() { adminSessions.revokeAll() }

func sessionCookieValue(r *http.Request) string {
	c, err := r.Cookie(adminSessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// requestIsHTTPS reports TLS directly or through a proxy that says so.
func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, id string, s *adminSession) {
	c := &http.Cookie{
		Name:     adminSessionCookie,
		Value:    id,
		Path:     "/admin",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	}
	if s.Remember {
		// Persistent cookie; plain sessions end when the browser closes.
		c.MaxAge = int(time.Until(s.ExpiresAt).Seconds())
	}
	http.SetCookie(w, c)
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    "",
		Path:     "/admin",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func isJSONRequest(r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mt == "application/json"
}

// csrfSafe: state-changing calls authenticated by cookie must carry a JSON
// body or the X-Requested-With header, neither of which a cross-site form can
// send without a CORS preflight (which the admin API never grants). This is
// on top of SameSite=Strict.
func csrfSafe(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return isJSONRequest(r) || r.Header.Get("X-Requested-With") != ""
}

func writeAdminJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// authenticateAdminSession accepts a valid session cookie (with the CSRF
// rule). It writes a 403 itself when the cookie is valid but the request
// fails the CSRF check.
func authenticateAdminSession(w http.ResponseWriter, r *http.Request) (ok, handled bool) {
	id := sessionCookieValue(r)
	if id == "" {
		return false, false
	}
	if adminSessions.validate(id, time.Now()) == nil {
		return false, false
	}
	if !csrfSafe(r) {
		writeAdminJSON(w, http.StatusForbidden, map[string]string{"error": "Missing X-Requested-With header or JSON content type"})
		return false, true
	}
	return true, false
}

func sessionView(s *adminSession, via string) map[string]interface{} {
	return map[string]interface{}{
		"authenticated":   true,
		"via":             via,
		"remember":        s.Remember,
		"createdAt":       s.CreatedAt.Unix(),
		"expiresAt":       s.ExpiresAt.Unix(),
		"idleExpiresAt":   s.idleExpiry().Unix(),
		"idleTimeoutSecs": int(s.idleTimeout().Seconds()),
	}
}

// handleAdminSession serves /admin/api/session (before the normal admin auth).
func (h *Handler) handleAdminSession(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createAdminSession(w, r)
	case http.MethodGet:
		if pw := r.Header.Get("X-Admin-Password"); pw != "" {
			if !h.checkAdminAuth(w, r) {
				return
			}
			writeAdminJSON(w, http.StatusOK, map[string]interface{}{"authenticated": true, "via": "header"})
			return
		}
		s := adminSessions.validate(sessionCookieValue(r), time.Now())
		if s == nil {
			writeAdminJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Unauthorized", "authenticated": false})
			return
		}
		writeAdminJSON(w, http.StatusOK, sessionView(s, "cookie"))
	case http.MethodDelete:
		if !csrfSafe(r) {
			writeAdminJSON(w, http.StatusForbidden, map[string]string{"error": "Missing X-Requested-With header or JSON content type"})
			return
		}
		if id := sessionCookieValue(r); id != "" {
			if adminSessions.validate(id, time.Now()) != nil {
				h.audit(r, "session.delete", "", nil)
			}
			adminSessions.revoke(id)
		}
		clearSessionCookie(w, r)
		writeAdminJSON(w, http.StatusOK, map[string]bool{"success": true})
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		writeAdminJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
	}
}

func (h *Handler) createAdminSession(w http.ResponseWriter, r *http.Request) {
	if !isJSONRequest(r) {
		writeAdminJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
		return
	}
	ip := clientIP(r)
	now := time.Now()
	if locked, wait := adminAuthLimiter.locked(ip, now); locked {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeAdminJSON(w, http.StatusTooManyRequests, map[string]string{"error": "Too many failed login attempts. Try again later."})
		return
	}
	var req struct {
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeAdminJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}
	if !adminPasswordMatches(req.Password) {
		adminAuthLimiter.fail(ip, now)
		h.audit(r, "login.failed", "", nil)
		writeAdminJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}
	adminAuthLimiter.succeed(ip)
	// Replace any session this browser already had.
	if old := sessionCookieValue(r); old != "" {
		adminSessions.revoke(old)
	}
	id, s, err := adminSessions.create(req.Remember, ip, r.UserAgent(), now)
	if err != nil {
		writeAdminJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create session"})
		return
	}
	h.audit(r, "session.create", "", map[string]string{"remember": strconv.FormatBool(req.Remember)})
	setSessionCookie(w, r, id, s)
	writeAdminJSON(w, http.StatusOK, sessionView(s, "cookie"))
}
