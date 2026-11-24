package proxy

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"kiro-go/config"
	"kiro-go/pool"
)

func TestThrottleBackoffGrowsAndHonoursRetryAfter(t *testing.T) {
	p := pool.NewTestPool([]config.Account{{ID: "a"}})
	if d := p.ThrottleBackoff("a", 30*time.Second, 5*time.Minute, 0); d != 30*time.Second {
		t.Fatalf("first: %s", d)
	}
	if d := p.ThrottleBackoff("a", 30*time.Second, 5*time.Minute, 0); d != time.Minute {
		t.Fatalf("second: %s", d)
	}
	for i := 0; i < 10; i++ {
		p.ThrottleBackoff("a", 30*time.Second, 5*time.Minute, 0)
	}
	if d := p.ThrottleBackoff("a", 30*time.Second, 5*time.Minute, 0); d != 5*time.Minute {
		t.Fatalf("capped: %s", d)
	}
	p.RecordSuccess("a")
	if d := p.ThrottleBackoff("a", 30*time.Second, 5*time.Minute, 90*time.Second); d != 90*time.Second {
		t.Fatalf("retry-after should win over a shorter backoff: %s", d)
	}
}

func TestRetryAfterRoundTrip(t *testing.T) {
	msg := "HTTP 429 from kiro" + retryAfterSuffix("42") + ": slow down"
	if got := parseRetryAfterFromMessage(msg); got != 42*time.Second {
		t.Fatalf("got %s", got)
	}
	if retryAfterSuffix("Wed, 21 Oct 2015 07:28:00 GMT") != "" || parseRetryAfterFromMessage("HTTP 429") != 0 {
		t.Fatal("non-numeric Retry-After is ignored")
	}
}

func TestMonthlyLimitParksUntilNextUTCDay(t *testing.T) {
	now := time.Date(2026, 11, 24, 23, 10, 0, 0, time.UTC)
	if got := nextUTCMidnight(now); !got.Equal(time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("got %s", got)
	}
	if !isMonthlyLimitMessage("HTTP 429: MONTHLY_REQUEST_COUNT exceeded") {
		t.Fatal("monthly limit not detected")
	}
}

func TestAccountSlotsQueueRequests(t *testing.T) {
	t.Setenv("KIRO_MAX_CONCURRENT_PER_ACCOUNT", "1")
	accountSlots.Delete("acc-slots")
	release, err := acquireAccountSlot(context.Background(), "acc-slots")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := acquireAccountSlot(ctx, "acc-slots"); err == nil {
		t.Fatal("second request must wait while the slot is taken")
	}
	release()
	r2, err := acquireAccountSlot(context.Background(), "acc-slots")
	if err != nil {
		t.Fatal("slot should be free after release")
	}
	r2()
}

func TestRetryBackoffMatchesOfficialClient(t *testing.T) {
	if retryBackoff(0, false) != 250*time.Millisecond || retryBackoff(2, false) != time.Second {
		t.Fatal("transient backoff should be 250ms·2ⁿ")
	}
	if retryBackoff(0, true) != time.Second || retryBackoff(10, true) != 20*time.Second {
		t.Fatal("throttle backoff should be 1s·2ⁿ capped at 20s")
	}
}

func TestKiroRetryAfterSources(t *testing.T) {
	h := http.Header{}
	h.Set("x-amzn-kiro-ratelimit-retry-after", "1500")
	if d := kiroRetryAfter(h, nil); d != 1500*time.Millisecond {
		t.Fatalf("header ms: %s", d)
	}
	if d := kiroRetryAfter(http.Header{}, []byte(`{"reason":"USER_REQUEST_RATE_EXCEEDED","retryAfterMilliseconds": 2500}`)); d != 2500*time.Millisecond {
		t.Fatalf("body ms: %s", d)
	}
	h2 := http.Header{}
	h2.Set("Retry-After", "9999")
	if d := kiroRetryAfter(h2, nil); d != 300*time.Second {
		t.Fatalf("cap: %s", d)
	}
	if !isUsageLimitBody([]byte(`{"reason":"MONTHLY_REQUEST_COUNT"}`)) || isUsageLimitBody([]byte(`{"reason":"USER_REQUEST_RATE_EXCEEDED"}`)) {
		t.Fatal("usage-limit detection")
	}
	if retryAfterTag(1500*time.Millisecond) != " (retry-after=2)" {
		t.Fatal("tag rounds up to seconds")
	}
}

func TestClaudeQuotaProbeIsAnsweredLocally(t *testing.T) {
	probe := &ClaudeRequest{Model: "claude-haiku-4.5", MaxTokens: 1, Messages: []ClaudeMessage{{Role: "user", Content: "quota"}}}
	if !isClaudeQuotaProbe(probe) {
		t.Fatal("Claude Code quota probe not recognised")
	}
	blocks := &ClaudeRequest{MaxTokens: 1, Messages: []ClaudeMessage{{Role: "user", Content: []interface{}{map[string]interface{}{"type": "text", "text": "quota"}}}}}
	if !isClaudeQuotaProbe(blocks) {
		t.Fatal("block form not recognised")
	}
	for _, real := range []*ClaudeRequest{
		{MaxTokens: 1, Messages: []ClaudeMessage{{Role: "user", Content: "hello"}}},
		{MaxTokens: 50, Messages: []ClaudeMessage{{Role: "user", Content: "quota"}}},
		{MaxTokens: 1, Messages: []ClaudeMessage{{Role: "user", Content: "quota"}}, Tools: []ClaudeTool{{Name: "x"}}},
	} {
		if isClaudeQuotaProbe(real) {
			t.Fatalf("real request must go upstream: %+v", real)
		}
	}
	t.Setenv("KIRO_LOCAL_PROBES", "0")
	if isClaudeQuotaProbe(probe) {
		t.Fatal("must be disableable")
	}
}

func TestUpstreamCacheUsageIsReported(t *testing.T) {
	var read, write int
	stream := append(awsEventStreamFrame(t, "assistantResponseEvent", map[string]interface{}{"content": "hi"}),
		awsEventStreamFrame(t, "metadataEvent", map[string]interface{}{
			"stopReason": "end_turn",
			"tokenUsage": map[string]interface{}{"uncachedInputTokens": 120, "cacheReadInputTokens": 9000, "cacheWriteInputTokens": 300, "outputTokens": 5},
		})...)
	_, err := parseEventStreamTracked(bytes.NewReader(stream), &KiroStreamCallback{
		OnText:       func(string, bool) {},
		OnCacheUsage: func(r, w int) { read, write = r, w },
	})
	if err != nil {
		t.Fatal(err)
	}
	if read != 9000 || write != 300 {
		t.Fatalf("cache usage not surfaced: read=%d write=%d", read, write)
	}
}
