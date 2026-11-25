package proxy

import (
	"fmt"
	"github.com/springmusk026/Kiro-Gateway/config"
	"net/http"
	"strings"
)

const (
	kiroStreamingSDKVersion = "1.0.34"
	kiroRuntimeSDKVersion   = "1.0.0"
)

type kiroHeaderValues struct {
	UserAgent    string
	AmzUserAgent string
	Host         string
}

func buildStreamingHeaderValues(account *config.Account, host string) kiroHeaderValues {
	return buildKiroHeaderValues(account, host, "codewhispererstreaming", kiroStreamingSDKVersion, "m/E")
}

func buildRuntimeHeaderValues(account *config.Account, host string) kiroHeaderValues {
	return buildKiroHeaderValues(account, host, "codewhispererruntime", kiroRuntimeSDKVersion, "m/N,E")
}

func buildKiroHeaderValues(account *config.Account, host, apiName, sdkVersion, mode string) kiroHeaderValues {
	clientCfg := config.GetKiroClientConfig()
	machineID := ""
	if account != nil {
		machineID = account.MachineId
	}

	userAgent := fmt.Sprintf(
		"aws-sdk-js/%s ua/2.1 os/%s lang/js md/nodejs#%s api/%s#%s %s KiroIDE-%s",
		sdkVersion,
		clientCfg.SystemVersion,
		clientCfg.NodeVersion,
		apiName,
		sdkVersion,
		mode,
		clientCfg.KiroVersion,
	)
	amzUserAgent := fmt.Sprintf("aws-sdk-js/%s KiroIDE-%s", sdkVersion, clientCfg.KiroVersion)
	if machineID != "" {
		userAgent += "-" + machineID
		amzUserAgent += "-" + machineID
	}

	return kiroHeaderValues{
		UserAgent:    userAgent,
		AmzUserAgent: amzUserAgent,
		Host:         host,
	}
}

func applyKiroBaseHeaders(req *http.Request, account *config.Account, values kiroHeaderValues) {
	token := accountBearerToken(account)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// Kiro requires external identity-provider access tokens and API keys to be
	// identified explicitly. Keep this in the shared header path so streaming
	// and REST requests cannot drift apart.
	req.Header.Del("TokenType")
	req.Header.Del("tokentype")
	if account != nil {
		if config.IsAPIKeyAccount(account) {
			// Upstream accepts either casing; CLI captures use lowercase "tokentype".
			req.Header.Set("tokentype", "API_KEY")
		} else if strings.EqualFold(strings.TrimSpace(account.AuthMethod), "external_idp") {
			req.Header.Set("TokenType", "EXTERNAL_IDP")
		}
	}
	req.Header.Set("User-Agent", values.UserAgent)
	req.Header.Set("x-amz-user-agent", values.AmzUserAgent)
	req.Header.Set("x-amzn-codewhisperer-optout", "true")
	if values.Host != "" {
		req.Host = values.Host
	}
}

// accountBearerToken returns the token used for Authorization: Bearer.
// API Key accounts prefer KiroApiKey; OAuth accounts use AccessToken.
func accountBearerToken(account *config.Account) string {
	if account == nil {
		return ""
	}
	if config.IsAPIKeyAccount(account) {
		if key := strings.TrimSpace(account.KiroApiKey); key != "" {
			return key
		}
	}
	return strings.TrimSpace(account.AccessToken)
}

// Identity of the official clients' raw /v1/responses requests. That path is a
// plain HTTPS call (no AWS SDK), so it carries only the host application's own
// user agent: no aws-sdk-js prefix and no x-amz-user-agent.
const (
	kiroCLIVersion = "3.5.0"
	kiroKASVersion = "0.66.119"
)

// responsesUserAgent is the User-Agent for the raw /v1/responses transport.
// OAuth accounts present the Kiro IDE (origin AI_EDITOR), whose agent sends its
// legacy custom UA, KiroIDE-<version>-<machineId>. API-key accounts are a Kiro
// CLI feature and present the CLI (origin KIRO_CLI).
func responsesUserAgent(account *config.Account) string {
	if config.IsAPIKeyAccount(account) {
		return kiroCLICustomUA()
	}
	ua := "KiroIDE-" + config.GetKiroClientConfig().KiroVersion
	if account != nil && account.MachineId != "" {
		ua += "-" + account.MachineId
	}
	return ua
}

