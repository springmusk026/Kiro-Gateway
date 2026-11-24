package proxy

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFailureClassification(t *testing.T) {
	cases := []struct {
		msg                                string
		quota, rate, client, nonAcct, auth bool
	}{
		{msg: "HTTP 429 from Kiro responses: ThrottlingException: Rate exceeded", rate: true},
		{msg: "HTTP 429 from amazonq: You have reached the limit. MONTHLY_REQUEST_COUNT", quota: true, rate: true},
		{msg: "HTTP 400 from kiro: Improperly formed request", client: true},
		{msg: "HTTP 400 from kiro: CONTENT_LENGTH_EXCEEDS_THRESHOLD", client: true},
		{msg: "HTTP 403 from q: This version of Kiro is no longer supported. FEATURE_NOT_SUPPORTED", nonAcct: true, auth: true},
		{msg: "proxyconnect tcp: 403 Forbidden", nonAcct: true, auth: true},
		{msg: "authentication failed: HTTP 403 from Kiro responses: The bearer token is invalid", auth: true},
		{msg: "request id 14290 failed", rate: false},
	}
	for _, c := range cases {
		if got := isQuotaErrorMessage(c.msg); got != c.quota {
			t.Errorf("quota(%q) = %v", c.msg, got)
		}
		if got := isRateLimitMessage(c.msg); got != c.rate {
			t.Errorf("rate(%q) = %v", c.msg, got)
		}
		if got := isClientRequestErrorMessage(c.msg); got != c.client {
			t.Errorf("client(%q) = %v", c.msg, got)
		}
		if got := isNonAccountForbiddenMessage(c.msg); got != c.nonAcct {
			t.Errorf("nonAccount(%q) = %v", c.msg, got)
		}
		if got := isAuthErrorMessage(c.msg); got != c.auth {
			t.Errorf("auth(%q) = %v", c.msg, got)
		}
	}
}

func TestShortenToolNameAvoidsCollisionsAndSplitRunes(t *testing.T) {
	base := strings.Repeat("a", 70)
	a, b := shortenToolName(base+"_one"), shortenToolName(base+"_two")
	if a == b || len(a) > 64 || len(b) > 64 {
		t.Fatalf("long names must stay distinct and ≤64 bytes: %q %q", a, b)
	}
	u := shortenToolName(strings.Repeat("é", 40))
	if !utf8.ValidString(u) || len(u) > 64 {
		t.Fatalf("shortened name must be valid UTF-8 within 64 bytes: %q", u)
	}
}
