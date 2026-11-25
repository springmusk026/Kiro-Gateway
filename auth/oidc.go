package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/springmusk026/Kiro-Gateway/config"
	"io"
	"net/http"
	"strings"
	"time"
)

// oidcTokenURL 构造 idc/builderId 刷新 endpoint。测试可替换以拦截网络调用。
var oidcTokenURL = func(region string) string {
	return fmt.Sprintf("https://oidc.%s.amazonaws.com/token", config.SafeRegion(region))
}

// socialTokenURL 构造 social 刷新 endpoint。测试可替换以拦截网络调用。
var socialTokenURL = func() string {
	return "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken"
}

// RefreshToken 刷新 access token
// Returns: accessToken, refreshToken, expiresAt, profileArn, error
// The outcome is recorded per account for health alerts (refresh_health.go).
func RefreshToken(account *config.Account) (string, string, int64, string, error) {
	if config.IsAPIKeyAccount(account) {
		return "", "", 0, "", fmt.Errorf("API Key credentials do not support token refresh")
	}
	accessToken, refreshToken, expiresAt, profileArn, err := refreshAccountToken(account)
	if account != nil {
		recordRefreshResult(account.ID, err, time.Now())
	}
	return accessToken, refreshToken, expiresAt, profileArn, err
}

func refreshAccountToken(account *config.Account) (string, string, int64, string, error) {
	// Resolve per-account proxy: account.ProxyURL > global config
	proxyURL := account.ProxyURL
	if proxyURL == "" {
		proxyURL = config.GetProxyURL()
	}
	client := GetAuthClientForProxy(proxyURL)

	if strings.EqualFold(strings.TrimSpace(account.AuthMethod), MicrosoftSSOAuthMethod) {
		return refreshExternalIdpToken(
			account.RefreshToken,
			account.ClientID,
			account.TokenEndpoint,
			account.IssuerURL,
			account.Scopes,
			client,
		)
	}
	if account.AuthMethod == "social" {
		return refreshSocialToken(account.RefreshToken, client)
	}
	return refreshOIDCToken(account.RefreshToken, account.ClientID, account.ClientSecret, account.Region, client)
}

// refreshOIDCToken IdC/Builder ID token 刷新
func refreshOIDCToken(refreshToken, clientID, clientSecret, region string, client *http.Client) (string, string, int64, string, error) {
	if clientID == "" || clientSecret == "" {
		return "", "", 0, "", fmt.Errorf("OIDC refresh requires clientId and clientSecret")
	}
	if region == "" {
		region = "us-east-1"
	}

	url := oidcTokenURL(region)

	payload := map[string]string{
		"clientId":     clientID,
		"clientSecret": clientSecret,
		"refreshToken": refreshToken,
		"grantType":    "refresh_token",
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		return "", "", 0, "", fmt.Errorf("refresh failed: %d %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int    `json:"expiresIn"`
		ProfileArn   string `json:"profileArn"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", 0, "", err
	}

	return finishRefresh(result.AccessToken, result.RefreshToken, result.ExpiresIn, result.ProfileArn)
}

// refreshSocialToken Social (GitHub/Google) token 刷新
func refreshSocialToken(refreshToken string, client *http.Client) (string, string, int64, string, error) {
	url := socialTokenURL()

	payload := map[string]string{
		"refreshToken": refreshToken,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		return "", "", 0, "", fmt.Errorf("refresh failed: %d %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int    `json:"expiresIn"`
		ProfileArn   string `json:"profileArn"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", 0, "", err
	}

	return finishRefresh(result.AccessToken, result.RefreshToken, result.ExpiresIn, result.ProfileArn)
}

// errEmptyAccessToken: an IdP answered 200 without a usable token. Treating
// that as success stored a blank token, so every later request failed with
// 401/403 and the account was banned.
var errEmptyAccessToken = fmt.Errorf("refresh failed: response contained no access token")

// finishRefresh validates a refresh response and computes its expiry. A
// missing or non-positive expiresIn falls back to one hour instead of "now",
// which would otherwise force a refresh on every request.
func finishRefresh(accessToken, refreshToken string, expiresIn int, profileArn string) (string, string, int64, string, error) {
	if strings.TrimSpace(accessToken) == "" {
		return "", "", 0, "", errEmptyAccessToken
	}
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	return accessToken, refreshToken, time.Now().Unix() + int64(expiresIn), profileArn, nil
}
