package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/springmusk026/Kiro-Gateway/config"
)

// Keys are shown once: the create and regenerate responses carry the key,
// list and get never do, and the key authenticates by hash.
func TestAdminApiKeyShownOnce(t *testing.T) {
	mustInitConfig(t)
	requireAuth(t)
	h := &Handler{}

	w := httptest.NewRecorder()
	h.apiCreateApiKey(w, httptest.NewRequest(http.MethodPost, "/admin/api/api-keys", strings.NewReader(`{"name":"ci"}`)))
	var created struct {
		ID     string
		Key    string
		ApiKey map[string]interface{}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || !strings.HasPrefix(created.Key, "sk-") {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	if _, has := created.ApiKey["key"]; has || created.ApiKey["keyHint"] != created.Key[:7]+"…"+created.Key[len(created.Key)-4:] {
		t.Fatalf("view: %v", created.ApiKey)
	}

	w = httptest.NewRecorder()
	h.apiListApiKeys(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(w.Body.String(), created.Key) || strings.Contains(w.Body.String(), `"key"`) {
		t.Fatalf("list exposes the key: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	h.apiGetApiKey(w, httptest.NewRequest(http.MethodGet, "/", nil), created.ID)
	if strings.Contains(w.Body.String(), created.Key) {
		t.Fatalf("get exposes the key: %s", w.Body.String())
	}
	if e, err := h.authenticate(newAuthTestRequest(t, "Authorization", "Bearer "+created.Key)); err != nil || e.ID != created.ID {
		t.Fatalf("created key does not authenticate: %v", err)
	}

	// Regenerate: a new key once, the old one stops working, limits stay.
	w = httptest.NewRecorder()
	h.apiUpdateApiKey(w, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"regenerate":true}`)), created.ID)
	var regen struct {
		Key string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &regen); err != nil || regen.Key == "" || regen.Key == created.Key {
		t.Fatalf("regenerate: %d %s", w.Code, w.Body.String())
	}
	if _, err := h.authenticate(newAuthTestRequest(t, "Authorization", "Bearer "+created.Key)); err == nil {
		t.Fatal("old key still authenticates")
	}
	if e, err := h.authenticate(newAuthTestRequest(t, "X-Api-Key", regen.Key)); err != nil || e.ID != created.ID {
		t.Fatalf("regenerated key: %v", err)
	}
	// A plain update returns no key.
	w = httptest.NewRecorder()
	h.apiUpdateApiKey(w, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"name":"ci2"}`)), created.ID)
	if strings.Contains(w.Body.String(), `"key"`) {
		t.Fatalf("update returned a key: %s", w.Body.String())
	}

	// A custom key is hashed too.
	w = httptest.NewRecorder()
	h.apiCreateApiKey(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"key":"my-custom-key-42"}`)))
	if e := config.FindApiKeyByValue("my-custom-key-42"); e == nil || e.Key != "" || e.KeyHint != "…y-42" {
		t.Fatalf("custom key: %+v (%s)", e, w.Body.String())
	}
}
