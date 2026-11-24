package proxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/springmusk026/Kiro-Gateway/config"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

var testPNG = append([]byte("\x89PNG\r\n\x1a\n"), []byte("not-a-real-image-but-sniffable")...)

// allowTestFetch lets fetches reach httptest servers (plain http on
// loopback) for the duration of a test.
func allowTestFetch(t *testing.T) {
	t.Helper()
	if err := config.Init(t.TempDir() + "/config.json"); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	t.Setenv("KIRO_FETCH_ALLOW_HTTP", "1")
	t.Setenv("KIRO_FETCH_IMAGE_URLS", "")
	mediaFetchAllowLoopbackForTest = true
	origProxy := mediaEnvProxy
	mediaEnvProxy = func(*http.Request) (*url.URL, error) { return nil, nil }
	t.Cleanup(func() {
		mediaFetchAllowLoopbackForTest = false
		mediaEnvProxy = origProxy
	})
}

func imageServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/cat.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNG)
	})
	mux.HandleFunc("/html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	})
	mux.HandleFunc("/fake.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("<html>not an image</html>"))
	})
	mux.HandleFunc("/big.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNG)
		_, _ = w.Write(make([]byte, maxFetchedImageBytes))
	})
	mux.HandleFunc("/to-private", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.0.0.1/cat.png", http.StatusFound)
	})
	mux.HandleFunc("/to-metadata", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	})
	mux.HandleFunc("/to-cat", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/cat.png", http.StatusFound)
	})
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	})
	mux.HandleFunc("/doc.pdf", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4 test"))
	})
	return httptest.NewServer(mux)
}

func TestDisallowedFetchAddr(t *testing.T) {
	refused := []string{
		"127.0.0.1", "::1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254",
		"100.64.0.1", "fe80::1", "fd00:ec2::254", "::ffff:10.0.0.1", "64:ff9b::a00:1",
		"224.0.0.1", "ff02::1", "0.0.0.0", "::", "240.0.0.1", "255.255.255.255", "198.18.0.1",
	}
	for _, s := range refused {
		if why := disallowedFetchAddr(netip.MustParseAddr(s)); why == "" {
			t.Errorf("%s should be refused", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if why := disallowedFetchAddr(netip.MustParseAddr(s)); why != "" {
			t.Errorf("%s should be allowed, got %q", s, why)
		}
	}
}

func TestFetchImageSucceedsAndSniffs(t *testing.T) {
	allowTestFetch(t)
	srv := imageServer(t)
	defer srv.Close()

	media, err := fetchRemoteMedia(context.Background(), srv.URL+"/cat.png", remoteImage, maxFetchedImageBytes)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if media.MediaType != "image/png" || string(media.Data) != string(testPNG) {
		t.Fatalf("got %q (%d bytes)", media.MediaType, len(media.Data))
	}
	// One same-host redirect is followed.
	if _, err := fetchRemoteMedia(context.Background(), srv.URL+"/to-cat", remoteImage, maxFetchedImageBytes); err != nil {
		t.Fatalf("redirected fetch: %v", err)
	}
}

func TestFetchImageRefusals(t *testing.T) {
	allowTestFetch(t)
	srv := imageServer(t)
	defer srv.Close()

	cases := map[string]string{
		"/to-private":  "private address",
		"/to-metadata": "link-local address",
		"/loop":        "redirected more than 3 times",
		"/html":        "only image/png",
		"/fake.png":    "did not return a png",
		"/big.png":     "byte limit",
	}
	for path, want := range cases {
		_, err := fetchRemoteMedia(context.Background(), srv.URL+path, remoteImage, maxFetchedImageBytes)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want error containing %q, got %v", path, want, err)
		}
		if _, ok := asMediaError(err); err != nil && !ok {
			t.Errorf("%s: error is not a client media error: %v", path, err)
		}
	}
}

func TestFetchRefusesLoopbackWithoutTestHook(t *testing.T) {
	allowTestFetch(t)
	mediaFetchAllowLoopbackForTest = false
	srv := imageServer(t)
	defer srv.Close()
	_, err := fetchRemoteMedia(context.Background(), srv.URL+"/cat.png", remoteImage, maxFetchedImageBytes)
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("want loopback refusal, got %v", err)
	}
	_, err = fetchRemoteMedia(context.Background(), "http://localhost:1/x.png", remoteImage, maxFetchedImageBytes)
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("want localhost refusal, got %v", err)
	}
}

