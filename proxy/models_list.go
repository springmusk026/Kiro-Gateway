package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/logger"
	"net/http"
	"strings"
	"time"
)

// modelsListEpoch is the fixed creation time reported for every model. Kiro
// does not publish one, and clients only need a stable value.
const modelsListEpoch int64 = 1700000000

// handleModels 模型列表
func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.sendAPIMethodNotAllowed(w, r, "GET")
		return
	}
	models := filterModelsForKey(r, h.listModels())

	resp := map[string]interface{}{
		"object":   "list",
		"data":     models,
		"has_more": false,
		"first_id": nil,
		"last_id":  nil,
	}
	if len(models) > 0 {
		resp["first_id"] = models[0]["id"]
		resp["last_id"] = models[len(models)-1]["id"]
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(resp)
}

// handleModel answers GET /v1/models/{id} with one entry of the list.
func (h *Handler) handleModel(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.sendAPIMethodNotAllowed(w, r, "GET")
		return
	}
	for _, m := range filterModelsForKey(r, h.listModels()) {
		if mid, _ := m["id"].(string); strings.EqualFold(mid, id) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			json.NewEncoder(w).Encode(m)
			return
		}
	}
	h.sendAPIError(w, r, http.StatusNotFound, "not_found_error", "invalid_request_error", "model_not_found",
		fmt.Sprintf("model: %s", id))
}

// listModels builds the model list shared by the OpenAI and Anthropic shapes:
// every entry carries both the OpenAI fields (object, created, owned_by) and
// the Anthropic ones (type, display_name, created_at).
func (h *Handler) listModels() []map[string]interface{} {
	// Live upstream model list (cached, refreshed in the background once stale).
	cached := h.ensureModelsFresh()

	thinkingSuffix := config.GetThinkingConfig().Suffix

	models := buildAnthropicModelsResponse(cached, thinkingSuffix)
	if len(models) == 0 {
		models = fallbackAnthropicModels(thinkingSuffix)
	}

	// 添加别名模型
	models = append(models,
		buildModelInfoWithTools("auto", "kiro-proxy", true, true),
		buildModelInfoWithTools("gpt-4o", "kiro-proxy", true, true),
		buildModelInfoWithTools("gpt-4", "kiro-proxy", true, true),
	)
	models = appendConfiguredModelMappings(models, config.GetModelMappings(), thinkingSuffix)
	models = filterDisabledModels(models, thinkingSuffix, config.GetModelMappings())

	createdAt := time.Unix(modelsListEpoch, 0).UTC().Format(time.RFC3339)
	for _, m := range models {
		if _, ok := m["created"]; !ok {
			m["created"] = modelsListEpoch
		}
		if _, ok := m["type"]; !ok {
			m["type"] = "model"
		}
		if _, ok := m["created_at"]; !ok {
			m["created_at"] = createdAt
		}
		if _, ok := m["display_name"]; !ok {
			m["display_name"] = m["id"]
		}
	}
	return models
}

func buildAnthropicModelsResponse(cached []ModelInfo, thinkingSuffix string) []map[string]interface{} {
	if len(cached) == 0 {
		return nil
	}

	models := make([]map[string]interface{}, 0, len(cached)*2)
	if len(cached) > 0 {
		for _, m := range cached {
			supportsImage := modelSupportsImage(m.InputTypes)
			models = append(models, modelMetadata(buildModelInfo(m.ModelId, "anthropic", supportsImage), m))
			// 自动生成 thinking 变体
			models = append(models, modelMetadata(buildModelInfo(m.ModelId+thinkingSuffix, "anthropic", supportsImage), m))
		}
	}
	return models
}

func fallbackAnthropicModels(thinkingSuffix string) []map[string]interface{} {
	return []map[string]interface{}{
		buildModelInfo("claude-sonnet-4.6", "anthropic", true),
		buildModelInfo("claude-sonnet-4.6"+thinkingSuffix, "anthropic", true),
		buildModelInfo("claude-opus-4.6", "anthropic", true),
		buildModelInfo("claude-opus-4.6"+thinkingSuffix, "anthropic", true),
		buildModelInfo("claude-opus-4.7", "anthropic", true),
		buildModelInfo("claude-opus-4.7"+thinkingSuffix, "anthropic", true),
		buildModelInfo("claude-sonnet-4.5", "anthropic", true),
		buildModelInfo("claude-sonnet-4.5"+thinkingSuffix, "anthropic", true),
		buildModelInfo("claude-sonnet-4", "anthropic", true),
		buildModelInfo("claude-sonnet-4"+thinkingSuffix, "anthropic", true),
		buildModelInfo("claude-haiku-4.5", "anthropic", true),
		buildModelInfo("claude-haiku-4.5"+thinkingSuffix, "anthropic", true),
		buildModelInfo("claude-opus-4.5", "anthropic", true),
		buildModelInfo("claude-opus-4.5"+thinkingSuffix, "anthropic", true),
	}
}

