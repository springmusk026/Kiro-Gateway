package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"kiro-go/config"
	"kiro-go/telemetry"
)

func queryInt(r *http.Request, key string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil && v > 0 {
		return v
	}
	return def
}

func (h *Handler) requireReader(w http.ResponseWriter) bool {
	if h.logReader != nil {
		return true
	}
	w.WriteHeader(http.StatusNotImplemented)
	writeJSON(w, map[string]string{"error": "request history requires the database backend (set DATABASE_URL)"})
	return false
}

// apiQueryRequests: GET /admin/api/requests — persisted request log search.
func (h *Handler) apiQueryRequests(w http.ResponseWriter, r *http.Request) {
	if !h.requireReader(w) {
		return
	}
	q := r.URL.Query()
	limit := queryInt(r, "limit", 100)
	f := telemetry.LogFilter{
		Status:    q.Get("status"),
		Endpoint:  q.Get("endpoint"),
		Model:     q.Get("model"),
		AccountID: q.Get("account"),
		APIKeyID:  q.Get("apiKey"),
		ErrorType: q.Get("errorType"),
		Search:    q.Get("q"),
		BeforeID:  q.Get("before"),
		Limit:     limit + 1,
	}
	if hours := queryInt(r, "hours", 0); hours > 0 {
		f.Since = time.Now().Add(-time.Duration(hours) * time.Hour)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	recs, err := h.logReader.QueryRequests(ctx, f)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	hasMore := len(recs) > limit
	if hasMore {
		recs = recs[:limit]
	}
	writeJSON(w, map[string]interface{}{"records": recs, "hasMore": hasMore})
}

// apiGetRequest: GET /admin/api/requests/{id} — one record including bodies.
func (h *Handler) apiGetRequest(w http.ResponseWriter, r *http.Request, id string) {
	if !h.requireReader(w) {
		return
	}
	rec, err := h.logReader.GetRequest(r.Context(), id)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "request not found"})
		return
	}
	writeJSON(w, rec)
}

// apiUsageSeries: GET /admin/api/usage/series?hours=24&bucket=3600
func (h *Handler) apiUsageSeries(w http.ResponseWriter, r *http.Request) {
	if !h.requireReader(w) {
		return
	}
	hours := queryInt(r, "hours", 24)
	bucket := time.Duration(queryInt(r, "bucket", 3600)) * time.Second
	points, err := h.logReader.UsageSeries(r.Context(), time.Now().Add(-time.Duration(hours)*time.Hour), bucket)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"points": points, "bucketSeconds": int(bucket.Seconds())})
}

// apiUsageBy: GET /admin/api/usage/by?dimension=model|account|apiKey|endpoint|client&hours=24
func (h *Handler) apiUsageBy(w http.ResponseWriter, r *http.Request) {
	if !h.requireReader(w) {
		return
	}
	dim := r.URL.Query().Get("dimension")
	if dim == "" {
		dim = "model"
	}
	hours := queryInt(r, "hours", 24)
	groups, err := h.logReader.UsageBy(r.Context(), dim, time.Now().Add(-time.Duration(hours)*time.Hour), queryInt(r, "limit", 10))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"groups": groups})
}

// apiQueryAudit: GET /admin/api/audit?hours=168
func (h *Handler) apiQueryAudit(w http.ResponseWriter, r *http.Request) {
	if !h.requireReader(w) {
		return
	}
	events, err := h.logReader.QueryAudit(r.Context(), time.Now().Add(-time.Duration(queryInt(r, "hours", 24*7))*time.Hour), queryInt(r, "limit", 200))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"events": events})
}

// apiStorageInfo: GET /admin/api/storage — backend and recorder health.
func (h *Handler) apiStorageInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]interface{}{
		"backend":          config.CurrentStore().Name(),
		"database":         h.logReader != nil,
		"logBodiesKB":      config.GetLogBodiesKB(),
		"logRetentionDays": effectiveRetentionDays(),
	}
	if h.logReader != nil {
		info["recorder"] = h.logReader.SinkStats()
	}
	writeJSON(w, info)
}

// apiUpdateLogging: POST /admin/api/logging {logBodiesKB, logRetentionDays}
func (h *Handler) apiUpdateLogging(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LogBodiesKB      int `json:"logBodiesKB"`
		LogRetentionDays int `json:"logRetentionDays"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "Invalid JSON"})
		return
	}
	if err := config.UpdateLoggingSettings(req.LogBodiesKB, req.LogRetentionDays); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

// effectiveRetentionDays applies the 30-day default.
func effectiveRetentionDays() int {
	if d := config.GetLogRetentionDays(); d > 0 {
		return d
	}
	return 30
}

// requestsRouted reports whether path is one of the telemetry admin routes and serves it.
func (h *Handler) routeTelemetryAdmin(w http.ResponseWriter, r *http.Request, path string) bool {
	switch {
	case path == "/requests" && r.Method == http.MethodGet:
		h.apiQueryRequests(w, r)
	case strings.HasPrefix(path, "/requests/") && r.Method == http.MethodGet:
		h.apiGetRequest(w, r, strings.TrimPrefix(path, "/requests/"))
	case path == "/usage/series" && r.Method == http.MethodGet:
		h.apiUsageSeries(w, r)
	case path == "/usage/by" && r.Method == http.MethodGet:
		h.apiUsageBy(w, r)
	case path == "/audit" && r.Method == http.MethodGet:
		h.apiQueryAudit(w, r)
	case path == "/storage" && r.Method == http.MethodGet:
		h.apiStorageInfo(w, r)
	case path == "/logging" && r.Method == http.MethodPost:
		h.apiUpdateLogging(w, r)
	default:
		return false
	}
	return true
}
