package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
)

// Per-API-key restrictions enforced before any upstream call: expiry (in
// authenticate), allowed models and a requests-per-minute limit.

// keyRateLimiter is a token bucket per API key: capacity = RPM, refilled at
// RPM/60 tokens per second. In memory only; a restart refills every bucket.
type keyRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*keyBucket
}

type keyBucket struct {
	limit  int
	tokens float64
	last   time.Time
}

var apiKeyRPM = &keyRateLimiter{buckets: map[string]*keyBucket{}}

// allow consumes one request for key id. When the bucket is empty it returns
// false and how long until one request is available again.
func (l *keyRateLimiter) allow(id string, rpm int, now time.Time) (bool, time.Duration) {
	if rpm <= 0 || id == "" {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[id]
	if b == nil || b.limit != rpm {
		// New key or changed limit: start full.
		b = &keyBucket{limit: rpm, tokens: float64(rpm), last: now}
		l.buckets[id] = b
		if len(l.buckets) > 10000 {
			l.pruneLocked(now)
		}
	}
	rate := float64(rpm) / 60.0 // tokens per second
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = math.Min(float64(rpm), b.tokens+elapsed*rate)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / rate * float64(time.Second))
	if wait < time.Second {
		wait = time.Second
	}
	return false, wait
}

// pruneLocked drops buckets idle long enough to be full again.
func (l *keyRateLimiter) pruneLocked(now time.Time) {
	for id, b := range l.buckets {
		if now.Sub(b.last) > time.Minute {
			delete(l.buckets, id)
		}
	}
}

// rpmCounted reports whether a request counts against a key's RPM limit:
// inference calls only (not count_tokens, model lists or response lookups).
func rpmCounted(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch endpointForPath(r.URL.Path) {
	case "claude", "openai", "responses":
		return true
	}
	return false
}

// requestedModel reads the "model" field of a JSON request body and puts the
// body back for the handler. A read error is replayed to the handler, which
// reports it in its usual shape.
func requestedModel(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	data, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(data), errReader{err}))
		return ""
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	var probe struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(data, &probe)
	model := strings.TrimSpace(probe.Model)
	if model == "" && endpointForPath(r.URL.Path) == "responses" {
		model = defaultResponsesModel
	}
	return model
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// modelCandidates are the names a request's model is matched under: the name
// as sent, without the thinking suffix, and the resolved upstream ID.
func modelCandidates(model string) []string {
	if model == "" {
		return nil
	}
	suffix := config.GetThinkingConfig().Suffix
	out := []string{model}
	if suffix != "" && strings.HasSuffix(strings.ToLower(model), strings.ToLower(suffix)) {
		out = append(out, model[:len(model)-len(suffix)])
	}
	if resolved, _ := ParseModelAndThinking(model, suffix); resolved != "" {
		out = append(out, resolved)
	}
	return out
}

// enforceKeyRestrictions applies the allowed-model list and the RPM limit of
// an authenticated key. The model check runs first so rejected requests do
// not consume the key's rate budget.
func enforceKeyRestrictions(r *http.Request, entry *config.ApiKeyEntry) *authError {
	if entry == nil {
		return nil
	}
	if len(entry.AllowedModels) > 0 && r.Method == http.MethodPost {
		model := requestedModel(r)
		if !config.ApiKeyAllowsModel(*entry, modelCandidates(model)...) {
			name := model
			if name == "" {
				name = "(none)"
			}
			ae := newAuthError(http.StatusForbidden, "permission_error",
				fmt.Sprintf("This API key is not allowed to use model %q", name))
			ae.openAIType = "invalid_request_error"
			ae.openAICode = "model_not_allowed"
			return ae
		}
	}
	if entry.RPMLimit > 0 && rpmCounted(r) {
		if ok, wait := apiKeyRPM.allow(entry.ID, entry.RPMLimit, time.Now()); !ok {
			ae := newAuthError(http.StatusTooManyRequests, "rate_limit_error",
				fmt.Sprintf("Rate limit of %d requests per minute exceeded for this API key", entry.RPMLimit))
			ae.openAICode = "rate_limit_exceeded"
			ae.retryAfter = wait
			return ae
		}
	}
	return nil
}

// authenticateAndRestrict runs authenticate and the key's restrictions. On
// success it returns the request carrying the matched key in its context.
func (h *Handler) authenticateAndRestrict(r *http.Request) (*http.Request, *authError) {
	entry, err := h.authenticate(r)
	if err != nil {
		ae, _ := err.(*authError)
		if ae == nil {
			ae = newAuthError(http.StatusUnauthorized, "authentication_error", err.Error())
		}
		return nil, ae
	}
	if ae := enforceKeyRestrictions(r, entry); ae != nil {
		return nil, ae
	}
	return withApiKeyContext(r, entry), nil
}

// keyForModelList returns the presented API key's entry when it restricts
// models (so /v1/models lists only those), or nil.
func keyForModelList(r *http.Request) *config.ApiKeyEntry {
	provided := extractProvidedKey(r)
	if provided == "" {
		return nil
	}
	entry := config.FindApiKeyByValue(provided)
	if entry == nil || len(entry.AllowedModels) == 0 {
		return nil
	}
	return entry
}

// filterModelsForKey drops /v1/models entries the presented key may not use.
func filterModelsForKey(r *http.Request, models []map[string]interface{}) []map[string]interface{} {
	entry := keyForModelList(r)
	if entry == nil {
		return models
	}
	out := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		id, _ := m["id"].(string)
		if config.ApiKeyAllowsModel(*entry, modelCandidates(id)...) {
			out = append(out, m)
		}
	}
	return out
}
