package proxy

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
)

// routeCostAdmin serves the cost-control admin API:
//
//	GET  /model-fallbacks        fallback lists
//	POST /model-fallbacks        {"fallbacks":[{"model","fallbacks":[...]}]}
//	GET  /budgets                settings, today's usage, pool usage, alerts
//	POST /budgets                partial update of the budget settings
//	POST /budgets/test-webhook   sends a test alert to the configured webhook
//	GET  /alerts                 active alerts (console banners)
func (h *Handler) routeCostAdmin(w http.ResponseWriter, r *http.Request, path string) bool {
	switch {
	case path == "/model-fallbacks" && r.Method == http.MethodGet:
		writeCostJSON(w, http.StatusOK, map[string]interface{}{"fallbacks": config.GetModelFallbacks()})
	case path == "/model-fallbacks" && r.Method == http.MethodPost:
		h.apiUpdateModelFallbacks(w, r)
	case path == "/budgets" && r.Method == http.MethodGet:
		writeCostJSON(w, http.StatusOK, h.budgetsView())
	case path == "/budgets" && r.Method == http.MethodPost:
		h.apiUpdateBudgets(w, r)
	case path == "/budgets/test-webhook" && r.Method == http.MethodPost:
		h.apiTestAlertWebhook(w, r)
	case path == "/alerts" && r.Method == http.MethodGet:
		writeCostJSON(w, http.StatusOK, map[string]interface{}{"alerts": nonNilAlerts(h.computeAlerts(time.Now()))})
	default:
		return false
	}
	return true
}

func writeCostJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func nonNilAlerts(a []costAlert) []costAlert {
	if a == nil {
		return []costAlert{}
	}
	return a
}

func (h *Handler) apiUpdateModelFallbacks(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Fallbacks *[]config.ModelFallback `json:"fallbacks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Fallbacks == nil {
		writeCostJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON: expected {\"fallbacks\": [...]}"})
		return
	}
	if err := config.UpdateModelFallbacks(*req.Fallbacks); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, config.ErrInvalidModelFallback) {
			status = http.StatusBadRequest
		}
		writeCostJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeCostJSON(w, http.StatusOK, map[string]interface{}{"success": true, "fallbacks": config.GetModelFallbacks()})
}

type budgetsResponse struct {
	config.BudgetSettings
	UsageRoutingThreshold float64           `json:"usageRoutingThreshold"`
	ThresholdFromEnv      bool              `json:"thresholdFromEnv"`
	Today                 config.DailyUsage `json:"today"`
	Pool                  poolUsage         `json:"pool"`
	Alerts                []costAlert       `json:"alerts"`
}

func (h *Handler) budgetsView() budgetsResponse {
	now := time.Now()
	return budgetsResponse{
		BudgetSettings:        config.GetBudgetSettings(),
		UsageRoutingThreshold: config.GetUsageRoutingThreshold(),
		ThresholdFromEnv:      config.UsageThresholdFromEnv(),
		Today:                 config.GetDailyUsage(now),
		Pool:                  h.computePoolUsage(),
		Alerts:                nonNilAlerts(h.computeAlerts(now)),
	}
}

func (h *Handler) apiUpdateBudgets(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DailyCreditLimit      *float64 `json:"dailyCreditLimit"`
		BudgetTimeZone        *string  `json:"budgetTimeZone"`
		AlertWebhookURL       *string  `json:"alertWebhookURL"`
		UsageRoutingThreshold *float64 `json:"usageRoutingThreshold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCostJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}
	s := config.GetBudgetSettings()
	if req.DailyCreditLimit != nil {
		s.DailyCreditLimit = *req.DailyCreditLimit
	}
	if req.BudgetTimeZone != nil {
		s.BudgetTimeZone = *req.BudgetTimeZone
	}
	if req.AlertWebhookURL != nil {
		s.AlertWebhookURL = *req.AlertWebhookURL
	}
	if err := config.UpdateBudgetSettings(s); err != nil {
		writeCostJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.UsageRoutingThreshold != nil {
		if err := config.UpdateUsageRoutingThreshold(*req.UsageRoutingThreshold); err != nil {
			writeCostJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	writeCostJSON(w, http.StatusOK, h.budgetsView())
}

func (h *Handler) apiTestAlertWebhook(w http.ResponseWriter, r *http.Request) {
	url := config.GetBudgetSettings().AlertWebhookURL
	if url == "" {
		writeCostJSON(w, http.StatusBadRequest, map[string]string{"error": "No alert webhook URL is configured"})
		return
	}
	err := postAlertWebhook(url, costAlert{
		Kind: "test", Level: "warning",
		Text: "Kiro Gateway: test alert. Budget and usage alerts will be posted here.",
	})
	if err != nil {
		writeCostJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeCostJSON(w, http.StatusOK, map[string]bool{"success": true})
}
