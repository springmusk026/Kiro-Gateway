package config

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // budget time zones must resolve in minimal container images
)

// CostSettings groups the cost and reliability controls. It is embedded in
// Config, so its fields are stored at the top level of the configuration.
type CostSettings struct {
	// UsageRoutingThreshold is the share of an account's usage limit (0–1)
	// at which routing stops preferring it. 0 means the default (0.97).
	// KIRO_USAGE_THRESHOLD overrides it.
	UsageRoutingThreshold float64 `json:"usageRoutingThreshold,omitempty"`

	// ModelFallbacks lists, per upstream model, the models to retry on when
	// the model itself is unavailable. Only the first entry is used today.
	ModelFallbacks []ModelFallback `json:"modelFallbacks,omitempty"`

	// DailyCreditLimit caps credits used by all requests per budget day
	// (0 = no cap). Per-key daily caps live on ApiKeyEntry.
	DailyCreditLimit float64 `json:"dailyCreditLimit,omitempty"`
	// BudgetTimeZone is the IANA zone whose midnight starts a budget day
	// (empty = UTC).
	BudgetTimeZone string `json:"budgetTimeZone,omitempty"`
	// AlertWebhookURL receives budget and pool-usage alerts (optional).
	AlertWebhookURL string `json:"alertWebhookURL,omitempty"`
	// CreditPriceUSD is what one Kiro credit costs, used to show spend in
	// dollars (0 = DefaultCreditPriceUSD).
	CreditPriceUSD float64 `json:"creditPriceUsd,omitempty"`

	// BudgetState holds today's credit counters and sent alerts. It is
	// runtime state persisted with the debounced save so a restart keeps
	// the day's totals.
	BudgetState *BudgetState `json:"budgetState,omitempty"`
}

// ModelFallback is an ordered fallback list for one model.
type ModelFallback struct {
	Model     string   `json:"model"`
	Fallbacks []string `json:"fallbacks"`
}

// BudgetState is the credit usage of one budget day.
type BudgetState struct {
	Day           string             `json:"day"` // YYYY-MM-DD in the budget time zone
	GlobalCredits float64            `json:"globalCredits,omitempty"`
	KeyCredits    map[string]float64 `json:"keyCredits,omitempty"`
	// AlertsSent records alert keys already delivered today.
	AlertsSent map[string]bool `json:"alertsSent,omitempty"`
}

var ErrInvalidModelFallback = errors.New("invalid model fallback")

const (
	DefaultUsageRoutingThreshold = 0.97
	maxModelFallbackEntries      = 200
	maxFallbacksPerModel         = 5
)

// GetUsageRoutingThreshold returns the usage share (0–1] at which an account
// is treated as saturated for routing.
func GetUsageRoutingThreshold() float64 {
	if v := strings.TrimSpace(os.Getenv("KIRO_USAGE_THRESHOLD")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && validUsageThreshold(f) {
			return f
		}
	}
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if cfg != nil && validUsageThreshold(cfg.UsageRoutingThreshold) {
		return cfg.UsageRoutingThreshold
	}
	return DefaultUsageRoutingThreshold
}

// UsageThresholdFromEnv reports whether KIRO_USAGE_THRESHOLD overrides the
// stored threshold.
func UsageThresholdFromEnv() bool {
	v := strings.TrimSpace(os.Getenv("KIRO_USAGE_THRESHOLD"))
	if v == "" {
		return false
	}
	f, err := strconv.ParseFloat(v, 64)
	return err == nil && validUsageThreshold(f)
}

func validUsageThreshold(f float64) bool { return f > 0 && f <= 1 && !math.IsNaN(f) }

// UpdateUsageRoutingThreshold stores the threshold (0 restores the default).
func UpdateUsageRoutingThreshold(f float64) error {
	if f != 0 && !validUsageThreshold(f) {
		return errors.New("usageRoutingThreshold must be between 0 and 1")
	}
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		return ErrConfigUnavailable
	}
	cfg.UsageRoutingThreshold = f
	return Save()
}

// ---- Model fallbacks ----

// GetModelFallbacks returns a detached copy of the fallback lists.
func GetModelFallbacks() []ModelFallback {
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if cfg == nil {
		return []ModelFallback{}
	}
	out := make([]ModelFallback, 0, len(cfg.ModelFallbacks))
	for _, f := range cfg.ModelFallbacks {
		out = append(out, ModelFallback{Model: f.Model, Fallbacks: append([]string(nil), f.Fallbacks...)})
	}
	return out
}

// FallbackModelsFor returns the ordered fallbacks for model (case-insensitive).
func FallbackModelsFor(model string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if cfg == nil {
		return nil
	}
	for _, f := range cfg.ModelFallbacks {
		if strings.EqualFold(f.Model, model) {
			return append([]string(nil), f.Fallbacks...)
		}
	}
	return nil
}

