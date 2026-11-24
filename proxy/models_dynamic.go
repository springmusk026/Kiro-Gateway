package proxy

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
)

// modelsCacheTTL is how long the aggregated upstream model list is served
// before a background refresh is started. Requests never wait on a refresh
// unless the cache is completely empty.
// It matches the 30-minute background refresh, so clients polling /v1/models
// do not add extra ListAvailableModels calls per account.
const modelsCacheTTL = 30 * time.Minute

// ensureModelsFresh makes sure the aggregated model cache is populated and
// schedules a background refresh once it is older than modelsCacheTTL.
// Concurrent callers share a single in-flight refresh.
func (h *Handler) ensureModelsFresh() []ModelInfo {
	h.modelsCacheMu.RLock()
	cached := h.cachedModels
	age := time.Since(time.Unix(h.modelsCacheTime, 0))
	h.modelsCacheMu.RUnlock()

	switch {
	case len(cached) == 0:
		// Nothing to serve yet: refresh synchronously, but only once across
		// concurrent requests.
		h.modelsRefreshMu.Lock()
		h.modelsCacheMu.RLock()
		cached = h.cachedModels
		h.modelsCacheMu.RUnlock()
		if len(cached) == 0 {
			h.refreshModelsCache()
			h.modelsCacheMu.RLock()
			cached = h.cachedModels
			h.modelsCacheMu.RUnlock()
		}
		h.modelsRefreshMu.Unlock()
	case age > modelsCacheTTL:
		if h.modelsRefreshMu.TryLock() {
			go func() {
				defer h.modelsRefreshMu.Unlock()
				h.refreshModelsCache()
			}()
		}
	}
	return cached
}

// modelMetadata adds upstream metadata (display name, context window, output
// limit, credit multiplier) to a /v1/models entry, using the field names the
// common OpenAI-compatible clients read.
func modelMetadata(entry map[string]interface{}, m ModelInfo) map[string]interface{} {
	if name := strings.TrimSpace(m.ModelName); name != "" {
		entry["display_name"] = name
		entry["name"] = name
	}
	if desc := strings.TrimSpace(m.Description); desc != "" {
		entry["description"] = desc
	}
	if m.TokenLimits != nil {
		if m.TokenLimits.MaxInputTokens > 0 {
			entry["context_length"] = m.TokenLimits.MaxInputTokens
			entry["context_window"] = m.TokenLimits.MaxInputTokens
		}
		if m.TokenLimits.MaxOutputTokens > 0 {
			entry["max_output_tokens"] = m.TokenLimits.MaxOutputTokens
			entry["max_completion_tokens"] = m.TokenLimits.MaxOutputTokens
		}
	}
	if m.RateMultiplier > 0 {
		entry["rate_multiplier"] = m.RateMultiplier
	}
	if rc, ok := parseReasoningControls(m.AdditionalFieldsSchema); ok && len(rc.Levels) > 0 {
		entry["supported_reasoning_efforts"] = rc.Levels
		if rc.DefaultLevel != "" {
			entry["default_reasoning_effort"] = rc.DefaultLevel
		}
	}
	return entry
}

// adminModelEntry is one row of the admin models view.
type adminModelEntry struct {
	ID             string   `json:"id"`
	Name           string   `json:"name,omitempty"`
	Description    string   `json:"description,omitempty"`
	InputTypes     []string `json:"inputTypes,omitempty"`
	ContextLength  int      `json:"contextLength,omitempty"`
	MaxOutput      int      `json:"maxOutputTokens,omitempty"`
	RateMultiplier float64  `json:"rateMultiplier,omitempty"`
	Vision         bool     `json:"vision"`
	Enabled        bool     `json:"enabled"`
	// Prompt caching as reported by Kiro (nil when not reported).
	PromptCaching  *bool    `json:"promptCaching,omitempty"`
	CacheMinTokens int      `json:"cacheMinTokens,omitempty"`
	EffortLevels   []string `json:"effortLevels,omitempty"`
	DefaultEffort  string   `json:"defaultEffort,omitempty"`
	ThinkingToggle bool     `json:"thinkingToggle,omitempty"`
	Accounts       int      `json:"accounts"`
}

// apiGetModels returns the live, upstream-fetched model catalog with
// metadata and how many accounts can serve each model. ?refresh=1 forces a
// synchronous refresh first.
func (h *Handler) apiGetModels(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "1" {
		h.modelsRefreshMu.Lock()
		h.refreshModelsCache()
		h.modelsRefreshMu.Unlock()
	}
	models := h.ensureModelsFresh()

	h.modelsCacheMu.RLock()
	cachedAt := h.modelsCacheTime
	h.modelsCacheMu.RUnlock()

	accountCount := map[string]int{}
	for _, acc := range config.GetEnabledAccounts() {
		for _, id := range h.pool.GetModelList(acc.ID) {
			accountCount[strings.ToLower(id)]++
		}
	}

	out := make([]adminModelEntry, 0, len(models))
	for _, m := range models {
		e := adminModelEntry{
			ID:             m.ModelId,
			Name:           m.ModelName,
			Description:    m.Description,
			InputTypes:     m.InputTypes,
			RateMultiplier: m.RateMultiplier,
			Vision:         modelSupportsImage(m.InputTypes),
			Accounts:       accountCount[strings.ToLower(m.ModelId)],
			Enabled:        !config.IsModelDisabled(m.ModelId),
		}
		if m.TokenLimits != nil {
			e.ContextLength = m.TokenLimits.MaxInputTokens
			e.MaxOutput = m.TokenLimits.MaxOutputTokens
		}
		if m.PromptCaching != nil {
			v := m.PromptCaching.SupportsPromptCaching
			e.PromptCaching = &v
			e.CacheMinTokens = m.PromptCaching.MinimumTokensPerCacheCheckpoint
		}
		if rc, ok := parseReasoningControls(m.AdditionalFieldsSchema); ok {
			e.EffortLevels, e.DefaultEffort, e.ThinkingToggle = rc.Levels, rc.DefaultLevel, rc.Toggle
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	json.NewEncoder(w).Encode(map[string]interface{}{
		"models":         out,
		"cachedAt":       cachedAt,
		"ttlSeconds":     int(modelsCacheTTL.Seconds()),
		"thinkingSuffix": config.GetThinkingConfig().Suffix,
		"thinkingMode":   config.GetThinkingConfig().Mode,
		"mappings":       config.GetModelMappings(),
		"disabledModels": config.GetDisabledModels(),
	})
}
