package proxy

import (
	"context"
	"net/http"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/telemetry"
)

// GET /v1/usage: self-service usage for the API key that sends the request.
//
// The key is read the same way as on the API (Bearer or x-api-key). Unlike
// the API, a key that is over its limits or expired can still read its usage
// (that is when its holder needs it); disabled and unknown keys get 401. The
// answer covers that key only and never includes bodies, accounts or emails.

const (
	keyUsageDays   = 14
	keyUsageRecent = 20
)

type keyUsageTotals struct {
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	Tokens       int64   `json:"tokens"`
	Credits      float64 `json:"credits"`
}

type keyUsageRequest struct {
	Time         time.Time `json:"time"`
	Endpoint     string    `json:"endpoint"`
	Model        string    `json:"model"`
	Status       string    `json:"status"`
	HTTPStatus   int       `json:"httpStatus"`
	InputTokens  int       `json:"inputTokens"`
	OutputTokens int       `json:"outputTokens"`
	Credits      float64   `json:"credits"`
	DurationMs   int64     `json:"durationMs"`
}

type keyUsageResponse struct {
	Key struct {
		Name       string `json:"name"`
		KeyMasked  string `json:"keyMasked"`
		Enabled    bool   `json:"enabled"`
		CreatedAt  int64  `json:"createdAt"`
		LastUsedAt int64  `json:"lastUsedAt,omitempty"`
		ExpiresAt  int64  `json:"expiresAt,omitempty"`
		Expired    bool   `json:"expired"`
	} `json:"key"`
	Limits struct {
		TokenLimit       int64    `json:"tokenLimit"`
		CreditLimit      float64  `json:"creditLimit"`
		DailyCreditLimit float64  `json:"dailyCreditLimit"`
		RPMLimit         int      `json:"rpmLimit"`
		AllowedModels    []string `json:"allowedModels"`
	} `json:"limits"`
	// Period is the key's running total since its usage was last reset.
	Period struct {
		Requests int64   `json:"requests"`
		Tokens   int64   `json:"tokens"`
		Credits  float64 `json:"credits"`
	} `json:"period"`
	Today struct {
		keyUsageTotals
		Day          string    `json:"day"`
		ResetsAt     time.Time `json:"resetsAt"`
		DailyCredits float64   `json:"dailyCredits"` // the counter the daily cap is checked against
	} `json:"today"`
	TimeZone string                  `json:"timeZone"`
	History  bool                    `json:"history"` // daily and recent come from the request log (database backend)
	Daily    []telemetry.KeyUsageDay `json:"daily"`
	Recent   []keyUsageRequest       `json:"recent"`
}

func (h *Handler) handleKeyUsage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		h.sendAPIMethodNotAllowed(w, r, "GET")
		return
	}
	provided := extractProvidedKey(r)
	entry := config.FindApiKeyByValue(provided)
	if provided == "" || entry == nil {
		h.sendAPIError(w, r, http.StatusUnauthorized, "authentication_error", "authentication_error", "", "Invalid or missing API key")
		return
	}
	if !entry.Enabled {
		h.sendAPIError(w, r, http.StatusUnauthorized, "authentication_error", "authentication_error", "", "API key disabled")
		return
	}
	now := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	writeJSON(w, h.keyUsage(ctx, *entry, now))
}

func (h *Handler) keyUsage(ctx context.Context, e config.ApiKeyEntry, now time.Time) *keyUsageResponse {
	out := &keyUsageResponse{}
	out.Key.Name = e.Name
	out.Key.KeyMasked = config.MaskApiKey(e.Key)
	out.Key.Enabled = e.Enabled
	out.Key.CreatedAt = e.CreatedAt
	out.Key.LastUsedAt = e.LastUsedAt
	out.Key.ExpiresAt = e.ExpiresAt
	out.Key.Expired = config.ApiKeyExpired(e, now)
	out.Limits.TokenLimit = e.TokenLimit
	out.Limits.CreditLimit = e.CreditLimit
	out.Limits.DailyCreditLimit = e.DailyCreditLimit
	out.Limits.RPMLimit = e.RPMLimit
	out.Limits.AllowedModels = append([]string{}, e.AllowedModels...)
	out.Period.Requests = e.RequestsCount
	out.Period.Tokens = e.TokensUsed
	out.Period.Credits = e.CreditsUsed

	daily := config.GetDailyUsage(now)
	out.Today.Day = daily.Day
	out.Today.ResetsAt = daily.ResetsAt
	out.Today.DailyCredits = daily.KeyCredits[e.ID]
	loc := config.BudgetLocation()
	out.TimeZone = loc.String()
	out.Daily = []telemetry.KeyUsageDay{}
	out.Recent = []keyUsageRequest{}

	// Days (in the budget time zone) from 13 days ago through today.
	lt := now.In(loc)
	first := time.Date(lt.Year(), lt.Month(), lt.Day()-(keyUsageDays-1), 0, 0, 0, 0, loc)
	days := make([]telemetry.KeyUsageDay, keyUsageDays)
	for i := range days {
		days[i].Day = first.AddDate(0, 0, i).Format("2006-01-02")
	}

	kr, ok := h.logReader.(telemetry.KeyUsageReader)
	if !ok || h.logReader == nil || e.ID == "" {
		out.Today.Credits = out.Today.DailyCredits
		return out
	}
	out.History = true
	if rows, err := kr.KeyDailyUsage(ctx, e.ID, first, loc); err == nil {
		idx := make(map[string]int, len(days))
		for i, d := range days {
			idx[d.Day] = i
		}
		for _, d := range rows {
			if i, ok := idx[d.Day]; ok {
				days[i] = d
			}
		}
	}
	out.Daily = days
	if t := days[len(days)-1]; t.Day == daily.Day {
		out.Today.Requests = t.Requests
		out.Today.InputTokens = t.InputTokens
		out.Today.OutputTokens = t.OutputTokens
		out.Today.Tokens = t.InputTokens + t.OutputTokens
		out.Today.Credits = t.Credits
	}
	if recs, err := h.logReader.QueryRequests(ctx, telemetry.LogFilter{APIKeyID: e.ID, Limit: keyUsageRecent}); err == nil {
		for _, x := range recs {
			out.Recent = append(out.Recent, keyUsageRequest{
				Time: x.Time, Endpoint: x.Endpoint, Model: x.Model, Status: x.Status, HTTPStatus: x.HTTPStatus,
				InputTokens: x.InputTokens, OutputTokens: x.OutputTokens, Credits: x.Credits, DurationMs: x.DurationMs,
			})
		}
	}
	return out
}
