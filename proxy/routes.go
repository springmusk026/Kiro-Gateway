package proxy

import (
	"encoding/json"
	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/logger"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// ServeHTTP 路由分发
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Debug-level request trace for fine-grained visibility
	logger.Debugf("[HTTP] %s %s from %s", r.Method, path, r.RemoteAddr)

	isAdmin := path == "/admin" || strings.HasPrefix(path, "/admin/")
	if isAdmin {
		// The admin UI is same-origin only: no CORS, no framing.
		setAdminSecurityHeaders(w)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	} else {
		h.setAPICORSHeaders(w, r)
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		// Every API response carries a request id under both header names
		// clients look for (Anthropic SDKs read request-id, OpenAI SDKs
		// x-request-id). The trace reuses the same id.
		reqID := newRequestID()
		w.Header().Set("request-id", reqID)
		w.Header().Set("X-Request-Id", reqID)
	}
	if r.Body != nil && r.Method != http.MethodGet {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes())
	}

	// Cap upstream calls per client request (chat endpoints and playground).
	if !isAdmin || strings.HasPrefix(path, "/admin/api/playground/") {
		r = r.WithContext(withUpstreamBudget(r.Context(), maxUpstreamAttempts()))
	}

	// Trace API requests for the request log. The record is produced after
	// the handler returns and handed to an asynchronous writer.
	if ep := endpointForPath(path); ep != "" && ep != "models" && r.Method == http.MethodPost {
		tw, tr, finish := h.beginTrace(w, r, ep)
		defer finish()
		w, r = tw, tr
	}

	// 路由
	h.route(w, r, path)
}

// setAPICORSHeaders allows browser clients to call the public /v1 API.
func (h *Handler) setAPICORSHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	allowHeaders := "Content-Type, Authorization, X-Api-Key, anthropic-version, anthropic-beta, anthropic-dangerous-direct-browser-access, x-api-key, x-stainless-os, x-stainless-lang, x-stainless-package-version, x-stainless-runtime, x-stainless-runtime-version, x-stainless-arch, x-stainless-retry-count, x-stainless-timeout, openai-organization, openai-project, openai-beta"
	// Echo whatever the preflight asks for, so new SDK headers do not break
	// browser clients.
	if r != nil {
		if requested := strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers")); requested != "" {
			allowHeaders = requested
		}
	}
	w.Header().Set("Access-Control-Allow-Headers", allowHeaders)
	w.Header().Set("Access-Control-Max-Age", "86400")
	w.Header().Set("Access-Control-Expose-Headers", "request-id, x-request-id, retry-after, x-ratelimit-limit-requests, x-ratelimit-limit-tokens, x-ratelimit-remaining-requests, x-ratelimit-remaining-tokens, x-ratelimit-reset-requests, x-ratelimit-reset-tokens, x-kiro-gateway-fallback-model")
}

