package proxy

import (
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"kiro-go/config"
)

// Admin login throttling: after adminMaxFailures wrong passwords from one
// client IP within adminFailureWindow, further attempts from it are refused
// for adminLockout. The generated default password is strong, but operators
// can set their own (ADMIN_PASSWORD), and the admin API listens on 0.0.0.0.
const (
	adminMaxFailures   = 10
	adminFailureWindow = 15 * time.Minute
	adminLockout       = 15 * time.Minute
)

type adminFailure struct {
	count       int
	first       time.Time
	lockedUntil time.Time
}

type adminLimiter struct {
	mu       sync.Mutex
	failures map[string]*adminFailure
}

var adminAuthLimiter = &adminLimiter{failures: map[string]*adminFailure{}}

// clientIP uses the TCP peer address. X-Forwarded-For is deliberately ignored:
// it is client-controlled and would let an attacker pick a fresh "IP" per try.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (l *adminLimiter) locked(ip string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.failures[ip]
	if f == nil || !now.Before(f.lockedUntil) {
		return false, 0
	}
	return true, f.lockedUntil.Sub(now)
}

func (l *adminLimiter) fail(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Opportunistic cleanup keeps the map bounded.
	if len(l.failures) > 4096 {
		for k, v := range l.failures {
			if now.Sub(v.first) > adminFailureWindow && !now.Before(v.lockedUntil) {
				delete(l.failures, k)
			}
		}
	}
	f := l.failures[ip]
	if f == nil || now.Sub(f.first) > adminFailureWindow {
		f = &adminFailure{first: now}
		l.failures[ip] = f
	}
	f.count++
	if f.count >= adminMaxFailures {
		f.lockedUntil = now.Add(adminLockout)
		f.count = 0
		f.first = now
	}
}

func (l *adminLimiter) succeed(ip string) {
	l.mu.Lock()
	delete(l.failures, ip)
	l.mu.Unlock()
}

// adminPasswordMatches compares in constant time over fixed-length digests,
// so neither content nor length leaks through timing.
func adminPasswordMatches(provided string) bool {
	expected := config.GetPassword()
	if provided == "" || expected == "" {
		return false
	}
	a := sha256.Sum256([]byte(provided))
	b := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// checkAdminAuth validates X-Admin-Password and applies login throttling.
// It writes the error response itself and returns false on failure.
func (h *Handler) checkAdminAuth(w http.ResponseWriter, r *http.Request) bool {
	ip := clientIP(r)
	now := time.Now()
	if locked, wait := adminAuthLimiter.locked(ip, now); locked {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"Too many failed login attempts. Try again later."}`))
		return false
	}
	if !adminPasswordMatches(r.Header.Get("X-Admin-Password")) {
		adminAuthLimiter.fail(ip, now)
		h.audit(r, "login.failed", "", nil)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"Unauthorized"}`))
		return false
	}
	adminAuthLimiter.succeed(ip)
	return true
}

// setAdminSecurityHeaders hardens every admin page, asset and API response.
func setAdminSecurityHeaders(w http.ResponseWriter) {
	hdr := w.Header()
	hdr.Set("X-Frame-Options", "DENY")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Referrer-Policy", "no-referrer")
	hdr.Set("Content-Security-Policy", "frame-ancestors 'none'")
}

// maxRequestBodyBytes bounds client request bodies on the public API so a
// single request cannot exhaust memory. Large enough for multi-image prompts;
// override with KIRO_MAX_BODY_MB.
func maxRequestBodyBytes() int64 {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("KIRO_MAX_BODY_MB"))); err == nil && v > 0 {
		return int64(v) << 20
	}
	return 64 << 20
}

// serveWebFile serves one file from the web directory without directory
// listings. The path is cleaned and must stay inside web/.
func serveWebFile(w http.ResponseWriter, r *http.Request, rel string) {
	clean := filepath.Clean("/" + rel)
	full := filepath.Join("web", clean)
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, full)
}

// webFileExists reports whether rel is a regular file inside web/.
func webFileExists(rel string) bool {
	info, err := os.Stat(filepath.Join("web", filepath.Clean("/"+rel)))
	return err == nil && !info.IsDir()
}

// serveAdminShell serves the console's SPA entry document. It must not be
// cached, so a new build is picked up immediately (assets are hashed).
func serveAdminShell(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	for _, name := range []string{"_shell.html", "index.html"} {
		if webFileExists(name) {
			http.ServeFile(w, r, filepath.Join("web", name))
			return
		}
	}
	http.Error(w, "admin console not built: run `npm ci && npm run build` in frontend/", http.StatusNotFound)
}
