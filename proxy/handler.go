package proxy

import (
	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/pool"
	"github.com/springmusk026/Kiro-Gateway/telemetry"
	"net/http"
	"sync"
	"time"
)

// Handler HTTP 处理器
type Handler struct {
	pool *pool.AccountPool
	// 运行时统计 (使用原子操作)
	totalRequests   int64
	successRequests int64
	failedRequests  int64
	totalTokens     int64
	totalCredits    float64 // float64 需要用锁保护
	creditsMu       sync.RWMutex
	startTime       int64
	stopRefresh     chan struct{}
	stopStatsSaver  chan struct{}
	// 模型缓存
	cachedModels       []ModelInfo
	modelsCacheMu      sync.RWMutex
	modelsCacheTime    int64
	modelsRefreshMu    sync.Mutex // one upstream model refresh at a time
	promptCache        *promptCacheTracker
	tokenRefreshMu     sync.Mutex // legacy; per-account locks live in refreshLocks
	refreshLocks       sync.Map   // accountID → *sync.Mutex
	credentialImportMu sync.Mutex
	// 请求日志 (环形缓冲区，包含成功和失败)
	requestLogs   []RequestLog
	requestLogsMu sync.RWMutex

	microsoftSelections   map[string]*microsoftProfileSelection
	microsoftSelectionsMu sync.Mutex
	microsoftFlowMu       sync.Mutex
	microsoftCanceled     map[string]time.Time
	microsoftDiscoveries  map[string]*microsoftProfileDiscovery
	// Telemetry and persistence (database backend); nil when not configured.
	sink      telemetry.Sink
	logReader telemetry.Reader
	respStore telemetry.ResponseStore
	migrator  telemetry.Migrator
	// conversations maps conversationID → accountID (see pickAccount).
	conversations conversationBindings
	// sessionAffinity maps apiKeyID → accountID (see pickAccount).
	sessionAffinity sync.Map
	// cost holds usage refresh, budget and alert state (cost_controls.go).
	cost costControls
}

func NewHandler() *Handler {
	// 启动时应用代理配置
	applyProxyConfig(config.GetProxyURL())

	totalReq, successReq, failedReq, totalTokens, totalCredits := config.GetStats()
	h := &Handler{
		pool:                 pool.GetPool(),
		totalRequests:        int64(totalReq),
		successRequests:      int64(successReq),
		failedRequests:       int64(failedReq),
		totalTokens:          int64(totalTokens),
		totalCredits:         totalCredits,
		startTime:            time.Now().Unix(),
		stopRefresh:          make(chan struct{}),
		stopStatsSaver:       make(chan struct{}),
		promptCache:          newPromptCacheTracker(defaultPromptCacheTTL),
		microsoftSelections:  make(map[string]*microsoftProfileSelection),
		microsoftCanceled:    make(map[string]time.Time),
		microsoftDiscoveries: make(map[string]*microsoftProfileDiscovery),
	}
	// 启动后台刷新
	go h.backgroundRefresh()
	// 启动后台统计保存 (每30秒保存一次)
	go h.backgroundStatsSaver()
	h.startCostBackground()
	// 清理过期的 stored responses（>30 天）
	go responsesStoreJanitor(h.stopRefresh)
	return h
}

// validateApiKey 验证 API Key（Bool 包装，旧签名仍被部分调用方使用）
func (h *Handler) validateApiKey(r *http.Request) bool {
	_, err := h.authenticate(r)
	return err == nil
}

// authenticateForClaude runs authenticate and the key's restrictions and
// writes a Claude-style error on failure. Returns the request with the matched
// API key injected into context, or nil if auth failed.
func (h *Handler) authenticateForClaude(w http.ResponseWriter, r *http.Request) *http.Request {
	ar, ae := h.authenticateAndRestrict(r)
	if ae != nil {
		setRetryAfterHeader(w, ae.retryAfter)
		h.sendClaudeError(w, ae.status, ae.code, ae.message)
		return nil
	}
	if h.rejectOverBudget(w, ar, &config.ApiKeyEntry{ID: apiKeyIDFromContext(ar.Context())}, true) {
		return nil
	}
	return ar
}

// authenticateForOpenAI is authenticateForClaude with OpenAI-style errors.
func (h *Handler) authenticateForOpenAI(w http.ResponseWriter, r *http.Request) *http.Request {
	ar, ae := h.authenticateAndRestrict(r)
	if ae != nil {
		setRetryAfterHeader(w, ae.retryAfter)
		typ := ae.openAIType
		if typ == "" {
			typ = ae.code
		}
		h.sendOpenAIErrorWithCode(w, ae.status, typ, ae.openAICode, ae.message)
		return nil
	}
	if h.rejectOverBudget(w, ar, &config.ApiKeyEntry{ID: apiKeyIDFromContext(ar.Context())}, false) {
		return nil
	}
	return ar
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// UseTelemetry connects request logging, log queries and the stored-responses
// backend (all optional; nil keeps the in-memory/file behaviour).
// UseMigrator enables the console's config.json → database migration.
func (h *Handler) UseMigrator(m telemetry.Migrator) { h.migrator = m }

func (h *Handler) UseTelemetry(sink telemetry.Sink, reader telemetry.Reader, responses telemetry.ResponseStore) {
	h.sink = sink
	h.logReader = reader
	h.respStore = responses
	responseBackend = responses
}
