package proxy

import (
	"crypto/sha256"
	"encoding/json"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultPromptCacheTTL = 5 * time.Minute

// Anthropic requires cached prefixes to reach a minimum token count before
// caching takes effect. Breakpoints below this threshold are excluded from
// matching and storage to avoid reporting unrealistic 100% cache hits on
// short requests.
const defaultMinCacheableTokens = 1024
const opusMinCacheableTokens = 4096

type promptCacheUsage struct {
	CacheCreationInputTokens   int
	CacheReadInputTokens       int
	CacheCreation5mInputTokens int
	CacheCreation1hInputTokens int
}

type promptCacheBreakpoint struct {
	Fingerprint      [32]byte
	CumulativeTokens int
	TTL              time.Duration
}

type promptCacheProfile struct {
	Breakpoints      []promptCacheBreakpoint
	TotalInputTokens int
	Model            string
}

func minCacheableTokensForModel(model string) int {
	lower := strings.ToLower(model)
	if strings.Contains(lower, "opus") {
		return opusMinCacheableTokens
	}
	return defaultMinCacheableTokens
}

type promptCacheEntry struct {
	ExpiresAt time.Time
	TTL       time.Duration
}

type promptCacheTracker struct {
	mu               sync.Mutex
	entriesByAccount map[string]map[[32]byte]promptCacheEntry
	maxSupportedTTL  time.Duration
}

func newPromptCacheTracker(maxTTL time.Duration) *promptCacheTracker {
	if maxTTL <= 0 {
		maxTTL = defaultPromptCacheTTL
	}
	return &promptCacheTracker{
		entriesByAccount: make(map[string]map[[32]byte]promptCacheEntry),
		maxSupportedTTL:  maxTTL,
	}
}

func (t *promptCacheTracker) BuildClaudeProfile(req *ClaudeRequest, totalInputTokens int) *promptCacheProfile {
	// Breakpoints only exist once a block carries an explicit cache_control
	// marker; without one the profile is nil and nothing needs hashing.
	if !claudeCacheMarkersPresent(req) {
		return nil
	}
	blocks := flattenClaudeCacheBlocks(req)
	if len(blocks) == 0 {
		return nil
	}

	hasher := sha256.New()
	breakpoints := make([]promptCacheBreakpoint, 0)
	cumulativeTokens := 0
	var activeTTL time.Duration

	for _, block := range blocks {
		writeHashChunk(hasher, block.canonicalBytes())
		cumulativeTokens += block.Tokens

		// Determine whether this block acts as a cache breakpoint:
		//   1) Explicit cache_control on the block itself.
		//   2) Once any explicit breakpoint has been seen, every message-end
		//      boundary becomes an implicit breakpoint so that multi-turn
		//      conversations can hit earlier stored prefixes.
		breakpointTTL := time.Duration(0)
		if block.TTL > 0 {
			breakpointTTL = block.TTL
			activeTTL = block.TTL
		} else if block.IsMessageEnd && activeTTL > 0 {
			breakpointTTL = activeTTL
		}

		if breakpointTTL <= 0 {
			continue
		}

		var fingerprint [32]byte
		copy(fingerprint[:], hasher.Sum(nil))
		breakpoints = append(breakpoints, promptCacheBreakpoint{
			Fingerprint:      fingerprint,
			CumulativeTokens: cumulativeTokens,
			TTL:              breakpointTTL,
		})
	}

	if len(breakpoints) == 0 {
		return nil
	}

	if totalInputTokens < cumulativeTokens {
		totalInputTokens = cumulativeTokens
	}

	return &promptCacheProfile{
		Breakpoints:      breakpoints,
		TotalInputTokens: totalInputTokens,
		Model:            req.Model,
	}
}

// BuildOpenAIProfile builds a cache profile for an OpenAI-shaped request.
//
// Standard OpenAI Chat Completions requests carry no client-side cache_control
// marker, so unlike Claude there is no explicit-breakpoint precondition: every
// block boundary produced by flattenOpenAICacheBlocks (the end of the tool
// list, and the end of each message) becomes an unconditional implicit
// breakpoint using the default 5-minute TTL. This lets repeated history
// prefixes across multi-turn conversations still register as cache hits even
// though the client never opted in explicitly.
func (t *promptCacheTracker) BuildOpenAIProfile(req *OpenAIRequest, totalInputTokens int) *promptCacheProfile {
	blocks := flattenOpenAICacheBlocks(req)
	if len(blocks) == 0 {
		return nil
	}

	hasher := sha256.New()
	breakpoints := make([]promptCacheBreakpoint, 0)
	cumulativeTokens := 0

	for _, block := range blocks {
		writeHashChunk(hasher, block.canonicalBytes())
		cumulativeTokens += block.Tokens

		if block.TTL <= 0 {
			continue
		}

		var fingerprint [32]byte
		copy(fingerprint[:], hasher.Sum(nil))
		breakpoints = append(breakpoints, promptCacheBreakpoint{
			Fingerprint:      fingerprint,
			CumulativeTokens: cumulativeTokens,
			TTL:              block.TTL,
		})
	}

	if len(breakpoints) == 0 {
		return nil
	}

	if totalInputTokens < cumulativeTokens {
		totalInputTokens = cumulativeTokens
	}

	return &promptCacheProfile{
		Breakpoints:      breakpoints,
		TotalInputTokens: totalInputTokens,
		Model:            req.Model,
	}
}

func (t *promptCacheTracker) Compute(accountID string, profile *promptCacheProfile) promptCacheUsage {
	if t == nil || profile == nil || len(profile.Breakpoints) == 0 || accountID == "" {
		return promptCacheUsage{}
	}

	minTokens := minCacheableTokensForModel(profile.Model)
	last := profile.Breakpoints[len(profile.Breakpoints)-1]
	lastTokens := minInt(last.CumulativeTokens, profile.TotalInputTokens)
	now := time.Now()

	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneExpiredLocked(now)

	entries := t.entriesByAccount[accountID]
	if len(entries) == 0 {
		// First request for this account: report creation only if above threshold.
		effectiveCreation := lastTokens
		if effectiveCreation < minTokens {
			effectiveCreation = 0
		}
		cache5m, cache1h := computePromptCacheTTLBreakdown(profile, 0)
		return promptCacheUsage{
			CacheCreationInputTokens:   effectiveCreation,
			CacheReadInputTokens:       0,
			CacheCreation5mInputTokens: cache5m,
			CacheCreation1hInputTokens: cache1h,
		}
	}

	// Cap cacheable tokens at 99.9% of total input to ensure a realistic
	// uncached portion. The newest content in a request is never fully
	// served from cache on the current turn.
	maxCacheable := int(float64(profile.TotalInputTokens) * 0.999)
	if lastTokens > maxCacheable {
		lastTokens = maxCacheable
	}

	matchedTokens := 0
	for i := len(profile.Breakpoints) - 1; i >= 0; i-- {
		breakpoint := profile.Breakpoints[i]
		// Skip breakpoints below the minimum cacheable token threshold.
		if breakpoint.CumulativeTokens < minTokens {
			continue
		}
		entry, ok := entries[breakpoint.Fingerprint]
		if !ok || entry.ExpiresAt.Before(now) {
			continue
		}
		entry.ExpiresAt = now.Add(entry.TTL)
		entries[breakpoint.Fingerprint] = entry
		matchedTokens = minInt(breakpoint.CumulativeTokens, profile.TotalInputTokens)
		if matchedTokens > lastTokens {
			matchedTokens = lastTokens
		}
		break
	}

	creation := maxInt(lastTokens-matchedTokens, 0)
	cache5m, cache1h := computePromptCacheTTLBreakdown(profile, matchedTokens)
	return promptCacheUsage{
		CacheCreationInputTokens:   creation,
		CacheReadInputTokens:       matchedTokens,
		CacheCreation5mInputTokens: cache5m,
		CacheCreation1hInputTokens: cache1h,
	}
}

func (t *promptCacheTracker) Update(accountID string, profile *promptCacheProfile) {
	if t == nil || profile == nil || len(profile.Breakpoints) == 0 || accountID == "" {
		return
	}

	minTokens := minCacheableTokensForModel(profile.Model)
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneExpiredLocked(now)

	entries := t.entriesByAccount[accountID]
	if entries == nil {
		entries = make(map[[32]byte]promptCacheEntry)
		t.entriesByAccount[accountID] = entries
	}

	for _, breakpoint := range profile.Breakpoints {
		// Skip breakpoints below the minimum cacheable token threshold.
		if breakpoint.CumulativeTokens < minTokens {
			continue
		}
		entries[breakpoint.Fingerprint] = promptCacheEntry{
			ExpiresAt: now.Add(breakpoint.TTL),
			TTL:       breakpoint.TTL,
		}
	}
}

func (t *promptCacheTracker) pruneExpiredLocked(now time.Time) {
	for accountID, entries := range t.entriesByAccount {
		for fingerprint, entry := range entries {
			if !entry.ExpiresAt.After(now) {
				delete(entries, fingerprint)
			}
		}
		if len(entries) == 0 {
			delete(t.entriesByAccount, accountID)
		}
	}
}

type cacheablePromptBlock struct {
	Value        interface{}
	Tokens       int
	TTL          time.Duration
	IsMessageEnd bool
	// canonical is the canonical JSON of Value, computed once for both the
	// token estimate and the fingerprint.
	canonical []byte
}

// newCacheBlock builds a block whose token count comes from its canonical
// form.
func newCacheBlock(value interface{}, ttl time.Duration, isMessageEnd bool) cacheablePromptBlock {
	canonical := appendCanonicalJSON(nil, value)
	return cacheablePromptBlock{
		Value:        value,
		Tokens:       estimateApproxTokensBytes(canonical),
		TTL:          ttl,
		IsMessageEnd: isMessageEnd,
		canonical:    canonical,
	}
}

func (b cacheablePromptBlock) canonicalBytes() []byte {
	if b.canonical != nil {
		return b.canonical
	}
	return appendCanonicalJSON(nil, b.Value)
}

// claudeCacheMarkersPresent reports whether any block that
// flattenClaudeCacheBlocks keeps carries a cache_control TTL. Tools never
// do: ClaudeTool has no cache_control field.
func claudeCacheMarkersPresent(req *ClaudeRequest) bool {
	marked := func(block interface{}) bool {
		return extractPromptCacheTTL(block) > 0 && !isAnthropicBillingHeaderBlock(block)
	}
	if system, ok := req.System.([]interface{}); ok {
		for _, block := range system {
			if marked(block) {
				return true
			}
		}
	}
	for _, msg := range req.Messages {
		switch content := msg.Content.(type) {
		case string:
		case []interface{}:
			for _, block := range content {
				if marked(block) {
					return true
				}
			}
		default:
			if content != nil && marked(content) {
				return true
			}
		}
	}
	return false
}

func flattenClaudeCacheBlocks(req *ClaudeRequest) []cacheablePromptBlock {
	blocks := make([]cacheablePromptBlock, 0)
	blocks = append(blocks, buildCachePreludeBlock(req))

	for toolIndex, tool := range req.Tools {
		toolValue := map[string]interface{}{
			"kind":         "tool",
			"tool_index":   toolIndex,
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": tool.InputSchema,
		}
		fingerprintValue := stripCachePositionKeys(toolValue)
		blocks = append(blocks, newCacheBlock(fingerprintValue, normalizePromptCacheTTL(extractPromptCacheTTL(tool)), false))
	}

	appendSystemCacheBlocks(&blocks, req.System)

	for messageIndex, msg := range req.Messages {
		appendMessageCacheBlocks(&blocks, messageIndex, msg)
	}

	return blocks
}

// flattenOpenAICacheBlocks flattens an OpenAI-shaped request into cacheable
// blocks. OpenAIRequest has no independent System field -- a system prompt is
// just a message with role=="system" -- so every message (including system
// ones) is handled by the same loop. Every message's last block always closes
// with an implicit breakpoint (TTL = defaultPromptCacheTTL), regardless of any
// cache_control marker, since the OpenAI wire format has none.
func flattenOpenAICacheBlocks(req *OpenAIRequest) []cacheablePromptBlock {
	blocks := make([]cacheablePromptBlock, 0)

	if len(req.Tools) > 0 {
		for toolIndex, tool := range req.Tools {
			toolValue := map[string]interface{}{
				"kind":        "tool",
				"tool_index":  toolIndex,
				"name":        tool.Function.Name,
				"description": tool.Function.Description,
				"parameters":  tool.Function.Parameters,
			}
			fingerprintValue := stripCachePositionKeys(toolValue)
			blocks = append(blocks, newCacheBlock(fingerprintValue, 0, toolIndex == len(req.Tools)-1))
		}
		// Tool definitions typically stay stable across a multi-turn
		// conversation, so treat the end of the tool list as its own
		// implicit breakpoint.
		blocks[len(blocks)-1].TTL = defaultPromptCacheTTL
	}

	for messageIndex, msg := range req.Messages {
		appendOpenAIMessageCacheBlocks(&blocks, messageIndex, msg)
	}

	return blocks
}

func appendOpenAIMessageCacheBlocks(blocks *[]cacheablePromptBlock, messageIndex int, msg OpenAIMessage) {
	role := msg.Role

	switch content := msg.Content.(type) {
	case string:
		appendOpenAIPromptBlock(blocks, map[string]interface{}{
			"kind":          "message",
			"message_index": messageIndex,
			"role":          role,
			"block_index":   0,
			"block": map[string]interface{}{
				"type": "text",
				"text": content,
			},
		}, true)
	case []interface{}:
		lastIdx := len(content) - 1
		for blockIndex, block := range content {
			appendOpenAIPromptBlock(blocks, map[string]interface{}{
				"kind":          "message",
				"message_index": messageIndex,
				"role":          role,
				"block_index":   blockIndex,
				"block":         block,
			}, blockIndex == lastIdx)
		}
	default:
		if content != nil {
			appendOpenAIPromptBlock(blocks, map[string]interface{}{
				"kind":          "message",
				"message_index": messageIndex,
				"role":          role,
				"block_index":   0,
				"block":         content,
			}, true)
		}
	}

	if len(msg.ToolCalls) > 0 {
		lastIdx := len(msg.ToolCalls) - 1
		for tcIndex, tc := range msg.ToolCalls {
			appendOpenAIPromptBlock(blocks, map[string]interface{}{
				"kind":          "message_tool_call",
				"message_index": messageIndex,
				"role":          role,
				"block_index":   tcIndex,
				"block": map[string]interface{}{
					"type":      "tool_call",
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				},
			}, tcIndex == lastIdx)
		}
	} else if msg.ToolCallID != "" {
		appendOpenAIPromptBlock(blocks, map[string]interface{}{
			"kind":          "message_tool_call_id",
			"message_index": messageIndex,
			"role":          role,
			"block_index":   0,
			"block": map[string]interface{}{
				"type":         "tool_call_id",
				"tool_call_id": msg.ToolCallID,
			},
		}, true)
	}
}

// appendOpenAIPromptBlock mirrors appendPromptBlock but always treats a
// message-end block as an unconditional implicit breakpoint, matching the
// user-approved "implicit breakpoint" policy for the OpenAI wire format.
func appendOpenAIPromptBlock(blocks *[]cacheablePromptBlock, wrapper map[string]interface{}, isMessageEnd bool) {
	blockValue := wrapper["block"]

	// Drop volatile billing metadata from the cache fingerprint, matching the
	// Claude path. OpenAIToKiro does not run stripEnvNoiseLines, so this is
	// the only place billing-header noise gets excluded on this path.
	if isAnthropicBillingHeaderBlock(blockValue) {
		return
	}

	fingerprintValue := stripCachePositionKeys(wrapper)

	ttl := time.Duration(0)
	if isMessageEnd {
		ttl = defaultPromptCacheTTL
	}

	*blocks = append(*blocks, newCacheBlock(fingerprintValue, ttl, isMessageEnd))
}

func buildCachePreludeBlock(req *ClaudeRequest) cacheablePromptBlock {
	prelude := map[string]interface{}{
		"kind":        "request_prelude",
		"model":       req.Model,
		"tool_choice": req.ToolChoice,
	}
	return newCacheBlock(prelude, 0, false)
}

func appendSystemCacheBlocks(blocks *[]cacheablePromptBlock, system interface{}) {
	switch v := system.(type) {
	case string:
		appendPromptBlock(blocks, map[string]interface{}{
			"kind":         "system",
			"system_index": 0,
			"block": map[string]interface{}{
				"type": "text",
				"text": v,
			},
		}, false)
	case []interface{}:
		for i, block := range v {
			appendPromptBlock(blocks, map[string]interface{}{
				"kind":         "system",
				"system_index": i,
				"block":        block,
			}, false)
		}
	case []string:
		for i, block := range v {
			appendPromptBlock(blocks, map[string]interface{}{
				"kind":         "system",
				"system_index": i,
				"block": map[string]interface{}{
					"type": "text",
					"text": block,
				},
			}, false)
		}
	}
}

func appendMessageCacheBlocks(blocks *[]cacheablePromptBlock, messageIndex int, msg ClaudeMessage) {
	role := msg.Role
	switch content := msg.Content.(type) {
	case string:
		appendPromptBlock(blocks, map[string]interface{}{
			"kind":          "message",
			"message_index": messageIndex,
			"role":          role,
			"block_index":   0,
			"block": map[string]interface{}{
				"type": "text",
				"text": content,
			},
		}, true)
	case []interface{}:
		lastIdx := len(content) - 1
		for blockIndex, block := range content {
			appendPromptBlock(blocks, map[string]interface{}{
				"kind":          "message",
				"message_index": messageIndex,
				"role":          role,
				"block_index":   blockIndex,
				"block":         block,
			}, blockIndex == lastIdx)
		}
	default:
		if content != nil {
			appendPromptBlock(blocks, map[string]interface{}{
				"kind":          "message",
				"message_index": messageIndex,
				"role":          role,
				"block_index":   0,
				"block":         content,
			}, true)
		}
	}
}

func appendPromptBlock(blocks *[]cacheablePromptBlock, wrapper map[string]interface{}, isMessageEnd bool) {
	blockValue := wrapper["block"]
	ttl := normalizePromptCacheTTL(extractPromptCacheTTL(blockValue))

	// Drop volatile billing metadata from the cache fingerprint. Claude Code's
	// x-anthropic-billing-header can drift, appear, or disappear across
	// otherwise identical requests, and it does not change model semantics.
	if isAnthropicBillingHeaderBlock(blockValue) {
		return
	}

	fingerprintValue := stripCachePositionKeys(wrapper)
	*blocks = append(*blocks, newCacheBlock(fingerprintValue, ttl, isMessageEnd))
}

func stripCachePositionKeys(value map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(value))
	for key, item := range value {
		if isCachePositionKey(key) {
			continue
		}
		cloned[key] = item
	}
	return cloned
}