// responsesTokenType is the TokenType header the official raw path sends:
// IAM Identity Center tokens are SSO_OIDC; social and Builder ID send none.
func responsesTokenType(account *config.Account) string {
	if account == nil {
		return ""
	}
	if config.IsAPIKeyAccount(account) {
		return "API_KEY"
	}
	switch strings.ToLower(strings.TrimSpace(account.AuthMethod)) {
	case "external_idp":
		return "EXTERNAL_IDP"
	case "idc":
		return "SSO_OIDC"
	}
	return ""
}

// kiroCLICustomUA is the Kiro CLI host's custom user agent as observed on the
// wire (interactive session).
func kiroCLICustomUA() string {
	return fmt.Sprintf("KiroCLI/%s KAS/%s os/linux md/appVersion-%s app/AmazonQ-For-CLI acp-client/kiro-cli", kiroCLIVersion, kiroKASVersion, kiroCLIVersion)
}

// kasNodeVersion is the Node runtime bundled with Kiro CLI 3.5.0.
const kasNodeVersion = "22.23.4"

// kiroCLISmithyUserAgents returns the User-Agent / x-amz-user-agent pair the
// CLI's agent sends on its AWS SDK calls to the runtime service.
func kiroCLISmithyUserAgents() (userAgent, amzUserAgent string) {
	return kiroCLISDKUserAgents("kiroruntime", "m/N", false)
}

// kiroCLISDKUserAgents builds the AWS SDK user agents of one of the CLI
// agent's service clients: api is the client (kiroruntime,
// kirocontrolplanebearer), mode the SDK feature flags. dashTail selects the
// "control" form the capture shows on GetFeatureConfiguration, where the
// KiroCLI part of the tail is joined with dashes instead of spaces.
func kiroCLISDKUserAgents(api, mode string, dashTail bool) (userAgent, amzUserAgent string) {
	tail := kiroCLICustomUA()
	if dashTail {
		base, client, _ := strings.Cut(tail, " acp-client/")
		tail = strings.ReplaceAll(base, " ", "-") + " acp-client/" + client
	}
	osTok := "linux"
	if v := strings.TrimSpace(config.GetKiroClientConfig().SystemVersion); strings.HasPrefix(v, "linux#") {
		osTok = v
	}
	return fmt.Sprintf("aws-sdk-js/1.0.0 ua/2.1 os/%s lang/js md/nodejs#%s api/%s#1.0.0 %s %s", osTok, kasNodeVersion, api, mode, tail),
		"aws-sdk-js/1.0.0 " + tail
}

// setKiroCLIRuntimeHeaders applies the header set Kiro CLI 3.5.0's agent sends
// on KiroRuntimeService.GenerateAssistantResponse (observed on the wire): no
// origin, agent-mode or optout header; mode, attribution, attempt and fallback
// counters; AWS SDK user agents and invocation id.
func setKiroCLIRuntimeHeaders(req *http.Request, account *config.Account, target string, attempt int, invocationID string) {
	ua, amzUA := kiroCLISmithyUserAgents()
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", target)
	if token := accountBearerToken(account); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if tt := responsesTokenType(account); tt != "" {
		req.Header.Set("TokenType", tt)
	}
	req.Header.Set("x-amzn-kiro-client-attribution", "kiro-cli")
	req.Header.Set("x-amzn-kiro-mode-identifier", kiroCLIAgentMode)
	req.Header.Set("x-amzn-kiro-fallback", "0;max=0")
	req.Header.Set("x-amzn-kiro-attempt", fmt.Sprintf("%d;max=%d", attempt, responsesAttemptBudget))
	req.Header.Set("Amz-Sdk-Request", fmt.Sprintf("attempt=%d; max=%d", attempt, responsesAttemptBudget))
	req.Header.Set("Amz-Sdk-Invocation-Id", invocationID)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("x-amz-user-agent", amzUA)
}
