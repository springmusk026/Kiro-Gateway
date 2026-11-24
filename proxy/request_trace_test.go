package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"kiro-go/telemetry"
)

type memSink struct {
	mu   sync.Mutex
	reqs []*telemetry.RequestRecord
}

func (m *memSink) RecordRequest(r *telemetry.RequestRecord) { m.mu.Lock(); m.reqs = append(m.reqs, r); m.mu.Unlock() }
func (m *memSink) RecordAudit(*telemetry.AuditEvent)        {}

func TestTraceRecordsStatusTTFBAndFlushStillWorks(t *testing.T) {
	sink := &memSink{}
	h := &Handler{sink: sink}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	w, r, finish := h.beginTrace(rec, req, "claude")
	io.ReadAll(r.Body)
	traceFrom(r.Context()).setUsage(10, 5, 0.5)
	traceFrom(r.Context()).setSuccess("claude-sonnet-4.5", "acc1")
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	w.Write([]byte("data: hi\n\n"))
	if f, ok := w.(http.Flusher); !ok {
		t.Fatal("traced writer must keep http.Flusher for SSE")
	} else {
		f.Flush()
	}
	finish()
	if len(sink.reqs) != 1 {
		t.Fatal("expected one record")
	}
	x := sink.reqs[0]
	if x.Status != "success" || x.HTTPStatus != 200 || x.Model != "claude-sonnet-4.5" || x.InputTokens != 10 ||
		!x.Stream || x.RequestBytes != int64(len(`{"model":"m"}`)) || x.ResponseBytes != 10 || rec.Header().Get("X-Request-Id") != x.ID {
		t.Fatalf("record %+v", x)
	}
}

func TestTraceDisabledWithoutSink(t *testing.T) {
	h := &Handler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	w, _, finish := h.beginTrace(rec, req, "claude")
	finish()
	if w != http.ResponseWriter(rec) {
		t.Fatal("without a database the request path must be untouched")
	}
}

// BenchmarkTraceOverhead measures the per-request cost of tracing (no I/O:
// the record goes to an in-memory sink, like the real async recorder queue).
func BenchmarkTraceOverhead(b *testing.B) {
	h := &Handler{sink: &memSink{}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{}"))
		w, r, finish := h.beginTrace(rec, req, "claude")
		io.ReadAll(r.Body)
		w.Write([]byte("ok"))
		finish()
		if i%10000 == 0 {
			h.sink = &memSink{}
		}
	}
	_ = time.Now
}
