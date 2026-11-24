# Kiro CLI 3.5.0: live request capture against a local fake backend

All requests below were made by the unmodified official `kiro-cli` 3.5.0 binary (Rust host plus its embedded KAS
`@kiro/agent` 0.66.119 running on the binary's bundled Node `22.23.4`). The only credentials were fake: the API key
`ksk_FAKE…` and tokens minted by the fake server. **Nothing left the machine.** Every TLS connection went to a local
mitmproxy, which answered every request itself (`connection_strategy=lazy`, upstream never contacted). Each `CONNECT`
was matched one-to-one with a logged HTTP request, and there were no TLS failures. No real account was used.
This repository was only read.

`$W` = `<workdir>`

## 1. How it was done (exact commands)

### 1.1 Setup
```bash
cd $W && python3 -m venv venv && ./venv/bin/pip install mitmproxy      # mitmproxy 12.3.1
# fake backend = mitmproxy addon $W/fake_kiro.py (answers every op locally, logs JSONL: seq, t, headers in wire order, body)
./venv/bin/mitmdump --set confdir=$W/mitmconf --set connection_strategy=lazy \
    --listen-host 127.0.0.1 -p 18080 -s $W/fake_kiro.py -q        # wrapped in $W/run.sh / run_pty.sh / run_s.sh
```
The client environment (`$W/env.sh`) uses a scratch HOME. It trusts **only** the mitm CA, so anything that tried to
bypass the proxy would fail TLS instead of reaching the internet:
```bash
export HOME=$W/home XDG_CONFIG_HOME=$W/home/.config XDG_DATA_HOME=$W/home/.local/share XDG_CACHE_HOME=$W/home/.cache XDG_STATE_HOME=$W/home/.local/state
export HTTPS_PROXY=http://127.0.0.1:18080 HTTP_PROXY=… https_proxy=… http_proxy=… ALL_PROXY=… NO_PROXY= no_proxy=
export SSL_CERT_FILE=$W/mitmconf/mitmproxy-ca-cert.pem NODE_EXTRA_CA_CERTS=$W/mitmconf/mitmproxy-ca-cert.pem
export KIRO_API_KEY=ksk_<FAKE-REDACTED>      # fake
KIRO=$S/ex/kirocli/bin/kiro-cli
```
Both the Rust host and KAS honor `HTTPS_PROXY` plus the custom CA. Endpoint overrides were therefore not needed, and the
capture shows the **real production hostnames** the client resolves. For reference, these override knobs exist:
env `KIRO_KAS_ENDPOINT`, `KIRO_KAS_CONTROL_PLANE_ENDPOINT`, `KIRO_KAS_REGION`; settings `api.krs.service`,
`api.cps.service`, `api.q.service`, `api.kiroauth.service` (`{"endpoint","region"}`); KAS flags `--endpoint`,
`--control-plane-endpoint`, `--region`.

### 1.2 Fake responses
The addon (`$W/fake_kiro.py`) dispatches on `x-amz-target` (or the path) and returns:
* `GetProfile`: 200 only for `management.us-east-1`, 403 elsewhere.
* `ListAvailableModels`: one model, `claude-sonnet-4.5`.
* `GetUsageLimits`: a free-tier body.
* `GetFeatureConfiguration`: `{configuration:{}}` (GAR arm). For the CR arm it adds
  `sha256("kiro-feature-config-key-salt-3e9b1d7a"+"create_response_api") → "create_response"`. Feature keys are **salted-hashed** on the wire.
* `GenerateAssistantResponse`: an AWS event stream.
* `/v1/responses`: SSE.
* `InvokeMCP`: a JSON-RPC result.
* Cognito, OIDC and telemetry: stubs.

When the user text contains `SEARCHME`, the model replies with a `remote_web_search` tool call. That drives the MCP `tools/call` request.

### 1.3 Runs
| Run | Command (all with `timeout`, cwd = fresh `$W/work_<run>`) | Notes |
|---|---|---|
| run1 | `kiro-cli chat --no-interactive --trust-workspace "hi"` | first try; fake did not yet know awsJson targets, so everything was 404. Shows the Rust region probe sweeping **8 regions incl. ISO partitions** on failure |
| run2 | `kiro-cli chat --no-interactive "hi"` | default non-interactive = **v2 (Rust) engine** |
| run3 | `kiro-cli chat --no-interactive --agent-engine v3 --trust-workspace "hi"` (arm=gar) | v3/KAS, featureConfiguration `{}` |
| run4 | same, arm=cr | v3/KAS, `create_response_api=create_response` |
| run5 / run6 | `kiro-cli chat --no-interactive --agent-engine v3 --trust-all-tools --trust-workspace "SEARCHME please search the web for kiro cli"` (gar / cr) | tool-call turn + MCP `tools/call` |
| run7 | `python3 $W/pty_drive.py … kiro-cli chat --trust-workspace` (arm=cr) | **interactive v3** in a pty: "hi there" at ~20 s, "second turn please" at ~46 s, `/quit` |
| run8 | `HOME=$W/home_s kiro-cli login --social github --use-device-flow` (no `KIRO_API_KEY`) | the CLI actually ran the **Builder ID** SSO-OIDC device flow (startUrl `https://view.awsapps.com/start`) |
| run9 / run10 | as run5/6 with the run8 fake Builder ID login (cr / gar) | OAuth (Builder ID) variant |

Raw logs: `$W/run*.jsonl` (full bodies, unredacted fake tokens), `$W/run*.stdout|stderr`, `$W/run7.tty`. Every v3 and
v2 turn completed and printed `Hello from fake server.`

## 2. Headline findings

1. **KAS's smithy calls use AWS JSON 1.0, not restJson.** Every KAS SDK call is `POST /` with `content-type: application/x-amz-json-1.0`
   and `x-amz-target: KiroRuntimeService.<Op>` (runtime host) or `KiroControlPlaneBearerService.<Op>` (management host).
   This covers `GenerateAssistantResponse`, `GetFeatureConfiguration`, `InvokeMCP`, `ListAvailableModels`, `GetUsageLimits`,
   `GetUserPreference`, `GetProfile` and `GetEffectiveEnterpriseManagementResourceWithToken`. None of the restJson paths
   predicted in docs/kiro-protocol.md §1.2–1.6 (`/generateAssistantResponse`, `/featureConfiguration`, `/mcp`,
   `/List-Available-Models`, `/Get-Usage-Limits`, `/Get-Profile`) was ever requested. The `@http` traits in the bundle's
   schemas are not used on the wire. The **only** path-style call is `POST /v1/responses`.
2. **The default A/B arm is GenerateAssistantResponse.** `create_response_api` defaults to `"disabled"`. With an empty
   featureConfiguration, KAS sends `KiroRuntimeService.GenerateAssistantResponse` (awsJson) to `runtime.us-east-1.kiro.dev`.
   It uses `/v1/responses` only when the server returns the hashed `create_response_api=create_response` key.
3. **`/v1/responses` headers match the doc and Kiro-Go's current set exactly, in this order:** `Content-Type, Authorization,
   x-amzn-kiro-origin, x-amzn-codewhisperer-optout, [x-amzn-kiro-profile], [TokenType], User-Agent,
   x-amzn-kiro-attempt: 1;max=3, x-amzn-kiro-fallback: 0;max=0, Content-Length, Host, Connection: close`. It is HTTP/1.1 with
   no Accept, no Accept-Encoding and no x-amz-user-agent. **However, optout was `false`** (default settings, both API key and Builder ID),
   and the UA has an extra `acp-client/…` tag.
4. **The UA tail is `acp-client/kiro-cli` (interactive) or `acp-client/kiro-cli-non-interactive`.** Example:
   `KiroCLI/3.5.0 KAS/0.66.119 os/linux md/appVersion-3.5.0 app/AmazonQ-For-CLI acp-client/kiro-cli`.
5. **The system prompt goes into `input`/`history` as a user turn followed by the assistant reply `"I will follow these
   instructions."`.** This happens on both CR and GAR (v3), and `instructions` is absent. That is exactly Kiro-Go's priming pair.
   The v2 Rust engine instead uses `"Follow this instruction: …"` / `"I will fully incorporate this information …"`.
6. **conversation IDs are `sess_<uuid-v4>`** in v3, both in `metadata.kiro:conversation_id` and in
   `conversationState.conversationId`. `rootConversationId` equals it, and `agentContinuationId` is a fresh UUID v4. v2 uses a bare UUID v4.
   `kiro:mode_identifier: "vibe"` **is sent in vibe mode too**.
7. **MCP:** `tools/list` (`{"id":"tools_list","method":"tools/list","jsonrpc":"2.0","params":{"includeHidden":true}}`)
   is sent once at session start via `KiroRuntimeService.InvokeMCP` on the **runtime** host. `tools/call` uses id
   `"web_search_" + <toolUseId>`, the MCP tool name `web_search` (the model-facing name is `remote_web_search`) and
   arguments that include a `_meta` object. For OAuth logins `profileArn` is in the **JSON-RPC body** (top level), not in a header.
8. **q.\*.amazonaws.com is not used by v3 for chat, models, usage or MCP.** It appears only for:
   * v2's post-turn `AmazonCodeWhispererService.SendTelemetryEvent` to `q.us-east-1.amazonaws.com`;
   * the updater on `desktop-release.q.us-east-1.amazonaws.com`.
9. **Before the first turn, the Rust host makes its own awsJson calls with an aws-sdk-rust UA:**
   `AmazonCodeWhispererService.GetProfile`, then `AmazonCodeWhispererService.GetUsageLimits` with the query string *and*
   the body, both on management. These are followed by Cognito and toolkit telemetry.

## 3. Ordered request list (condensed; verbatim headers and bodies are in the Appendix)

### 3.1 v3 (default for interactive), API key: run7 interactive, CR arm
| t (s) | Client | Request | Notes |
|---|---|---|---|
| 2.65 | Rust | POST management.us-east-1 `/` `AmazonCodeWhispererService.GetProfile` body `{}` | aws-sdk-rust UA, `tokentype: API_KEY` |
| 2.66 | Rust | POST management.us-east-1 `/?origin=KIRO_CLI&isEmailRequired=false` `AmazonCodeWhispererService.GetUsageLimits` body `{"origin":"KIRO_CLI","isEmailRequired":false}` | |
| 2.68 | Rust | GET desktop-release.q.us-east-1.amazonaws.com/latest/manifest.json | UA `kiro-cli-linux-x86_64-3.5.0` |
| 2.69–2.71 | Rust | cognito-identity GetId / GetCredentialsForIdentity (pool `us-east-1:820fd6d1-…`) → POST client-telemetry.us-east-1.amazonaws.com/metrics ×2 (sigv4) | legacy toolkit telemetry |
| 3.06 | Rust | GET prod.download.cli.kiro.dev/stable/3.5.0/feed.json | interactive only |
| 4.27 | Rust | `AmazonCodeWhispererService.GetProfile` again | |
| 5.71 | KAS | **API-key region probe**: `KiroControlPlaneBearerService.GetProfile` `{}`, sent **in parallel** to management.{us-east-1, eu-central-1, us-gov-east-1, us-gov-west-1} | `amz-sdk-request: attempt=1; max=1` |
| 5.75 | KAS | in parallel: `KiroControlPlaneBearerService.ListAvailableModels` `{"origin":"KIRO_CLI"}`, `KiroRuntimeService.InvokeMCP` tools/list, `KiroRuntimeService.GetFeatureConfiguration` `{"origin":"KIRO_CLI","version":"3.5.0"}`, `KiroControlPlaneBearerService.GetEffectiveEnterpriseManagementResourceWithToken` `{"clientSurface":"CLI","clientVersion":"0.66.119"}` | |
| 5.98 | KAS | POST app.kiro.dev/service/KiroWebBearerService/operation/GetResolvedConfigManifest | `smithy-protocol: rpc-v2-cbor`, plain `aws-sdk-js` UA (no Kiro tail) |
| 6.02 | Rust | `AmazonCodeWhispererService.GetProfile` | |
| 23.25 | KAS | **POST runtime.us-east-1.kiro.dev/v1/responses** (turn 1) | |
| 49.70 | KAS | **POST /v1/responses** (turn 2) | **no** re-fetch of models, featureConfiguration or MCP between turns |
| 62.7 / 76.3 | Rust | telemetry.us-east-1.kiro.dev/v1/metrics (OTLP, UA `OTel-OTLP-Exporter-Rust/0.32.0`) | |
| 64.6, 64.9 | Rust | `AmazonCodeWhispererService.GetProfile` ×2 around `/quit` | |

The non-interactive v3 runs (run3 to run6) show the same order without `feed.json` and without the extra Rust GetProfiles.
With the GAR arm, the chat call is `KiroRuntimeService.GenerateAssistantResponse`. After a tool call there is one
`InvokeMCP tools/call` and then the second chat request (run5/run6).

### 3.2 v3, Builder ID (OAuth) token: run9 (CR) and run10 (GAR)
Rust: toolkit telemetry and the update manifest. There is no Rust GetProfile or GetUsageLimits before chat in these runs; GetUsageLimits ran right after login in run8.
KAS sends no region probe. It sends, in parallel:
* `ListAvailableModels` `{"origin":"KIRO_CLI","profileArn":…}`
* **`GetUserPreference` `{"profileArn":…,"filter":["MODEL_FALLBACKS"]}`**
* `InvokeMCP` tools/list
* `GetFeatureConfiguration` `{"origin","version","profileArn"}`
* **`KiroControlPlaneBearerService.GetUsageLimits` `{"profileArn":…,"origin":"KIRO_CLI"}`** (`attempt=1; max=1`)

These are followed by `app.kiro.dev …/GetUserSettings` (`X-Kiro-Idp: BuilderId`, `X-Kiro-Profile-Arn`) and `…/GetResolvedConfigManifest`,
then the chat request, `tools/call`, and the second chat request. **No `TokenType` header** for Builder ID.
`GetEffectiveEnterpriseManagementResourceWithToken` was not called for Builder ID.

### 3.3 v2 (Rust engine; the default for `--no-interactive` without `--trust-all-tools`), API key: run2
GetProfile → GetUsageLimits → manifest → Cognito → GetProfile → toolkit metrics → GetUsageLimits → GetProfile ×2 →
`KiroControlPlaneBearerService.ListAvailableModels` (POST `/?origin=KIRO_CLI` plus body `{"origin":"KIRO_CLI"}`) →
desktop-release `models/all-MiniLM-L6-v2.zip` ×2 → **`AmazonCodeWhispererStreamingService.GenerateAssistantResponse`
on `runtime.us-east-1.kiro.dev/`** → `AmazonCodeWhispererService.SendTelemetryEvent` on **q.us-east-1.amazonaws.com** → OTLP metrics.

### 3.4 Login (run8, Builder ID device flow, Rust)
* `POST oidc.us-east-1.amazonaws.com/client/register` `{"clientName":"Kiro CLI","clientType":"public","scopes":["codewhisperer:completions","codewhisperer:analysis","codewhisperer:conversations"]}`
* `POST /device_authorization` `{"clientId","clientSecret","startUrl":"https://view.awsapps.com/start"}`
* `POST /token` `{"clientId","clientSecret","deviceCode","grantType":"urn:ietf:params:oauth:grant-type:device_code"}` (UA `aws-sdk-rust/1.6.0 ua/2.1 api/ssooidc/1.100.0 os/linux lang/rust/1.94.1 m/E,N app/AmazonQ-For-CLI`)
* Then GetUsageLimits with the Builder ID default profile `arn:aws:codewhisperer:us-east-1:638616132270:profile/AAAACCCCXXXX`.

The social endpoints on `prod.us-east-1.auth.desktop.kiro.dev` were **not** contacted.

## 4. User-Agent strings per call (verbatim)

| Call | `User-Agent` | `x-amz-user-agent` |
|---|---|---|
| KAS `/v1/responses` | `KiroCLI/3.5.0 KAS/0.66.119 os/linux md/appVersion-3.5.0 app/AmazonQ-For-CLI acp-client/kiro-cli` (interactive) or `…acp-client/kiro-cli-non-interactive` | *(absent)* |
| KAS `KiroRuntimeService.GenerateAssistantResponse`, `InvokeMCP` | `aws-sdk-js/1.0.0 ua/2.1 os/linux#<kernel> lang/js md/nodejs#22.23.4 api/kiroruntime#1.0.0 m/N KiroCLI/3.5.0 KAS/0.66.119 os/linux md/appVersion-3.5.0 app/AmazonQ-For-CLI acp-client/kiro-cli[-non-interactive]` | `aws-sdk-js/1.0.0 KiroCLI/3.5.0 KAS/0.66.119 os/linux md/appVersion-3.5.0 app/AmazonQ-For-CLI acp-client/kiro-cli[-non-interactive]` |
| KAS `KiroRuntimeService.GetFeatureConfiguration` | `… api/kiroruntime#1.0.0 m/N,E KiroCLI/3.5.0-KAS/0.66.119-os/linux-md/appVersion-3.5.0-app/AmazonQ-For-CLI acp-client/kiro-cli[-non-interactive]` (**dash-joined** "control" UA) | `aws-sdk-js/1.0.0 KiroCLI/3.5.0-KAS/0.66.119-os/linux-md/appVersion-3.5.0-app/AmazonQ-For-CLI acp-client/…` |
| KAS CPS `ListAvailableModels`, `GetUsageLimits`, `GetUserPreference`, `GetEffectiveEnterprise…` | `aws-sdk-js/1.0.0 ua/2.1 os/linux#<kernel> lang/js md/nodejs#22.23.4 api/kirocontrolplanebearer#1.0.0 m/N,E KiroCLI/3.5.0 KAS/0.66.119 os/linux md/appVersion-3.5.0 app/AmazonQ-For-CLI acp-client/…` | `aws-sdk-js/1.0.0 KiroCLI/3.5.0 KAS/0.66.119 … acp-client/…` |
| KAS CPS `GetProfile` (API-key region probe) | `… api/kirocontrolplanebearer#1.0.0 m/N,E KiroCLI/3.5.0-KAS/0.66.119-os/linux-md/appVersion-3.5.0-app/AmazonQ-For-CLI` (dash-joined, **no** acp-client tag) | `aws-sdk-js/1.0.0 KiroCLI/3.5.0-KAS/0.66.119-os/linux-md/appVersion-3.5.0-app/AmazonQ-For-CLI` |
| KAS app.kiro.dev (rpc-v2-cbor) | `aws-sdk-js/1.0.0 ua/2.1 os/linux#<kernel> lang/js md/nodejs#22.23.4 m/N,M,E` | `aws-sdk-js/1.0.0` |
| Rust `AmazonCodeWhispererService.*` | `aws-sdk-rust/1.6.0 ua/2.1 api/codewhispererruntime/0.1.19636 os/linux lang/rust/1.94.1 md/appVersion-3.5.0 app/AmazonQ-For-CLI` | `aws-sdk-rust/1.6.0 ua/2.1 api/codewhispererruntime/0.1.19636 os/linux lang/rust/1.94.1 m/F app/AmazonQ-For-CLI` |
| Rust v2 `AmazonCodeWhispererStreamingService.GenerateAssistantResponse` | `aws-sdk-rust/1.6.0 ua/2.1 api/codewhispererstreaming/0.1.19636 os/linux lang/rust/1.94.1 md/appVersion-3.5.0 app/AmazonQ-For-CLI` | `… m/F app/AmazonQ-For-CLI` |
| Rust v2 `KiroControlPlaneBearerService.ListAvailableModels` | `aws-sdk-rust/1.6.0 ua/2.1 api/kirocontrolplanebearer/0.1.0 os/linux lang/rust/1.94.1 md/appVersion-3.5.0 app/AmazonQ-For-CLI` | `… m/F,C app/AmazonQ-For-CLI` |
| Rust OIDC | `aws-sdk-rust/1.6.0 ua/2.1 api/ssooidc/1.100.0 os/linux lang/rust/1.94.1 m/E,N app/AmazonQ-For-CLI` | same |
| Rust updater | `kiro-cli-linux-x86_64-3.5.0` | – |
| Rust OTLP | `OTel-OTLP-Exporter-Rust/0.32.0` | – |

Wire details:
* **Transport.** KAS sends `Connection: close` on **every** request (raw and smithy), over HTTP/1.1. Rust uses HTTP/1.1 keep-alive. Neither client offered h2 in ALPN, while curl through the same proxy did negotiate h2.
* **Header case.** KAS: `TokenType`, `Authorization` and `Connection` capitalized, the rest lowercase. Rust: all lowercase, including `tokentype`.
* **Optout.** KAS's smithy GAR/CPS/MCP calls omit `x-amzn-codewhisperer-optout`. Rust always sends `x-amzn-codewhisperer-optout: false`.
* **Accept headers.** Rust sends `accept: */*` and `accept-encoding: gzip`. KAS smithy sends neither, and raw `/v1/responses` sends neither.

## 5. Comparison with Kiro-Go (HEAD b0362127)

Kiro-Go sources read: `proxy/kiro_responses.go`, `proxy/kiro_headers.go`, `proxy/kiro.go`, `proxy/kiro_api.go`, `proxy/websearch.go`, `proxy/upstream_protocol.go`, `proxy/translator.go`.

### 5.1 `/v1/responses` (Kiro-Go: OAuth accounts, `callKiroResponsesAPIContext`)
| Item | Official (observed) | Kiro-Go | Verdict |
|---|---|---|---|
| URL | `https://runtime.us-east-1.kiro.dev/v1/responses` | same (`kiro_responses.go:179`) | **match** |
| Header names, order of Set calls | Content-Type, Authorization, x-amzn-kiro-origin, x-amzn-codewhisperer-optout, [x-amzn-kiro-profile], [TokenType], User-Agent, x-amzn-kiro-attempt, x-amzn-kiro-fallback, Content-Length, Host, Connection | same set (`:429-443`). Go canonicalizes names (`Tokentype`, `X-Amzn-Kiro-Origin`), writes Host/User-Agent first and the rest alphabetically, adds `Accept-Encoding: gzip` (`DisableCompression:false`), uses keep-alive (no `Connection: close`), and uses **HTTP/2** when no proxy is configured (`ForceAttemptHTTP2:true`, `kiro.go:150`) | **set matches; wire framing differs** |
| `x-amzn-kiro-attempt` / `-fallback` | `1;max=3` / `0;max=0` | `%d;max=3` / `0;max=0` | **match** |
| `x-amzn-codewhisperer-optout` | **`false`** (default settings, API key and Builder ID) | `true` (`:434`) | **difference** (IDE value unobserved) |
| `TokenType` | `API_KEY` for API key; **absent** for Builder ID | `API_KEY` / `EXTERNAL_IDP` / `SSO_OIDC` (IdC); none for social/BuilderId | **match** (IdC/`SSO_OIDC` and external IdP unobserved) |
| `x-amzn-kiro-profile` | present with OAuth (Builder ID), absent for API key | present when ARN known | **match** |
| User-Agent | `KiroCLI/3.5.0 KAS/0.66.119 os/linux md/appVersion-3.5.0 app/AmazonQ-For-CLI acp-client/kiro-cli[-non-interactive]` | API key: same **without the `acp-client/…` tag**; OAuth: `KiroIDE-<ver>-<machineId>` (IDE identity, cannot be compared here) | **difference** (missing `acp-client/…`) |
| `origin` (header and body) | `KIRO_CLI` | API key: `KIRO_CLI`; OAuth: `AI_EDITOR` | match for CLI identity |
| Body key order | `model, input, tools, stream, origin, [profileArn], metadata` (no `instructions`; no `reasoning` for a model without an effort schema) | `model,input,instructions,tools,stream,origin,profileArn,metadata,reasoning`, all omitempty | **match** |
| System prompt | first `input` items are `{"role":"user","content":<system>}` then `{"role":"assistant","content":"I will follow these instructions."}` | same priming pair (`translator.go:341`) | **confirmed match** |
| `metadata` | `kiro:agent_mode:"vibe"`, **`kiro:mode_identifier:"vibe"`**, `kiro:conversation_id`, `kiro:root_conversation_id`, `kiro:agent_continuation_id` (insertion order) | ordered marshal; **no `kiro:mode_identifier`**; `agent_continuation_id` only if the payload carries one (nothing sets it) | **difference** |
| conversation id format | `sess_<uuid v4>`; root equals it; continuation id is a UUID v4 | bare UUID **v5** (SHA-1 of model+system+anchor, `translator.go:2398`), or v4 for synthetic anchors | **difference** |
| Tool history | `function_call {call_id,name,arguments}` + `function_call_output {call_id,output}` (no status) | structured only with `KIRO_KEEP_STRUCTURED_TOOL_HISTORY=1` | previously documented difference, confirmed shape |

### 5.2 GenerateAssistantResponse
| Item | Official v3 KAS (default arm) | Official v2 Rust | Kiro-Go API-key path (`kiro.go:77`, legacy branch) | Kiro-Go OAuth legacy arm (`kiro.go:54-73`) |
|---|---|---|---|---|
| Host / path | `runtime.us-east-1.kiro.dev` `POST /` | `runtime.us-east-1.kiro.dev` `POST /` | `runtime.{region}.kiro.dev/` | `q.us-east-1…/generateAssistantResponse`, `codewhisperer…` |
| Protocol / target | awsJson1.0, **`KiroRuntimeService.GenerateAssistantResponse`** | awsJson1.0, `AmazonCodeWhispererStreamingService.GenerateAssistantResponse` | awsJson1.0, `AmazonCodeWhispererStreamingService.GenerateAssistantResponse`: **matches v2** | restJson-ish on retired hosts: **matches neither** |
| Extra headers | `TokenType`, `x-amzn-kiro-client-attribution: kiro-cli`, `x-amzn-kiro-mode-identifier: vibe`, `x-amzn-kiro-fallback: 0;max=0`, `x-amzn-kiro-attempt: 1;max=3`, `amz-sdk-request: attempt=1; max=3`, `amz-sdk-invocation-id`, `Connection: close`. No optout, no origin header, no agent-mode header | `x-amzn-codewhisperer-optout: false`, `tokentype`, `x-amzn-kiro-attempt: 1;max=3`, `x-amzn-kiro-fallback: 0;max=0`, `amz-sdk-*`, `accept: */*`, `accept-encoding: gzip` | optout false, tokentype, `Accept: */*`, `Amz-Sdk-Request`, `Amz-Sdk-Invocation-Id`; **no `x-amzn-kiro-attempt`/`-fallback`** | adds `x-amzn-kiro-agent-mode: vibe` |
| UA | aws-sdk-js + `api/kiroruntime#1.0.0 m/N` + KiroCLI tail | aws-sdk-rust `api/codewhispererstreaming/0.1.19636` + `md/appVersion-3.5.0 app/AmazonQ-For-CLI` | aws-sdk-js `api/codewhispererstreaming#1.0.34 m/E KiroIDE-<ver>-<machineId>` | same as API-key path |
| Body top level | `conversationState`, [`profileArn`], **`agentMode:"vibe"`** (body, not header) | `conversationState` only | `conversationState`, [`profileArn`], `inferenceConfig?` | same |
| conversationState key order | `currentMessage, chatTriggerType, conversationId(sess_…), history, agentContinuationId, agentTaskType:"vibe", rootConversationId` | `conversationId(uuid4), history, currentMessage, chatTriggerType:"MANUAL", agentTaskType:"vibe"` | `agentContinuationId?, agentTaskType?, chatTriggerType, conversationId(uuid5), rootConversationId, currentMessage, history` | same |
| Priming | user `{content,origin,modelId}` + assistant `"I will follow these instructions."` | `"Follow this instruction: …"` + `"I will fully incorporate this information…"` | v3 wording on the v2 transport (a hybrid) | – |
| currentMessage | `{content, userInputMessageContext{[toolResults],tools}, origin:"KIRO_CLI", modelId}` | `{content, userInputMessageContext{envState,tools}, origin, modelId}` | similar | – |

**Verdict.** Kiro-Go's API-key path matches the v2 Rust engine in host, target and content type. The UA, the missing
attempt/fallback headers, the body order and the priming text are differences. The real v3 default is awsJson
`KiroRuntimeService.GenerateAssistantResponse`, which Kiro-Go does not implement at all.

### 5.3 Side calls
| Call | Official (observed) | Kiro-Go | Verdict |
|---|---|---|---|
| ListAvailableModels | management.{region}.kiro.dev `POST /`, `KiroControlPlaneBearerService.ListAvailableModels`, body `{"origin":"KIRO_CLI"[,"profileArn"]}`, no maxResults; once per session (5 min TTL), not per turn | `GET codewhisperer.us-east-1…/ListAvailableModels?origin=AI_EDITOR&maxResults=50&profileArn=` | **difference** (host, protocol, origin) |
| GetUsageLimits | KAS (OAuth only): `KiroControlPlaneBearerService.GetUsageLimits` `{"profileArn","origin":"KIRO_CLI"}`, `attempt=1; max=1`. Rust host (API key too): `AmazonCodeWhispererService.GetUsageLimits` on **management** with query and body `{"origin":"KIRO_CLI","isEmailRequired":false}` | `GET {codewhisperer\|q}/getUsageLimits?origin=AI_EDITOR&resourceType=AGENTIC_REQUEST&isEmailRequired=true` | **difference** |
| GetFeatureConfiguration | `KiroRuntimeService.GetFeatureConfiguration` `{"origin":"KIRO_CLI","version":"3.5.0"[,"profileArn"]}`, every session before the first turn; response keys are sha256(salt+key) | never called | **missing** |
| Profile / region | API key: parallel `KiroControlPlaneBearerService.GetProfile {}` to 4 regions. Rust: `AmazonCodeWhispererService.GetProfile {}` (sweeps 8 regions incl. ISO on failure) | `ListAvailableProfiles` on codewhisperer/q hosts | **difference** |
| GetUserPreference | `{"profileArn","filter":["MODEL_FALLBACKS"]}` (OAuth sessions) | not implemented | missing |
| GetEffectiveEnterpriseManagementResourceWithToken | `{"clientSurface":"CLI","clientVersion":"0.66.119"}` (API-key session) | not implemented | missing |
| app.kiro.dev KiroWebBearerService (rpc-v2-cbor) | `GetResolvedConfigManifest`; with OAuth also `GetUserSettings` (+`X-Kiro-Idp`, `X-Kiro-Profile-Arn`) | not implemented | missing (not needed for chat) |
| MCP tools/list | once per session, `KiroRuntimeService.InvokeMCP` on runtime host, `{"id":"tools_list","method":"tools/list","jsonrpc":"2.0","params":{"includeHidden":true}}` | never | **missing** |
| MCP tools/call | runtime host `InvokeMCP` (awsJson), body key order `id, method, [profileArn], jsonrpc, params`; `id = "web_search_" + toolUseId` (e.g. `web_search_tooluse_<…>`); `params.arguments = {query, _meta:{_isValid:true,_activePath:["query"],_completedPaths:[["query"]]}}`; profileArn in **body** | `POST https://q.{region}.amazonaws.com/mcp` (restJson), id `web_search_tooluse_<22>_<ms>_<8>`, order `id,jsonrpc,method,params`, no `_meta`, profileArn in header `x-amzn-kiro-profile-arn`, IDE streaming UA | **difference** (host, protocol, id format, args) |
| Telemetry | Rust: Cognito + toolkit PostMetrics, OTLP metrics to telemetry.us-east-1.kiro.dev; v2 also SendTelemetryEvent | none | difference (by design) |
| OIDC RegisterClient (Builder ID) | clientName `"Kiro CLI"`, clientType `public`, 3 codewhisperer scopes, startUrl `https://view.awsapps.com/start` | `"Kiro"` (`auth/builderid.go:50`, `auth/iam_sso.go:162`, `auth/sso_token.go:72`) | **difference** (official value confirmed: `"Kiro CLI"`) |

## 6. Answers to the open questions in docs/kiro-protocol.md

1. **Is management.{region}.kiro.dev used for ListAvailableModels and GetUsageLimits?** **Yes, confirmed.** It is used via
   awsJson 1.0 (`POST /` + `x-amz-target KiroControlPlaneBearerService.ListAvailableModels|GetUsageLimits` from KAS, and
   `AmazonCodeWhispererService.GetUsageLimits` from the Rust host), **not** via the restJson `GET /List-Available-Models` / `/Get-Usage-Limits` paths in the doc.
   Only us-east-1 was observed; other regions were exercised only as `GetProfile` probe targets.
2. **Is q.\*.amazonaws.com used?** **Not by v3** for any model, usage, chat or MCP call.
   * Observed only for `AmazonCodeWhispererService.SendTelemetryEvent` from the v2 engine.
   * `desktop-release.q.us-east-1.amazonaws.com` is used by the updater (`/latest/manifest.json`) and v2's embedding model download.
   * `codewhisperer.*.amazonaws.com`: never contacted.
3. **What is the featureConfiguration request shape?** `POST https://runtime.us-east-1.kiro.dev/`,
   `x-amz-target: KiroRuntimeService.GetFeatureConfiguration`, `content-type: application/x-amz-json-1.0`,
   body `{"origin":"KIRO_CLI","version":"3.5.0"}`, plus `"profileArn"` for OAuth.
   * It is sent with `amz-sdk-request: attempt=1; max=1` and the dash-joined control UA.
   * There is **no `x-amzn-kiro-profile-arn` header**; profileArn travels in the body.
   * Response `{"configuration":{<sha256("kiro-feature-config-key-salt-3e9b1d7a"+key)>: value}}`.
   * The default `create_response_api="disabled"` means GAR.
   * Called once per session before the first turn, not between turns (15 min TTL per the bundle).
4. **Is MCP tools/list called?** **Yes, observed.** It is sent once per session at startup, in parallel with ListAvailableModels and featureConfiguration, and *before* any tool use (see §5.3 for shape).
   * In an earlier run3 attempt (log since overwritten), the fake server answered 404, and KAS re-sent tools/list twice more (3 total) before the chat turn.
   * `tools/call` followed only when the model used `remote_web_search`.
5. **What is the API-key region discovery?** **Observed.** KAS sends `KiroControlPlaneBearerService.GetProfile {}` to
   management.{us-east-1, eu-central-1, us-gov-east-1, us-gov-west-1} in parallel, with `max=1`.
   * The Rust host has its own sequential probe (`AmazonCodeWhispererService.GetProfile`) covering 8 endpoints including ISO partitions (`kiro-management.us-iso-east-1.c2s.ic.gov`, etc.), observed in run1.
6. **Does the official client send Accept / Accept-Encoding / x-amz-user-agent on `/v1/responses`?** **No, confirmed.**
7. **Unobserved:**
   * IDE (`AI_EDITOR`) traffic.
   * IdC / `SSO_OIDC` and external-IdP token types.
   * Social login and refresh (`prod.us-east-1.auth.desktop.kiro.dev`): `--social github --use-device-flow` fell back to the Builder ID device flow.
   * Token refresh of any kind.
   * Regions other than us-east-1 for chat.
   * `system_field_injection` / `instructions` field.
   * `reasoning` block (the fake model had no effort schema).
   * `create_response_api_auxiliary`, compaction calls.
   * `InvokeMCPStream`.
   * KAS's own OTLP traces (only the Rust OTLP metrics were seen).
   * `ListAvailableModels` pagination.
   * Retry behavior on 429/5xx.
   * Any response from real Kiro servers (all responses were fake).

## 7. Caveats
* The response bodies came from the fake server. Its choices only influenced *which* later calls happened: the
  region-probe results, the arm, and the tool call. The request shapes are the client's own.
* The `os/linux#6.18.44-fc-v819` kernel string and the Node version `22.23.4` come from this sandbox and the bundled runtime.
* One-time behaviors may also be cached in the scratch HOME. Each run started from an empty HOME, except run9/run10, which reused the run8 login.


---

