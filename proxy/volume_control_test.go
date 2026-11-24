package proxy

import (
	"context"
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
