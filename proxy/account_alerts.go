package proxy

import (
	"fmt"
	"strings"
	"time"

	"github.com/springmusk026/Kiro-Gateway/auth"
	"github.com/springmusk026/Kiro-Gateway/config"
)

// Account health alerts. They are computed from in-memory state only (the
// configuration, the pool's cooldowns and the refresh outcomes recorded by
// auth.RefreshToken) and share the budget alerts' banner, webhook and
// once-per-day-and-level dedup (computeAlerts / dispatchAlerts).
const (
	alertTokenExpiryWindow     = 24 * time.Hour
	alertRefreshFailuresInARow = 3
	alertLongCooldown          = time.Hour
)

func accountAlertName(a *config.Account) string {
	for _, s := range []string{a.Nickname, a.Email, a.ID} {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return "unnamed"
}

func isBannedStatus(s string) bool {
	s = strings.ToUpper(strings.TrimSpace(s))
	return s != "" && s != "ACTIVE"
}

// computeAccountAlerts lists the account events that need attention:
//
//	account_banned          ban status BANNED / SUSPENDED (critical)
//	account_refresh_failing 3+ token refreshes failed in a row (critical)
//	account_token_expiring  token expires within 24 h (critical once expired) and
//	                        the last refresh failed or there is no refresh token
//	account_cooling         cooling down or parked for more than another hour
//	accounts_unavailable    no account can take a request right now (critical)
func (h *Handler) computeAccountAlerts(now time.Time) []costAlert {
	var out []costAlert
	add := func(kind, level, subject, name string, value, limit float64, text string) {
		out = append(out, costAlert{Kind: kind, Level: level, Subject: subject, Name: name,
			Value: value, Limit: limit, Text: "Kiro Gateway: " + text})
	}
	var cooling map[string]time.Time
	if h.pool != nil {
		cooling = h.pool.CooldownsUntil()
	}
	accounts := config.GetAccounts()
	for i := range accounts {
		a := &accounts[i]
		name := accountAlertName(a)
		if isBannedStatus(a.BanStatus) {
			text := fmt.Sprintf("account %q is %s", name, strings.ToLower(a.BanStatus))
			if r := strings.TrimSpace(a.BanReason); r != "" {
				text += " (" + r + ")"
			}
			add("account_banned", "critical", a.ID, name, 1, 1, text+".")
			continue
		}
		if !a.Enabled {
			continue
		}
		if !config.IsAPIKeyAccount(a) {
			rh := auth.GetRefreshHealth(a.ID)
			if rh.ConsecutiveFailures >= alertRefreshFailuresInARow {
				add("account_refresh_failing", "critical", a.ID, name, float64(rh.ConsecutiveFailures), alertRefreshFailuresInARow,
					fmt.Sprintf("token refresh for account %q failed %d times in a row (last error: %s).",
						name, rh.ConsecutiveFailures, truncateAlertText(rh.LastError, 160)))
			}
			if a.ExpiresAt > 0 {
				left := time.Unix(a.ExpiresAt, 0).Sub(now)
				noRefresh := strings.TrimSpace(a.RefreshToken) == "" || rh.ConsecutiveFailures > 0
				if left <= alertTokenExpiryWindow && noRefresh {
					level, state := "warning", fmt.Sprintf("expires in %s", roundAlertDuration(left))
					if left <= 0 {
						level, state = "critical", "has expired"
					}
					add("account_token_expiring", level, a.ID, name, left.Hours(), alertTokenExpiryWindow.Hours(),
						fmt.Sprintf("the token of account %q %s and has not been refreshed.", name, state))
				}
			}
		}
		if until, ok := cooling[a.ID]; ok {
			if left := until.Sub(now); left > alertLongCooldown {
				add("account_cooling", "warning", a.ID, name, left.Hours(), alertLongCooldown.Hours(),
					fmt.Sprintf("account %q is cooling down or parked for another %s (until %s).",
						name, roundAlertDuration(left), until.UTC().Format(time.RFC3339)))
			}
		}
	}
	if h.pool != nil && len(accounts) > 0 && h.pool.AvailableCount() == 0 {
		add("accounts_unavailable", "critical", "", "", 0, float64(len(accounts)),
			"no account is available: every account is disabled, banned, out of quota or cooling down.")
	}
	return out
}

func roundAlertDuration(d time.Duration) string {
	if d >= time.Hour {
		return fmt.Sprintf("%dh", int(d.Round(time.Hour)/time.Hour))
	}
	return fmt.Sprintf("%dm", int(d.Round(time.Minute)/time.Minute))
}

func truncateAlertText(s string, n int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
