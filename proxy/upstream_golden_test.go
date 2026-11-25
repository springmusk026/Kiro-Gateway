package proxy

import (
	"bytes"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/springmusk026/Kiro-Gateway/config"
)

// The bytes sent to Kiro must not change when the request path is optimised:
// these golden files hold the serialized upstream body for a fixed, large
// request on both transports and both client APIs. Regenerate only for an
// intended wire change:
//
//	go test ./proxy/ -run TestUpstreamPayloadGolden -update-golden
var updateGolden = flag.Bool("update-golden", false, "rewrite the upstream payload golden files")

// captureTransport records request bodies and answers every call with a
// client error, so exactly one upstream request is made.
type captureTransport struct {
	mu     sync.Mutex
	bodies [][]byte
}

func (c *captureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.bodies = append(c.bodies, body)
	c.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"message":"golden capture","reason":"INVALID_INPUT"}`)),
		Request:    r,
	}, nil
}

// agentContinuationID matches the one per-request random id in the body.
var agentContinuationID = regexp.MustCompile(`("agentContinuationId":"|"kiro:agent_continuation_id":")[0-9a-f-]{36}`)

func captureUpstreamBody(t *testing.T, protocol, path string, body []byte) []byte {
	t.Helper()
	h, cleanup := setupResponsesTestHandler(t)
	defer cleanup()
	if err := config.UpdateUpstreamProtocol(protocol); err != nil {
		t.Fatal(err)
	}
	ct := &captureTransport{}
	old := kiroHttpStore.Load()
	kiroHttpStore.Store(&http.Client{Transport: ct})
	defer kiroHttpStore.Store(old)

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(httptest.NewRecorder(), req)

	ct.mu.Lock()
	defer ct.mu.Unlock()
	if len(ct.bodies) != 1 {
		t.Fatalf("upstream requests = %d, want 1", len(ct.bodies))
	}
	return agentContinuationID.ReplaceAll(ct.bodies[0], []byte("${1}00000000-0000-4000-8000-000000000000"))
}

func TestUpstreamPayloadGolden(t *testing.T) {
	cases := []struct {
		name, protocol, path string
		body                 []byte
	}{
		{"anthropic_legacy", "legacy", "/v1/messages", perfClaudeRequestJSON(false)},
		{"anthropic_responses", "responses", "/v1/messages", perfClaudeRequestJSON(false)},
		{"openai_legacy", "legacy", "/v1/chat/completions", perfOpenAIRequestJSON(false)},
		{"openai_responses", "responses", "/v1/chat/completions", perfOpenAIRequestJSON(false)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := captureUpstreamBody(t, tc.protocol, tc.path, tc.body)
			file := filepath.Join("testdata", "upstream_golden", tc.name+".json")
			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read golden (run with -update-golden to create): %v", err)
			}
			if !bytes.Equal(got, want) {
				i := 0
				for i < len(got) && i < len(want) && got[i] == want[i] {
					i++
				}
				lo := max(0, i-80)
				t.Fatalf("upstream payload changed at byte %d (got %d bytes, want %d)\n got: %s\nwant: %s",
					i, len(got), len(want), got[lo:min(len(got), i+80)], want[lo:min(len(want), i+80)])
			}
		})
	}
}