func modelSupportsImage(inputTypes []string) bool {
	for _, t := range inputTypes {
		lt := strings.ToLower(t)
		if strings.Contains(lt, "image") || strings.Contains(lt, "vision") {
			return true
		}
	}
	return false
}

func buildModelInfo(id, ownedBy string, supportsImage bool) map[string]interface{} {
	return buildModelInfoWithTools(id, ownedBy, supportsImage, false)
}

// buildModelInfoWithTools 在 buildModelInfo 基础上可选声明 function_calling/tools 能力。
// supportsTools 仅应对 gpt 别名分支（ownedBy=="kiro-proxy"）传 true，
// 用于让 ChatGPT/Codex 等客户端在拉取 /v1/models 时能探测到工具调用能力，
// 不影响 Claude/anthropic 分支现有的能力声明。
func buildModelInfoWithTools(id, ownedBy string, supportsImage, supportsTools bool) map[string]interface{} {
	modalities := []string{"text"}
	if supportsImage {
		modalities = append(modalities, "image")
	}
	modalitiesMap := map[string][]string{
		"input":  modalities,
		"output": []string{"text"},
	}

	capabilities := map[string]bool{
		"vision":       supportsImage,
		"image":        supportsImage,
		"image_vision": supportsImage,
	}
	metaCapabilities := map[string]bool{
		"vision":       supportsImage,
		"image_vision": supportsImage,
	}
	if supportsTools {
		capabilities["function_calling"] = true
		capabilities["tools"] = true
		metaCapabilities["function_calling"] = true
		metaCapabilities["tools"] = true
	}

	info := map[string]interface{}{
		"id":               id,
		"object":           "model",
		"owned_by":         ownedBy,
		"supports_image":   supportsImage,
		"input_modalities": modalities,
		"modalities":       modalitiesMap,
		"capabilities":     capabilities,
		"info": map[string]interface{}{
			"meta": map[string]interface{}{
				"capabilities": metaCapabilities,
			},
		},
	}
	if supportsTools {
		info["supports_tools"] = true
		info["supports_function_calling"] = true
	}
	return info
}

func appendConfiguredModelMappings(models []map[string]interface{}, mappings []config.ModelMapping, thinkingSuffix string) []map[string]interface{} {
	if len(mappings) == 0 {
		return models
	}
	modelIndexByID := make(map[string]int, len(models)+len(mappings))
	supportsImageByID := make(map[string]bool, len(models))
	for i, model := range models {
		id, _ := model["id"].(string)
		if id == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(id))
		modelIndexByID[key] = i
		supportsImage, _ := model["supports_image"].(bool)
		supportsImageByID[key] = supportsImage
	}
	explicitSources := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		explicitSources[strings.ToLower(mapping.Source)] = struct{}{}
	}
	upsertMapping := func(id, target string) {
		key := strings.ToLower(id)
		supportsImage := supportsImageByID[strings.ToLower(target)]
		mappedModel := buildModelInfo(id, "kiro-proxy", supportsImage)
		if index, exists := modelIndexByID[key]; exists {
			models[index] = mappedModel
			return
		}
		modelIndexByID[key] = len(models)
		models = append(models, mappedModel)
	}

	// Add every exact source first so an explicitly configured source such as
	// "custom-thinking" wins over a suffix-derived variant of "custom".
	for _, mapping := range mappings {
		upsertMapping(mapping.Source, mapping.Target)
	}
	if thinkingSuffix == "" {
		return models
	}
	for _, mapping := range mappings {
		if strings.HasSuffix(strings.ToLower(mapping.Source), strings.ToLower(thinkingSuffix)) {
			continue
		}
		variant := mapping.Source + thinkingSuffix
		if _, explicit := explicitSources[strings.ToLower(variant)]; explicit {
			continue
		}
		upsertMapping(variant, mapping.Target)
	}
	return models
}

