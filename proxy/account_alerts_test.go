package proxy

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/springmusk026/Kiro-Gateway/auth"
	"github.com/springmusk026/Kiro-Gateway/config"
	accountpool "github.com/springmusk026/Kiro-Gateway/pool"
)

// accountAlertHandler sets up a fresh config with the given accounts and a
// private pool over them.
func accountAlertHandler(t *testing.T, accounts ...config.Account) *Handler {
	t.Helper()
	if err := config.InitForTest(t, filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatal(err)
	}
	for _, a := range accounts {
		if err := config.AddAccount(a); err != nil {
			t.Fatal(err)
		}
	}
	auth.ResetRefreshHealthForTest()
	t.Cleanup(auth.ResetRefreshHealthForTest)
	return &Handler{pool: accountpool.NewTestPool(config.GetEnabledAccounts())}
}

func alertsByKind(h *Handler, now time.Time) map[string]costAlert {
	out := map[string]costAlert{}
	for _, a := range h.computeAccountAlerts(now) {
		out[a.Kind+"|"+a.Subject] = a
	}
	return out
}

func oauthAccount(id string) config.Account {
	return config.Account{ID: id, Email: id + "@x.io", Enabled: true, AuthMethod: "social",
		AccessToken: "at-" + id, RefreshToken: "rt-" + id, ExpiresAt: time.Now().Add(time.Hour).Unix()}
}

func TestAccountAlertHealthyPoolIsQuiet(t *testing.T) {
	h := accountAlertHandler(t, oauthAccount("ok"))
	if got := h.computeAccountAlerts(time.Now()); len(got) != 0 {
		t.Fatalf("healthy account raised %v", got)
	}
	// A successful refresh history keeps it quiet too.
	auth.RecordRefreshResultForTest("ok", nil, time.Now())
	if got := h.computeAccountAlerts(time.Now()); len(got) != 0 {
		t.Fatalf("after success: %v", got)
	}
}

func TestAccountAlertTokenExpiring(t *testing.T) {
	soon := oauthAccount("soon")
	soon.ExpiresAt = time.Now().Add(3 * time.Hour).Unix()
	noRT := oauthAccount("nort")
	noRT.RefreshToken = ""
	gone := oauthAccount("gone")
	gone.ExpiresAt = time.Now().Add(-time.Minute).Unix()
	later := oauthAccount("later")
	later.ExpiresAt = time.Now().Add(48 * time.Hour).Unix()
	later.RefreshToken = ""
	h := accountAlertHandler(t, soon, noRT, gone, later)
	now := time.Now()

	// Expiring with a refresh token and no failed refresh: nothing yet.
	if a, ok := alertsByKind(h, now)["account_token_expiring|soon"]; ok {
		t.Fatalf("refreshable token alerted: %+v", a)
	}
	auth.RecordRefreshResultForTest("soon", errors.New("HTTP 500"), now)
	auth.RecordRefreshResultForTest("gone", errors.New("HTTP 500"), now)
	got := alertsByKind(h, now)
	if a := got["account_token_expiring|soon"]; a.Level != "warning" || !strings.Contains(a.Text, "expires in 3h") {
		t.Fatalf("soon: %+v", a)
	}
	if a := got["account_token_expiring|nort"]; a.Level != "warning" {
		t.Fatalf("no refresh token: %+v", a)
	}
	if a := got["account_token_expiring|gone"]; a.Level != "critical" || !strings.Contains(a.Text, "has expired") {
		t.Fatalf("expired: %+v", a)
	}
	if _, ok := got["account_token_expiring|later"]; ok {
		t.Fatal("token expiring in 48 h alerted")
	}
	// A later successful refresh clears it.
	auth.RecordRefreshResultForTest("soon", nil, now)
	if _, ok := alertsByKind(h, now)["account_token_expiring|soon"]; ok {
		t.Fatal("alert stayed after a successful refresh")
	}
}

