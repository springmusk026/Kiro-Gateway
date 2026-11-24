package proxy

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConsoleSPAFallbackAndAssets(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "web", "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "web", "_shell.html"), []byte("<html>shell</html>"), 0o644)
	os.WriteFile(filepath.Join(dir, "web", "assets", "app-abc123.js"), []byte("console.log(1)"), 0o644)
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(dir)

	h := &Handler{}
	get := func(p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		return w
	}
	for _, p := range []string{"/admin", "/admin/", "/admin/accounts", "/admin/settings"} {
		if w := get(p); w.Code != 200 || !strings.Contains(w.Body.String(), "shell") {
			t.Fatalf("%s: want shell, got %d %q", p, w.Code, w.Body.String())
		}
	}
	w := get("/admin/assets/app-abc123.js")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d cache=%q", w.Code, w.Header().Get("Cache-Control"))
	}
	if w := get("/admin/assets/missing.js"); w.Code != 404 {
		t.Fatalf("missing asset must 404, got %d", w.Code)
	}
	if w := get("/admin/accounts"); w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal("shell must not be cached")
	}
}