func TestFetchRefusesHostResolvingToPrivate(t *testing.T) {
	allowTestFetch(t)
	orig := mediaResolver
	mediaResolver = func(ctx context.Context, host string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.215.14"), netip.MustParseAddr("192.168.0.10")}, nil
	}
	defer func() { mediaResolver = orig }()
	_, err := fetchRemoteMedia(context.Background(), "https://images.example.test/a.png", remoteImage, maxFetchedImageBytes)
	if err == nil || !strings.Contains(err.Error(), "private address") {
		t.Fatalf("want private refusal, got %v", err)
	}
}

func TestFetchSchemeAndDisableSwitch(t *testing.T) {
	allowTestFetch(t)
	t.Setenv("KIRO_FETCH_ALLOW_HTTP", "")
	_, err := fetchRemoteMedia(context.Background(), "http://93.184.215.14/a.png", remoteImage, maxFetchedImageBytes)
	if err == nil || !strings.Contains(err.Error(), "must use https") {
		t.Fatalf("want https requirement, got %v", err)
	}
	_, err = fetchRemoteMedia(context.Background(), "ftp://example.com/a.png", remoteImage, maxFetchedImageBytes)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("want scheme refusal, got %v", err)
	}
	t.Setenv("KIRO_FETCH_IMAGE_URLS", "0")
	_, err = fetchRemoteMedia(context.Background(), "https://example.com/a.png", remoteImage, maxFetchedImageBytes)
	if err == nil || !strings.Contains(err.Error(), "KIRO_FETCH_IMAGE_URLS=0") {
		t.Fatalf("want disabled message, got %v", err)
	}
}

func TestPrepareClaudeImageURL(t *testing.T) {
	allowTestFetch(t)
	srv := imageServer(t)
	defer srv.Close()

	var req ClaudeRequest
	body := `{"model":"claude-sonnet-4.5","max_tokens":10,"messages":[{"role":"user","content":[
		{"type":"text","text":"what is this"},
		{"type":"image","source":{"type":"url","url":"` + srv.URL + `/cat.png"}},
		{"type":"tool_result","tool_use_id":"t1","content":[{"type":"image","source":{"type":"url","url":"` + srv.URL + `/cat.png"}}]}
	]}]}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	if err := prepareClaudeMedia(context.Background(), &req); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	payload := ClaudeToKiro(&req, false)
	imgs := payload.ConversationState.CurrentMessage.UserInputMessage.Images
	if len(imgs) != 2 || imgs[0].Format != "png" || imgs[0].Source.Bytes != base64.StdEncoding.EncodeToString(testPNG) {
		t.Fatalf("images = %+v", imgs)
	}
}

func TestPrepareOpenAIImageURL(t *testing.T) {
	allowTestFetch(t)
	srv := imageServer(t)
	defer srv.Close()

	var req OpenAIRequest
	body := `{"model":"claude-sonnet-4.5","messages":[{"role":"user","content":[
		{"type":"text","text":"describe"},
		{"type":"image_url","image_url":{"url":"` + srv.URL + `/cat.png","detail":"high"}}
	]}]}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	if err := prepareOpenAIMedia(context.Background(), req.Messages); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	payload := OpenAIToKiro(&req, false)
	if imgs := payload.ConversationState.CurrentMessage.UserInputMessage.Images; len(imgs) != 1 || imgs[0].Format != "png" {
		t.Fatalf("images = %+v", imgs)
	}

	// Responses input_image with a URL string.
	msgs, err := parseResponsesInput(json.RawMessage(`[{"role":"user","content":[{"type":"input_text","text":"hi"},{"type":"input_image","image_url":"` + srv.URL + `/cat.png"}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareOpenAIMedia(context.Background(), msgs); err != nil {
		t.Fatalf("prepare responses: %v", err)
	}
	payload = OpenAIToKiro(&OpenAIRequest{Model: "claude-sonnet-4.5", Messages: msgs}, false)
	if imgs := payload.ConversationState.CurrentMessage.UserInputMessage.Images; len(imgs) != 1 {
		t.Fatalf("responses images = %+v", imgs)
	}
}

func TestHandlersRejectImageURLWhenDisabled(t *testing.T) {
	server := textUpstream(t, "hello")
	defer server.Close()
	h := setupIntegrityPathTest(t, server)
	t.Setenv("KIRO_FETCH_IMAGE_URLS", "0")

	rec := httptest.NewRecorder()
	h.handleClaudeMessages(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"claude-sonnet-4.5","max_tokens":10,
		"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://example.com/a.png"}}]}]}`)))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "invalid_request_error") || !strings.Contains(rec.Body.String(), "KIRO_FETCH_IMAGE_URLS") {
		t.Fatalf("anthropic: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.handleOpenAIChat(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"claude-sonnet-4.5",
		"messages":[{"role":"user","content":[{"type":"text","text":"x"},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}`)))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "KIRO_FETCH_IMAGE_URLS") {
		t.Fatalf("openai: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.handleOpenAIResponses(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"claude-sonnet-4.5",
		"input":[{"role":"user","content":[{"type":"input_text","text":"x"},{"type":"input_image","image_url":"https://example.com/a.png"}]}]}`)))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "KIRO_FETCH_IMAGE_URLS") {
		t.Fatalf("responses: %d %s", rec.Code, rec.Body.String())
	}
}