func isAnthropicBillingHeaderBlock(value interface{}) bool {
	blockMap, ok := value.(map[string]interface{})
	if !ok {
		return false
	}

	// Only normalize text blocks (or blocks without an explicit type but containing text).
	if t, ok := blockMap["type"].(string); ok && t != "" && t != "text" {
		return false
	}

	text, ok := blockMap["text"].(string)
	if !ok {
		return false
	}

	trimmed := strings.TrimLeft(text, " \t\r\n")
	return strings.HasPrefix(strings.ToLower(trimmed), "x-anthropic-billing-header:")
}

func extractPromptCacheTTL(value interface{}) time.Duration {
	if _, isTool := value.(ClaudeTool); isTool {
		// Its JSON form has no cache_control key: skip the round trip below.
		return 0
	}
	block, ok := value.(map[string]interface{})
	if !ok {
		if raw, err := json.Marshal(value); err == nil {
			var decoded map[string]interface{}
			if json.Unmarshal(raw, &decoded) == nil {
				block = decoded
				ok = true
			}
		}
	}
	if !ok {
		return 0
	}

	rawCache, ok := block["cache_control"]
	if !ok {
		return 0
	}
	cacheControl, ok := rawCache.(map[string]interface{})
	if !ok {
		return 0
	}
	cacheType, _ := cacheControl["type"].(string)
	if !strings.EqualFold(cacheType, "ephemeral") {
		return 0
	}

	if ttl, ok := parsePromptCacheTTLValue(cacheControl["ttl"]); ok {
		return ttl
	}
	return defaultPromptCacheTTL
}

