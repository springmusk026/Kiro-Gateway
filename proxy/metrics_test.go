package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kiro-go/telemetry"
)

func withMetrics(t *testing.T, on bool) {
	t.Helper()
	v := int32(2)
	if on {
		v = 1
	}
	metricsEnabledOverride.Store(v)
	old := metrics
	metrics = newMetricsRegistry()
	t.Cleanup(func() {
		metricsEnabledOverride.Store(0)
		metrics = old
	})
}

func scrape(t *testing.T, h *Handler, remote, auth string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	r.RemoteAddr = remote
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	h.route(w, r, "/metrics")
	return w
}

func TestMetricsDisabledByDefault(t *testing.T) {
	withMetrics(t, false)
	w := scrape(t, &Handler{}, "127.0.0.1:1234", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d", w.Code)
	}
}

func TestMetricsAccessControl(t *testing.T) {
	withMetrics(t, true)
	mustInitConfig(t)
	h := &Handler{}
	for remote, want := range map[string]int{
		"127.0.0.1:1": 200, "[::1]:1": 200, "10.1.2.3:1": 200, "192.168.1.9:1": 200, "172.18.0.1:1": 200,
		"8.8.8.8:1": 403, "[2001:db8::1]:1": 403,
	} {
		if got := scrape(t, h, remote, "").Code; got != want {
			t.Fatalf("%s: got %d want %d", remote, got, want)
		}
	}
	t.Setenv("KIRO_METRICS_TOKEN", "s3cret")
	if got := scrape(t, h, "127.0.0.1:1", "").Code; got != http.StatusUnauthorized {
		t.Fatalf("token set, no auth from loopback: %d", got)
	}
	if got := scrape(t, h, "8.8.8.8:1", "Bearer wrong").Code; got != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", got)
	}
	if got := scrape(t, h, "8.8.8.8:1", "Bearer s3cret").Code; got != http.StatusOK {
		t.Fatalf("right token: %d", got)
	}
}

func TestMetricsCollectsFromTrace(t *testing.T) {
	withMetrics(t, true)
	mustInitConfig(t)
	h := &Handler{} // no sink: metrics alone still trace requests
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	tw, tr, finish := h.beginTrace(httptest.NewRecorder(), r, "claude")
	if got := metrics.inFlight.Load(); got != 1 {
		t.Fatalf("in flight %d", got)
	}
	if t2 := traceFrom(tr.Context()); t2 == nil {
		t.Fatal("no trace without a sink")
	} else {
		t2.setSuccess("Claude-Sonnet-4.5", "")
		t2.setUsage(100, 20, 0.5)
		t2.setCache(70, 5)
	}
	tw.WriteHeader(200)
	tw.Write([]byte("ok"))
	finish()
	if got := metrics.inFlight.Load(); got != 0 {
		t.Fatalf("in flight after finish %d", got)
	}
	metrics.observe(&telemetry.RequestRecord{Endpoint: "openai", Model: "x", HTTPStatus: 429, Status: "error", ErrorType: "rate_limit", DurationMs: 1500, Attempts: 3})

	body := scrape(t, h, "127.0.0.1:1", "").Body.String()
	for _, want := range []string{
		`kiro_requests_total{endpoint="claude",status="200",model="claude-sonnet-4.5"} 1`,
		`kiro_tokens_total{model="claude-sonnet-4.5",direction="input"} 100`,
		`kiro_tokens_total{model="claude-sonnet-4.5",direction="output"} 20`,
		`kiro_cache_tokens_total{model="claude-sonnet-4.5",kind="read"} 70`,
		`kiro_cache_tokens_total{model="claude-sonnet-4.5",kind="write"} 5`,
		`kiro_credits_total{model="claude-sonnet-4.5"} 0.5`,
		`kiro_request_errors_total{endpoint="openai",class="rate_limit"} 1`,
		`kiro_upstream_attempts_total{endpoint="openai"} 3`,
		`kiro_request_duration_seconds_bucket{endpoint="openai",le="1"} 0`,
		`kiro_request_duration_seconds_bucket{endpoint="openai",le="2.5"} 1`,
		`kiro_request_duration_seconds_bucket{endpoint="openai",le="+Inf"} 1`,
		`kiro_request_duration_seconds_count{endpoint="openai"} 1`,
		`kiro_requests_in_flight 0`,
		`kiro_build_info{version=`,
		`kiro_accounts{state="available"} 0`,
		"# TYPE kiro_request_duration_seconds histogram",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
}

func TestMetricsModelCardinalityCapped(t *testing.T) {
	withMetrics(t, true)
	for i := 0; i < maxModelLabels+25; i++ {
		metrics.observe(&telemetry.RequestRecord{Endpoint: "claude", Model: fmt.Sprintf("m-%d", i), HTTPStatus: 200})
	}
	if len(metrics.models) != maxModelLabels {
		t.Fatalf("%d model labels", len(metrics.models))
	}
	var b strings.Builder
	metrics.write(promWriter{&b})
	if !strings.Contains(b.String(), `kiro_requests_total{endpoint="claude",status="200",model="other"} 25`) {
		t.Fatalf("overflow not folded:\n%s", b.String())
	}
}

func TestMetricsLabelEscaping(t *testing.T) {
	if got := labels("model", "a\"b\\c\nd"); got != `{model="a\"b\\c\nd"}` {
		t.Fatalf("got %s", got)
	}
}
