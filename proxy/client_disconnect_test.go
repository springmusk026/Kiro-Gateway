package proxy

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A client that goes away mid-stream must (1) cancel the upstream call, (2) not
// be held against the account, and (3) be recorded as cancelled, not success.
func TestClientDisconnectCancelsUpstreamAndIsRecordedAsCancelled(t *testing.T) {
	upstreamCancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(awsEventStreamFrame(t, "assistantResponseEvent", map[string]interface{}{"content": "partial answer " + strings.Repeat("streamed text ", 40)}))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			close(upstreamCancelled)
		case <-time.After(5 * time.Second):
		}
	}))
	defer upstream.Close()
	h := setupIntegrityPathTest(t, upstream)
	sink := &memSink{}
	h.sink = sink
	before := h.pool.AvailableCount()

	gateway := httptest.NewServer(h)
	defer gateway.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, gateway.URL+"/v1/messages", strings.NewReader(`{
		"model":"claude-sonnet-4.5","max_tokens":100,"stream":true,
		"messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Transport: &http.Transport{}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	// Read until the partial text arrives, then hang up.
	br := bufio.NewReader(resp.Body)
	var got strings.Builder
	for {
		line, err := br.ReadString('\n')
		got.WriteString(line)
		if strings.Contains(line, "partial answer") {
			break
		}
		if err == io.EOF || err != nil {
			t.Fatalf("stream ended before content (status %d): %v\n%s", resp.StatusCode, err, got.String())
		}
	}
	hangUp := time.Now()
	cancel()
	resp.Body.Close()

	select {
	case <-upstreamCancelled:
		if d := time.Since(hangUp); d > 500*time.Millisecond {
			t.Fatalf("upstream was cancelled %s after the client left; expected promptly", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client disconnect did not cancel the upstream request")
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		sink.mu.Lock()
		n := len(sink.reqs)
		sink.mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.reqs) != 1 {
		t.Fatalf("records = %d", len(sink.reqs))
	}
	rec := sink.reqs[0]
	if rec.Status != "cancelled" || rec.ErrorType != "client_closed" || rec.HTTPStatus != 499 {
		t.Fatalf("record status=%q type=%q http=%d, want cancelled/client_closed/499", rec.Status, rec.ErrorType, rec.HTTPStatus)
	}
	if after := h.pool.AvailableCount(); after != before {
		t.Fatalf("account availability changed after a client disconnect: %d -> %d", before, after)
	}
}

// A request that finished normally stays a success even if the context is
// cancelled afterwards (e.g. the client closes right after the last event).
func TestCompletedRequestStaysSuccessAfterCancel(t *testing.T) {
	sink := &memSink{}
	h := &Handler{sink: sink}
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`)).WithContext(ctx)
	w, r, finish := h.beginTrace(httptest.NewRecorder(), req, "claude")
	traceFrom(r.Context()).setSuccess("m", "a")
	w.WriteHeader(200)
	cancel()
	finish()
	if got := sink.reqs[0].Status; got != "success" {
		t.Fatalf("status = %q", got)
	}
}
