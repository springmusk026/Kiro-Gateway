package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"kiro-go/config"
	"kiro-go/logger"
	"kiro-go/pool"
)

// costControls holds the runtime state of usage refresh, budgets and alerts.
type costControls struct {
	// usageForced: accountID → time of the last forced usage refresh request;
	// usageForcedPending: accountID → true while a forced refresh is queued.
	usageForced        sync.Map
	usageForcedPending sync.Map
}

const alertEvalInterval = 30 * time.Second

// startCostBackground runs the usage refresh and alert loops until
// stopRefresh is closed.
func (h *Handler) startCostBackground() {
	go h.usageRefreshLoop()
	go h.alertLoop()
}

// ---- Budget enforcement ----

// budgetEnforced reports whether path is a chat endpoint that spends credits.
func budgetEnforced(path string) bool {
	switch endpointForPath(path) {
	case "claude", "openai", "responses":
		return true
	}
	return false
}

// rejectOverBudget answers 429 when the global or the key's daily credit cap
// is reached. It reads memory only, so it is cheap enough for every request.
func (h *Handler) rejectOverBudget(w http.ResponseWriter, r *http.Request, entry *config.ApiKeyEntry, anthropic bool) bool {
	if r == nil || !budgetEnforced(r.URL.Path) {
		return false
	}
	keyID := ""
	if entry != nil {
		keyID = entry.ID
	}
	b := config.CheckDailyBudget(keyID, time.Now())
	if b == nil {
		return false
	}
	scope := "The gateway's daily credit budget"
	if b.Scope == "key" {
		scope = "This API key's daily credit budget"
	}
	msg := fmt.Sprintf("%s is exhausted (%s of %s credits used); it resets at %s.",
		scope, formatCredits(b.Used), formatCredits(b.Limit), b.ResetsAt.UTC().Format(time.RFC3339))
	setRetryAfterHeader(w, time.Until(b.ResetsAt))
	if tr := traceFrom(r.Context()); tr != nil {
		tr.setFailure("", "", msg, "budget")
	}
	if anthropic {
		h.sendClaudeError(w, http.StatusTooManyRequests, "rate_limit_error", msg)
	} else {
		h.sendOpenAIErrorWithCode(w, http.StatusTooManyRequests, "insufficient_quota", "insufficient_quota", msg)
	}
	return true
}

