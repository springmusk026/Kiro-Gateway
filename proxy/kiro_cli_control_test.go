package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
)

type capturedControlCall struct {
	method, host, path, rawQuery string
	header                       http.Header
	body                         string
	close                        bool
}

// installControlServer sends every REST call to an httptest server and
// records each request as the client built it (original host included).
// answer maps an x-amz-target to the JSON response body.
func installControlServer(t *testing.T, answer map[string]string) (calls func() []capturedControlCall) {
	t.Helper()
	if err := config.Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []capturedControlCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := answer[r.Header.Get("X-Amz-Target")]
		if !ok {
			http.Error(w, `{"__type":"UnknownOperationException"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	inner := &http.Transport{}
	client := &http.Client{Timeout: 5 * time.Second, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var b []byte
		if r.Body != nil {
			b, _ = io.ReadAll(r.Body)
		}
		mu.Lock()
		got = append(got, capturedControlCall{method: r.Method, host: r.URL.Host, path: r.URL.Path, rawQuery: r.URL.RawQuery,
			header: r.Header.Clone(), body: string(b), close: r.Close})
		mu.Unlock()
		out := r.Clone(r.Context())
		out.URL.Scheme, out.URL.Host, out.Host = target.Scheme, target.Host, ""
		out.Body = io.NopCloser(strings.NewReader(string(b)))
		return inner.RoundTrip(out)
	})}
	old := kiroRestHttpStore.Load()
	kiroRestHttpStore.Store(client)
	t.Cleanup(func() { kiroRestHttpStore.Store(old) })
	return func() []capturedControlCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]capturedControlCall(nil), got...)
	}
}

func apiKeyControlAccount() *config.Account {
	return &config.Account{ID: "ak1", AuthMethod: "api_key", KiroApiKey: "ksk_FAKE", Region: "eu-central-1", Enabled: true}
}

// assertCLIControlRequest checks the header set shared by the CLI agent's
// control calls.
func assertCLIControlRequest(t *testing.T, c capturedControlCall, host, target, body, sdkRequest string, dashUA bool) {
	t.Helper()
	if c.method != http.MethodPost || c.host != host || c.path != "/" || c.rawQuery != "" {
		t.Fatalf("%s: %s %s%s?%s", target, c.method, c.host, c.path, c.rawQuery)
	}
	if c.body != body {
		t.Fatalf("%s body %s, want %s", target, c.body, body)
	}
	h := c.header
	want := map[string]string{
		"Content-Type":    "application/x-amz-json-1.0",
		"X-Amz-Target":    target,
		"Authorization":   "Bearer ksk_FAKE",
		"Tokentype":       "API_KEY",
		"Amz-Sdk-Request": sdkRequest,
	}
	for k, v := range want {
		if h.Get(k) != v {
			t.Fatalf("%s header %s = %q, want %q", target, k, h.Get(k), v)
		}
	}
	if h.Get("Amz-Sdk-Invocation-Id") == "" || !c.close {
		t.Fatalf("%s: invocation id %q, close %v", target, h.Get("Amz-Sdk-Invocation-Id"), c.close)
	}
	for _, absent := range []string{"X-Amzn-Codewhisperer-Optout", "X-Amzn-Kiro-Origin", "Accept", "X-Amzn-Kiro-Profile-Arn"} {
		if h.Get(absent) != "" {
			t.Fatalf("%s: unexpected %s: %q", target, absent, h.Get(absent))
		}
	}
	ua, amz := h.Get("User-Agent"), h.Get("X-Amz-User-Agent")
	tail := "KiroCLI/3.5.0 KAS/0.66.119 os/linux md/appVersion-3.5.0 app/AmazonQ-For-CLI acp-client/kiro-cli"
	if dashUA {
		tail = "KiroCLI/3.5.0-KAS/0.66.119-os/linux-md/appVersion-3.5.0-app/AmazonQ-For-CLI acp-client/kiro-cli"
	}
	if !strings.HasPrefix(ua, "aws-sdk-js/1.0.0 ua/2.1 os/linux") || !strings.HasSuffix(ua, " m/N,E "+tail) || amz != "aws-sdk-js/1.0.0 "+tail {
		t.Fatalf("%s user agents:\n%s\n%s", target, ua, amz)
	}
}

func TestListAvailableModelsAPIKeyUsesCLIControlPlane(t *testing.T) {
	calls := installControlServer(t, map[string]string{
		"KiroControlPlaneBearerService.ListAvailableModels": `{"defaultModel":{"modelId":"auto"},"models":[
			{"modelId":"claude-sonnet-4.5","modelName":"Claude Sonnet 4.5","rateMultiplier":1.3,"rateUnit":"credit",
			 "supportedInputTypes":["TEXT","IMAGE"],"tokenLimits":{"maxInputTokens":200000,"maxOutputTokens":64000},
			 "promptCaching":{"supportsPromptCaching":true},
			 "additionalModelRequestFieldsSchema":{"type":"object","properties":{"thinking":{"properties":{"type":{"enum":["adaptive","between_tools"]}}}}}},
			{"modelId":"gpt-5","modelName":"GPT-5","additionalModelRequestFieldsSchema":{"properties":{"reasoning":{"properties":{"effort":{"enum":["none","low","max"]}}}}}}]}`,
	})
	models, err := ListAvailableModels(apiKeyControlAccount())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ModelId != "claude-sonnet-4.5" || models[0].RateMultiplier != 1.3 ||
		models[0].TokenLimits == nil || models[0].TokenLimits.MaxInputTokens != 200000 ||
		models[0].PromptCaching == nil || !models[0].PromptCaching.SupportsPromptCaching ||
		!strings.Contains(string(models[0].AdditionalFieldsSchema), "between_tools") || len(models[0].InputTypes) != 2 {
		t.Fatalf("models: %+v", models)
	}
	c := calls()
	if len(c) != 1 {
		t.Fatalf("%d calls", len(c))
	}
	assertCLIControlRequest(t, c[0], "management.eu-central-1.kiro.dev", "KiroControlPlaneBearerService.ListAvailableModels",
		`{"origin":"KIRO_CLI"}`, "attempt=1; max=3", false)
}

func TestGetUsageLimitsAPIKeyUsesCLIControlPlane(t *testing.T) {
	calls := installControlServer(t, map[string]string{
		"KiroControlPlaneBearerService.GetUsageLimits": `{"nextDateReset":1.7700192E9,"overageConfiguration":{"overageStatus":"DISABLED"},
			"subscriptionInfo":{"type":"Q_DEVELOPER_STANDALONE_PRO","subscriptionTitle":"KIRO PRO"},
			"usageBreakdownList":[{"resourceType":"CREDIT","currentUsage":12.5,"currentUsageWithPrecision":12.5,"usageLimit":1000,
			"usageLimitWithPrecision":1000,"currency":"USD","unit":"INVOCATIONS","overageRate":0.04,"bonuses":[]}],
			"userInfo":{"userId":"u-1","email":"dev@example.com"}}`,
	})
	info, err := RefreshAccountInfo(apiKeyControlAccount())
	if err != nil {
		t.Fatal(err)
	}
	if info.UsageCurrent != 12.5 || info.UsageLimit != 1000 || info.SubscriptionType != "PRO" ||
		info.Email != "dev@example.com" || info.NextResetDate == "" {
		t.Fatalf("info: %+v", info)
	}
	c := calls()
	if len(c) != 1 {
		t.Fatalf("%d calls", len(c))
	}
	assertCLIControlRequest(t, c[0], "management.eu-central-1.kiro.dev", "KiroControlPlaneBearerService.GetUsageLimits",
		`{"origin":"KIRO_CLI"}`, "attempt=1; max=1", false)
}

func TestFeatureConfigurationPrefetchAPIKeyOnly(t *testing.T) {
	calls := installControlServer(t, map[string]string{
		"KiroRuntimeService.GetFeatureConfiguration": `{"configuration":{"9f0b":true,"a1c3":"x"}}`,
	})
	featureConfigPrefetchDisabled = false
	featureConfigMu.Lock()
	featureConfigCache = map[string]*featureConfigEntry{}
	featureConfigMu.Unlock()
	t.Cleanup(func() { featureConfigPrefetchDisabled = true })

	acc := apiKeyControlAccount()
	prefetchFeatureConfiguration(acc)
	prefetchFeatureConfiguration(acc) // within 15 minutes: no second call
	prefetchFeatureConfiguration(&config.Account{ID: "oauth", AccessToken: "at"})
	deadline := time.Now().Add(2 * time.Second)
	for cachedFeatureConfiguration(acc.ID) == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cfg := cachedFeatureConfiguration(acc.ID)
	if len(cfg) != 2 || string(cfg["9f0b"]) != "true" {
		t.Fatalf("cached configuration: %v", cfg)
	}
	c := calls()
	if len(c) != 1 {
		t.Fatalf("%d calls, want 1 (OAuth accounts and repeats must not call)", len(c))
	}
	assertCLIControlRequest(t, c[0], "runtime.eu-central-1.kiro.dev", "KiroRuntimeService.GetFeatureConfiguration",
		`{"origin":"KIRO_CLI","version":"3.5.0"}`, "attempt=1; max=1", true)

	// After the TTL it is fetched again.
	featureConfigMu.Lock()
	featureConfigCache[acc.ID].attempted = time.Now().Add(-featureConfigTTL - time.Second)
	featureConfigMu.Unlock()
	prefetchFeatureConfiguration(acc)
	deadline = time.Now().Add(2 * time.Second)
	for len(calls()) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(calls()) != 2 {
		t.Fatalf("no refetch after the TTL: %d calls", len(calls()))
	}
}

// OAuth accounts keep their existing (IDE) calls.
func TestOAuthControlCallsUnchanged(t *testing.T) {
	calls := installControlServer(t, map[string]string{})
	acc := &config.Account{ID: "o", AccessToken: "at", ProfileArn: "arn:aws:codewhisperer:us-east-1:123456789012:profile/ABC"}
	_, _ = ListAvailableModels(acc)
	_, _ = GetUsageLimits(acc)
	for _, c := range calls() {
		if c.method != http.MethodGet || strings.Contains(c.host, "kiro.dev") || c.header.Get("X-Amz-Target") != "" {
			t.Fatalf("OAuth call changed: %s %s%s", c.method, c.host, c.path)
		}
	}
	if len(calls()) != 2 {
		t.Fatalf("%d calls", len(calls()))
	}
}

func TestKiroCLISDKUserAgentsDashTail(t *testing.T) {
	ua, amz := kiroCLISDKUserAgents("kiroruntime", "m/N,E", true)
	if !strings.Contains(ua, "api/kiroruntime#1.0.0 m/N,E KiroCLI/3.5.0-KAS/0.66.119-os/linux-md/appVersion-3.5.0-app/AmazonQ-For-CLI acp-client/kiro-cli") ||
		amz != "aws-sdk-js/1.0.0 KiroCLI/3.5.0-KAS/0.66.119-os/linux-md/appVersion-3.5.0-app/AmazonQ-For-CLI acp-client/kiro-cli" {
		t.Fatalf("%s | %s", ua, amz)
	}
}

// Control-plane schemas arrive as JSON objects and carry values beyond the
// ones the gateway acts on (thinking.type "between_tools", effort "none").
func TestReasoningControlsTolerateControlPlaneSchemas(t *testing.T) {
	sonnet := []byte(`{"properties":{"thinking":{"properties":{"type":{"enum":["adaptive","between_tools"]}}},
		"output_config":{"properties":{"effort":{"enum":["low","medium","high","xhigh","max"]}}}}}`)
	rc, ok := parseReasoningControls(sonnet)
	if !ok || rc.Path != "output_config" || len(rc.Levels) != 5 || rc.Toggle {
		t.Fatalf("sonnet: %+v %v", rc, ok)
	}
	gpt := []byte(`{"properties":{"reasoning":{"properties":{"effort":{"enum":["none","low","medium","high","xhigh","max"]}}}}}`)
	rc, ok = parseReasoningControls(gpt)
	if !ok || rc.Path != "reasoning" || rc.Levels[0] != "none" || pickEffortLevel("medium", rc.Levels) != "medium" {
		t.Fatalf("gpt: %+v %v", rc, ok)
	}
}