func TestAccountAlertRefreshFailing(t *testing.T) {
	h := accountAlertHandler(t, oauthAccount("f"))
	now := time.Now()
	for i := 0; i < 2; i++ {
		auth.RecordRefreshResultForTest("f", errors.New("invalid_grant"), now)
	}
	if _, ok := alertsByKind(h, now)["account_refresh_failing|f"]; ok {
		t.Fatal("2 failures alerted")
	}
	auth.RecordRefreshResultForTest("f", errors.New("invalid_grant"), now)
	a := alertsByKind(h, now)["account_refresh_failing|f"]
	if a.Level != "critical" || a.Value != 3 || !strings.Contains(a.Text, "invalid_grant") {
		t.Fatalf("3 failures: %+v", a)
	}
	auth.RecordRefreshResultForTest("f", nil, now)
	if _, ok := alertsByKind(h, now)["account_refresh_failing|f"]; ok {
		t.Fatal("success did not reset the streak")
	}
}

func TestAccountAlertBanned(t *testing.T) {
	banned := oauthAccount("b")
	h := accountAlertHandler(t, banned, oauthAccount("ok"))
	if err := config.SetAccountBanStatus("b", "SUSPENDED", "unusual activity"); err != nil {
		t.Fatal(err)
	}
	a := alertsByKind(h, time.Now())["account_banned|b"]
	if a.Level != "critical" || !strings.Contains(a.Text, "suspended (unusual activity)") {
		t.Fatalf("banned: %+v", a)
	}
}

func TestAccountAlertLongCooldownAndPoolUnavailable(t *testing.T) {
	h := accountAlertHandler(t, oauthAccount("p"), oauthAccount("q"))
	h.pool.ParkUntil("p", time.Now().Add(5*time.Hour))
	h.pool.SetCooldownForTest("q", 10*time.Minute)
	got := alertsByKind(h, time.Now())
	if a := got["account_cooling|p"]; a.Level != "warning" || !strings.Contains(a.Text, "another 5h") {
		t.Fatalf("parked: %+v", a)
	}
	if _, ok := got["account_cooling|q"]; ok {
		t.Fatal("short throttle alerted")
	}
	if a := got["accounts_unavailable|"]; a.Level != "critical" {
		t.Fatalf("pool unavailable: %v", got)
	}
	// One account back: the pool alert clears.
	h.pool.RecordSuccess("q")
	if _, ok := alertsByKind(h, time.Now())["accounts_unavailable|"]; ok {
		t.Fatal("pool alert with an available account")
	}
}

// Account alerts go through the budget alerts' webhook and are posted once
// per budget day and level.
func TestAccountAlertsWebhookDedup(t *testing.T) {
	h := accountAlertHandler(t, oauthAccount("w"))
	var posts atomic.Int32
	var bodies atomic.Value
	bodies.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		for {
			old := bodies.Load().(string)
			if bodies.CompareAndSwap(old, old+string(buf[:n])+"\n") {
				break
			}
		}
		posts.Add(1)
	}))
	defer srv.Close()
	old := alertWebhookClient
	alertWebhookClient = &http.Client{Timeout: time.Second, Transport: &http.Transport{}}
	defer func() { alertWebhookClient = old }()
	if err := config.UpdateBudgetSettings(config.BudgetSettings{AlertWebhookURL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < 3; i++ {
		auth.RecordRefreshResultForTest("w", errors.New("HTTP 400"), now)
	}
	// Refresh failing (3 in a row) and, since the last refresh failed, the
	// token expiring within the hour: two alerts, each posted once.
	if n := h.dispatchAlerts(now); n != 2 {
		t.Fatalf("first evaluation sent %d, want 2", n)
	}
	if n := h.dispatchAlerts(now.Add(30 * time.Second)); n != 0 {
		t.Fatalf("repeat evaluation sent %d, want 0", n)
	}
	deadline := time.Now().Add(2 * time.Second)
	for posts.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if posts.Load() != 2 {
		t.Fatalf("webhook got %d posts", posts.Load())
	}
	if body := bodies.Load().(string); !strings.Contains(body, `"kind":"account_refresh_failing"`) ||
		!strings.Contains(body, `"kind":"account_token_expiring"`) || !strings.Contains(body, `"subject":"w"`) {
		t.Fatalf("payload %s", body)
	}
	// The alerts endpoint lists it.
	rec := httptest.NewRecorder()
	h.routeCostAdmin(rec, httptest.NewRequest(http.MethodGet, "/admin/api/alerts", nil), "/alerts")
	if !strings.Contains(rec.Body.String(), "account_refresh_failing") {
		t.Fatalf("/alerts: %s", rec.Body.String())
	}
}
