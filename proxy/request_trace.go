package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"kiro-go/config"
	"kiro-go/telemetry"
)

// requestTrace accumulates what the gateway learns about one client request.
// Handlers fill it through the existing record helpers; it is turned into a
// telemetry.RequestRecord after the response has been written, so recording
// adds no latency to inference.
type requestTrace struct {
	mu       sync.Mutex
	id       string
	start    time.Time
	method   string
	path     string
	endpoint string
	clientIP string
	agent    string
	stream   bool
	protocol string
	apiKeyID string

	model, accountID, errMsg, errType, status string
	input, output                             int
	credits                                   float64

	reqBytes int64
	reqBody  *cappedBuffer
	respBody *cappedBuffer
}

type traceKey struct{}

func traceFrom(ctx context.Context) *requestTrace {
	if ctx == nil {
		return nil
	}
	t, _ := ctx.Value(traceKey{}).(*requestTrace)
	return t
}

func (t *requestTrace) setUsage(in, out int, credits float64) {
	t.mu.Lock()
	t.input, t.output, t.credits = in, out, credits
	t.mu.Unlock()
}

func (t *requestTrace) setSuccess(model, accountID string) {
	t.mu.Lock()
	t.model, t.accountID, t.status = model, accountID, "success"
	t.errMsg, t.errType = "", ""
	t.mu.Unlock()
}

func (t *requestTrace) setFailure(model, accountID, msg, typ string) {
	t.mu.Lock()
	if t.status != "success" {
		t.model, t.errMsg, t.errType, t.status = model, msg, typ, "error"
		if accountID != "" {
			t.accountID = accountID
		}
	}
	t.mu.Unlock()
}

func (t *requestTrace) setProtocol(p string) {
	t.mu.Lock()
	t.protocol = p
	t.mu.Unlock()
}

func newRequestID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "req_" + hex.EncodeToString(b)
}

// endpointForPath names the API surface for a request path ("" = not traced).
func endpointForPath(path string) string {
	switch {
	case path == "/v1/messages" || path == "/messages" || path == "/anthropic/v1/messages" || path == "/admin/api/playground/messages":
		return "claude"
	case path == "/v1/chat/completions" || path == "/chat/completions" || path == "/admin/api/playground/chat/completions":
		return "openai"
	case path == "/v1/responses" || path == "/responses":
		return "responses"
	case path == "/v1/messages/count_tokens" || path == "/messages/count_tokens":
		return "count_tokens"
	case path == "/v1/models" || path == "/models":
		return "models"
	}
	return ""
}

// bodyCaptureLimit returns the max bytes of request/response body to record
// (0 = disabled). Off by default: bodies are large and may hold sensitive data.
func bodyCaptureLimit() int {
	if v := strings.TrimSpace(os.Getenv("KIRO_LOG_BODIES_KB")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n << 10
		}
	}
	if kb := config.GetLogBodiesKB(); kb > 0 {
		return kb << 10
	}
	return 0
}

// cappedBuffer keeps the first n bytes written to it.
type cappedBuffer struct {
	buf bytes.Buffer
	n   int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.n - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil
}

// countingReader counts request body bytes and optionally tees a prefix.
type countingReader struct {
	io.ReadCloser
	n   *int64
	tee io.Writer
}

func (c countingReader) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	if n > 0 {
		atomic.AddInt64(c.n, int64(n))
		if c.tee != nil {
			c.tee.Write(p[:n])
		}
	}
	return n, err
}

// tracingWriter records status, time to first byte and response size. It
// forwards Flush (SSE streaming) and exposes Unwrap for http.ResponseController.
type tracingWriter struct {
	http.ResponseWriter
	status    int
	firstByte time.Time
	bytes     int64
	tee       io.Writer
}

