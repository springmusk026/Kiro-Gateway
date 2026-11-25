package proxy

import (
	"os"
	"strings"
	"sync"
)

// Prompt-cache points (experimental, off by default: KIRO_CACHE_POINTS=1).
//
// The CodeWhisperer streaming schema that generateAssistantResponse uses
// (namespace com.amazon.aws.codewhisperer, as bundled in the official Kiro
// agent) declares a CachePoint structure {"type": "default"} in three
// places:
//
//   - UserInputMessage.cachePoint
//   - AssistantResponseMessage.cachePoint
//   - Tool, a union of {toolSpecification} | {cachePoint}
//
// The official clients never send it, so the effect is UNVERIFIED: Kiro may
// ignore the markers, honour them, or reject the request. The /v1/responses
// schema has no such field (caching there is implicit), so markers are only
// added on the legacy transport.
//
// When enabled, for a model whose catalog entry reports supportsPromptCaching,
// a marker goes on the assistant half of the system-prompt priming pair (the
// end of the system prefix) and one at the end of the tool list, each only
// when the prefix it closes reaches minimumTokensPerCacheCheckpoint, and never
// more than maximumCacheCheckpointsPerRequest markers. The payload the rest of
// the gateway sees is never modified: markers are added on a copy that is
// serialized for the request body only.

// KiroCachePoint is the CodeWhisperer CachePoint structure.
type KiroCachePoint struct {
	Type string `json:"type"`
}

const (
	cachePointTypeDefault = "default"
	// defaultMaxCachePoints applies when the catalog reports no maximum.
	defaultMaxCachePoints = 2
	// systemPrimingAck is the assistant half of the system-prompt priming
	// pair built by the translator.
	systemPrimingAck = "I will follow these instructions."
)

func cachePointsEnabled() bool {
	v := strings.TrimSpace(os.Getenv("KIRO_CACHE_POINTS"))
	return v == "1" || strings.EqualFold(v, "true")
}

// newCachePointTool is a cache point tool entry: the {cachePoint} member of
// the Tool union, serialized without a toolSpecification. (A plain struct
// encoding, rather than a MarshalJSON method, keeps the per-tool encoding
// and re-validation off the request path.)
func newCachePointTool() KiroToolWrapper {
	return KiroToolWrapper{
		ToolSpecification: KiroToolSpecification{InputSchema: InputSchema{JSON: cachePointOnlySpec}},
		CachePoint:        &KiroCachePoint{Type: cachePointTypeDefault},
	}
}

// promptCachingInfo is a model's prompt-cache capability from the catalog.
type promptCachingInfo struct {
	Supported      bool
	MinTokens      int
	MaxCheckpoints int
}

var modelPromptCaching sync.Map // lowercased model id → promptCachingInfo

// recordModelPromptCaching stores the prompt-cache capability of each model
// that reports one.
func recordModelPromptCaching(models []ModelInfo) {
	for _, m := range models {
		key := strings.ToLower(strings.TrimSpace(m.ModelId))
		if key == "" || m.PromptCaching == nil {
			continue
		}
		modelPromptCaching.Store(key, promptCachingInfo{
			Supported:      m.PromptCaching.SupportsPromptCaching,
			MinTokens:      m.PromptCaching.MinimumTokensPerCacheCheckpoint,
			MaxCheckpoints: m.PromptCaching.MaximumCacheCheckpointsPerRequest,
		})
	}
}

func lookupPromptCaching(model string) (promptCachingInfo, bool) {
	v, ok := modelPromptCaching.Load(strings.ToLower(strings.TrimSpace(model)))
	if !ok {
		return promptCachingInfo{}, false
	}
	return v.(promptCachingInfo), true
}

// withCachePoints returns the payload to serialize for generateAssistantResponse:
// p itself, or a copy carrying cache points when they are enabled and apply.
func withCachePoints(p *KiroPayload) *KiroPayload {
	if p == nil || !cachePointsEnabled() {
		return p
	}
	return addCachePoints(p)
}

func addCachePoints(p *KiroPayload) *KiroPayload {
	info, ok := lookupPromptCaching(currentMessageModelID(p))
	if !ok || !info.Supported {
		return p
	}
	budget := info.MaxCheckpoints
	if budget <= 0 {
		budget = defaultMaxCachePoints
	}

	hist := p.ConversationState.History
	primed := len(hist) >= 2 && hist[0].UserInputMessage != nil &&
		hist[1].AssistantResponseMessage != nil && hist[1].AssistantResponseMessage.Content == systemPrimingAck
	systemTokens := 0
	if primed {
		systemTokens = estimateApproxTokens(hist[0].UserInputMessage.Content)
	}
	ctx := p.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext
	toolTokens := 0
	if ctx != nil && len(ctx.Tools) > 0 {
		toolTokens = estimateJSONTokens(ctx.Tools)
	}

	markSystem := primed && systemTokens >= info.MinTokens && budget > 0
	if markSystem {
		budget--
	}
	markTools := toolTokens > 0 && systemTokens+toolTokens >= info.MinTokens && budget > 0
	if !markSystem && !markTools {
		return p
	}

	out := *p
	if markSystem {
		h := make([]KiroHistoryMessage, len(hist))
		copy(h, hist)
		ack := *h[1].AssistantResponseMessage
		ack.CachePoint = &KiroCachePoint{Type: cachePointTypeDefault}
		h[1].AssistantResponseMessage = &ack
		out.ConversationState.History = h
	}
	if markTools {
		c := *ctx
		c.Tools = append(append(make([]KiroToolWrapper, 0, len(ctx.Tools)+1), ctx.Tools...),
			newCachePointTool())
		out.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext = &c
	}
	return &out
}