// UpdateModelFallbacks validates and replaces all fallback lists.
func UpdateModelFallbacks(list []ModelFallback) error {
	normalized, err := NormalizeModelFallbacks(list)
	if err != nil {
		return err
	}
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		return ErrConfigUnavailable
	}
	cfg.ModelFallbacks = normalized
	return Save()
}

// NormalizeModelFallbacks trims IDs, drops empty and self-referencing
// entries and duplicates, and rejects malformed input.
func NormalizeModelFallbacks(list []ModelFallback) ([]ModelFallback, error) {
	if len(list) > maxModelFallbackEntries {
		return nil, fmt.Errorf("%w: at most %d models", ErrInvalidModelFallback, maxModelFallbackEntries)
	}
	out := make([]ModelFallback, 0, len(list))
	seen := map[string]bool{}
	for i, f := range list {
		model := strings.TrimSpace(f.Model)
		if model == "" {
			return nil, fmt.Errorf("%w at index %d: model is required", ErrInvalidModelFallback, i)
		}
		if !validModelMappingID(model) {
			return nil, fmt.Errorf("%w at index %d: invalid model ID", ErrInvalidModelFallback, i)
		}
		key := strings.ToLower(model)
		if seen[key] {
			return nil, fmt.Errorf("%w at index %d: duplicate model %q", ErrInvalidModelFallback, i, model)
		}
		seen[key] = true
		var fbs []string
		used := map[string]bool{key: true}
		for _, fb := range f.Fallbacks {
			fb = strings.TrimSpace(fb)
			if fb == "" || used[strings.ToLower(fb)] {
				continue
			}
			if !validModelMappingID(fb) {
				return nil, fmt.Errorf("%w at index %d: invalid fallback model ID", ErrInvalidModelFallback, i)
			}
			used[strings.ToLower(fb)] = true
			fbs = append(fbs, fb)
		}
		if len(fbs) == 0 {
			continue
		}
		if len(fbs) > maxFallbacksPerModel {
			return nil, fmt.Errorf("%w at index %d: at most %d fallbacks per model", ErrInvalidModelFallback, i, maxFallbacksPerModel)
		}
		out = append(out, ModelFallback{Model: model, Fallbacks: fbs})
	}
	return out, nil
}

// ---- Budgets ----

// BudgetSettings is the editable part of the budget configuration.
type BudgetSettings struct {
	DailyCreditLimit float64 `json:"dailyCreditLimit"`
	BudgetTimeZone   string  `json:"budgetTimeZone"`
	AlertWebhookURL  string  `json:"alertWebhookURL"`
	CreditPriceUSD   float64 `json:"creditPriceUsd"`
}

// DefaultCreditPriceUSD is the price of a credit included in Kiro's plans
// (Pro: $20 for 1,000 credits; Pro+ and Power are priced the same per credit).
const DefaultCreditPriceUSD = 0.02

// CreditPriceUSD is the configured price of one Kiro credit in dollars.
func CreditPriceUSD() float64 {
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if cfg == nil || cfg.CreditPriceUSD <= 0 {
		return DefaultCreditPriceUSD
	}
	return cfg.CreditPriceUSD
}

// GetBudgetSettings returns the current budget settings.
func GetBudgetSettings() BudgetSettings {
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if cfg == nil {
		return BudgetSettings{}
	}
	return BudgetSettings{
		DailyCreditLimit: cfg.DailyCreditLimit,
		BudgetTimeZone:   cfg.BudgetTimeZone,
		AlertWebhookURL:  cfg.AlertWebhookURL,
		CreditPriceUSD:   creditPriceOrDefault(cfg.CreditPriceUSD),
	}
}

func creditPriceOrDefault(v float64) float64 {
	if v <= 0 {
		return DefaultCreditPriceUSD
	}
	return v
}

// UpdateBudgetSettings validates and stores budget settings.
func UpdateBudgetSettings(s BudgetSettings) error {
	if s.DailyCreditLimit < 0 || math.IsNaN(s.DailyCreditLimit) || math.IsInf(s.DailyCreditLimit, 0) {
		return errors.New("dailyCreditLimit must be a non-negative number")
	}
	s.BudgetTimeZone = strings.TrimSpace(s.BudgetTimeZone)
	if s.BudgetTimeZone != "" {
		if _, err := time.LoadLocation(s.BudgetTimeZone); err != nil {
			return fmt.Errorf("unknown time zone %q", s.BudgetTimeZone)
		}
	}
	if s.CreditPriceUSD < 0 || s.CreditPriceUSD > 100 || math.IsNaN(s.CreditPriceUSD) {
		return errors.New("creditPriceUsd must be between 0 and 100")
	}
	s.AlertWebhookURL = strings.TrimSpace(s.AlertWebhookURL)
	if s.AlertWebhookURL != "" {
		u, err := url.Parse(s.AlertWebhookURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("alertWebhookURL must be an http(s) URL")
		}
	}
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		return ErrConfigUnavailable
	}
	cfg.DailyCreditLimit = s.DailyCreditLimit
	cfg.BudgetTimeZone = s.BudgetTimeZone
	cfg.AlertWebhookURL = s.AlertWebhookURL
	if s.CreditPriceUSD == DefaultCreditPriceUSD {
		s.CreditPriceUSD = 0 // keep following the default
	}
	cfg.CreditPriceUSD = s.CreditPriceUSD
	return Save()
}

