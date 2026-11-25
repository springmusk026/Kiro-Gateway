package proxy

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/telemetry"
)

// fakeHistory is an in-memory request history for the admin and usage endpoints.
type fakeHistory struct {
	recs      []telemetry.RequestRecord
	lastQuery telemetry.LogFilter
	exported  telemetry.LogFilter
	exportMax int
	convs     []telemetry.ConversationSummary
	convQuery telemetry.ConversationFilter
	days      []telemetry.KeyUsageDay
}

func (f *fakeHistory) QueryRequests(_ context.Context, q telemetry.LogFilter) ([]telemetry.RequestRecord, error) {
	f.lastQuery = q
	var out []telemetry.RequestRecord
	for _, r := range f.recs {
		if q.APIKeyID != "" && r.APIKeyID != q.APIKeyID {
			continue
		}
		out = append(out, r)
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}
func (f *fakeHistory) CountRequests(context.Context, telemetry.LogFilter) (int64, bool, error) {
	return int64(len(f.recs)), false, nil
}
func (f *fakeHistory) GetRequest(context.Context, string) (*telemetry.RequestRecord, error) {
	return nil, nil
}
func (f *fakeHistory) UsageSeries(context.Context, time.Time, time.Duration) ([]telemetry.UsagePoint, error) {
	return nil, nil
}
func (f *fakeHistory) UsageBy(context.Context, string, time.Time, int) ([]telemetry.UsageGroup, error) {
	return nil, nil
}
func (f *fakeHistory) QueryAudit(context.Context, time.Time, int) ([]telemetry.AuditEvent, error) {
	return nil, nil
}
func (f *fakeHistory) SinkStats() telemetry.SinkStats { return telemetry.SinkStats{} }
func (f *fakeHistory) QueryConversations(_ context.Context, q telemetry.ConversationFilter) ([]telemetry.ConversationSummary, error) {
	f.convQuery = q
	return f.convs, nil
}
func (f *fakeHistory) ConversationRequests(_ context.Context, id string, _ int) ([]telemetry.RequestRecord, error) {
	var out []telemetry.RequestRecord
	for _, r := range f.recs {
		if r.ConversationID == id {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeHistory) KeyDailyUsage(context.Context, string, time.Time, *time.Location) ([]telemetry.KeyUsageDay, error) {
	return f.days, nil
}
func (f *fakeHistory) ExportRequests(_ context.Context, q telemetry.LogFilter, max int, fn func(*telemetry.RequestRecord) error) error {
	f.exported, f.exportMax = q, max
	for i := range f.recs {
		if err := fn(&f.recs[i]); err != nil {
			return err
		}
	}
	return nil
}

func adminGet(h *Handler, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/admin/api"+path, nil)
	w := httptest.NewRecorder()
	u := r.URL.Path[len("/admin/api"):]
	if !h.routeTelemetryAdmin(w, r, u) {
		w.WriteHeader(http.StatusTeapot)
	}
	return w
}

func TestCSVSafe(t *testing.T) {
	cases := map[string]string{
		"":                  "",
		"plain":             "plain",
		"=HYPERLINK(\"x\")": "'=HYPERLINK(\"x\")",
		"+1":                "'+1",
		"-2+3":              "'-2+3",
		"@SUM(A1)":          "'@SUM(A1)",
		"\tcmd":             "'\tcmd",
		"\rcmd":             "'\rcmd",
		"a=b":               "a=b",
		"claude-sonnet-4.5": "claude-sonnet-4.5",
	}
	for in, want := range cases {
		if got := csvSafe(in); got != want {
			t.Errorf("csvSafe(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRequestExportCSV(t *testing.T) {
	mustInitConfig(t)
	k, err := config.AddApiKey(config.ApiKeyEntry{Name: "=evil", Key: "sk-csv", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	fh := &fakeHistory{recs: []telemetry.RequestRecord{{
		ID: "req_1", Time: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Status: "error", HTTPStatus: 500, Model: "m",
		APIKeyID: k.ID, Error: "line one\nsaid \"no\", twice", UserAgent: "@agent", InputTokens: 3, Credits: 0.25,
		ConversationID: "sess_1", RequestBody: "secret body",
	}}}
	h := &Handler{logReader: fh}
	w := adminGet(h, "/requests/export.csv?status=error&model=m&apiKey="+k.ID+"&q=x&hours=24")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("status %d headers %v", w.Code, w.Header())
	}
	if fh.exportMax != requestExportCap || fh.exported.Status != "error" || fh.exported.Model != "m" || fh.exported.APIKeyID != k.ID ||
		fh.exported.Search != "x" || fh.exported.Since.IsZero() {
		t.Fatalf("filters not passed: %+v max=%d", fh.exported, fh.exportMax)
	}
	if strings.Contains(w.Body.String(), "secret body") {
		t.Fatal("bodies must not be exported")
	}
	rows, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	if err != nil || len(rows) != 2 {
		t.Fatalf("csv parse: %v rows=%d\n%s", err, len(rows), w.Body.String())
	}
	col := map[string]string{}
	for i, name := range rows[0] {
		col[name] = rows[1][i]
	}
	if col["error"] != "line one\nsaid \"no\", twice" || col["user_agent"] != "'@agent" || col["api_key_name"] != "'=evil" ||
		col["credits"] != "0.25" || col["conversation_id"] != "sess_1" || col["time"] != "2026-01-02T03:04:05Z" {
		t.Fatalf("row = %v", col)
	}
}

func TestConversationEndpoints(t *testing.T) {
	now := time.Now()
	fh := &fakeHistory{
		convs: []telemetry.ConversationSummary{{ID: "sess_a", Requests: 2}, {ID: "sess_b", Requests: 1}},
		recs: []telemetry.RequestRecord{
			{ID: "r1", Time: now.Add(-time.Minute), ConversationID: "sess_a"},
			{ID: "r2", Time: now, ConversationID: "sess_a"},
		},
	}
	h := &Handler{logReader: fh}

	w := adminGet(h, "/conversations?hours=24&q=opus&limit=1&before=sess_z")
	var list struct {
		Conversations []telemetry.ConversationSummary `json:"conversations"`
		HasMore       bool                            `json:"hasMore"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if len(list.Conversations) != 1 || !list.HasMore || fh.convQuery.Limit != 2 || fh.convQuery.Search != "opus" ||
		fh.convQuery.BeforeID != "sess_z" || fh.convQuery.Since.IsZero() {
		t.Fatalf("list = %+v query = %+v", list, fh.convQuery)
	}

	w = adminGet(h, "/conversations/sess_a")
	var one struct {
		ID       string                    `json:"id"`
		Requests []telemetry.RequestRecord `json:"requests"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &one); err != nil || w.Code != 200 || one.ID != "sess_a" || len(one.Requests) != 2 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w = adminGet(h, "/conversations/sess_missing"); w.Code != http.StatusNotFound {
		t.Fatalf("missing conversation: %d", w.Code)
	}

	// Without a database the endpoints say so.
	if w = adminGet(&Handler{}, "/conversations"); w.Code != http.StatusNotImplemented {
		t.Fatalf("no database: %d", w.Code)
	}
}

func TestTraceRecordsConversationID(t *testing.T) {
	withFakeProtocols(t, func(string, *KiroStreamCallback) error { return nil })
	tr := &requestTrace{}
	ctx := context.WithValue(context.Background(), traceKey{}, tr)
	p := &KiroPayload{}
	p.ConversationState.ConversationID = "sess_trace"
	if err := CallKiroAPIContext(ctx, &config.Account{ID: "acc-conv", RefreshToken: "r"}, p, &KiroStreamCallback{}); err != nil {
		t.Fatal(err)
	}
	if tr.conversationID != "sess_trace" {
		t.Fatalf("conversation id = %q", tr.conversationID)
	}
}