// refreshModelsCache 从 Kiro API 拉取模型列表并缓存
func (h *Handler) refreshModelsCache() {
	accounts := config.GetEnabledAccounts()
	if len(accounts) == 0 {
		return
	}

	aggregated := make([]ModelInfo, 0)
	for i := range accounts {
		account := &accounts[i]
		if err := h.ensureValidToken(account); err != nil {
			logger.Warnf("[ModelsCache] Skip %s token refresh failed: %v", account.Email, err)
			h.handleAccountFailure(account, err)
			continue
		}

		models, err := ListAvailableModels(account)
		if err != nil {
			logger.Warnf("[ModelsCache] Failed to refresh for %s: %v", account.Email, err)
			h.handleAccountFailure(account, err)
			continue
		}
		// 缓存每账号可用模型，用于路由时过滤
		modelIDs := make([]string, 0, len(models))
		for _, m := range models {
			modelIDs = append(modelIDs, m.ModelId)
		}
		h.pool.SetModelList(account.ID, modelIDs)
		aggregated = mergeUniqueModels(aggregated, models)
	}

	if len(aggregated) > 0 {
		h.modelsCacheMu.Lock()
		h.cachedModels = aggregated
		h.modelsCacheTime = time.Now().Unix()
		h.modelsCacheMu.Unlock()
		logger.Infof("[ModelsCache] Cached %d models", len(aggregated))
	}
}

// modelPrefetchDisabled stops model-list fetches against the real upstream;
// tests set it so adding fake accounts never reaches Kiro.
var (
	modelPrefetchDisabled    bool
	errModelPrefetchDisabled = errors.New("model prefetch disabled")
)

// fetchAndCacheAccountModels 为单个账号拉取并写入模型缓存。
// 同时更新 pool 的路由缓存与全局聚合模型列表。
func (h *Handler) fetchAndCacheAccountModels(account *config.Account) error {
	if modelPrefetchDisabled {
		return errModelPrefetchDisabled
	}
	if err := h.ensureValidToken(account); err != nil {
		return fmt.Errorf("token refresh failed: %w", err)
	}
	models, err := ListAvailableModels(account)
	if err != nil {
		return err
	}
	modelIDs := make([]string, 0, len(models))
	for _, m := range models {
		modelIDs = append(modelIDs, m.ModelId)
	}
	h.pool.SetModelList(account.ID, modelIDs)

	// 合并到聚合缓存
	h.modelsCacheMu.Lock()
	h.cachedModels = mergeUniqueModels(h.cachedModels, models)
	h.modelsCacheTime = time.Now().Unix()
	h.modelsCacheMu.Unlock()

	logger.Infof("[ModelsCache] Refreshed %d models for account %s", len(models), account.Email)
	return nil
}

func mergeUniqueModels(existing []ModelInfo, incoming []ModelInfo) []ModelInfo {
	if len(incoming) == 0 {
		return existing
	}

	indexByID := make(map[string]int, len(existing))
	merged := make([]ModelInfo, len(existing))
	copy(merged, existing)
	for i, model := range merged {
		indexByID[strings.ToLower(strings.TrimSpace(model.ModelId))] = i
	}

	for _, model := range incoming {
		key := strings.ToLower(strings.TrimSpace(model.ModelId))
		if key == "" {
			continue
		}
		if idx, ok := indexByID[key]; ok {
			merged[idx] = mergeModelInfo(merged[idx], model)
			continue
		}
		indexByID[key] = len(merged)
		merged = append(merged, model)
	}

	return merged
}

func mergeModelInfo(base ModelInfo, extra ModelInfo) ModelInfo {
	if base.ModelName == "" {
		base.ModelName = extra.ModelName
	}
	if base.Description == "" {
		base.Description = extra.Description
	}
	if base.RateMultiplier == 0 {
		base.RateMultiplier = extra.RateMultiplier
	}
	if base.TokenLimits == nil {
		base.TokenLimits = extra.TokenLimits
	}
	if base.PromptCaching == nil {
		base.PromptCaching = extra.PromptCaching
	}
	base.InputTypes = mergeStringLists(base.InputTypes, extra.InputTypes)
	return base
}

func mergeStringLists(base []string, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]bool, len(base)+len(extra))
	merged := make([]string, 0, len(base)+len(extra))
	for _, item := range base {
		key := strings.ToLower(strings.TrimSpace(item))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, item)
	}
	for _, item := range extra {
		key := strings.ToLower(strings.TrimSpace(item))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, item)
	}
	return merged
}