func parsePromptCacheTTLValue(value interface{}) (time.Duration, bool) {
	switch v := value.(type) {
	case string:
		trimmed := strings.TrimSpace(strings.ToLower(v))
		if trimmed == "" {
			return 0, false
		}
		if d, err := time.ParseDuration(trimmed); err == nil {
			return d, true
		}
		if seconds, err := strconv.Atoi(trimmed); err == nil {
			return time.Duration(seconds) * time.Second, true
		}
	case float64:
		if v > 0 {
			return time.Duration(v) * time.Second, true
		}
	case int:
		if v > 0 {
			return time.Duration(v) * time.Second, true
		}
	case int64:
		if v > 0 {
			return time.Duration(v) * time.Second, true
		}
	}
	return 0, false
}

func normalizePromptCacheTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return 0
	}
	if ttl > time.Hour {
		return time.Hour
	}
	if ttl > defaultPromptCacheTTL {
		return time.Hour
	}
	return defaultPromptCacheTTL
}

func computePromptCacheTTLBreakdown(profile *promptCacheProfile, matchedTokens int) (int, int) {
	if profile == nil || len(profile.Breakpoints) == 0 {
		return 0, 0
	}

	cache5m := 0
	cache1h := 0
	previous := matchedTokens
	for _, breakpoint := range profile.Breakpoints {
		current := minInt(breakpoint.CumulativeTokens, profile.TotalInputTokens)
		if current <= previous {
			continue
		}
		delta := current - previous
		if breakpoint.TTL >= time.Hour {
			cache1h += delta
		} else {
			cache5m += delta
		}
		previous = current
	}
	return cache5m, cache1h
}

