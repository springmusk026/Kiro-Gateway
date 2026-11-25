package proxy

import (
	"fmt"
	"github.com/springmusk026/Kiro-Gateway/auth"
	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/logger"
	"strings"
	"sync"
	"time"
)

const tokenRefreshSkewSeconds int64 = 120

// backgroundRefresh 后台定时刷新账户信息
func (h *Handler) backgroundRefresh() {
	ticker := time.NewTicker(30 * time.Minute) // 每 30 分钟刷新一次
	defer ticker.Stop()

	// 启动时延迟 10 秒后执行一次
	time.Sleep(10 * time.Second)
	h.refreshModelsCache()
	h.refreshAllAccounts()

	for {
		select {
		case <-ticker.C:
			h.modelsRefreshMu.Lock()
			h.refreshModelsCache()
			h.modelsRefreshMu.Unlock()
			h.refreshAllAccounts()
		case <-h.stopRefresh:
			return
		}
	}
}

// refreshAllAccounts 刷新所有账户信息
func (h *Handler) refreshAllAccounts() {
	accounts := config.GetAccounts()
	for i := range accounts {
		account := &accounts[i]
		if !account.Enabled {
			continue
		}
		if accountBearerToken(account) == "" {
			continue
		}

		// API Key accounts skip OAuth refresh; still sync usage/subscription.
		if !config.IsAPIKeyAccount(account) {
			// 检查 token 是否需要刷新
			if tokenNeedsRefresh(account) {
				if _, err := h.refreshAccountToken(account, false); err != nil {
					logger.Warnf("[BackgroundRefresh] Token refresh failed for %s: %v", account.Email, err)
					h.handleAccountFailure(account, err)
					continue
				}
			}
		}

		// 刷新账户信息
		info, err := RefreshAccountInfo(account)
		if err != nil {
			logger.Warnf("[BackgroundRefresh] Failed to refresh %s: %v", account.Email, err)
			continue
		}

		config.UpdateAccountInfo(account.ID, *info)
		logger.Infof("[BackgroundRefresh] Refreshed %s: %s %.1f/%.1f", account.Email, info.SubscriptionType, info.UsageCurrent, info.UsageLimit)
	}
	h.pool.Reload()
}

// refreshAccountToken serializes the complete refresh-token rotation lifecycle:
// load the latest persisted credential, refresh it, persist any rotation, and
// only then publish it to the runtime pool. A single lock is intentionally used
// across accounts because refreshes are rare and this keeps every refresh entry
// point consistent.
func (h *Handler) refreshAccountToken(account *config.Account, force bool) (bool, error) {
	if account == nil || strings.TrimSpace(account.ID) == "" {
		return false, fmt.Errorf("account is required for token refresh")
	}

	// Per-account lock: one slow identity provider must not block token
	// refresh for every other account. Holding it across re-reading the stored
	// account gives double-checked refresh: a waiter sees the fresh token.
	unlock := h.lockAccountRefresh(account.ID)
	defer unlock()

	var latest *config.Account
	accounts := config.GetAccounts()
	for i := range accounts {
		if accounts[i].ID == account.ID {
			latest = &accounts[i]
			break
		}
	}
	if latest == nil {
		return false, fmt.Errorf("account %s no longer exists", account.ID)
	}
	working := *latest

	// API Key credentials never expire and cannot be OAuth-refreshed.
	if config.IsAPIKeyAccount(&working) {
		token := strings.TrimSpace(working.KiroApiKey)
		if token == "" {
			token = strings.TrimSpace(working.AccessToken)
		}
		if token == "" {
			return false, fmt.Errorf("account %s has no kiroApiKey", working.ID)
		}
		h.pool.UpdateCredentialState(
			account,
			working.ID,
			token,
			"",
			0,
			"",
		)
		return false, nil
	}

	if !force && !tokenNeedsRefresh(&working) {
		h.pool.UpdateCredentialState(
			account,
			working.ID,
			working.AccessToken,
			working.RefreshToken,
			working.ExpiresAt,
			working.ProfileArn,
		)
		return false, nil
	}
	if strings.TrimSpace(working.RefreshToken) == "" {
		return false, fmt.Errorf("account %s has no refresh token", working.ID)
	}

	accessToken, refreshToken, expiresAt, profileArn, err := auth.RefreshToken(&working)
	if err != nil {
		return false, err
	}
	if refreshToken == "" {
		refreshToken = working.RefreshToken
	}

	if err := config.UpdateAccountCredentialState(
		working.ID,
		accessToken,
		refreshToken,
		expiresAt,
		profileArn,
	); err != nil {
		return false, fmt.Errorf("persist refreshed token for account %s: %w", working.ID, err)
	}

	// Do not expose a rotated credential through the pool until persistence has
	// succeeded. This ordering prevents a later refresh from reading stale state.
	h.pool.UpdateCredentialState(
		account,
		working.ID,
		accessToken,
		refreshToken,
		expiresAt,
		profileArn,
	)
	return true, nil
}

// ensureValidToken 确保 token 有效
func (h *Handler) ensureValidToken(account *config.Account) error {
	if config.IsAPIKeyAccount(account) {
		if accountBearerToken(account) == "" {
			return fmt.Errorf("account %s has no kiroApiKey", account.ID)
		}
		return nil
	}
	if !tokenNeedsRefresh(account) {
		return nil
	}

	_, err := h.refreshAccountToken(account, false)
	return err
}

// lockAccountRefresh serializes token refresh for one account and returns
// the unlock function.
func (h *Handler) lockAccountRefresh(accountID string) func() {
	v, _ := h.refreshLocks.LoadOrStore(accountID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// tokenNeedsRefresh reports whether an OAuth account's access token must be
// refreshed before use. An unknown expiry (ExpiresAt == 0) with a refresh
// token available counts as expired: such accounts (raw JSON imports, legacy
// configs) were otherwise never refreshed, and their token silently expired
// an hour later, after which the account was banned on the resulting 401.
func tokenNeedsRefresh(account *config.Account) bool {
	if account == nil || config.IsAPIKeyAccount(account) {
		return false
	}
	if account.ExpiresAt == 0 {
		return strings.TrimSpace(account.RefreshToken) != ""
	}
	return time.Now().Unix() >= account.ExpiresAt-tokenRefreshSkewSeconds
}