// Through an outbound proxy the hostname is resolved and checked before the
// request is handed to the proxy; allowed hosts go through the proxy.
func TestFetchThroughConfiguredProxy(t *testing.T) {
	allowTestFetch(t)
	var proxied []string
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied = append(proxied, r.URL.String())
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNG)
	}))
	defer proxySrv.Close()
	if err := config.UpdateProxySettings(proxySrv.URL); err != nil {
		t.Fatal(err)
	}
	orig := mediaResolver
	mediaResolver = func(ctx context.Context, host string) ([]netip.Addr, error) {
		if host == "internal.example.test" {
			return []netip.Addr{netip.MustParseAddr("10.20.30.40")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("93.184.215.14")}, nil
	}
	defer func() { mediaResolver = orig }()

	if _, err := fetchRemoteMedia(context.Background(), "http://internal.example.test/a.png", remoteImage, maxFetchedImageBytes); err == nil || !strings.Contains(err.Error(), "private address") {
		t.Fatalf("want private refusal, got %v", err)
	}
	if len(proxied) != 0 {
		t.Fatalf("refused request reached the proxy: %v", proxied)
	}
	media, err := fetchRemoteMedia(context.Background(), "http://cdn.example.test/a.png", remoteImage, maxFetchedImageBytes)
	if err != nil {
		t.Fatalf("proxied fetch: %v", err)
	}
	if media.MediaType != "image/png" || len(proxied) != 1 || proxied[0] != "http://cdn.example.test/a.png" {
		t.Fatalf("media=%v proxied=%v", media.MediaType, proxied)
	}
}

// The dialer re-checks the address it connects to, so a DNS answer that
// changes after validation (rebinding) is still refused.
func TestSafeDialControlRefusesPrivateTargets(t *testing.T) {
	for _, addr := range []string{"10.0.0.1:443", "127.0.0.1:80", "[::1]:443", "169.254.169.254:80"} {
		if err := safeDialControl("tcp", addr, nil); err == nil {
			t.Errorf("%s should be refused", addr)
		}
	}
	if err := safeDialControl("tcp", "93.184.215.14:443", nil); err != nil {
		t.Errorf("public address refused: %v", err)
	}
}