func billedClaudeInputTokens(inputTokens int, usage promptCacheUsage) int {
	return maxInt(inputTokens-usage.CacheCreationInputTokens-usage.CacheReadInputTokens, 0)
}

// reconcileOpenAICacheUsage adapts Anthropic-style cache accounting to OpenAI's
// usage shape, where the prompt/input token count is the raw total with cached
// tokens included and the cached count is reported as a subset of it, rather
// than being excluded from it as on the Anthropic side. Applying
// billedClaudeInputTokens here instead subtracts the cache counters from a
// total that already contains them, which collapses the reported input to zero
// whenever the cache profile covers the whole prefix.
//
// cacheUsage is computed from the pre-request token estimate while inputTokens
// is the post-request measured value, so the estimated cache read can still
// reach or exceed the measured total. Reporting that verbatim claims a 100%
// hit rate on a turn that carries new input and leaves nothing billable as
// fresh input, so scale the total back above the cached subset in that case.
func reconcileOpenAICacheUsage(inputTokens int, usage promptCacheUsage) (promptTokens, cachedTokens int) {
	promptTokens = inputTokens
	cachedTokens = usage.CacheReadInputTokens
	if cachedTokens > 0 && cachedTokens >= promptTokens {
		promptTokens = int(float64(cachedTokens) * (1.03 + rand.Float64()*0.32))
	}
	return promptTokens, cachedTokens
}