func (w *tracingWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *tracingWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.firstByte.IsZero() {
		w.firstByte = time.Now()
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	if w.tee != nil && n > 0 {
		w.tee.Write(p[:n])
	}
	return n, err
}

func (w *tracingWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *tracingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// beginTrace instruments an API request. It returns the wrapped writer and
// request plus a finish function to call after the handler returns.
func (h *Handler) beginTrace(w http.ResponseWriter, r *http.Request, endpoint string) (http.ResponseWriter, *http.Request, func()) {
	if h.sink == nil {
		return w, r, func() {}
	}
	t := &requestTrace{
		id:       newRequestID(),
		start:    time.Now(),
		method:   r.Method,
		path:     r.URL.Path,
		endpoint: endpoint,
		clientIP: clientIP(r),
		agent:    r.UserAgent(),
		stream:   strings.Contains(r.Header.Get("Accept"), "text/event-stream"),
	}
	tw := &tracingWriter{ResponseWriter: w}
	if limit := bodyCaptureLimit(); limit > 0 && (endpoint == "claude" || endpoint == "openai" || endpoint == "responses") {
		t.reqBody = &cappedBuffer{n: limit}
		t.respBody = &cappedBuffer{n: limit}
		tw.tee = t.respBody
	}
	if r.Body != nil {
		cr := countingReader{ReadCloser: r.Body, n: &t.reqBytes}
		if t.reqBody != nil {
			cr.tee = t.reqBody
		}
		r.Body = cr
	}
	w.Header().Set("X-Request-Id", t.id)
	r = r.WithContext(context.WithValue(r.Context(), traceKey{}, t))
	return tw, r, func() { h.finishTrace(t, tw, r) }
}

func (h *Handler) finishTrace(t *requestTrace, tw *tracingWriter, r *http.Request) {
	end := time.Now()
	t.mu.Lock()
	rec := &telemetry.RequestRecord{
		ID:            t.id,
		Time:          t.start,
		Method:        t.method,
		Path:          t.path,
		Endpoint:      t.endpoint,
		Model:         t.model,
		AccountID:     t.accountID,
		APIKeyID:      t.apiKeyID,
		ClientIP:      t.clientIP,
		UserAgent:     t.agent,
		Stream:        t.stream || strings.HasPrefix(tw.Header().Get("Content-Type"), "text/event-stream"),
		Protocol:      t.protocol,
		Status:        t.status,
		HTTPStatus:    tw.status,
		Error:         t.errMsg,
		ErrorType:     t.errType,
		InputTokens:   t.input,
		OutputTokens:  t.output,
		Credits:       t.credits,
		DurationMs:    end.Sub(t.start).Milliseconds(),
		RequestBytes:  atomic.LoadInt64(&t.reqBytes),
		ResponseBytes: tw.bytes,
	}
	if t.reqBody != nil {
		rec.RequestBody = t.reqBody.buf.String()
		rec.ResponseBody = t.respBody.buf.String()
	}
	t.mu.Unlock()
	if !tw.firstByte.IsZero() {
		rec.TTFBMs = tw.firstByte.Sub(t.start).Milliseconds()
	}
	if rec.Status == "" {
		if rec.HTTPStatus >= 400 {
			rec.Status = "error"
		} else {
			rec.Status = "success"
		}
	}
	rec.Attempts = attemptsUsed(r.Context())
	if rec.AccountID != "" && h.pool != nil {
		if acc := h.pool.GetByID(rec.AccountID); acc != nil {
			rec.AccountEmail = acc.Email
		}
	}
	h.sink.RecordRequest(rec)
}

// audit records an admin or security event (no-op without a sink).
func (h *Handler) audit(r *http.Request, action, target string, detail map[string]string) {
	if h.sink == nil {
		return
	}
	ip := ""
	if r != nil {
		ip = clientIP(r)
	}
	h.sink.RecordAudit(&telemetry.AuditEvent{Time: time.Now(), Actor: "admin", IP: ip, Action: action, Target: target, Detail: detail})
}