func formatCredits(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

// ---- Pool usage ----

// poolUsage summarises the enabled accounts' usage for the current period.
type poolUsage struct {
	Accounts     int     `json:"accounts"`     // enabled accounts
	WithLimit    int     `json:"withLimit"`    // accounts reporting a limit
	Saturated    int     `json:"saturated"`    // at or above the routing threshold
	Used         float64 `json:"used"`         // credits used (reported + local estimate)
	Limit        float64 `json:"limit"`        // sum of limits
	Remaining    float64 `json:"remaining"`    // sum of per-account headroom
	Ratio        float64 `json:"ratio"`        // Used / Limit
	Threshold    float64 `json:"threshold"`    // usage routing threshold
	NextResetDay string  `json:"nextResetDay"` // earliest account reset date
}

func (h *Handler) computePoolUsage() poolUsage {
	out := poolUsage{Threshold: config.GetUsageRoutingThreshold()}
	for _, acc := range config.GetEnabledAccounts() {
		out.Accounts++
		used, limit := pool.AccountUsage(&acc)
		if limit <= 0 {
			continue
		}
		if h.pool != nil {
			used += h.pool.LocalUsageCredits(acc.ID, acc.LastRefresh)
		}
		out.WithLimit++
		out.Used += used
		out.Limit += limit
		out.Remaining += math.Max(0, limit-used)
		if used/limit >= out.Threshold {
			out.Saturated++
		}
		if acc.NextResetDate != "" && (out.NextResetDay == "" || acc.NextResetDate < out.NextResetDay) {
			out.NextResetDay = acc.NextResetDate
		}
	}
	if out.Limit > 0 {
		out.Ratio = out.Used / out.Limit
	}
	return out
}

// ---- Alerts ----

// costAlert is one active budget or usage alert.
type costAlert struct {
	Kind    string  `json:"kind"`  // global_daily | key_daily | key_credits | key_tokens | pool_usage
	Level   string  `json:"level"` // warning | critical
	Subject string  `json:"subject,omitempty"`
	Name    string  `json:"name,omitempty"`
	Value   float64 `json:"value"`
	Limit   float64 `json:"limit"`
	Text    string  `json:"text"`
}

const (
	alertWarnRatio     = 0.8
	alertPoolWarnRatio = 0.9
)

func alertLevel(value, limit, warn float64) string {
	if limit <= 0 {
		return ""
	}
	switch r := value / limit; {
	case r >= 1:
		return "critical"
	case r >= warn:
		return "warning"
	}
	return ""
}

// computeAlerts lists the caps currently at 80 % or more (pool usage: 90 %).
func (h *Handler) computeAlerts(now time.Time) []costAlert {
	var out []costAlert
	add := func(kind, subject, name string, value, limit, warn float64, what string) {
		level := alertLevel(value, limit, warn)
		if level == "" {
			return
		}
		state := "reached"
		if level == "warning" {
			state = fmt.Sprintf("at %.0f%%", value/limit*100)
		}
		out = append(out, costAlert{
			Kind: kind, Level: level, Subject: subject, Name: name, Value: value, Limit: limit,
			Text: fmt.Sprintf("Kiro-Go: %s %s (%s of %s).", what, state, trimNumber(value), trimNumber(limit)),
		})
	}
	daily := config.GetDailyUsage(now)
	budget := config.GetBudgetSettings()
	add("global_daily", "", "", daily.GlobalCredits, budget.DailyCreditLimit, alertWarnRatio, "daily credit budget")
	for _, k := range config.ListApiKeys() {
		name := k.Name
		if name == "" {
			name = config.MaskApiKey(k.Key)
		}
		add("key_daily", k.ID, name, daily.KeyCredits[k.ID], k.DailyCreditLimit, alertWarnRatio, fmt.Sprintf("API key %q daily credit budget", name))
		add("key_credits", k.ID, name, k.CreditsUsed, k.CreditLimit, alertWarnRatio, fmt.Sprintf("API key %q credit limit", name))
		add("key_tokens", k.ID, name, float64(k.TokensUsed), float64(k.TokenLimit), alertWarnRatio, fmt.Sprintf("API key %q token limit", name))
	}
	pu := h.computePoolUsage()
	add("pool_usage", "", "", pu.Used, pu.Limit, alertPoolWarnRatio, "account pool usage for this period")
	sort.SliceStable(out, func(i, j int) bool { return out[i].Level == "critical" && out[j].Level != "critical" })
	return out
}

func trimNumber(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.2f", v)
}

func (h *Handler) alertLoop() {
	ticker := time.NewTicker(alertEvalInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			h.dispatchAlerts(time.Now())
		case <-h.stopRefresh:
			return
		}
	}
}

// dispatchAlerts posts each newly active alert to the webhook once per
// budget day and level. Delivery is asynchronous with a timeout.
func (h *Handler) dispatchAlerts(now time.Time) int {
	url := config.GetBudgetSettings().AlertWebhookURL
	if url == "" {
		return 0
	}
	sent := 0
	for _, a := range h.computeAlerts(now) {
		if !config.MarkAlertSent(a.Kind+"|"+a.Subject+"|"+a.Level, now) {
			continue
		}
		sent++
		go postAlertWebhook(url, a)
	}
	return sent
}

// alertWebhookClient is swapped in tests.
var alertWebhookClient = &http.Client{Timeout: 10 * time.Second}

// alertWebhookPayload is Slack/Discord compatible through its text field.
func alertWebhookPayload(a costAlert) map[string]interface{} {
	p := map[string]interface{}{
		"text":    a.Text,
		"content": a.Text, // Discord reads "content"
		"kind":    a.Kind,
		"level":   a.Level,
		"value":   a.Value,
		"limit":   a.Limit,
	}
	if a.Subject != "" {
		p["subject"] = a.Subject
	}
	return p
}

func postAlertWebhook(url string, a costAlert) error {
	body, _ := json.Marshal(alertWebhookPayload(a))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "kiro-go/"+config.Version)
	resp, err := alertWebhookClient.Do(req)
	if err != nil {
		logger.Warnf("[Alerts] webhook delivery failed: %v", err)
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		logger.Warnf("[Alerts] webhook answered HTTP %d", resp.StatusCode)
		return fmt.Errorf("webhook answered HTTP %d", resp.StatusCode)
	}
	return nil
}