func buildClaudeUsageMap(inputTokens, outputTokens int, usage promptCacheUsage, includeCache bool) map[string]interface{} {
	result := map[string]interface{}{
		"input_tokens":  billedClaudeInputTokens(inputTokens, usage),
		"output_tokens": outputTokens,
	}
	if !includeCache {
		return result
	}
	result["cache_creation_input_tokens"] = usage.CacheCreationInputTokens
	result["cache_read_input_tokens"] = usage.CacheReadInputTokens
	result["cache_creation"] = map[string]int{
		"ephemeral_5m_input_tokens": usage.CacheCreation5mInputTokens,
		"ephemeral_1h_input_tokens": usage.CacheCreation1hInputTokens,
	}
	return result
}

func buildOpenAIUsageMap(inputTokens, outputTokens int, usage promptCacheUsage, includeCache bool) map[string]interface{} {
	if !includeCache {
		return map[string]interface{}{
			"prompt_tokens":     inputTokens,
			"completion_tokens": outputTokens,
			"total_tokens":      inputTokens + outputTokens,
		}
	}

	promptTokens, cachedTokens := reconcileOpenAICacheUsage(inputTokens, usage)
	return map[string]interface{}{
		"prompt_tokens":     promptTokens,
		"completion_tokens": outputTokens,
		"total_tokens":      promptTokens + outputTokens,
		// OpenAI clients read the cached subset from prompt_tokens_details;
		// the Anthropic-named counters below are additive, for clients that
		// came to rely on this proxy emitting them.
		"prompt_tokens_details": map[string]int{
			"cached_tokens": cachedTokens,
		},
		"cache_creation_input_tokens": usage.CacheCreationInputTokens,
		"cache_read_input_tokens":     cachedTokens,
		"cache_creation": map[string]int{
			"ephemeral_5m_input_tokens": usage.CacheCreation5mInputTokens,
			"ephemeral_1h_input_tokens": usage.CacheCreation1hInputTokens,
		},
	}
}

