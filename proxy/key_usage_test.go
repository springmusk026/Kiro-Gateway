package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/telemetry"
)

func usageGet(h *Handler, header, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
	switch header {
	case "bearer":
		r.Header.Set("Authorization", "Bearer "+key)
	case "x-api-key":
		r.Header.Set("x-api-key", key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestKeyUsageEndpoint(t *testing.T) {
	mustInitConfig(t)
	mine, err := config.AddApiKey(config.ApiKeyEntry{Name: "laptop", Key: "sk-usage-mine-0123456789", Enabled: true,
		TokenLimit: 1000, CreditLimit: 5, DailyCreditLimit: 1, RPMLimit: 30, AllowedModels: []string{"claude-sonnet-*"}})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := config.AddApiKey(config.ApiKeyEntry{Name: "other", Key: "sk-usage-other-0123456789", Enabled: true})
	_ = config.RecordApiKeyUsage(mine.ID, 1500, 2) // over its token limit: usage must still be readable
	config.RecordDailyCredits(mine.ID, 0.75, time.Now())
	requireAuth(t)

	today := time.Now().In(config.BudgetLocation()).Format("2006-01-02")
	fh := &fakeHistory{
		recs: []telemetry.RequestRecord{
			{ID: "r1", Time: time.Now(), APIKeyID: mine.ID, Model: "claude-sonnet-4.5", Status: "success", HTTPStatus: 200,
				InputTokens: 10, OutputTokens: 5, AccountEmail: "secret@x.io", AccountID: "acc", RequestBody: "body", Error: "e"},
			{ID: "r2", Time: time.Now(), APIKeyID: other.ID, Model: "claude-opus-4.5"},
		},
		days: []telemetry.KeyUsageDay{{Day: today, Requests: 3, InputTokens: 100, OutputTokens: 20, Credits: 0.5}, {Day: "1999-01-01", Requests: 9}},
	}
	h := &Handler{logReader: fh}

	for _, hdr := range []string{"", "bearer"} {
		if w := usageGet(h, hdr, "sk-wrong"); w.Code != http.StatusUnauthorized {
			t.Fatalf("%q: want 401, got %d", hdr, w.Code)
		}
	}

	w := usageGet(h, "x-api-key", "sk-usage-mine-0123456789")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, leak := range []string{"secret@x.io", "acc", "body", "sk-usage-mine-0123456789", "claude-opus-4.5"} {
		if strings.Contains(body, `"`+leak+`"`) {
			t.Fatalf("response leaks %q: %s", leak, body)
		}
	}
	var got keyUsageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Key.Name != "laptop" || got.Key.KeyHint == "" || got.Limits.TokenLimit != 1000 || got.Limits.RPMLimit != 30 ||
		got.Limits.DailyCreditLimit != 1 || len(got.Limits.AllowedModels) != 1 || got.Period.Tokens != 1500 || got.Period.Requests != 1 {
		t.Fatalf("key/limits/period = %+v", got)
	}
	if !got.History || len(got.Daily) != keyUsageDays || got.Daily[keyUsageDays-1].Day != today ||
		got.Today.Requests != 3 || got.Today.Tokens != 120 || got.Today.DailyCredits != 0.75 {
		t.Fatalf("today/daily = %+v %+v", got.Today, got.Daily)
	}
	if len(got.Recent) != 1 || got.Recent[0].Model != "claude-sonnet-4.5" || fh.lastQuery.APIKeyID != mine.ID || fh.lastQuery.Limit != keyUsageRecent {
		t.Fatalf("recent = %+v query = %+v", got.Recent, fh.lastQuery)
	}

	// A disabled key is refused; without a database only the counters are shown.
	_ = config.UpdateApiKey(other.ID, config.ApiKeyEntry{Enabled: false})
	if w := usageGet(h, "bearer", "sk-usage-other-0123456789"); w.Code != http.StatusUnauthorized {
		t.Fatalf("disabled key: %d", w.Code)
	}
	w = usageGet(&Handler{}, "bearer", "sk-usage-mine-0123456789")
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.History || got.Today.Credits != 0.75 || len(got.Recent) != 0 {
		t.Fatalf("no database: %s", w.Body.String())
	}
}
