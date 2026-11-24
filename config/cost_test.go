package config

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func initCostTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNormalizeModelFallbacks(t *testing.T) {
	got, err := NormalizeModelFallbacks([]ModelFallback{
		{Model: " a ", Fallbacks: []string{"b", "B", "a", "", "c"}},
		{Model: "empty", Fallbacks: []string{"empty"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Model != "a" || len(got[0].Fallbacks) != 2 || got[0].Fallbacks[0] != "b" || got[0].Fallbacks[1] != "c" {
		t.Fatalf("normalized = %+v", got)
	}
	if _, err := NormalizeModelFallbacks([]ModelFallback{{Model: "a", Fallbacks: []string{"b"}}, {Model: "A", Fallbacks: []string{"c"}}}); !errors.Is(err, ErrInvalidModelFallback) {
		t.Fatalf("duplicate model must be rejected, got %v", err)
	}
	if _, err := NormalizeModelFallbacks([]ModelFallback{{Model: "a b", Fallbacks: []string{"c"}}}); !errors.Is(err, ErrInvalidModelFallback) {
		t.Fatalf("invalid id must be rejected, got %v", err)
	}
}

func TestModelFallbacksPersistAndResolve(t *testing.T) {
	path := initCostTestConfig(t)
	if err := UpdateModelFallbacks([]ModelFallback{{Model: "Primary", Fallbacks: []string{"backup"}}}); err != nil {
		t.Fatal(err)
	}
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	if fb := FallbackModelsFor("primary"); len(fb) != 1 || fb[0] != "backup" {
		t.Fatalf("FallbackModelsFor = %v", fb)
	}
}

func TestDailyBudgetResetsAtMidnightInZone(t *testing.T) {
	initCostTestConfig(t)
	if err := UpdateBudgetSettings(BudgetSettings{DailyCreditLimit: 10, BudgetTimeZone: "America/New_York"}); err != nil {
		t.Fatal(err)
	}
	ny, _ := time.LoadLocation("America/New_York")
	evening := time.Date(2026, 3, 10, 23, 30, 0, 0, ny) // already the 11th in UTC
	RecordDailyCredits("", 10, evening)
	if b := CheckDailyBudget("", evening); b == nil || b.Scope != "global" || b.Limit != 10 {
		t.Fatalf("expected global breach, got %+v", b)
	}
	want := time.Date(2026, 3, 11, 0, 0, 0, 0, ny)
	if b := CheckDailyBudget("", evening); !b.ResetsAt.Equal(want) {
		t.Fatalf("resets at %v, want %v", b.ResetsAt, want)
	}
	nextMorning := time.Date(2026, 3, 11, 0, 5, 0, 0, ny)
	if b := CheckDailyBudget("", nextMorning); b != nil {
		t.Fatalf("budget must reset at local midnight, got %+v", b)
	}
	RecordDailyCredits("", 1, nextMorning)
	if u := GetDailyUsage(nextMorning); u.GlobalCredits != 1 || u.Day != "2026-03-11" {
		t.Fatalf("new day usage = %+v", u)
	}
}

func TestPerKeyDailyBudgetAndPersistence(t *testing.T) {
	path := initCostTestConfig(t)
	k, err := AddApiKey(ApiKeyEntry{Key: "sk-x", Enabled: true, DailyCreditLimit: 3})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	RecordDailyCredits(k.ID, 2, now)
	if b := CheckDailyBudget(k.ID, now); b != nil {
		t.Fatalf("under the cap, got %+v", b)
	}
	RecordDailyCredits(k.ID, 1, now)
	if b := CheckDailyBudget(k.ID, now); b == nil || b.Scope != "key" {
		t.Fatalf("expected key breach, got %+v", b)
	}
	if b := CheckDailyBudget("", now); b != nil {
		t.Fatalf("no global cap configured, got %+v", b)
	}
	Flush()
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	if u := GetDailyUsage(now); u.KeyCredits[k.ID] != 3 || u.GlobalCredits != 3 {
		t.Fatalf("daily usage must survive a restart, got %+v", u)
	}
}

func TestMarkAlertSentOncePerDay(t *testing.T) {
	initCostTestConfig(t)
	day1 := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if !MarkAlertSent("global_daily||warning", day1) || MarkAlertSent("global_daily||warning", day1) {
		t.Fatal("an alert must be marked once per day")
	}
	if !MarkAlertSent("global_daily||critical", day1) {
		t.Fatal("a different level is a different alert")
	}
	if !MarkAlertSent("global_daily||warning", day1.Add(24*time.Hour)) {
		t.Fatal("alerts must fire again on the next budget day")
	}
}

func TestBudgetSettingsValidation(t *testing.T) {
	initCostTestConfig(t)
	for _, s := range []BudgetSettings{
		{DailyCreditLimit: -1},
		{BudgetTimeZone: "Not/AZone"},
		{AlertWebhookURL: "ftp://example.com"},
		{AlertWebhookURL: "not a url"},
	} {
		if err := UpdateBudgetSettings(s); err == nil {
			t.Fatalf("expected %+v to be rejected", s)
		}
	}
	if err := UpdateUsageRoutingThreshold(1.5); err == nil {
		t.Fatal("threshold above 1 must be rejected")
	}
}

func TestUsageRoutingThreshold(t *testing.T) {
	initCostTestConfig(t)
	if got := GetUsageRoutingThreshold(); got != DefaultUsageRoutingThreshold {
		t.Fatalf("default = %v", got)
	}
	if err := UpdateUsageRoutingThreshold(0.8); err != nil {
		t.Fatal(err)
	}
	if got := GetUsageRoutingThreshold(); got != 0.8 {
		t.Fatalf("stored = %v", got)
	}
	t.Setenv("KIRO_USAGE_THRESHOLD", "0.9")
	if got := GetUsageRoutingThreshold(); got != 0.9 || !UsageThresholdFromEnv() {
		t.Fatalf("env override = %v", got)
	}
	t.Setenv("KIRO_USAGE_THRESHOLD", "abc")
	if got := GetUsageRoutingThreshold(); got != 0.8 {
		t.Fatalf("invalid env must be ignored, got %v", got)
	}
}
