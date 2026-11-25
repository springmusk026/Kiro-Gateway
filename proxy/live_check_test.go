package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLiveGateway is the nightly live check (.github/workflows/live-check.yml).
// It builds the gateway, starts it with one Kiro API-key account added through
// the admin API, and sends one minimal request to each client API with the
// cheapest model on offer, checking the response shapes end to end.
//
// It is skipped unless KIRO_LIVE_API_KEY (a Kiro API key, ksk_...) is set, so
// `go test ./...` stays hermetic. It costs a handful of tiny requests:
//
//	KIRO_LIVE_API_KEY=ksk_... [KIRO_LIVE_REGION=us-east-1] go test ./proxy -run TestLiveGateway -v
//
// Optional: KIRO_LIVE_GATEWAY_BIN uses an already built binary,
// KIRO_LIVE_PROXY_URL sets the gateway's outbound proxy, KIRO_LIVE_MODEL
// forces the model.
func TestLiveGateway(t *testing.T) {
	apiKey := strings.TrimSpace(os.Getenv("KIRO_LIVE_API_KEY"))
	if apiKey == "" {
		t.Skip("set KIRO_LIVE_API_KEY to run the live check against Kiro")
	}
	g := startLiveGateway(t, apiKey)

	// Add the account the way an operator would: the credentials import route.
	creds := map[string]string{"kiroApiKey": apiKey, "nickname": "live-check"}
	if region := strings.TrimSpace(os.Getenv("KIRO_LIVE_REGION")); region != "" {
		creds["region"] = region
	}
	var imported struct {
		Success bool `json:"success"`
		Account struct {
			ID string `json:"id"`
		} `json:"account"`
	}
	g.adminJSON(t, http.MethodPost, "/admin/api/auth/credentials", creds, &imported)
	if !imported.Success || imported.Account.ID == "" {
		t.Fatalf("account import did not succeed: %+v", imported)
	}

	model := strings.TrimSpace(os.Getenv("KIRO_LIVE_MODEL"))
	if model == "" {
		model = g.cheapestModel(t)
	}
	t.Logf("model: %s", model)
	prompt := "Reply with the single word: pong"

	t.Run("anthropic messages", func(t *testing.T) {
		var resp struct {
			Type       string `json:"type"`
			Role       string `json:"role"`
			StopReason string `json:"stop_reason"`
			Content    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		g.postJSON(t, "/v1/messages", map[string]any{
			"model": model, "max_tokens": 16,
			"messages": []map[string]any{{"role": "user", "content": prompt}},
		}, &resp)
		if resp.Type != "message" || resp.Role != "assistant" {
			t.Errorf("type/role = %q/%q, want message/assistant", resp.Type, resp.Role)
		}
		if text := anthropicText(resp.Content); text == "" {
			t.Errorf("no text content: %+v", resp.Content)
		}
		if resp.StopReason == "" {
			t.Error("stop_reason missing")
		}
		if resp.Usage.InputTokens <= 0 || resp.Usage.OutputTokens <= 0 {
			t.Errorf("usage missing: %+v", resp.Usage)
		}
	})

	t.Run("anthropic messages stream", func(t *testing.T) {
		events := g.postSSE(t, "/v1/messages", map[string]any{
			"model": model, "max_tokens": 16, "stream": true,
			"messages": []map[string]any{{"role": "user", "content": prompt}},
		})
		var names []string
		var text strings.Builder
		var outputTokens float64
		for _, ev := range events {
			if ev.name == "ping" {
				continue
			}
			names = append(names, ev.name)
			var data map[string]any
			if err := json.Unmarshal([]byte(ev.data), &data); err != nil {
				t.Fatalf("event %s: invalid JSON %q", ev.name, ev.data)
			}
			if typ, _ := data["type"].(string); typ != ev.name {
				t.Errorf("event %s carries type %q", ev.name, typ)
			}
			switch ev.name {
			case "content_block_delta":
				if d, ok := data["delta"].(map[string]any); ok && d["type"] == "text_delta" {
					s, _ := d["text"].(string)
					text.WriteString(s)
				}
			case "message_delta":
				if u, ok := data["usage"].(map[string]any); ok {
					outputTokens, _ = u["output_tokens"].(float64)
				}
			case "error":
				t.Fatalf("error event: %s", ev.data)
			}
		}
		if err := checkAnthropicEventOrder(names); err != nil {
			t.Errorf("%v\nevents: %v", err, names)
		}
		if strings.TrimSpace(text.String()) == "" {
			t.Error("no text deltas")
		}
		if outputTokens <= 0 {
			t.Error("message_delta has no output_tokens usage")
		}
	})

	t.Run("openai chat completions", func(t *testing.T) {
		var resp struct {
			Object  string `json:"object"`
			Choices []struct {
				FinishReason string `json:"finish_reason"`
				Message      struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		g.postJSON(t, "/v1/chat/completions", map[string]any{
			"model": model, "max_tokens": 16,
			"messages": []map[string]any{{"role": "user", "content": prompt}},
		}, &resp)
		if resp.Object != "chat.completion" {
			t.Errorf("object = %q", resp.Object)
		}
		if len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
			t.Fatalf("no message content: %+v", resp.Choices)
		}
		if resp.Choices[0].Message.Role != "assistant" || resp.Choices[0].FinishReason == "" {
			t.Errorf("role/finish_reason = %q/%q", resp.Choices[0].Message.Role, resp.Choices[0].FinishReason)
		}
		if resp.Usage.PromptTokens <= 0 || resp.Usage.CompletionTokens <= 0 || resp.Usage.TotalTokens < resp.Usage.PromptTokens+resp.Usage.CompletionTokens {
			t.Errorf("usage: %+v", resp.Usage)
		}
	})

	t.Run("openai responses", func(t *testing.T) {
		var resp struct {
			Object string `json:"object"`
			Status string `json:"status"`
			Output []struct {
				Type    string `json:"type"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"output"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
				TotalTokens  int `json:"total_tokens"`
			} `json:"usage"`
		}
		g.postJSON(t, "/v1/responses", map[string]any{
			"model": model, "max_output_tokens": 16, "store": false, "input": prompt,
		}, &resp)
		if resp.Object != "response" || resp.Status != "completed" {
			t.Errorf("object/status = %q/%q", resp.Object, resp.Status)
		}
		var text string
		for _, o := range resp.Output {
			if o.Type != "message" {
				continue
			}
			for _, c := range o.Content {
				if c.Type == "output_text" {
					text += c.Text
				}
			}
		}
		if strings.TrimSpace(text) == "" {
			t.Errorf("no output_text: %+v", resp.Output)
		}
		if resp.Usage.InputTokens <= 0 || resp.Usage.OutputTokens <= 0 {
			t.Errorf("usage: %+v", resp.Usage)
		}
	})
}

// checkAnthropicEventOrder validates the Messages API stream grammar:
// message_start (content_block_start content_block_delta* content_block_stop)+
// message_delta message_stop.
func checkAnthropicEventOrder(names []string) error {
	if len(names) < 5 {
		return fmt.Errorf("only %d events", len(names))
	}
	if names[0] != "message_start" {
		return fmt.Errorf("first event is %s, want message_start", names[0])
	}
	n := len(names)
	if names[n-2] != "message_delta" || names[n-1] != "message_stop" {
		return fmt.Errorf("stream ends with %s, %s; want message_delta, message_stop", names[n-2], names[n-1])
	}
	open, blocks := false, 0
	for i, name := range names[1 : n-2] {
		switch name {
		case "content_block_start":
			if open {
				return fmt.Errorf("event %d: content_block_start inside an open block", i+1)
			}
			open = true
			blocks++
		case "content_block_delta":
			if !open {
				return fmt.Errorf("event %d: content_block_delta outside a block", i+1)
			}
		case "content_block_stop":
			if !open {
				return fmt.Errorf("event %d: content_block_stop without a start", i+1)
			}
			open = false
		default:
			return fmt.Errorf("event %d: unexpected %s", i+1, name)
		}
	}
	if open || blocks == 0 {
		return fmt.Errorf("blocks: %d, last one open: %v", blocks, open)
	}
	return nil
}

func TestCheckAnthropicEventOrder(t *testing.T) {
	ok := []string{"message_start", "content_block_start", "content_block_delta", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	if err := checkAnthropicEventOrder(ok); err != nil {
		t.Fatalf("valid stream rejected: %v", err)
	}
	bad := [][]string{
		{"content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"},
		{"message_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"},
		{"message_start", "content_block_start", "content_block_delta", "message_delta", "message_stop"},
		{"message_start", "content_block_start", "content_block_stop", "message_stop", "message_delta"},
		{"message_start", "content_block_start", "content_block_start", "content_block_stop", "message_delta", "message_stop"},
	}
	for _, b := range bad {
		if checkAnthropicEventOrder(b) == nil {
			t.Errorf("invalid stream accepted: %v", b)
		}
	}
}

func anthropicText(blocks []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return strings.TrimSpace(sb.String())
}

// liveGateway is a gateway process started for the live check.
type liveGateway struct {
	url      string
	password string
	secret   string // the Kiro API key, masked in everything this test prints
	client   *http.Client
}

// mask hides the Kiro API key in text that may be printed.
func (g *liveGateway) mask(s string) string {
	if g.secret == "" {
		return s
	}
	return strings.ReplaceAll(s, g.secret, maskSecret(g.secret))
}

func maskSecret(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "…" + s[len(s)-2:]
}

func startLiveGateway(t *testing.T, apiKey string) *liveGateway {
	t.Helper()
	dir := t.TempDir()
	bin := os.Getenv("KIRO_LIVE_GATEWAY_BIN")
	if bin == "" {
		bin = filepath.Join(dir, "kiro-gateway")
		build := exec.Command("go", "build", "-o", bin, ".")
		build.Dir = ".."
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("go build: %v\n%s", err, out)
		}
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	settings := map[string]any{"port": port, "host": "127.0.0.1"}
	if p := os.Getenv("KIRO_LIVE_PROXY_URL"); p != "" {
		settings["proxyURL"] = p
	}
	cfg, _ := json.Marshal(settings)
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	pw := make([]byte, 16)
	rand.Read(pw)
	g := &liveGateway{
		url:      fmt.Sprintf("http://127.0.0.1:%d", port),
		password: hex.EncodeToString(pw),
		secret:   apiKey,
		client:   &http.Client{Timeout: 2 * time.Minute},
	}

	// A clean environment: production defaults (not this package's TestMain
	// overrides), no database, JSON config in the temp dir.
	var env []string
	for _, kv := range os.Environ() {
		k := kv[:strings.IndexByte(kv, '=')]
		if strings.HasPrefix(k, "KIRO_") || k == "DATABASE_URL" || k == "ADMIN_PASSWORD" || k == "CONFIG_PATH" || k == "LOG_LEVEL" {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "CONFIG_PATH="+cfgPath, "ADMIN_PASSWORD="+g.password, "LOG_LEVEL=info")

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin)
	cmd.Dir = ".." // serves ./web; not needed here, but matches production
	cmd.Env = env
	var logs syncBuffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("gateway log (key masked):\n%s", g.mask(logs.String()))
		}
	})

	deadline := time.Now().Add(30 * time.Second)
	for {
		res, err := http.Get(g.url + "/health")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return g
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("gateway did not start: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (g *liveGateway) do(t *testing.T, method, path string, body any, header map[string]string) (int, http.Header, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, g.url+path, rd)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := g.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %s", method, path, g.mask(err.Error()))
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, data
}

func (g *liveGateway) adminJSON(t *testing.T, method, path string, body, out any) {
	t.Helper()
	status, _, data := g.do(t, method, path, body, map[string]string{"X-Admin-Password": g.password, "X-Requested-With": "live-check"})
	if status != http.StatusOK {
		t.Fatalf("%s %s: HTTP %d: %s", method, path, status, g.mask(string(data)))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("%s %s: %v: %s", method, path, err, g.mask(string(data)))
		}
	}
}

func (g *liveGateway) postJSON(t *testing.T, path string, body, out any) {
	t.Helper()
	status, h, data := g.do(t, http.MethodPost, path, body, nil)
	if status != http.StatusOK {
		t.Fatalf("POST %s: HTTP %d: %s", path, status, g.mask(string(data)))
	}
	if ct := h.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("POST %s: %v: %s", path, err, g.mask(string(data)))
	}
}

type liveSSEEvent struct{ name, data string }

func (g *liveGateway) postSSE(t *testing.T, path string, body any) []liveSSEEvent {
	t.Helper()
	status, h, data := g.do(t, http.MethodPost, path, body, nil)
	if status != http.StatusOK {
		t.Fatalf("POST %s: HTTP %d: %s", path, status, g.mask(string(data)))
	}
	if ct := h.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q", ct)
	}
	var events []liveSSEEvent
	var cur liveSSEEvent
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if cur.name != "" || cur.data != "" {
				events = append(events, cur)
			}
			cur = liveSSEEvent{}
		case strings.HasPrefix(line, "event:"):
			cur.name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			cur.data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	if cur.name != "" || cur.data != "" {
		events = append(events, cur)
	}
	return events
}

// cheapestModel picks a haiku-class model from /v1/models (the newest one,
// without the thinking variant).
func (g *liveGateway) cheapestModel(t *testing.T) string {
	t.Helper()
	status, _, data := g.do(t, http.MethodGet, "/v1/models", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /v1/models: HTTP %d: %s", status, g.mask(string(data)))
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	var haiku, all []string
	for _, m := range list.Data {
		all = append(all, m.ID)
		id := strings.ToLower(m.ID)
		if strings.Contains(id, "haiku") && !strings.Contains(id, "thinking") {
			haiku = append(haiku, m.ID)
		}
	}
	if len(haiku) == 0 {
		t.Fatalf("no haiku-class model in /v1/models (set KIRO_LIVE_MODEL): %v", all)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(haiku)))
	return haiku[0]
}

// syncBuffer collects the gateway's output from two pipes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