func (h *Handler) route(w http.ResponseWriter, r *http.Request, path string) {
	switch {
	// API 端点（需要验证 API Key）
	case path == "/v1/messages" || path == "/messages" || path == "/anthropic/v1/messages":
		ar := h.authenticateForClaude(w, r)
		if ar == nil {
			return
		}
		h.handleClaudeMessages(w, ar)
	case path == "/v1/messages/count_tokens" || path == "/messages/count_tokens":
		ar := h.authenticateForClaude(w, r)
		if ar == nil {
			return
		}
		h.handleCountTokens(w, ar)
	case path == "/v1/chat/completions" || path == "/chat/completions":
		ar := h.authenticateForOpenAI(w, r)
		if ar == nil {
			return
		}
		h.handleOpenAIChat(w, ar)
	case path == "/v1/responses" || path == "/responses":
		ar := h.authenticateForOpenAI(w, r)
		if ar == nil {
			return
		}
		h.handleOpenAIResponses(w, ar)
	case strings.HasPrefix(path, "/v1/responses/") || strings.HasPrefix(path, "/responses/"):
		ar := h.authenticateForOpenAI(w, r)
		if ar == nil {
			return
		}
		h.handleResponseByID(w, ar, path[strings.Index(path, "/responses/")+len("/responses/"):])
	case path == "/v1/models" || path == "/models":
		h.handleModels(w, r)
	case strings.HasPrefix(path, "/v1/models/") || strings.HasPrefix(path, "/models/"):
		id := path[strings.Index(path, "/models/")+len("/models/"):]
		if id == "" {
			h.handleModels(w, r)
			return
		}
		h.handleModel(w, r, id)
	case path == "/api/event_logging/batch":
		// Claude Code 遥测端点 - 直接返回 200 OK
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write([]byte(`{"status":"ok"}`))

	// 管理端点
	case path == "/admin" || path == "/admin/":
		h.serveAdminPage(w, r)
	case strings.HasPrefix(path, "/admin/api/"):
		h.handleAdminAPI(w, r)
	case strings.HasPrefix(path, "/admin/"):
		h.serveStaticFile(w, r)

	case path == "/metrics":
		h.handleMetrics(w, r)

	// 健康检查
	case path == "/health" || path == "/":
		h.handleHealth(w, r)

	case path == "/v1/usage":
		h.handleKeyUsage(w, r) // proxy/key_usage.go

	// 统计端点（需要 API Key 鉴权）
	case path == "/v1/stats":
		if !h.validateApiKey(r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid or missing API key"})
			return
		}
		h.handleStats(w, r)

	default:
		if isAPIPath(path) {
			h.sendAPINotFound(w, r)
			return
		}
		http.Error(w, "Not Found", 404)
	}
}

// isAPIPath reports paths under the public API surface, which answer with JSON
// errors rather than plain-text ones.
func isAPIPath(path string) bool {
	return strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/anthropic/") ||
		strings.HasPrefix(path, "/chat/") || strings.HasPrefix(path, "/responses/") ||
		strings.HasPrefix(path, "/messages/") || strings.HasPrefix(path, "/models/")
}

// handleHealth 健康检查（不暴露统计数据）
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"version": config.Version,
		"uptime":  time.Now().Unix() - h.startTime,
	})
}

// handleStats 统计数据（需要 API Key 鉴权）
func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "ok",
		"version":         config.Version,
		"accounts":        h.pool.Count(),
		"available":       h.pool.AvailableCount(),
		"totalRequests":   atomic.LoadInt64(&h.totalRequests),
		"successRequests": atomic.LoadInt64(&h.successRequests),
		"failedRequests":  atomic.LoadInt64(&h.failedRequests),
		"totalTokens":     atomic.LoadInt64(&h.totalTokens),
		"totalCredits":    h.getCredits(),
		"uptime":          time.Now().Unix() - h.startTime,
	})
}

// ==================== 管理 API ====================

