package proxy

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/logger"
	"github.com/springmusk026/Kiro-Gateway/telemetry"
)

// logFilterFromQuery reads the request-log filters shared by the list and
// the CSV export (status, endpoint, model, account, apiKey, errorType, q, hours).
func logFilterFromQuery(r *http.Request) telemetry.LogFilter {
	q := r.URL.Query()
	f := telemetry.LogFilter{
		Status:    q.Get("status"),
		Endpoint:  q.Get("endpoint"),
		Model:     q.Get("model"),
		AccountID: q.Get("account"),
		APIKeyID:  q.Get("apiKey"),
		ErrorType: q.Get("errorType"),
		Search:    q.Get("q"),
	}
	if hours := queryInt(r, "hours", 0); hours > 0 {
		f.Since = time.Now().Add(-time.Duration(hours) * time.Hour)
	}
	return f
}

func writeAdminError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"error": msg})
}

// apiConversations: GET /admin/api/conversations?hours=&q=&limit=&before=
// One row per upstream conversation, most recently active first.
func (h *Handler) apiConversations(w http.ResponseWriter, r *http.Request) {
	cr, ok := h.logReader.(telemetry.ConversationReader)
	if !ok || h.logReader == nil {
		writeAdminError(w, http.StatusNotImplemented, "conversations require the database backend (set DATABASE_URL)")
		return
	}
	q := r.URL.Query()
	limit := min(queryInt(r, "limit", 50), 200)
	f := telemetry.ConversationFilter{Search: q.Get("q"), BeforeID: q.Get("before"), Limit: limit + 1}
	if hours := queryInt(r, "hours", 0); hours > 0 {
		f.Since = time.Now().Add(-time.Duration(hours) * time.Hour)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	convs, err := cr.QueryConversations(ctx, f)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hasMore := len(convs) > limit
	if hasMore {
		convs = convs[:limit]
	}
	writeJSON(w, map[string]interface{}{"conversations": convs, "hasMore": hasMore})
}

// conversationTimelineCap bounds GET /admin/api/conversations/{id}.
const conversationTimelineCap = 1000

// apiConversation: GET /admin/api/conversations/{id} — its requests in order.
func (h *Handler) apiConversation(w http.ResponseWriter, r *http.Request, id string) {
	cr, ok := h.logReader.(telemetry.ConversationReader)
	if !ok || h.logReader == nil {
		writeAdminError(w, http.StatusNotImplemented, "conversations require the database backend (set DATABASE_URL)")
		return
	}
	if id == "" {
		writeAdminError(w, http.StatusNotFound, "conversation not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	recs, err := cr.ConversationRequests(ctx, id, conversationTimelineCap+1)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(recs) == 0 {
		writeAdminError(w, http.StatusNotFound, "conversation not found")
		return
	}
	truncated := len(recs) > conversationTimelineCap
	if truncated {
		recs = recs[:conversationTimelineCap]
	}
	writeJSON(w, map[string]interface{}{"id": id, "requests": recs, "truncated": truncated})
}

// requestExportCap bounds one CSV export.
const requestExportCap = 100000

var requestCSVHeader = []string{"time", "id", "status", "http_status", "endpoint", "protocol", "stream", "model",
	"fallback_model", "account_id", "account_email", "api_key_id", "api_key_name", "client_ip", "user_agent",
	"input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens", "credits", "duration_ms", "ttfb_ms",
	"attempts", "error_type", "error", "conversation_id"}

// csvSafe neutralises a text cell that a spreadsheet would read as a formula
// (leading =, +, -, @, tab or carriage return) by prefixing a single quote.
// Quoting of commas, quotes and newlines is left to encoding/csv.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func requestCSVRow(x *telemetry.RequestRecord, keyNames map[string]string) []string {
	return []string{
		x.Time.UTC().Format(time.RFC3339Nano), csvSafe(x.ID), csvSafe(x.Status), strconv.Itoa(x.HTTPStatus),
		csvSafe(x.Endpoint), csvSafe(x.Protocol), strconv.FormatBool(x.Stream), csvSafe(x.Model),
		csvSafe(x.FallbackModel), csvSafe(x.AccountID), csvSafe(x.AccountEmail), csvSafe(x.APIKeyID),
		csvSafe(keyNames[x.APIKeyID]), csvSafe(x.ClientIP), csvSafe(x.UserAgent),
		strconv.Itoa(x.InputTokens), strconv.Itoa(x.OutputTokens), strconv.Itoa(x.CacheRead), strconv.Itoa(x.CacheWrite),
		strconv.FormatFloat(x.Credits, 'f', -1, 64), strconv.FormatInt(x.DurationMs, 10), strconv.FormatInt(x.TTFBMs, 10),
		strconv.Itoa(x.Attempts), csvSafe(x.ErrorType), csvSafe(x.Error), csvSafe(x.ConversationID),
	}
}

// apiExportRequests: GET /admin/api/requests/export.csv?<request log filters>
// streams the matching rows (newest first, no bodies, at most 100,000).
func (h *Handler) apiExportRequests(w http.ResponseWriter, r *http.Request) {
	ex, ok := h.logReader.(telemetry.RequestExporter)
	if !ok || h.logReader == nil {
		writeAdminError(w, http.StatusNotImplemented, "request history requires the database backend (set DATABASE_URL)")
		return
	}
	keyNames := map[string]string{}
	for _, k := range config.ListApiKeys() {
		keyNames[k.ID] = k.Name
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	hdr := w.Header()
	hdr.Set("Content-Type", "text/csv; charset=utf-8")
	hdr.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="kiro-requests-%s.csv"`, time.Now().UTC().Format("20060102-150405")))
	hdr.Set("Cache-Control", "no-store")
	cw := csv.NewWriter(w)
	if err := cw.Write(requestCSVHeader); err != nil {
		return
	}
	n := 0
	err := ex.ExportRequests(ctx, logFilterFromQuery(r), requestExportCap, func(x *telemetry.RequestRecord) error {
		if err := cw.Write(requestCSVRow(x, keyNames)); err != nil {
			return err
		}
		if n++; n%2000 == 0 {
			cw.Flush()
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		return cw.Error()
	})
	cw.Flush()
	if err != nil {
		// Headers are gone; the file simply ends early.
		logger.Warnf("[Export] request CSV stopped after %d rows: %v", n, err)
	}
}
