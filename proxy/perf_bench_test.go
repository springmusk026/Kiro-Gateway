package proxy

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/logger"
	accountpool "github.com/springmusk026/Kiro-Gateway/pool"
)

// Request-path benchmarks. See docs/development.md ("Performance"):
//
//	go test ./proxy/ -run '^$' -bench . -benchmem

func perfEventFrame(eventType string, payload interface{}) []byte {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	name, value := ":event-type", eventType
	headerBytes := []byte{byte(len(name))}
	headerBytes = append(headerBytes, name...)
	headerBytes = append(headerBytes, 7, byte(len(value)>>8), byte(len(value)))
	headerBytes = append(headerBytes, value...)
	total := 12 + len(headerBytes) + len(payloadBytes) + 4
	frame := make([]byte, 12, total)
	binary.BigEndian.PutUint32(frame[0:4], uint32(total))
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(headerBytes)))
	binary.BigEndian.PutUint32(frame[8:12], crc32.ChecksumIEEE(frame[:8]))
	frame = append(frame, headerBytes...)
	frame = append(frame, payloadBytes...)
	frame = append(frame, 0, 0, 0, 0)
	binary.BigEndian.PutUint32(frame[len(frame)-4:], crc32.ChecksumIEEE(frame[:len(frame)-4]))
	return frame
}

// perfUpstreamStream is a typical answer: 400 short text chunks, one tool
// call, then usage and the stop reason.
func perfUpstreamStream() []byte {
	var b bytes.Buffer
	for i := 0; i < 400; i++ {
		b.Write(perfEventFrame("assistantResponseEvent", map[string]interface{}{"content": fmt.Sprintf("word%d and some more text ", i)}))
	}
	b.Write(perfEventFrame("toolUseEvent", map[string]interface{}{"name": "Tool01", "toolUseId": "tooluse_1", "input": `{"path":"/repo/a.go",`}))
	b.Write(perfEventFrame("toolUseEvent", map[string]interface{}{"name": "Tool01", "toolUseId": "tooluse_1", "input": `"mode":"read"}`, "stop": true}))
	b.Write(perfEventFrame("contextUsageEvent", map[string]interface{}{"contextUsagePercentage": 12.5}))
	b.Write(perfEventFrame("meteringEvent", map[string]interface{}{"unit": "credit", "usage": 0.42}))
	b.Write(perfEventFrame("metadataEvent", map[string]interface{}{"stopReason": "tool_use"}))
	return b.Bytes()
}

// replayTransport answers every upstream call with the same body.
type replayTransport struct{ body []byte }

func (t replayTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	_, _ = io.Copy(io.Discard, r.Body)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/vnd.amazon.eventstream"}},
		Body:       io.NopCloser(bytes.NewReader(t.body)),
		Request:    r,
	}, nil
}

func setupPerfHandler(b *testing.B, upstream []byte) *Handler {
	b.Helper()
	prevLevel := logger.GetLevel()
	logger.SetLevel(logger.LevelWarn)
	b.Cleanup(func() { logger.SetLevel(prevLevel) })
	if err := config.InitForTest(b, b.TempDir()+"/config.json"); err != nil {
		b.Fatal(err)
	}
	if err := config.AddAccount(config.Account{ID: "bench-account", Enabled: true, AccessToken: "token",
		ProfileArn: "arn:aws:codewhisperer:us-east-1:000000000000:profile/bench"}); err != nil {
		b.Fatal(err)
	}
	if err := config.UpdateUpstreamProtocol("legacy"); err != nil {
		b.Fatal(err)
	}
	if err := config.UpdatePreferredEndpoint("kiro"); err != nil {
		b.Fatal(err)
	}
	if err := config.UpdateEndpointFallback(false); err != nil {
		b.Fatal(err)
	}
	old := kiroHttpStore.Load()
	kiroHttpStore.Store(&http.Client{Transport: replayTransport{body: upstream}})
	b.Cleanup(func() { kiroHttpStore.Store(old) })
	h := &Handler{promptCache: newPromptCacheTracker(defaultPromptCacheTTL)}
	h.pool = accountpool.GetPool()
	h.pool.Reload()
	return h
}

func BenchmarkClaudeToKiro(b *testing.B) {
	raw := perfClaudeRequestJSON(false)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		var req ClaudeRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		_ = ClaudeToKiro(&req, false)
	}
}

func BenchmarkOpenAIToKiro(b *testing.B) {
	raw := perfOpenAIRequestJSON(false)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		var req OpenAIRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		_ = OpenAIToKiro(&req, false)
	}
}

func BenchmarkEstimateClaudeInputTokens(b *testing.B) {
	req := perfClaudeRequest()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = estimateClaudeRequestInputTokens(req)
	}
}

func BenchmarkEstimateOpenAIInputTokens(b *testing.B) {
	req := perfOpenAIRequest()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = estimateOpenAIRequestInputTokens(req)
	}
}

func BenchmarkPromptCacheProfile(b *testing.B) {
	req := perfClaudeRequest()
	tracker := newPromptCacheTracker(defaultPromptCacheTTL)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tracker.BuildClaudeProfile(req, 30000)
	}
}

// BenchmarkUpstreamBody is the serialization of the translated payload, as
// sent on the legacy transport.
func BenchmarkUpstreamBody(b *testing.B) {
	payload := ClaudeToKiro(perfClaudeRequest(), false)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(withCachePoints(payload)); err != nil {
			b.Fatal(err)
		}
	}
}

// benchServe runs one full request through the handler against the replayed
// upstream: request preparation, translation, upstream body, stream parse
// and the client response.
func benchServe(b *testing.B, path string, body []byte) {
	h := setupPerfHandler(b, perfUpstreamStream())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "word399") {
			b.Fatalf("incomplete response: %s", rec.Body.String())
		}
	}
}

func BenchmarkServeClaudeStream(b *testing.B) {
	benchServe(b, "/v1/messages", perfClaudeRequestJSON(true))
}

func BenchmarkServeClaudeNonStream(b *testing.B) {
	benchServe(b, "/v1/messages", perfClaudeRequestJSON(false))
}

func BenchmarkServeOpenAIStream(b *testing.B) {
	benchServe(b, "/v1/chat/completions", perfOpenAIRequestJSON(true))
}

// BenchmarkStreamParseToSSE isolates the streaming path: event stream in,
// Anthropic SSE out, with a tiny request so translation does not dominate.
func BenchmarkStreamParseToSSE(b *testing.B) {
	body := []byte(`{"model":"claude-sonnet-4.5","max_tokens":1024,"stream":true,"messages":[{"role":"user","content":"hello there"}]}`)
	benchServe(b, "/v1/messages", body)
}
