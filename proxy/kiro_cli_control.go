package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/logger"
)

// Control-plane calls of Kiro API-key accounts, shaped like the Kiro CLI
// 3.5.0 agent's (docs/kiro-capture.md §2, §4, §5.3): AWS JSON 1.0, POST /,
// x-amz-target naming the operation, origin KIRO_CLI in the body, the agent's
// AWS SDK user agents, TokenType API_KEY, amz-sdk-request/invocation-id and
// Connection: close; no optout, origin or Accept header.
//
//	ListAvailableModels      management.{region}.kiro.dev  KiroControlPlaneBearerService  {"origin":"KIRO_CLI"}                      attempt=1; max=3
//	GetUsageLimits           management.{region}.kiro.dev  KiroControlPlaneBearerService  {"origin":"KIRO_CLI"}                      attempt=1; max=1
//	GetFeatureConfiguration  runtime.{region}.kiro.dev     KiroRuntimeService             {"origin":"KIRO_CLI","version":"3.6.0"}   attempt=1; max=1, dash-joined UA
//
// OAuth accounts keep their existing calls (their IDE identity is not
// covered by the capture).

const (
	cpsTargetPrefix = "KiroControlPlaneBearerService."
	krsTargetPrefix = "KiroRuntimeService."
	// maxControlResponseBytes bounds a control-plane response body.
	maxControlResponseBytes = 8 << 20
)

// kiroCLIManagementURL is the CLI's control-plane endpoint for an account.
func kiroCLIManagementURL(account *config.Account) string {
	return fmt.Sprintf("https://management.%s.kiro.dev/", cliAccountRegion(account))
}

func cliAccountRegion(account *config.Account) string {
	region := "us-east-1"
	if account != nil {
		if r := strings.TrimSpace(account.Region); r != "" {
			region = r
		}
	}
	return config.SafeRegion(region)
}

// kiroCLIControlCall performs one AWS JSON 1.0 call with the CLI agent's
// header set and decodes a 200 response into out.
func kiroCLIControlCall(account *config.Account, url, target string, body interface{}, maxAttempts int, ua, amzUA string, out interface{}) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", target)
	if token := accountBearerToken(account); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if tt := responsesTokenType(account); tt != "" {
		req.Header.Set("TokenType", tt)
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("x-amz-user-agent", amzUA)
	req.Header.Set("Amz-Sdk-Request", fmt.Sprintf("attempt=1; max=%d", maxAttempts))
	req.Header.Set("Amz-Sdk-Invocation-Id", uuid.New().String())
	req.Close = true // the agent sends Connection: close on every call

	resp, err := GetRestClientForProxy(ResolveAccountProxyURL(account)).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxControlResponseBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	return json.Unmarshal(data, out)
}

// listAvailableModelsCLI is ListAvailableModels for API-key accounts.
func listAvailableModelsCLI(account *config.Account) ([]ModelInfo, error) {
	ua, amzUA := kiroCLISDKUserAgents("kirocontrolplanebearer", "m/N,E", false)
	var result struct {
		Models []ModelInfo `json:"models"`
	}
	err := kiroCLIControlCall(account, kiroCLIManagementURL(account), cpsTargetPrefix+"ListAvailableModels",
		map[string]string{"origin": "KIRO_CLI"}, 3, ua, amzUA, &result)
	if err != nil {
		return nil, err
	}
	recordModelTokenLimits(result.Models)
	return result.Models, nil
}

// getUsageLimitsCLI is GetUsageLimits for API-key accounts. The response
// uses the field names UsageLimitsResponse already reads.
func getUsageLimitsCLI(account *config.Account) (*UsageLimitsResponse, error) {
	ua, amzUA := kiroCLISDKUserAgents("kirocontrolplanebearer", "m/N,E", false)
	body := map[string]string{"origin": "KIRO_CLI"}
	if arn := strings.TrimSpace(account.ProfileArn); arn != "" {
		body["profileArn"] = arn
	}
	var result UsageLimitsResponse
	if err := kiroCLIControlCall(account, kiroCLIManagementURL(account), cpsTargetPrefix+"GetUsageLimits",
		body, 1, ua, amzUA, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ---- GetFeatureConfiguration ----

// featureConfigTTL matches the agent's cache: one fetch per account every
// 15 minutes at most, whatever the outcome.
const featureConfigTTL = 15 * time.Minute

type featureConfigEntry struct {
	attempted time.Time
	fetched   time.Time
	// configuration is stored as returned (keys are salted hashes); the
	// gateway does not act on it.
	configuration map[string]json.RawMessage
}

var (
	featureConfigMu    sync.Mutex
	featureConfigCache = map[string]*featureConfigEntry{}
	// featureConfigPrefetchDisabled keeps tests from starting the call.
	featureConfigPrefetchDisabled bool
)

// prefetchFeatureConfiguration starts GetFeatureConfiguration for an API-key
// account in the background when it was not attempted in the last 15
// minutes, the way the CLI fetches it as a session starts. It never blocks
// the caller.
func prefetchFeatureConfiguration(account *config.Account) {
	if featureConfigPrefetchDisabled || account == nil || !config.IsAPIKeyAccount(account) || account.ID == "" {
		return
	}
	now := time.Now()
	featureConfigMu.Lock()
	e := featureConfigCache[account.ID]
	if e != nil && now.Sub(e.attempted) < featureConfigTTL {
		featureConfigMu.Unlock()
		return
	}
	if e == nil {
		e = &featureConfigEntry{}
		featureConfigCache[account.ID] = e
	}
	e.attempted = now
	featureConfigMu.Unlock()

	acc := *account
	go func() {
		cfg, err := getFeatureConfigurationCLI(&acc)
		if err != nil {
			logger.Debugf("[FeatureConfig] %s: %v", accountEmailForLog(&acc), err)
			return
		}
		featureConfigMu.Lock()
		if cur := featureConfigCache[acc.ID]; cur != nil {
			cur.fetched, cur.configuration = time.Now(), cfg
		}
		featureConfigMu.Unlock()
	}()
}

func getFeatureConfigurationCLI(account *config.Account) (map[string]json.RawMessage, error) {
	ua, amzUA := kiroCLISDKUserAgents("kiroruntime", "m/N,E", true)
	var result struct {
		Configuration map[string]json.RawMessage `json:"configuration"`
	}
	err := kiroCLIControlCall(account, cliRuntimeURL(account), krsTargetPrefix+"GetFeatureConfiguration",
		map[string]string{"origin": "KIRO_CLI", "version": kiroCLIVersion}, 1, ua, amzUA, &result)
	if err != nil {
		return nil, err
	}
	return result.Configuration, nil
}

// cachedFeatureConfiguration returns the stored configuration of an account
// (nil when none was fetched).
func cachedFeatureConfiguration(accountID string) map[string]json.RawMessage {
	featureConfigMu.Lock()
	defer featureConfigMu.Unlock()
	if e := featureConfigCache[accountID]; e != nil {
		return e.configuration
	}
	return nil
}