// BudgetLocation is the time zone budget days follow (UTC by default or
// when the stored zone cannot be loaded).
func BudgetLocation() *time.Location {
	cfgLock.RLock()
	tz := ""
	if cfg != nil {
		tz = cfg.BudgetTimeZone
	}
	cfgLock.RUnlock()
	return loadBudgetLocation(tz)
}

// budgetLocations caches loaded zones: LoadLocation parses tzdata on every
// call and budget checks run on every request.
var budgetLocations sync.Map

func loadBudgetLocation(tz string) *time.Location {
	if tz == "" {
		return time.UTC
	}
	if v, ok := budgetLocations.Load(tz); ok {
		return v.(*time.Location)
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	budgetLocations.Store(tz, loc)
	return loc
}

// BudgetDay returns the budget day of t and when the next one starts.
func BudgetDay(t time.Time) (day string, next time.Time) {
	loc := BudgetLocation()
	lt := t.In(loc)
	next = time.Date(lt.Year(), lt.Month(), lt.Day()+1, 0, 0, 0, 0, loc)
	return lt.Format("2006-01-02"), next
}

// rollBudgetLocked returns today's state, starting a new day when needed.
// Caller holds cfgLock for writing.
func rollBudgetLocked(day string) *BudgetState {
	if cfg.BudgetState == nil || cfg.BudgetState.Day != day {
		cfg.BudgetState = &BudgetState{Day: day}
	}
	return cfg.BudgetState
}

// RecordDailyCredits adds credits to today's global and per-key counters.
// It only touches memory and arms the debounced save.
func RecordDailyCredits(keyID string, credits float64, now time.Time) {
	if credits <= 0 || math.IsNaN(credits) || math.IsInf(credits, 0) {
		return
	}
	day, _ := BudgetDay(now)
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		return
	}
	st := rollBudgetLocked(day)
	st.GlobalCredits += credits
	if keyID != "" {
		if st.KeyCredits == nil {
			st.KeyCredits = map[string]float64{}
		}
		st.KeyCredits[keyID] += credits
	}
	scheduleSaveLocked()
}

// DailyUsage is a snapshot of today's credit counters.
type DailyUsage struct {
	Day           string             `json:"day"`
	ResetsAt      time.Time          `json:"resetsAt"`
	GlobalCredits float64            `json:"globalCredits"`
	KeyCredits    map[string]float64 `json:"keyCredits"`
}

// GetDailyUsage returns today's counters (zero when the stored day is over).
func GetDailyUsage(now time.Time) DailyUsage {
	day, next := BudgetDay(now)
	out := DailyUsage{Day: day, ResetsAt: next, KeyCredits: map[string]float64{}}
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if cfg == nil || cfg.BudgetState == nil || cfg.BudgetState.Day != day {
		return out
	}
	out.GlobalCredits = cfg.BudgetState.GlobalCredits
	for k, v := range cfg.BudgetState.KeyCredits {
		out.KeyCredits[k] = v
	}
	return out
}

// BudgetBreach describes an exceeded cap.
type BudgetBreach struct {
	Scope    string // "global" or "key"
	Used     float64
	Limit    float64
	ResetsAt time.Time
}

// CheckDailyBudget reports whether a new request (for keyID, may be empty)
// would exceed the global or the key's daily credit cap. It only reads memory.
func CheckDailyBudget(keyID string, now time.Time) *BudgetBreach {
	day, next := BudgetDay(now)
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if cfg == nil {
		return nil
	}
	var global, key float64
	if st := cfg.BudgetState; st != nil && st.Day == day {
		global = st.GlobalCredits
		key = st.KeyCredits[keyID]
	}
	if cfg.DailyCreditLimit > 0 && global >= cfg.DailyCreditLimit {
		return &BudgetBreach{Scope: "global", Used: global, Limit: cfg.DailyCreditLimit, ResetsAt: next}
	}
	if keyID != "" {
		for i := range cfg.ApiKeys {
			if cfg.ApiKeys[i].ID == keyID {
				if l := cfg.ApiKeys[i].DailyCreditLimit; l > 0 && key >= l {
					return &BudgetBreach{Scope: "key", Used: key, Limit: l, ResetsAt: next}
				}
				break
			}
		}
	}
	return nil
}

// MarkAlertSent records that alert key was delivered today. It returns false
// when it had already been recorded (so each alert fires once per day).
func MarkAlertSent(key string, now time.Time) bool {
	day, _ := BudgetDay(now)
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		return false
	}
	st := rollBudgetLocked(day)
	if st.AlertsSent[key] {
		return false
	}
	if st.AlertsSent == nil {
		st.AlertsSent = map[string]bool{}
	}
	st.AlertsSent[key] = true
	scheduleSaveLocked()
	return true
}