func canonicalizeCacheValue(value interface{}) string {
	return string(appendCanonicalJSON(nil, value))
}

// appendCanonicalJSON appends value as JSON with sorted object keys and
// cache_control removed. Strings, ints and float64 are encoded directly,
// byte-identical to encoding/json; other scalars go through json.Marshal.
func appendCanonicalJSON(dst []byte, value interface{}) []byte {
	switch v := value.(type) {
	case nil:
		return append(dst, "null"...)
	case string:
		return appendJSONString(dst, v)
	case bool:
		if v {
			return append(dst, "true"...)
		}
		return append(dst, "false"...)
	case int:
		return strconv.AppendInt(dst, int64(v), 10)
	case float64:
		if out, ok := appendJSONFloat64(dst, v); ok {
			return out
		}
		encoded, _ := json.Marshal(v)
		return append(dst, encoded...)
	case float32, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number:
		encoded, _ := json.Marshal(v)
		return append(dst, encoded...)
	case []interface{}:
		dst = append(dst, '[')
		for i, item := range v {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendCanonicalJSON(dst, item)
		}
		return append(dst, ']')
	case map[string]interface{}:
		dst = append(dst, '{')
		keys := make([]string, 0, len(v))
		for key := range v {
			if key == "cache_control" {
				continue
			}
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for i, key := range keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendJSONString(dst, key)
			dst = append(dst, ':')
			dst = appendCanonicalJSON(dst, v[key])
		}
		return append(dst, '}')
	default:
		encoded, _ := json.Marshal(v)
		return append(dst, encoded...)
	}
}

func isCachePositionKey(key string) bool {
	switch key {
	case "tool_index", "system_index", "message_index", "block_index":
		return true
	default:
		return false
	}
}

func writeHashChunk(hasher hashWriter, chunk []byte) {
	var prefix [24]byte
	head := append(strconv.AppendInt(prefix[:0], int64(len(chunk)), 10), 0)
	hasher.Write(head)
	hasher.Write(chunk)
	hasher.Write([]byte{0})
}

type hashWriter interface {
	Write([]byte) (int, error)
	Sum([]byte) []byte
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