func (h *Handler) handleAdminAPI(w http.ResponseWriter, r *http.Request) {
	// Console sign-in / sign-out / whoami (handles its own auth).
	if r.URL.Path == "/admin/api/session" {
		h.handleAdminSession(w, r)
		return
	}
	// Session cookie (SameSite=Strict + CSRF header rule) or the
	// X-Admin-Password header; constant time, throttled.
	if !h.checkAdminAuth(w, r) {
		return
	}
	// Audit every state-changing admin call (method, path and result only:
	// bodies can contain credentials and are never recorded).
	if r.Method != http.MethodGet && !strings.HasPrefix(r.URL.Path, "/admin/api/playground/") {
		sw := &tracingWriter{ResponseWriter: w}
		w = sw
		defer func() {
			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			h.audit(r, r.Method+" "+strings.TrimPrefix(r.URL.Path, "/admin/api"), "", map[string]string{"status": strconv.Itoa(status)})
		}()
	}

	path := strings.TrimPrefix(r.URL.Path, "/admin/api")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if h.routeTelemetryAdmin(w, r, path) || h.routeCostAdmin(w, r, path) {
		return
	}

	switch {
	case path == "/accounts" && r.Method == "GET":
		h.apiGetAccounts(w, r)
	case path == "/models" && r.Method == "GET":
		h.apiGetModels(w, r)
	case path == "/models/toggle" && r.Method == "POST":
		h.apiSetModelEnabled(w, r)
	// Playground: the dashboard talks to the real API handlers, authenticated
	// by the admin session instead of a client API key.
	case path == "/playground/chat/completions" && r.Method == "POST":
		h.handleOpenAIChat(w, r)
	case path == "/playground/messages" && r.Method == "POST":
		h.handleClaudeMessages(w, r)
	case path == "/accounts" && r.Method == "POST":
		h.apiAddAccount(w, r)
	case path == "/accounts/batch" && r.Method == "POST":
		h.apiBatchAccounts(w, r)
	// models/refresh 必须在通用 /refresh 前匹配，否则会被误拦截
	case path == "/accounts/models/refresh" && r.Method == "POST":
		h.apiRefreshAllAccountsModels(w, r)
	case strings.HasPrefix(path, "/accounts/") && strings.HasSuffix(path, "/models/refresh") && r.Method == "POST":
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/accounts/"), "/models/refresh")
		h.apiRefreshAccountModels(w, r, id)
	case strings.HasPrefix(path, "/accounts/") && strings.HasSuffix(path, "/refresh") && r.Method == "POST":
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/accounts/"), "/refresh")
		h.apiRefreshAccount(w, r, id)
	case strings.HasPrefix(path, "/accounts/") && strings.HasSuffix(path, "/test") && r.Method == "POST":
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/accounts/"), "/test")
		h.apiTestAccount(w, r, id)
	case strings.HasPrefix(path, "/accounts/") && strings.HasSuffix(path, "/models/cached") && r.Method == "GET":
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/accounts/"), "/models/cached")
		h.apiGetAccountModelsCached(w, r, id)
	case strings.HasPrefix(path, "/accounts/") && strings.HasSuffix(path, "/models") && r.Method == "GET":
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/accounts/"), "/models")
		h.apiGetAccountModels(w, r, id)

	case strings.HasPrefix(path, "/accounts/") && strings.HasSuffix(path, "/overage") && r.Method == "POST":
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/accounts/"), "/overage")
		h.apiSetAccountOverage(w, r, id)
	case strings.HasPrefix(path, "/accounts/") && strings.HasSuffix(path, "/overage") && r.Method == "GET":
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/accounts/"), "/overage")
		h.apiGetAccountOverage(w, r, id)

	case strings.HasPrefix(path, "/accounts/") && strings.HasSuffix(path, "/full") && r.Method == "GET":
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/accounts/"), "/full")
		h.apiGetAccountFull(w, r, id)
	case strings.HasPrefix(path, "/accounts/") && r.Method == "DELETE":
		h.apiDeleteAccount(w, r, strings.TrimPrefix(path, "/accounts/"))
	case strings.HasPrefix(path, "/accounts/") && r.Method == "PUT":
		h.apiUpdateAccount(w, r, strings.TrimPrefix(path, "/accounts/"))
	case path == "/auth/iam-sso/start" && r.Method == "POST":
		h.apiStartIamSso(w, r)
	case path == "/auth/iam-sso/complete" && r.Method == "POST":
		h.apiCompleteIamSso(w, r)
	case path == "/auth/microsoft-sso/start" && r.Method == "POST":
		h.apiStartMicrosoftSSO(w, r)
	case path == "/auth/microsoft-sso/complete" && r.Method == "POST":
		h.apiCompleteMicrosoftSSO(w, r)
	case path == "/auth/microsoft-sso/select-profile" && r.Method == "POST":
		h.apiSelectMicrosoftSSOProfile(w, r)
	case path == "/auth/microsoft-sso/cancel" && r.Method == "POST":
		h.apiCancelMicrosoftSSO(w, r)
	case path == "/auth/builderid/start" && r.Method == "POST":
		h.apiStartBuilderIdLogin(w, r)
	case path == "/auth/builderid/poll" && r.Method == "POST":
		h.apiPollBuilderIdAuth(w, r)
	case path == "/auth/sso-token" && r.Method == "POST":
		h.apiImportSsoToken(w, r)
	case path == "/auth/credentials" && r.Method == "POST":
		h.apiImportCredentials(w, r)
	case path == "/status" && r.Method == "GET":
		h.apiGetStatus(w, r)
	case path == "/settings" && r.Method == "GET":
		h.apiGetSettings(w, r)
	case path == "/settings" && r.Method == "POST":
		h.apiUpdateSettings(w, r)
	case path == "/stats" && r.Method == "GET":
		h.apiGetStats(w, r)
	case path == "/stats/reset" && r.Method == "POST":
		h.apiResetStats(w, r)
	case path == "/logs" && r.Method == "GET":
		h.apiGetLogs(w, r)
	case path == "/logs" && r.Method == "DELETE":
		h.apiClearLogs(w, r)
	case path == "/generate-machine-id" && r.Method == "GET":
		h.apiGenerateMachineId(w, r)
	case path == "/thinking" && r.Method == "GET":
		h.apiGetThinkingConfig(w, r)
	case path == "/thinking" && r.Method == "POST":
		h.apiUpdateThinkingConfig(w, r)
	case path == "/model-mappings" && r.Method == "GET":
		h.apiGetModelMappings(w, r)
	case path == "/model-mappings" && r.Method == "POST":
		h.apiUpdateModelMappings(w, r)
	case path == "/endpoint" && r.Method == "GET":
		h.apiGetEndpointConfig(w, r)
	case path == "/endpoint" && r.Method == "POST":
		h.apiUpdateEndpointConfig(w, r)
	case path == "/proxy" && r.Method == "GET":
		h.apiGetProxy(w, r)
	case path == "/proxy" && r.Method == "POST":
		h.apiUpdateProxy(w, r)
	case path == "/prompt-filter" && r.Method == "GET":
		h.apiGetPromptFilter(w, r)
	case path == "/prompt-filter" && r.Method == "POST":
		h.apiUpdatePromptFilter(w, r)
	case path == "/version" && r.Method == "GET":
		h.apiGetVersion(w, r)
	case path == "/export" && r.Method == "POST":
		h.apiExportAccounts(w, r)
	case path == "/api-keys" && r.Method == "GET":
		h.apiListApiKeys(w, r)
	case path == "/api-keys" && r.Method == "POST":
		h.apiCreateApiKey(w, r)
	case strings.HasPrefix(path, "/api-keys/") && strings.HasSuffix(path, "/reset-usage") && r.Method == "POST":
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/api-keys/"), "/reset-usage")
		h.apiResetApiKeyUsage(w, r, id)
	case strings.HasPrefix(path, "/api-keys/") && r.Method == "GET":
		h.apiGetApiKey(w, r, strings.TrimPrefix(path, "/api-keys/"))
	case strings.HasPrefix(path, "/api-keys/") && r.Method == "PUT":
		h.apiUpdateApiKey(w, r, strings.TrimPrefix(path, "/api-keys/"))
	case strings.HasPrefix(path, "/api-keys/") && r.Method == "DELETE":
		h.apiDeleteApiKey(w, r, strings.TrimPrefix(path, "/api-keys/"))
	default:
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]string{"error": "Not Found"})
	}
}

// ==================== 静态文件服务 ====================

func (h *Handler) serveAdminPage(w http.ResponseWriter, r *http.Request) {
	serveAdminShell(w, r)
}

// serveStaticFile serves built console assets; any other path under /admin is
// a client-side route and gets the SPA shell.
func (h *Handler) serveStaticFile(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/admin/")
	if webFileExists(rel) {
		if strings.HasPrefix(rel, "assets/") {
			// Content-hashed build output: safe to cache forever.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		serveWebFile(w, r, rel)
		return
	}
	if strings.HasPrefix(rel, "assets/") || strings.Contains(path.Base(rel), ".") {
		http.NotFound(w, r)
		return
	}
	serveAdminShell(w, r)
}
