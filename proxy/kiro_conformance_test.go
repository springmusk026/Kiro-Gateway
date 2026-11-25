package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/springmusk026/Kiro-Gateway/config"
)

func TestResponsesIdentity(t *testing.T) {
	oauth := &config.Account{ID: "a", AuthMethod: "social", MachineId: "m123"}
	if ua := responsesUserAgent(oauth); !strings.HasPrefix(ua, "KiroIDE-") || !strings.HasSuffix(ua, "-m123") || strings.Contains(ua, "aws-sdk-js") {
		t.Fatalf("oauth UA = %q", ua)
	}
	key := &config.Account{ID: "k", AuthMethod: "api_key", KiroApiKey: "ksk_x"}
	if ua := responsesUserAgent(key); !strings.HasPrefix(ua, "KiroCLI/3.5.0 KAS/") || !strings.HasSuffix(ua, "app/AmazonQ-For-CLI acp-client/kiro-cli") {
		t.Fatalf("api key UA = %q", ua)
	}
	cases := map[string]string{"social": "", "": "", "idc": "SSO_OIDC", "external_idp": "EXTERNAL_IDP"}
	for method, want := range cases {
		if got := responsesTokenType(&config.Account{AuthMethod: method}); got != want {
			t.Errorf("TokenType(%q) = %q, want %q", method, got, want)
		}
	}
	if got := responsesTokenType(key); got != "API_KEY" {
		t.Errorf("api key TokenType = %q", got)
	}
}

func TestResponsesMetadataOrder(t *testing.T) {
	m := responsesMetadata{
		"kiro:root_conversation_id": "r",
		"kiro:conversation_id":      "c",
		"kiro:agent_mode":           "vibe",
		"z:extra":                   "x",
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kiro:agent_mode":"vibe","kiro:conversation_id":"c","kiro:root_conversation_id":"r","z:extra":"x"}`
	if string(b) != want {
		t.Fatalf("metadata = %s", b)
	}
}

func TestResponsesIncompleteIsTerminal(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"partial"}`, ``,
		`data: {"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`, ``,
	}, "\n")
	var stop string
	cb := &KiroStreamCallback{OnText: func(string, bool) {}, OnStopReason: func(r string) { stop = r }}
	if _, err := parseResponsesSSE(strings.NewReader(stream), testPayload(), cb); err != nil {
		t.Fatalf("response.incomplete must end the stream cleanly, got %v", err)
	}
	if stop != "MAX_TOKENS" {
		t.Fatalf("stop = %q", stop)
	}
}

func TestResponsesRefusalIsText(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"response.refusal.delta","delta":"I can't help with that."}`, ``,
		`data: {"type":"response.completed","response":{"status":"completed"}}`, ``,
	}, "\n")
	var text strings.Builder
	cb := &KiroStreamCallback{OnText: func(s string, th bool) {
		if !th {
			text.WriteString(s)
		}
	}}
	if _, err := parseResponsesSSE(strings.NewReader(stream), testPayload(), cb); err != nil {
		t.Fatal(err)
	}
	if text.String() != "I can't help with that." {
		t.Fatalf("refusal text = %q", text.String())
	}
}

func TestEventStreamInlineErrorEvents(t *testing.T) {
	for _, et := range []string{"throttlingError", "validationError", "serviceUnavailableError", "error"} {
		var body []byte
		body = append(body, awsEventStreamFrame(t, "assistantResponseEvent", map[string]interface{}{"content": "hi"})...)
		body = append(body, awsEventStreamFrame(t, et, map[string]interface{}{"message": "Rate exceeded", "reason": "TOO_MANY"})...)
		_, err := parseEventStreamTracked(bytes.NewReader(body), &KiroStreamCallback{OnText: func(string, bool) {}})
		if err == nil || !errors.Is(err, errKiroEventStreamUpstream) || !strings.Contains(err.Error(), et) {
			t.Fatalf("%s: err = %v", et, err)
		}
	}
}

func TestAPIKeyRuntimeCallMatchesCapturedClient(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://runtime.us-east-1.kiro.dev/", nil)
	acct := &config.Account{ID: "k", AuthMethod: "api_key", KiroApiKey: "ksk_x"}
	setKiroCLIRuntimeHeaders(req, acct, kiroCLIEndpoint.AmzTarget, 1, "inv-1")
	want := map[string]string{
		"Content-Type":                   "application/x-amz-json-1.0",
		"X-Amz-Target":                   "KiroRuntimeService.GenerateAssistantResponse",
		"Tokentype":                      "API_KEY",
		"X-Amzn-Kiro-Client-Attribution": "kiro-cli",
		"X-Amzn-Kiro-Mode-Identifier":    "default-v2", // kiro-cli 3.5 v3 engine, live capture
		"X-Amzn-Kiro-Fallback":           "0;max=0",
		"X-Amzn-Kiro-Attempt":            "1;max=3",
		"Amz-Sdk-Request":                "attempt=1; max=3",
		"Amz-Sdk-Invocation-Id":          "inv-1",
	}
	for k, v := range want {
		if got := req.Header.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	for _, absent := range []string{"x-amzn-kiro-origin", "x-amzn-kiro-agent-mode", "x-amzn-codewhisperer-optout", "Accept"} {
		if req.Header.Get(absent) != "" {
			t.Errorf("%s must not be sent", absent)
		}
	}
	if ua := req.Header.Get("User-Agent"); !strings.HasPrefix(ua, "aws-sdk-js/1.0.0 ua/2.1 os/") || !strings.Contains(ua, "api/kiroruntime#1.0.0 m/N KiroCLI/3.5.0") {
		t.Errorf("UA = %q", ua)
	}
}

func TestConversationIDShape(t *testing.T) {
	a := buildConversationID("m", "sys", "hello there")
	if a != buildConversationID("m", "sys", "hello there") {
		t.Fatal("conversation id must be stable for the same conversation")
	}
	if !strings.HasPrefix(a, "sess_") || len(a) != len("sess_")+36 || a[len("sess_")+14] != '4' {
		t.Fatalf("id %q is not sess_<uuid v4>", a)
	}
}
