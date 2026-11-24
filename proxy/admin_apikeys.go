package proxy

import (
	"encoding/json"
	"github.com/springmusk026/Kiro-Gateway/config"
	"net/http"
	"time"
)

// apiKeyView is the response payload for listing/inspecting API keys. The Key field
// is masked so admins can identify entries without exposing the secret.
type apiKeyView struct {
	ID          string  `json:"id"`
	Name        string  `json:"name,omitempty"`
	KeyMasked   string  `json:"keyMasked"`
	Enabled     bool    `json:"enabled"`
	Migrated    bool    `json:"migrated,omitempty"`
	CreatedAt   int64   `json:"createdAt"`
	LastUsedAt  int64   `json:"lastUsedAt,omitempty"`
	TokenLimit  int64   `json:"tokenLimit,omitempty"`
	CreditLimit float64 `json:"creditLimit,omitempty"`
	// DailyCreditLimit and DailyCreditsUsed: today's budget (cost_controls.go).
	DailyCreditLimit float64 `json:"dailyCreditLimit,omitempty"`
	DailyCreditsUsed float64 `json:"dailyCreditsUsed"`
	TokensUsed       int64   `json:"tokensUsed"`
	CreditsUsed      float64 `json:"creditsUsed"`
	RequestsCount    int64   `json:"requestsCount"`

	// Restrictions.
	AllowedModels []string `json:"allowedModels,omitempty"`
	RPMLimit      int      `json:"rpmLimit,omitempty"`
	ExpiresAt     int64    `json:"expiresAt,omitempty"`
	Expired       bool     `json:"expired,omitempty"`
}

func toApiKeyView(e config.ApiKeyEntry) apiKeyView {
	return apiKeyView{
		ID:               e.ID,
		Name:             e.Name,
		KeyMasked:        config.MaskApiKey(e.Key),
		Enabled:          e.Enabled,
		Migrated:         e.Migrated,
		CreatedAt:        e.CreatedAt,
		LastUsedAt:       e.LastUsedAt,
		TokenLimit:       e.TokenLimit,
		CreditLimit:      e.CreditLimit,
		DailyCreditLimit: e.DailyCreditLimit,
		DailyCreditsUsed: config.GetDailyUsage(time.Now()).KeyCredits[e.ID],
		TokensUsed:       e.TokensUsed,
		CreditsUsed:      e.CreditsUsed,
		RequestsCount:    e.RequestsCount,
		AllowedModels:    e.AllowedModels,
		RPMLimit:         e.RPMLimit,
		ExpiresAt:        e.ExpiresAt,
		Expired:          config.ApiKeyExpired(e, time.Now()),
	}
}

func (h *Handler) apiListApiKeys(w http.ResponseWriter, r *http.Request) {
	entries := config.ListApiKeys()
	out := make([]apiKeyView, len(entries))
	for i, e := range entries {
		out[i] = toApiKeyView(e)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"apiKeys": out})
}

func (h *Handler) apiGetApiKey(w http.ResponseWriter, r *http.Request, id string) {
	entry := config.GetApiKeyEntry(id)
	if entry == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "API key not found"})
		return
	}
	json.NewEncoder(w).Encode(toApiKeyView(*entry))
}

type apiKeyCreateRequest struct {
	Name             string  `json:"name,omitempty"`
	Key              string  `json:"key,omitempty"`
	Enabled          *bool   `json:"enabled,omitempty"`
	TokenLimit       int64   `json:"tokenLimit,omitempty"`
	CreditLimit      float64 `json:"creditLimit,omitempty"`
	DailyCreditLimit float64 `json:"dailyCreditLimit,omitempty"`

	AllowedModels []string `json:"allowedModels,omitempty"`
	RPMLimit      int      `json:"rpmLimit,omitempty"`
	ExpiresAt     int64    `json:"expiresAt,omitempty"`
}

func (h *Handler) apiCreateApiKey(w http.ResponseWriter, r *http.Request) {
	var req apiKeyCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	keyValue := req.Key
	if keyValue == "" {
		keyValue = config.GenerateApiKeyValue()
	}

	newEntry := config.ApiKeyEntry{
		Name:             req.Name,
		Key:              keyValue,
		Enabled:          enabled,
		TokenLimit:       req.TokenLimit,
		CreditLimit:      req.CreditLimit,
		AllowedModels:    req.AllowedModels,
		RPMLimit:         req.RPMLimit,
		ExpiresAt:        req.ExpiresAt,
		DailyCreditLimit: req.DailyCreditLimit,
	}
	if err := config.ValidateApiKeyRestrictions(newEntry); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	entry, err := config.AddApiKey(newEntry)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Return the cleartext key exactly once on creation so the operator can copy it.
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"id":      entry.ID,
		"key":     entry.Key,
		"apiKey":  toApiKeyView(entry),
	})
}

type apiKeyUpdateRequest struct {
	Name        *string  `json:"name,omitempty"`
	Key         *string  `json:"key,omitempty"`
	Enabled     *bool    `json:"enabled,omitempty"`
	TokenLimit  *int64   `json:"tokenLimit,omitempty"`
	CreditLimit *float64 `json:"creditLimit,omitempty"`

	// Restrictions: omitted = unchanged; [] / 0 clear them.
	AllowedModels *[]string `json:"allowedModels,omitempty"`
	RPMLimit      *int      `json:"rpmLimit,omitempty"`
	ExpiresAt     *int64    `json:"expiresAt,omitempty"`

	DailyCreditLimit *float64 `json:"dailyCreditLimit,omitempty"`
}

func (h *Handler) apiUpdateApiKey(w http.ResponseWriter, r *http.Request, id string) {
	existing := config.GetApiKeyEntry(id)
	if existing == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "API key not found"})
		return
	}

	var req apiKeyUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	patch := *existing
	if req.Name != nil {
		patch.Name = *req.Name
	}
	if req.Key != nil {
		patch.Key = *req.Key
	}
	if req.Enabled != nil {
		patch.Enabled = *req.Enabled
	}
	if req.TokenLimit != nil {
		patch.TokenLimit = *req.TokenLimit
	}
	if req.CreditLimit != nil {
		patch.CreditLimit = *req.CreditLimit
	}
	if req.AllowedModels != nil {
		patch.AllowedModels = *req.AllowedModels
	}
	if req.RPMLimit != nil {
		patch.RPMLimit = *req.RPMLimit
	}
	if req.ExpiresAt != nil {
		patch.ExpiresAt = *req.ExpiresAt
	}
	if err := config.ValidateApiKeyRestrictions(patch); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if req.DailyCreditLimit != nil {
		patch.DailyCreditLimit = *req.DailyCreditLimit
	}

	if err := config.UpdateApiKey(id, patch); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	updated := config.GetApiKeyEntry(id)
	if updated == nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to reload entry"})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"apiKey":  toApiKeyView(*updated),
	})
}

func (h *Handler) apiDeleteApiKey(w http.ResponseWriter, r *http.Request, id string) {
	if err := config.DeleteApiKey(id); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (h *Handler) apiResetApiKeyUsage(w http.ResponseWriter, r *http.Request, id string) {
	if err := config.ResetApiKeyUsage(id); err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	updated := config.GetApiKeyEntry(id)
	if updated == nil {
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"apiKey":  toApiKeyView(*updated),
	})
}
