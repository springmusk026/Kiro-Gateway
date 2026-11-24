# Kiro CLI 3.5.0 / @kiro/agent 0.66.119: network API map, cross-checked against Kiro-Go

> **Status:** research notes from the Kiro CLI 3.5.0 binary, its bundled agent (`@kiro/agent` 0.66.119) and the
> open-source Amazon Q Developer CLI, cross-checked against Kiro-Go at the time of writing. Items in §6 that are
> done: `/v1/responses` header set and identity, `response.incomplete`, refusals, in-stream error events,
> `TokenType: SSO_OIDC`, OIDC client name, tool-result error status. Host moves (§6 P0.3) and native
> `thinking.type` are pending live verification. Line numbers refer to the code at the time of the review.


Read-only research. Nothing in this repository was modified.

## 0. Sources, notation, and caveats

| Tag | Meaning |
|---|---|
| **[JS]** | `$S/kas/node_modules/@kiro/agent/dist/server/acp-server.js` (the minified KAS "Kiro Agent Server", `@kiro/agent` 0.66.119, monorepo `kiro-agent-monorepo` version `0.66.119`). Citations give the function name or a literal you can grep. |
| **[BIN]** | Strings from `$S/ex/kirocli/bin/kiro-cli` (`$S/strings.txt`). BUILD-INFO: `BUILD_VERSION=3.5.0`, `BUILD_DATE=2026-11-17`. |
| **[QCLI]** | The open-source predecessor `amazon-q-developer-cli` (`$S/qcli`). Used only to interpret the binary. |
| **[KG]** | Kiro-Go at this repository (HEAD `2a490fbe`). |
| `$S` | `<kiro-cli-3.5.0 package>` |
| **[unverified]** | Inferred, not directly proven by a string or snippet. |

The minified smithy schemas were decoded with a small resolver that substitutes the short variable names with their string literals. All request and response shapes quoted below come from that output, for example `He.CreateResponseRequest$`.

### How 3.5.0 is wired: which component makes which calls

* `kiro-cli` is a Rust binary. It contains three chat engines: `crates/chat-cli` (v1, legacy), `crates/chat-cli-v2` plus `crates/agent` (v2), and the **v3 engine, which is KAS**: a bundled Node process, `@kiro/agent`. [BIN] says: *"When nothing selects an engine: v3 interactively"* and *"Agent engine to use: "v2" or "v3" ... When neither is set: v3"*. **The default 3.5.0 chat traffic therefore comes from the JS bundle.**
* The Rust host launches KAS with `--auth=acp-callback` and `KIRO_CUSTOM_USER_AGENT` set ([BIN] near `kas_acp_relay.rs`: `--transport=ws--auth=acp-callback ... KIRO_CUSTOM_USER_AGENT`). The Rust host owns token refresh, which KAS calls via `_kiro/auth/getAccessToken`. [JS]: `"Auth: --auth=acp-callback (host-mediated refresh via _kiro/auth/getAccessToken)"`.
* The same `@kiro/agent` bundle is built to run inside the IDE too. [JS] `oPu={"kiro-ide":"KiroIDE","kiro-vscode":"KiroIDE","kiro-web":"KiroWeb","kiro-cli":"KiroCLI",...}` and the origin map `{"kiro-ide":"AI_EDITOR",...,"kiro-cli":"KIRO_CLI"}`. So the request shapes below are very likely what the current IDE sends as well. Only the host-provided UA string and the origin differ. [unverified for the IDE build itself]
* **The JS bundle contains no `q.*.amazonaws.com` or `codewhisperer.*.amazonaws.com` URL at all.** Grepping `https://q\.` and `https://codewhisperer` returns 0 hits. The agent talks only to `runtime.{region}.kiro.dev` (KRS), `management.{region}.kiro.dev` (CPS), `oidc.{region}.amazonaws.com`, `prod.us-east-1.auth.desktop.kiro.dev`, and `telemetry.{region}.kiro.dev`.

### Host table

| Logical service | Smithy namespace / serviceId | Host (prod) | Protocol | Evidence |
|---|---|---|---|---|
| KRS (Kiro Runtime Service) | `com.amazon.kiro.runtimeservice` / `KiroRuntime` | `https://runtime.{region}.kiro.dev` (iso partitions: `https://kiro-runtime.{region}.{dnsSuffix}`) | restJson1 | [JS] `n8c`, `pee()=PZi("runtime",…)`; `D$e="https://runtime.us-east-1.kiro.dev"` |
| CPS (Control Plane Bearer) | `com.amazon.kiro.controlplane` / `KiroControlPlaneBearer` | `https://management.{region}.kiro.dev` | restJson1 | [JS] `Xb()=PZi("management",…)`; [BIN] `https://management.us-east-1.kiro.dev`, `https://management.eu-central-1.kiro.dev` |
| CPS governance | `com.amazon.kiro.controlplane.bearer` | management host | awsJson (`KiroControlPlaneBearerService.*` target in Rust) | [BIN] `KiroControlPlaneBearerService.GetEffectiveEnterpriseManagementResourceWithToken` |
| Legacy CodeWhisperer (Rust v1/v2 only) | `AmazonCodeWhispererService` / `AmazonCodeWhispererStreamingService` | `https://q.{us-east-1,eu-central-1}.amazonaws.com` (Rust `Endpoint`) and/or the runtime host | awsJson1.0, `POST /`, `x-amz-target` | [BIN] `AmazonCodeWhispererStreamingService.GenerateAssistantResponse`, `application/x-amz-json-1.0x-amz-target`; [QCLI] `api_client/endpoints.rs` |
| Social auth | n/a | `https://prod.us-east-1.auth.desktop.kiro.dev` (`/refreshToken`, `/oauth/device/authorization`, `/oauth/device/poll`, `/oauth/token`) | JSON | [JS] `p9d`; [BIN] `/oauth/device/poll` |
| AWS SSO OIDC | `SSO OIDC` | `https://oidc.{region}.amazonaws.com` | restJson | [JS] `Rr.CreateToken$ ... "/token"` |
| Kiro telemetry (OTLP) | n/a | `https://telemetry.{us-east-1,eu-central-1}.kiro.dev` (`/v1/traces`, `/v1/metrics`) | OTLP/HTTP | [JS] `iEt.prodByRegion`, `OTLPTraceExporter({url:\`${h}/v1/traces\``; [BIN] `kiro-telemetry/src/client.rs` |
| Legacy toolkit telemetry (Rust) | `PostMetrics` + Cognito | `https://client-telemetry.us-east-1.amazonaws.com`, `cognito-identity.us-east-1`, pool `us-east-1:820fd6d1-95c0-4ca4-bffb-3f01d32da842` | sigv4 | [BIN] `legacy_sink.rs`, `cognito.rs` |
| Kiro Web portal (cloud sessions) | `com.amazon.kirowebportalservice.bearer` | not relevant | restJson | [JS] `H.ListSessions$` etc. |

Region routing ([JS] `t8c`, `y3`) is: explicit override, then the region parsed from `profileArn`, then the token's region, then `us-east-1`. Regions offered: `IZi=["us-east-1","eu-central-1","us-gov-east-1","us-gov-west-1"]`.

---

## 1. Endpoint and operation inventory

### 1.1 KRS `CreateResponse` (`POST /v1/responses`): the main chat path when the A/B flag is on

**Selection.** [JS] `Ybr`:
`t?.get(Dn.AB_CREATE_RESPONSE_API)!=="create_response" ? "GenerateAssistantResponse" : (callPath==="main" || AB_CREATE_RESPONSE_API_AUXILIARY==="create_response") ? "CreateResponse" : "GenerateAssistantResponse"`.
So the server-side experiment `create_response_api` (served by GetFeatureConfiguration, §1.3) decides whether a session uses `/v1/responses` or `/generateAssistantResponse`. Auxiliary calls (compaction, summaries) stay on GAR unless `create_response_api_auxiliary` is also set.

**Transport.** This is **not** the smithy client. It is a raw `node:https` request, `sendCreateResponseRaw` ([JS] `async function CKo`). That means HTTP/1.1, and no SDK user-agent middleware, `amz-sdk-*` headers or `x-amz-user-agent`.

**Exact header set** ([JS] `CKo`, called from `streamOneAttempt`):
```
Content-Type: application/json
Authorization: Bearer <accessToken>        # re-resolved per retry: KWt(authProvider,…,"create-response")
x-amzn-kiro-origin: <origin>               # KIRO_CLI for kiro-cli, AI_EDITOR for IDE
x-amzn-codewhisperer-optout: "false"|"true"  # contentCollectionEnabled ? "false":"true" (default false => "true")
x-amzn-kiro-profile: <profileArn>          # only if profileArn
TokenType: <SSO_OIDC|EXTERNAL_IDP|API_KEY|KIRO_MACHINE_TOKEN>  # Q4n(authMethod); omitted for social/BuilderId
User-Agent: <custom UA, see §3.1>          # this.getUserAgent() = backendUserAgent.current.forHttp()
x-amzn-kiro-attempt: "<n>;max=<budget>"    # nJo/HWt: `${prior+n};max=${prior+budget}`, budget = 3 + progress bonus (0..3)
x-amzn-kiro-fallback: "0;max=0"            # fSr(Ge); default W4o=Qfr({taken:0,max:0}); with fallback: "1;max=2;cause=<trigger>;from=<model>"
Content-Length: <n>
```
Headers the official raw path does **not** send: `Accept`, `x-amz-user-agent`, `amz-sdk-invocation-id`, `amz-sdk-request`, `x-amzn-kiro-agent-mode` (agent mode travels only in `metadata`), and `Accept-Encoding` (Node does not add it).

**Body** ([JS] `TKo` = `buildCreateResponseRequest`; schema `He.CreateResponseRequest$`). Key order as emitted:
```jsonc
{
  "model": "<modelId>",
  "input": [ ...items ],                 // bXu: history then currentMessage
  "instructions": "<systemPrompt>",      // only when AB system_field_injection is on (extractSystemPrompt)
  "tools": [{"type":"function","name","description","parameters"}],  // SXu, from currentMessage tools only
  "stream": true,
  "origin": "KIRO_CLI|AI_EDITOR",
  "profileArn": "<arn>",                 // also in body
  "metadata": {                          // insertion order:
    "kiro:agent_mode": "<agentMode>",    //   e.g. "vibe" (default session mode), "spec", "plan", ...
    "kiro:mode_identifier": "...",       //   only spec-family modes
    "kiro:agent_task_type": "spectask",  //   only SPEC_TASK
    "kiro:conversation_id": "<uuid v4>",
    "kiro:root_conversation_id": "<uuid>",
    "kiro:agent_continuation_id": "<id>"
  },
  "reasoning": {"type": "adaptive|disabled", "effort": "low|medium|high|xhigh|max", "summary": "<thinking.display>"}  // vXu
}
```
The schema also allows `tool_choice`, `max_output_tokens`, `temperature`, `top_p`, `truncation` (`auto|disabled`) and `previous_response_id`. The client never sets them ([JS] `He.CreateResponseRequest$`, `wo.TruncationStrategy`).

**Input item mapping** ([JS] `EKo`):
* User turn. First, one `{"type":"function_call_output","call_id","output"}` per toolResult, with text parts joined by `\n` and **no status field**. Then either `{"role":"user","content":"<text>"}`, or, when there are images or documents, `{"role":"user","content":[{"type":"input_text"},{"type":"input_image","image_url":"data:image/<fmt>;base64,…"},{"type":"input_file","filename":"<name>.<ext>","file_data":"data:<mime>;base64,…"}]}`.
* Assistant turn. First the reasoning item `{"type":"reasoning","summary":[],"encrypted_content":"<base64 json envelope>"}` if the turn had `reasoningContent` (signature or redacted). Then `{"role":"assistant","content":"<text>"}`. Then one `{"type":"function_call","call_id","name","arguments":"<json string>"}` per toolUse.
* History keeps **structured** tool calls and results for every turn. It is not flattened.

**SSE response** ([JS] `jKo` = `parseCreateResponseSse`). Lines start with `data:`; `[DONE]` is ignored; the maximum line buffer is 4 MiB (`GXu`). Event types:

| `type` | Meaning in the client |
|---|---|
| `kiro:keep_alive` | keepalive (stall watchdog only) |
| `response.output_text.delta` (`delta`) | assistant text |
| `response.reasoning_text.delta` / `response.reasoning.delta` | reasoning text |
| `response.output_item.done` with `item.type=="reasoning"` | `item.encrypted_content` holds a signature or redacted content, kept for replay |
| `response.refusal.delta` / `response.refusal.done` (`refusal`) | refusal text, shown as assistant text |
| `response.output_item.added` with `item.type=="function_call"` | tool start (`call_id`, `name`, `arguments`) |
| `response.function_call_arguments.delta` / `.done` | tool args / tool stop |
| `response.output_item.done` with `function_call` | tool complete (`status=="incomplete"` means the tool is not stopped) |
| `response.completed` / **`response.incomplete`** | terminal; `incomplete_details.reason`: `max_output_tokens`→`MAX_TOKENS`, `content_filter`→`CONTENT_FILTERED`, `compaction_required_for_model_handoff`→`COMPACTION_REQUIRED_FOR_MODEL_HANDOFF` (`zXu`) |
| `response.usage["kiro:context_usage_percentage"]` | context % |
| `response.usage["kiro:credits"] = {value, unit, unit_plural}` | metering |
| `error` / `response.failed` (`error.type`, `error.code`) | throws (`PKo`) |
| `kiro:code_reference.added` (`annotations[]`) | license references |

**HTTP errors** ([JS] `IKo`, `NKo`, `IXu`, `CXu`). The body is either `{"error":{"message","type","code"}}` or the smithy style `{"__type","message","reason","retryAfterMilliseconds"}`.
* Retry hint, in priority order: header `x-amzn-kiro-ratelimit-retry-after` (milliseconds), then body `retryAfterMilliseconds`, then `retry-after` (seconds or HTTP date).
* Code mapping: `context_length_exceeded`→`CONTENT_LENGTH_EXCEEDS_THRESHOLD` (413), `model_not_found`→`INVALID_MODEL_ID`, `invalid_function_parameters`→`TOOL_SCHEMA_INVALID`, `invalid_json`→`REQUEST_BODY_INVALID_JSON`, `kiro:model_requires_user_opt_in`→`MODEL_REQUIRES_USER_OPT_IN` (403, but treated as a validation error), `server_is_overloaded`→`MODEL_TEMPORARILY_UNAVAILABLE` (503), `rate_limit_exceeded`→`USER_REQUEST_RATE_EXCEEDED` (429), `credit_balance_exhausted`/`insufficient_quota`→402.

**Timeouts** ([JS] `bnd`): connect 30 s (`KIRO_CONVERSE_CONNECTION_TIMEOUT_MS`). TTFB/request 300 s (`KIRO_CONVERSE_REQUEST_TIMEOUT_MS`, default `3e5`). The raw default if unset is 60 s.

**Retry** ([JS] `hce`/`QClientRetryStrategy`):
* Attempts: `3 + progressBonus(callPosition)`, where the bonus is 0 for ≤2 calls, 1 for ≤6, 2 for ≤11, and 3 above that.
* Backoff, with jitter 0.2: transient base 250 ms (max 20 s); throttling base 1000 ms (max 20 s); connection base 250 ms (max 5 s), plus a 90 s connection window.
* Modeled `retryAfterMilliseconds` is clamped to 300 s.
* Adaptive token bucket from the SDK (`AdaptiveRetryStrategy`).
* Mid-stream there is also a "stall retry" budget and an "authExpiry" retry.

**[KG] status: partial and divergent.** See `proxy/kiro_responses.go`.

| Item | Kiro-Go | Official | Status |
|---|---|---|---|
| URL | `https://runtime.%s.kiro.dev/v1/responses` (`kiro_responses.go:128`) | same | matches |
| Body key order | `model,input,instructions,tools,stream,origin,profileArn,metadata,reasoning` (struct `responsesRequest`) | same | matches |
| `metadata` keys | Go map, so serialized **alphabetically**; `kiro:agent_mode` is always `"vibe"` (`:148`) | insertion order (agent_mode first) | divergent (low) |
| `instructions` | never set. The system prompt is injected as a fake user/assistant pair `"I will follow these instructions."` (`translator.go:341,1611`) | only with AB flag; otherwise the system prompt is the first human message [unverified exact shape] | partial |
| History tool calls | flattened to text unless `KIRO_KEEP_STRUCTURED_TOOL_HISTORY=1` (`translator.go:1983`) | always structured `function_call`/`function_call_output` | **divergent** |
| Reasoning replay | not captured or replayed | `{"type":"reasoning","encrypted_content"}` replayed | missing |
| Documents (`input_file`) | not supported (files are reduced to text or images) | supported (pdf/csv/doc/docx/xls/xlsx/html/txt/md) | missing |
| `reasoning.summary` | only from `reasoning.summary` | from `thinking.display` | partial |
| Headers | adds `Accept: text/event-stream`, `x-amz-user-agent`, **`x-amzn-kiro-agent-mode: vibe`**, `Accept-Encoding: gzip` (Go default) (`kiro_responses.go:373-377`, `kiro_headers.go`) | none of these | **divergent** |
| `x-amzn-kiro-fallback` | not sent | `0;max=0` on every main turn | **missing** |
| `x-amzn-kiro-attempt` | `1;max=2` (`:377`) | `1;max=3` (or more) | divergent |
| `TokenType` | `API_KEY` / `EXTERNAL_IDP` only | also **`SSO_OIDC` for IdC** | **missing for IdC** |
| `User-Agent` | `aws-sdk-js/1.0.0 ua/2.1 os/… lang/js md/nodejs#22.22.0 api/codewhispererruntime#1.0.0 m/N,E KiroIDE-<ver>-<machineId>` | raw custom UA only, no `aws-sdk-js` prefix (§3.1) | **divergent** |
| `response.incomplete` | **not handled**: the stream ends without `completed`, so it returns `errUpstreamTruncatedResponse` (`kiro_responses.go:560-569`, `:619`) | terminal event with MAX_TOKENS / COMPACTION_REQUIRED | **bug** |
| `response.refusal.*` | ignored, so a refusal can surface as an empty-stream error | shown as text | missing |
| `kiro:code_reference.added`, `kiro:keep_alive` | ignored | used | fine (not needed) |
| Error envelope | parses `error.{message,type,code}` only | also `__type`, `reason`, `retryAfterMilliseconds` | partial (retry hint is handled by `kiroRetryAfter`) |
| 429 handling | retries once if the hint is ≤3 s, otherwise hands off to failover | adaptive retry honoring the hint up to 300 s | partial (deliberate for a pool) |

### 1.2 KRS `GenerateAssistantResponse` (`POST /generateAssistantResponse`, restJson): the A/B control arm and auxiliary calls

Schema `He.GenerateAssistantResponseRequest$` ([JS]):
```
conversationState (required), profileArn (body), agentMode -> HEADER x-amzn-kiro-agent-mode,
additionalModelRequestFields (document), systemPrompt (body string)
```
* `ConversationState` = `currentMessage, chatTriggerType, conversationId, history, agentContinuationId, agentTaskType, rootConversationId`.
* `UserInputMessage` = `content, userInputMessageContext{editorState, envState, additionalContext, toolResults, tools}, origin, images[{format,source.bytes}], documents[{name,format,source.bytes}], modelId`.
* `AssistantResponseMessage` = `content, messageId, toolUses[{toolUseId,name,input}], reasoningContent{reasoningText{text,signature}|redactedContent}`.
* `ToolResult` = `toolUseId, content[], status` (`success|error`).
* `ToolSpecification` = `inputSchema{json}, name, description`.

Enums ([JS] `wo.*`):
* `AgentTaskType={spectask,vibe}`; `ChatTriggerType={MANUAL,DIAGNOSTIC,INLINE_CHAT}`.
* `ImageFormat={png,jpeg,gif,webp}`; `DocumentFormat={csv,doc,docx,html,md,pdf,txt,xls,xlsx}`.
* `Origin` includes `AI_EDITOR, CLI, KIRO_CLI, KIRO_CREW, KIRO_WEB, IDE, …` but **not** `KIRO_IDE`.
* `ReasoningEffort={none,low,medium,high,xhigh}`; `ReasoningSummary={auto,concise,detailed}`.
* `StopReason={END_TURN,TOOL_USE,MAX_TOKENS,CONTENT_FILTERED,MODEL_CONTEXT_WINDOW_EXCEEDED,PAUSE_TURN,UNKNOWN}`.

How the official client builds `conversationState` ([JS] the `q.converse.identity_fallback` function):
* `conversationId` comes from the session, falling back to `crypto.randomUUID()` (UUID v4).
* `rootConversationId` defaults to `conversationId`.
* `chatTriggerType` is always `"MANUAL"`. `agentTaskType` comes from the session (`SPEC_TASK` is forced in spec modes, `RJo`).
* Tools are attached **only to the current message**. The current message's `modelId` and `origin` are re-stamped.
* Tool-use/tool-result pairing is validated and "alternation filler" messages are inserted where needed.

Response: the header `x-amzn-kiro-conversation-id` is returned. The body is an AWS binary event stream (`hpr` = `parseBinaryEventStream`). The `ChatResponseStream` union members are:
* `assistantResponseEvent{content,modelId}`
* `reasoningContentEvent{text,redactedContent,signature}`
* `codeReferenceEvent`
* `toolUseEvent{toolUseId,name,input,stop}`
* `toolResultEvent`
* `metadataEvent{tokenUsage{uncachedInputTokens,outputTokens,totalTokens,cacheReadInputTokens,cacheWriteInputTokens,contextUsagePercentage,normalizedTokenUsage},stopReason,stopDetails{refusal{category,explanation,recommendedModel}}}`
* `meteringEvent{usage,unit,unitPlural}`
* `contextUsageEvent{contextUsagePercentage}`
* `documentCitationEvent`
* **error members delivered as normal events**: `error`, `throttlingError`, `validationError`, `serviceUnavailableError`. These are mapped to exceptions by `uUo`.

**Smithy client headers** ([JS] `getKrsClient` in `A3r`): the AWS SDK v3 UA middleware gives `user-agent: aws-sdk-js/1.0.0 ua/2.1 os/<platform>#<release> lang/js md/nodejs#<ver> api/kiroruntime#1.0.0 m/<features> <customUA>` and `x-amz-user-agent: aws-sdk-js/1.0.0 <customUA>`. The client package is `@amzn/kiro-runtime-service-typescript-client` v1.0.0 with `serviceId "KiroRuntime"`. [unverified: exact `m/` feature letters]
* `amz-sdk-invocation-id`, `amz-sdk-request: attempt=N; max=M`
* `content-type: application/json`
* `x-amzn-kiro-agent-mode`
* `x-amzn-kiro-attempt` (`hSr`), `x-amzn-kiro-fallback` (`OKo`)
* `TokenType` (`_g`)
* `x-amzn-codewhisperer-optout: true` **only when content collection is disabled** (`pLi`: otherwise the header is omitted)
* optional: `x-amzn-kiro-client-attribution` (`fLi`), `x-amzn-kiro-mode-identifier` (`mLi`), `x-kiro-eval-strategy` / `x-kiro-eval-treatments` (`dRt`), test headers `x-amzn-kiro-execution-id`, `-agent-name`, `-session-id`, `-agent-presentation` (`gLi`)
* `Authorization` is re-minted on retries (`oJo`, "kiroRetryBearerMiddleware")

HTTP errors are JSON `{__type,message,reason,retryAfterMilliseconds}` plus the `x-amzn-kiro-ratelimit-retry-after`, `retry-after` and `x-amz-retry-after` headers ([JS] `dUo`).

**[KG] status: missing (wrong host).** Kiro-Go's "legacy" protocol (`kiro.go:54-73`) targets `q.us-east-1.amazonaws.com/generateAssistantResponse` (restJson, empty target) and `codewhisperer.us-east-1.amazonaws.com` (with `X-Amz-Target`, which is meaningless for restJson). Kiro-Go's own comment in `kiro_responses.go:5-9` says those hosts now answer `403 FEATURE_NOT_SUPPORTED`. The live restJson GAR is on **`runtime.{region}.kiro.dev/generateAssistantResponse`**. The parser itself (`parseEventStreamTracked`) is compatible, with these gaps:
* (a) event-types `error|throttlingError|validationError|serviceUnavailableError` are **silently ignored** (`kiro.go:752-806` switch), because only `:message-type` error/exception is checked;
* (b) `reasoningContentEvent.signature`/`redactedContent` are dropped;
* (c) `metadataEvent.stopDetails.refusal` is ignored.

### 1.3 KRS `GetFeatureConfiguration` (`GET /featureConfiguration?origin=<origin>&version=<clientVersion>`)

* Header `x-amzn-kiro-profile-arn: <arn>`. Response `{configuration: {<key>: <value>}}`.
* Client: `maxAttempts:1`, timeout 5 s (`JYu`), cache TTL 15 min (`GVo=900e3`), re-fetch after failure in 60 s (`zVo`). The cache is dropped on identity change ([JS] `HVo`, `ExperimentConfigService`).
* The UA for this call is the "control" UA (`backendUserAgent.experimentFetch`).
* Keys that matter: `create_response_api`, `create_response_api_auxiliary`, `system_field_injection`, `user_agent_refactoring_enabled` ([JS] `Dn.AB_*`), plus the eval strategy.

**[KG] status: missing.** Kiro-Go never calls it. As a consequence:
* (1) a statistical gap: every official session fetches this before its first turn;
* (2) Kiro-Go cannot learn which arm (CreateResponse vs GAR) an account is assigned to. It hard-codes CreateResponse, while the official client sends GAR for accounts in the control arm.

### 1.4 KRS `InvokeMCP` (`POST /mcp`) and `InvokeMCPStream` (`POST /mcp/stream`): remote tools (web search etc.)

* Request: JSON-RPC body `{id, method, jsonrpc:"2.0", params}`. `profileArn` goes in **header `x-amzn-kiro-profile-arn`** (schema `He.InvokeMCPRequest$`).
* `MCPMethod={initialize, notifications/initialized, tools/list, tools/call}`.
* The official remote MCP client ([JS] `kUr`) first calls `tools/list` (`id:"tools_list"`) and then `tools/call` `{name, arguments}`, with `id` = the tool-call id. Retry strategy `withProgressBonus`. Stream responses are either `application/vnd.amazon.eventstream` or SSE (`M5u`).
* The built-in tool `web_search` (alias `remote_web_search`) is a remote tool ([JS] capability map `remote_web_search:"web_search"`). `web_fetch` runs **locally** (axios, UA `KiroIDE`, `WebFetchClient`); it is not a backend call.

**[KG] status: divergent.** `proxy/websearch.go:268` posts to **`https://q.{region}.amazonaws.com/mcp`**, which is not used by KAS. It never calls `tools/list`. It uses a synthetic id `web_search_tooluse_<22>_<ms>_<8>` and the IDE-streaming UA, plus `Amz-Sdk-Request: attempt=1; max=3`. Rust v2 still has `AmazonCodeWhispererStreamingService.InvokeMCP` (awsJson) [BIN], so the q host may still work, but it is not what the default client does.

### 1.5 KRS `GenerateCompletions` (`POST /generatecompletions`)
Inline completions / next-edit predictions. **Not needed** by a chat gateway.

### 1.6 CPS (`management.{region}.kiro.dev`): control-plane operations

All operations use bearer auth. Client: `maxAttempts:3`, connect/request timeout 15 s (`fee`). UA = custom UA via the smithy middleware, giving `api/kirocontrolplanebearer#1.0.0`. `TokenType` comes from `_g`. Schemas below are from [JS] `Oe.*`.

| Operation | Method and path | Request | Response (key fields) |
|---|---|---|---|
| ListAvailableModels | `GET /List-Available-Models?origin=&maxResults=&nextToken=&profileArn=&modelProvider=` | origin required | `models[Model]`, `defaultModel`, `nextToken`. `Model` = `modelId, modelName, description, disclaimers, status, modelProvider, rateMultiplier, rateUnit, tokenLimits{maxInputTokens,maxOutputTokens}, supportedInputTypes, promptCaching{supportsPromptCaching,minimumTokensPerCacheCheckpoint,maximumCacheCheckpointsPerRequest}, availableOrigins, additionalModelRequestFieldsSchema, refusalFallbackModels, consentStatus, termsUrl, consentDescription` |
| GetUsageLimits | `GET /Get-Usage-Limits?origin=&profileArn=&resourceType=&isEmailRequired=` | the official client sends **only origin and profileArn** ([JS] `Xnd`) | `limits, nextDateReset, daysUntilReset, usageBreakdownList[UsageBreakdown{currentUsage,currentOverages,usageLimit,overageCharges,currency,resourceType,displayName,…WithPrecision,unit,overageRate,nextDateReset,overageCap,freeTrialInfo,bonuses,overageCredits,addOnMetadata,dimensionType}], subscriptionInfo{type,upgradeCapability,overageCapability,subscriptionManagementTarget,subscriptionTitle}, overageConfiguration{overageStatus}, userInfo{userId,email}` |
| GetUserPreference | `GET /Get-User-Preference?profileArn=&filter=` | | `modelConsents[{modelId,status,consentedAt}]`, `modelFallbacks{perModel}` |
| SetUserPreference | `POST /Set-User-Preference` | `{profileArn, overageConfiguration{overageStatus}, modelConsents, modelFallbacks{perModel}}` | |
| ListAvailableProfiles | `POST /List-Available-Profiles` | `{maxResults, nextToken}` | `profiles, nextToken` |
| GetProfile | `POST /Get-Profile` | `{profileArn}` | `profile` (also used as the API-key **region probe**: [JS] `jaa`, 10 s timeout, `maxAttempts:1`, 403 means wrong region) |
| CreateSubscriptionToken | `POST /Create-Subscription-Token` | `{profileArn, clientToken(idempotency), statusOnly, provider, subscriptionType, successUrl, cancelUrl, creditQuantity}` | `status, encodedVerificationUrl, token` |
| ListAvailableSubscriptions | `POST /List-Available-Subscriptions` | `{profileArn}` | `subscriptionPlans, disclaimer` |
| ListNotifications | `GET /List-Notifications?profileArn=&origin=&clientVersion=` | | |
| Create/Delete/ListApiKeys | `POST /Create-Api-Key`, `/Delete-Api-Key`, `/List-Api-Keys` | | |
| GetEffectiveEnterpriseManagementResourceWithToken | governance (awsJson target) | | |

CPS errors:
* `ThrottlingException{message}` (429)
* `ValidationException{message, reason, missingPermissions}` (400), with `reason` from `cr.ValidationExceptionReason` (e.g. `FallbackChainTooLong`, `BuilderIdProTier4xx`, `FieldValidationFailed`, …)
* `AccessDeniedException{message, reason: FEATURE_NOT_SUPPORTED|KmsAccessDenied}` (403)
* `NewUserAccessPausedError` / "Kiro access not available for this account" ([JS] `MZi`)

ListAvailableModels caching ([JS] `won`=`ACPModelConfigProvider`): TTL 5 min (`_nd=300e3`); empty-result TTL 30 s; fetch timeout 30 s; pagination cap 10 pages (`QAo`); a periodic refresh is skipped when signed out. Each model is post-processed into `effortLevels/effortSchemaPath/defaultEffortLevel` (`yPu`) and `thinkingToggleable/defaultThinkingEnabled` (`bPu`).

**[KG] status: divergent (old IDE REST surface).**

| Op | Kiro-Go | Status |
|---|---|---|
| ListAvailableModels | `GET https://codewhisperer.us-east-1.amazonaws.com/ListAvailableModels?origin=AI_EDITOR&maxResults=50&profileArn=…` (`kiro_api.go:218-223`). **No pagination**: `nextToken` is ignored. Drops `status`, `consentStatus`, `refusalFallbackModels`, `availableOrigins`, `defaultModel`. | divergent / partial |
| GetUsageLimits | `GET {codewhisperer\|q}/getUsageLimits?origin=AI_EDITOR&resourceType=AGENTIC_REQUEST&isEmailRequired=true&profileArn=` (`kiro_api.go:152`, `kiro_overage.go:51`) | divergent (old IDE shape) |
| SetUserPreference (overage) | `POST https://q.us-east-1.amazonaws.com/setUserPreference` `{overageConfiguration:{overageStatus},profileArn}` (`kiro_overage.go:116-153`) | divergent host and path, same body |
| ListAvailableProfiles | `POST {codewhisperer\|q.{region}}/ListAvailableProfiles {maxResults:10,nextToken}` (`kiro_api.go:536`) | divergent host |
| GetUserInfo | `POST codewhisperer.us-east-1.amazonaws.com/GetUserInfo {"origin":"KIRO_IDE"}` (`kiro_api.go:187`). This operation **does not exist** in either the CLI or KAS, and `KIRO_IDE` is not a valid runtime `Origin` enum value. | **divergent / suspicious** |
| GetUserPreference, model consents, model fallbacks, CreateSubscriptionToken, ListNotifications | not implemented | missing (mostly not needed) |

### 1.7 Rust-engine legacy CodeWhisperer operations (v1/v2 engine and the `/usage` command)

[BIN] has awsJson1.0 targets for:
* `AmazonCodeWhispererService.GetUsageLimits`, `.ListAvailableProfiles`, `.GetProfile`, `.SendTelemetryEvent`
* `AmazonCodeWhispererStreamingService.GenerateAssistantResponse`, `.InvokeMCP`
* `KiroControlPlaneBearerService.ListAvailableModels`, `.GetUserPreference`, `.SetUserPreference`

Calls go to `POST /`, `content-type: application/x-amz-json-1.0`, `x-amz-target: <Service>.<Op>`. The Rust endpoint table ([BIN] `api_client/endpoints.rs:176`) lists `q.{region}.amazonaws.com`, `runtime.{region}.kiro.dev` and `management.{region}.kiro.dev` per partition.

Rust interceptors ([BIN] and [QCLI] `api_client/mod.rs`):
* `OptOutInterceptor`: `x-amzn-codewhisperer-optout` = `!shareCodeWhispererContentWithAWS`; the [QCLI] default is share=true, so `false`
* `UserAgentOverrideInterceptor`: AWS UA + `md/appVersion-3.5.0` + `app/AmazonQ-For-CLI`
* `TokenTypeInterceptor` (`EXTERNAL_IDP`, `API_KEY`)
* `AttemptHeaderInterceptor` (`x-amzn-kiro-attempt` / `-fallback`)
* `DelayTrackingInterceptor`
* Retry: adaptive, max 3 attempts, max backoff 10 s. Timeouts: 5 min, with a 5 min stalled-stream grace period ([QCLI] `retry_config`, `timeout_config`).

**[KG] API-key path** (`kiro.go:77-82`, `:497-518`): `POST https://runtime.{region}.kiro.dev/`, `X-Amz-Target: AmazonCodeWhispererStreamingService.GenerateAssistantResponse`, `application/x-amz-json-1.0`, origin `KIRO_CLI`, optout `false`, `tokentype: API_KEY`. **The protocol matches Rust v2, but the UA is the KiroIDE/aws-sdk-js one.** A real v2 client would send `aws-sdk-rust/… md/appVersion-3.5.0 app/AmazonQ-For-CLI`, and a real v3 client would use restJson or `/v1/responses` with the `KiroCLI/3.5.0 KAS/…` UA. The result is a hybrid that no real client produces (divergent, see §3).

### 1.8 Authentication and token refresh

| Flow | Official | Kiro-Go | Status |
|---|---|---|---|
| Social refresh | `POST https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken`, `Content-Type: application/json`, body `{"refreshToken"}`. Response `{accessToken, refreshToken, profileArn, expiresIn}`. KAS keeps the old `profileArn` if none comes back ([JS] `w9d`). Rust sets a `User-Agent` ([BIN] `kirocli:social:tokenUser-AgentrefreshToken`). Refresh happens when <180 s remain ([JS] `m9d`). | `auth/oidc.go:101` uses the same URL and body, with **Go default UA `Go-http-client/x`** and no explicit Accept. Skew is 120 s (`handler.go:25`) / 300 s (`pool/account.go:14`). | partial (UA) |
| Social login | device flow `/oauth/device/authorization` + `/oauth/device/poll`, and PKCE `/oauth/token` on the same host ([BIN] `social.rs`). Default social profile `arn:aws:codewhisperer:us-east-1:699475941385:profile/EHGA3GRVQMUK` ([BIN]). | import only | not-needed |
| IdC / BuilderId refresh | `SSOOIDCClient.CreateToken{clientId,clientSecret,refreshToken,grantType:"refresh_token"}` at `oidc.{region}.amazonaws.com/token` using the registration in `~/.aws/sso/cache/<clientIdHash>.json`. Fails if the registration has expired ([JS] `b9d`). SDK UA `aws-sdk-js/3.x … api/ssooidc#…` (KAS) or `aws-sdk-rust … api/ssooidc/1.100.0` (Rust, crate `aws-sdk-ssooidc-1.100.0`). | `auth/oidc.go:54`: same JSON body, Go UA, no `amz-sdk-*`. Does **not** check client-registration expiry. | partial |
| External IdP refresh | `POST <tokenEndpoint>` form `grant_type=refresh_token&client_id&refresh_token` (**no scope**) ([JS] `S9d`) | `auth/microsoft_sso.go:633` adds `scope`, validates issuer and endpoint (good) | partial (extra `scope`) |
| OIDC RegisterClient | `POST /client/register`; [QCLI] clientName `"Amazon Q Developer for command line"`, clientType `public`, scopes `codewhisperer:{completions,analysis,conversations}`. 3.5 Rust has `Kiro CLI` strings and `api.oidc.scopePrefix`. | `builderid.go:50`/`iam_sso.go:162` use `"Kiro"`. **`sso_token.go:72` uses `"Kiro API Proxy"`.** | **divergent (self-identifying)** |
| StartDeviceAuthorization / CreateToken(device_code) | `/device_authorization`, `/token` | `sso_token.go` + `builderid.go`. `sso_token.go` also **auto-approves** the device code using a portal SSO bearer (`/token/whoAmI`, `/session/device`, `/device_authorization/accept_user_code`, `/associate_token`). | divergent (no official equivalent) |
| API key | `KIRO_API_KEY` sent as the bearer, `TokenType: API_KEY`; region discovered via CPS `GetProfile` probe ([JS] `jaa`, `Mfn`); machineId header `x-kiro-machineid` on telemetry/BFF ([JS] `FPn`) | API-key accounts set `tokentype: API_KEY` (lowercase, `kiro_headers.go:72`), machineId = `sha256("KiroAPIKey/"+key)` (`config.go:621`), region from `key|region` | partial |

Token file / refresh correctness notes from KAS ([JS] `FileAuthProvider`, `s4e`):
* token file is `~/.aws/sso/cache/kiro-auth-token.json`; a **symlink there is rejected** (`SymlinkDetectedError`); it is written with mode `0600`.
* refresh is guarded by a single in-flight `refreshPromise` per provider (no stampede).
* profileArn is read from the IDE's `profile.json` (`globalStorage/kiro.kiroagent/profile.json`) when absent from the token.

### 1.9 Telemetry

* **KAS (v3) OTLP telemetry**: `https://telemetry.{us-east-1,eu-central-1}.kiro.dev` → `/v1/traces` and `/v1/metrics` ([JS] `iEt`, `OTLPTraceExporter`). Headers include `x-kiro-machineid` (anonymous) and, when signed in, `authorization: Bearer <token>`, `x-kiro-profile-arn`, `x-kiro-token-type` ([JS] `FPn`, `Q_t`). Metric dimensions: `KiroClientVersion, KasVersion, Platform, machineId, channel, version, ClientType, clientName, userCohort, isContentCollectionOptIn, activeExperiments` ([JS] `_R`, `PPn`). Controlled by `KIRO_TELEMETRY_ENABLED`. Residency follows the profile region.
* **Agent activity publish**: `POST <endpoint>/agents/activity` with `User-Agent: KiroAgent`, `Authorization: Bearer`, `TokenType`, `x-amzn-kiro-profile-arn` ([JS] `publishBatch`, `_td="/agents/activity"`). Only enabled with `--activity-file`.
* **Rust legacy toolkit telemetry**: `AmazonCodeWhispererService.SendTelemetryEvent` (bearer) + `PostMetrics` to `https://client-telemetry.us-east-1.amazonaws.com` using **Cognito** sigv4 (identity pool `us-east-1:820fd6d1-95c0-4ca4-bffb-3f01d32da842`, `GetId`+`GetCredentialsForIdentity`) ([BIN] `legacy_sink.rs`, `cognito.rs`). Event types include `KiroChatMessageEvent`, `ChatInteractWithMessageEvent`, etc.

**[KG] status: not-needed by design, but see §3.** Kiro-Go is a multi-account pool; sending per-account telemetry would be counter-productive. Its own telemetry (`telemetry/telemetry.go`) is internal only. The *absence* of any telemetry is itself a conformance signal (§3.4).

---

## 2. Cross-check summary table (Kiro-Go vs official)

| Area | Official (3.5.0) | Kiro-Go | Verdict |
|---|---|---|---|
| Main chat host | `runtime.{region}.kiro.dev` (`/v1/responses` or `/generateAssistantResponse`) | `/v1/responses` on same host; "legacy" arm points at dead `q`/`codewhisperer` hosts | partial |
| Which API per account | decided by `GetFeatureConfiguration.create_response_api` | always `/v1/responses` (auto, with legacy fallback) | divergent |
| Body shapes (`/v1/responses`) | §1.1 | mostly match | partial |
| GAR event-stream parse | full union incl. inline error events, reasoning signature, refusal | ignores inline error events, signature, refusal | partial |
| ListAvailableModels | CPS `management` host, paginated, rich Model | old `codewhisperer` REST, no pagination | divergent |
| GetUsageLimits/overage | CPS `management` host | old `q`/`codewhisperer` host | divergent |
| MCP (web search) | `runtime…/mcp` + `tools/list` first | `q…/mcp`, no `tools/list` | divergent |
| GetFeatureConfiguration | always, before first turn | never | missing |
| Token refresh | per §1.8, single-flight, symlink-safe | matches intent; UA + IdC token-type gaps | partial |
| Native effort / thinking | `reasoning.effort` + `thinking.type` from model schema | `reasoning.effort` only (`thinking_request.go`), `thinking.type` parsed but never emitted | partial |
| Prompt caching | model-declared `promptCaching`, `cachePoint` blocks | experimental cache points on GAR behind `KIRO_CACHE_POINTS=1` (`proxy/cache_points.go`), unverified | partial |
| Documents | pdf/docx/csv/… as `input_file` | not supported | missing |
| Images | per-family size/edge limits (§4) | generic base64 passthrough | partial |
| User-Agent | `KiroCLI/3.5.0 KAS/<ver> os/… md/appVersion-3.5.0 app/AmazonQ-For-CLI` (raw) + SDK UA on smithy calls | `aws-sdk-js/… KiroIDE-<ver>-<machineId>` everywhere | **divergent** |

---

## 3. Conformance analysis (how Kiro-Go's traffic differs from a genuine 3.5.0 client)

This is a wire-protocol conformance review: the gateway must emit the same request shape the backend's own clients emit, or requests risk 4xx rejections and the account-health heuristics the backend runs. Ranked by how likely each is to cause a request to be rejected or an account to be flagged.

### 3.1 User-Agent is the single strongest signal — **HIGH**

* **Official CLI (`/v1/responses` raw path):** `User-Agent` is the raw custom UA only, built by `ZAo`/`buildClientUserAgent`: for kiro-cli it is
  `KiroCLI/3.5.0 KAS/<0.66.119> os/<platform> md/appVersion-3.5.0` (plus any client UA tags). The binary literal is
  `KiroCLI/3.5.0 KAS/<kas> os/<os> md/appVersion-3.5.0 app/AmazonQ-For-CLI` ([BIN] od dump). There is **no `aws-sdk-js` token** and **no `x-amz-user-agent`** on this path.
* **Official smithy calls (GAR / CPS / KRS MCP):** `user-agent: aws-sdk-js/1.0.0 ua/2.1 os/<os> lang/js md/nodejs#<ver> api/<svc>#1.0.0 m/<feats> <customUA>` and `x-amz-user-agent: aws-sdk-js/1.0.0 <customUA>`, where `<customUA>` is the same `KiroCLI/3.5.0 KAS/… app/AmazonQ-For-CLI` string.
* **Kiro-Go (every call):** `aws-sdk-js/1.0.0 ua/2.1 os/<SystemVersion> lang/js md/nodejs#<node> api/codewhispererruntime#1.0.0 m/N,E KiroIDE-<kiroVersion>-<machineId>` and `x-amz-user-agent: aws-sdk-js/1.0.0 KiroIDE-<kiroVersion>-<machineId>` (`kiro_headers.go:35-47`).

Differences a backend can key on:
1. `KiroIDE-…` vs `KiroCLI/… app/AmazonQ-For-CLI`. Kiro-Go advertises the **IDE**, which should send origin `AI_EDITOR`; Kiro-Go does send `AI_EDITOR` for OAuth, so that is at least internally consistent, but the app-name token is wrong (`app/AmazonQ-For-CLI` is absent).
2. `api/codewhispererruntime#1.0.0` — the real service token is `api/kiroruntime#1.0.0` (KRS) / `api/kirocontrolplanebearer#1.0.0` (CPS). `codewhispererruntime`/`codewhispererstreaming` are the retired names.
3. The `-<machineId>` suffix appended to the UA is not something any official client does. The official machineId travels in `x-kiro-machineid` (telemetry) and never in the UA.
4. `KiroVersion` default `1.4.154` (`config.go:1513`) is an IDE version; the CLI advertises `3.5.0` and KAS `0.66.119`.

**Recommendation (P0):** split the UA by transport. For `/v1/responses`, send exactly `KiroCLI/3.5.0 KAS/0.66.119 os/<os> md/appVersion-3.5.0 app/AmazonQ-For-CLI`, no `aws-sdk-js`, no `x-amz-user-agent`, no machineId suffix. For any smithy-shaped call, send the `aws-sdk-js/… api/kiroruntime#1.0.0 … <customUA>` form with the same custom tail. Drop the machineId from the UA entirely.

### 3.2 Header set on `/v1/responses` — **HIGH**

The official raw path sends a tight, fixed set (§1.1). Kiro-Go adds headers that the raw path never sends: `Accept: text/event-stream`, `x-amz-user-agent`, `x-amzn-kiro-agent-mode: vibe`, `Amz-Sdk-Request`-style attempt (actually it sends `x-amzn-kiro-attempt: 1;max=2`), and Go's default `Accept-Encoding: gzip`. It also omits `x-amzn-kiro-fallback`. Recommended fixed header set (P0), in order: `Content-Type, Authorization, x-amzn-kiro-origin, x-amzn-codewhisperer-optout, x-amzn-kiro-profile?, TokenType?, User-Agent, x-amzn-kiro-attempt, x-amzn-kiro-fallback, Content-Length`. Remove `Accept`, `x-amz-user-agent`, `x-amzn-kiro-agent-mode`. Note `agent_mode` already rides in `metadata["kiro:agent_mode"]`, which is correct and sufficient.

### 3.3 `x-amzn-kiro-attempt` / `-fallback` values — **MEDIUM**

Official: attempt `1;max=3` (plus the progress bonus, so `1;max=3..6`), fallback `0;max=0` on the first main turn. Kiro-Go sends `1;max=2` and omits fallback. Set `max` to 3 and always add `x-amzn-kiro-fallback: 0;max=0` on `/v1/responses` and GAR.

### 3.4 Request cadence and the missing side-calls — **MEDIUM**

A genuine session, before its first chat turn, does: `GetFeatureConfiguration` (once, 15-min TTL), `ListAvailableModels` (CPS, 5-min TTL), and emits OTLP telemetry. Kiro-Go does none of these per-conversation; it refreshes models every **30 min** (`handler.go:328`, `models_dynamic.go:18`) against the wrong host. A backend that correlates "chat turns with no preceding featureConfiguration/model-list and no telemetry" can distinguish the pool. Mitigation is a trade-off for a pool: at minimum, call `GetFeatureConfiguration` and CPS `ListAvailableModels` on the real hosts so the model/consent/effort metadata is correct, and consider a low-rate `GetUsageLimits`.

### 3.5 Metadata key ordering and `agent_mode` — **LOW**

Go marshals `metadata` alphabetically; the client emits `kiro:agent_mode` first. JSON object order is rarely load-bearing, but to match exactly, emit metadata via an ordered encoder. Also, Kiro-Go hard-codes `agent_mode:"vibe"`; the official client can send `spec/plan/...`. For a chat gateway `vibe` is correct.

### 3.6 `conversationId` derivation — **LOW/INFORMATIONAL**

Official: `crypto.randomUUID()` minted once per session and reused across turns; `rootConversationId==conversationId`. Kiro-Go derives a **deterministic SHA1 UUID** from `(model, systemPrompt, firstMessage)` (`translator.go:2392`) so that stateless OpenAI/Anthropic clients get a stable id. This is a reasonable adaptation, but note: (a) two different users with identical first prompts collide on one conversationId; (b) the official ids are v4 (random), so a v5 (SHA1, `NameSpaceURL`) id is statistically distinguishable by version nibble. Consider minting v4 and caching by session-affinity key instead.

### 3.7 TokenType completeness — **MEDIUM (also correctness)**

Official `Q4n`/`ide` map: `{external_idp:EXTERNAL_IDP, machine_token:KIRO_MACHINE_TOKEN, api_key:API_KEY, IdC:SSO_OIDC}`; social/BuilderId send **no** TokenType. Kiro-Go sends only `API_KEY` and `EXTERNAL_IDP` (`kiro_headers.go:72-74`) and never `SSO_OIDC` for IdC accounts. Add `SSO_OIDC` for `authMethod=="idc"`.

---

## 4. Features the official client uses that Kiro-Go could adopt

1. **Native thinking toggle `thinking.type`** ([JS] `bPu`,`SPu`,`B6t`). Models whose schema has `thinking.type.enum` containing both `adaptive` and `disabled` are toggleable; emit `additionalModelRequestFields.thinking.type = "adaptive"|"disabled"` alongside effort. Kiro-Go already parses the toggle (`thinking_request.go:232`,`:318`) but **never emits it** — `nativeReasoningFields` only writes `{path:{effort}}`. Add the `thinking.type` field, and apply the "top effort needs thinking on" cap (`B6t`/`Rme`: when thinking is off, `xhigh`/`max` are downgraded to the highest non-{xhigh,max} level — Kiro-Go already does a similar clamp manually at `thinking_request.go:380`).
2. **`reasoning.summary` from `thinking.display`** — surface a concise/detailed summary toggle.
3. **Documents** (`input_file`, formats csv/doc/docx/html/md/pdf/txt/xls/xlsx; `DocumentBlock`) — map Anthropic `document` blocks and OpenAI file inputs to `input_file` data URLs. Validation reasons to respect: `DOCUMENT_COUNT_EXCEEDED`, `DOCUMENT_SIZE_EXCEEDED`, `DOCUMENT_MAXIMUM_PAGES_EXCEEDED`, `DOCUMENT_MODEL_NOT_SUPPORTED`, `DOCUMENT_PASSWORD_PROTECTED`, `DOCUMENT_DUPLICATE_NAME`.
4. **Per-model image limits** ([JS] `h0c`,`f0c`): claude `maxSide 8000, maxBase64 5_242_880, maxMediaBlocks 100` (readLongEdge 1568, or 2576 for claude-4..7); gpt `maxBase64 11_582_828`; kimi/grok `6_730_572`. No-vision models: `deepseek-3.2, glm-5, minimax-m2.5, qwen3-coder-next, minimax-m2.1, kirin-inference-glm` — reject/placeholder images for these. On `IMAGE_SIZE_EXCEEDED`/`IMAGE_DIMENSION_EXCEEDED` the client tightens and retries (`ImageProfileCorrections`).
5. **Prompt caching** — model `promptCaching{supportsPromptCaching,minimumTokensPerCacheCheckpoint,maximumCacheCheckpointsPerRequest}` is already stored (`kiro_api.go:904`). The GAR union has a `CachePoint`/`CachePointType={default}` block (codewhisperer namespace). Emitting cache points on long stable prefixes would cut credits. (CreateResponse body has no explicit cachePoint field — caching there is implicit/server-side.) Surface `cacheReadInputTokens`/`cacheWriteInputTokens` into client `usage` (Kiro-Go already reads them in `OnCacheUsage`).
6. **Context usage %** → the official UI shows it and triggers auto-compaction; Kiro-Go reads `contextUsagePercentage` (good) but does not expose a compaction signal.
7. **Model fallback semantics** — `refusalFallbackModels` per model, and the `x-amzn-kiro-fallback` header `taken;max;cause=<trigger>;from=<model>` describing a mid-turn model handoff. Useful if Kiro-Go ever retries on a different model.
8. **`response.incomplete` / `COMPACTION_REQUIRED_FOR_MODEL_HANDOFF`** terminal handling (currently a bug, §1.1).
9. **Tool result `status`** — Kiro-Go hard-codes `"success"` (`translator.go:1571`). Map Anthropic `is_error:true` (it is parsed at `translator.go:835`) to `status:"error"` so the model sees failed tools.
10. **Reasoning replay** — carry `encrypted_content` reasoning items back into history on multi-turn tool loops, as the official client does.

---

## 5. Security notes

1. **No TLS downgrade / host-injection risk in Kiro-Go.** Regions are validated (`config.SafeRegion`, `awsRegionPattern`) before interpolation into hostnames, and profile ARNs are parsed strictly (`parseKiroProfileArn`). Good.
2. **Token-at-rest / symlink.** The official KAS refuses a symlinked token file and writes `0600` ([JS] `Faa`,`g9d`). Kiro-Go stores credentials in `config.json`; ensure that file is `0600` and not a symlink (not verified here).
3. **IdC client-registration expiry** — the official refresh checks the SSO client registration's `expiresAt` and raises a distinct error before calling CreateToken ([JS] `b9d`). Kiro-Go's `refreshOIDCToken` does not; an expired registration yields an opaque failure. Low risk.
4. **Refresh-token rotation** — Kiro-Go correctly preserves the old refresh token when a refresh response omits one (`kiro_api.go` ResolveProfileArn path; `oidc.go finishRefresh`) and persists rotated tokens for external_idp. This matches the official "keep old on absent" behavior ([JS] `w9d`). Good.
5. **`clientName: "Kiro API Proxy"`** in `auth/sso_token.go:72` registers an OIDC client that self-identifies as a proxy to AWS. Change to `"Kiro"` (the other two call sites already use `"Kiro"`).
6. **`GetUserInfo` with `origin:"KIRO_IDE"`** (`kiro_api.go:187`) calls a non-existent operation on the retired host with an invalid origin enum; it will 4xx and leaks a non-conforming request. Replace with CPS `GetUsageLimits` (which returns `userInfo{email,userId}`), as `auth/sso_token.go:305` already does.
7. **Debug payload logging** (`kiro.go:425`, `kiro_responses.go:354`) logs full request bodies (including user content) at debug level. Ensure debug is off in production or redact.
8. **Bearer over plaintext** — the official raw path warns if the endpoint is not https ([JS] `CKo`). Kiro-Go always uses https literals, so fine.

---

## 6. Prioritized action list

### P0 (request rejections / strongest conformance signals)
1. **Fix the User-Agent per transport** (`proxy/kiro_headers.go buildKiroHeaderValues`, split callers in `kiro.go`/`kiro_responses.go`). For `/v1/responses`: `User-Agent: KiroCLI/3.5.0 KAS/0.66.119 os/<os> md/appVersion-3.5.0 app/AmazonQ-For-CLI`; drop `x-amz-user-agent` and the machineId suffix. For smithy-shaped calls: `aws-sdk-js/1.0.0 … api/kiroruntime#1.0.0 … <that custom tail>`. Change `DefaultKiroVersion` usage so the CLI identity is advertised, not `KiroIDE-1.4.154`.
2. **Trim `/v1/responses` headers** (`kiro_responses.go:373-379`): remove `Accept`, `x-amz-user-agent`, `x-amzn-kiro-agent-mode`; add `x-amzn-kiro-fallback: 0;max=0`; set `x-amzn-kiro-attempt: 1;max=3`. Disable transparent gzip or match the client (Node sends none).
3. **Move the data-plane host off the dead `q`/`codewhisperer` hosts for control-plane calls.** Point `ListAvailableModels`, `GetUsageLimits`, overage `SetUserPreference`, `ListAvailableProfiles` at `management.{region}.kiro.dev` with the `/List-Available-Models`, `/Get-Usage-Limits`, `/Set-User-Preference`, `/List-Available-Profiles` restJson paths (`proxy/kiro_api.go`, `proxy/kiro_overage.go`). Verify live before shipping.
4. **Handle `response.incomplete`** as a terminal event in `parseResponsesSSE` (`kiro_responses.go:560`), mapping `incomplete_details.reason` the same way `response.completed` does (MAX_TOKENS / CONTENT_FILTERED / COMPACTION_REQUIRED), instead of returning `errUpstreamTruncatedResponse`.

### P1 (correctness / likely-flagged behavior)
5. **Add `TokenType: SSO_OIDC` for IdC accounts** (`kiro_headers.go:66-75`).
6. **Call `GetFeatureConfiguration`** (`GET runtime…/featureConfiguration?origin=&version=`, header `x-amzn-kiro-profile-arn`) once per account with a 15-min cache, and honor `create_response_api`: send GAR for control-arm accounts instead of forcing `/v1/responses`. New file, called from the model-refresh loop.
7. **Emit `thinking.type`** in `nativeReasoningFields` (`thinking_request.go:365`) for toggleable models; keep the effort clamp.
8. **Map tool-result error status** (`translator.go:1571` and the Anthropic `is_error` path at `:835`) to `status:"error"`.
9. **Fix `GetUserInfo`** (`kiro_api.go:187`): drop the `/GetUserInfo` call and `origin:"KIRO_IDE"`; derive email/userId from CPS `GetUsageLimits.userInfo`.
10. **MCP web search**: post to `runtime…/mcp` and precede `tools/call` with `tools/list` (`websearch.go:268`). Put `profileArn` in `x-amzn-kiro-profile-arn` (already done) and drop `Amz-Sdk-Request`.
11. **Set OIDC `clientName:"Kiro"`** in `auth/sso_token.go:72`.
12. **Social-refresh UA**: set a `User-Agent` on `auth/oidc.go` refresh requests instead of Go's default (`Go-http-client`).

### P2 (parity features / polish)
13. Documents (`input_file`) support and the per-family image-size limits (§4.3–4.4).
14. Keep structured tool history on `/v1/responses` (drop the `sanitizeKiroHistory` flattening when on the responses transport — it is only needed for the retired GAR awsJson path).
15. Reasoning replay (`encrypted_content`) across tool loops.
16. Mint conversationId as v4 keyed by session affinity rather than SHA1-of-prompt.
17. Paginate `ListAvailableModels` (`nextToken`) and capture `defaultModel`, `status`, `consentStatus`, `refusalFallbackModels`.
18. Ordered `metadata` encoding with `kiro:agent_mode` first.
19. Prompt-cache point emission on stable prefixes for GAR.

---

## Appendix A: operation → wire reference (quick lookup)

```
KRS  POST /v1/responses                 restJson(raw https)  CreateResponse
KRS  POST /generateAssistantResponse    restJson             GenerateAssistantResponse (event-stream resp)
KRS  POST /generatecompletions          restJson             GenerateCompletions
KRS  GET  /featureConfiguration         restJson             GetFeatureConfiguration (q: origin,version; hdr x-amzn-kiro-profile-arn)
KRS  POST /mcp                          restJson             InvokeMCP (profileArn in hdr)
KRS  POST /mcp/stream                   restJson             InvokeMCPStream
CPS  GET  /List-Available-Models        restJson             ListAvailableModels
CPS  GET  /Get-Usage-Limits             restJson             GetUsageLimits
CPS  GET  /Get-User-Preference          restJson             GetUserPreference
CPS  POST /Set-User-Preference          restJson             SetUserPreference
CPS  POST /List-Available-Profiles      restJson             ListAvailableProfiles
CPS  POST /Get-Profile                  restJson             GetProfile (also API-key region probe)
CPS  POST /Create-Subscription-Token    restJson             CreateSubscriptionToken
CPS  POST /List-Available-Subscriptions restJson             ListAvailableSubscriptions
CPS  GET  /List-Notifications           restJson             ListNotifications
CPS  POST /Create-Api-Key /Delete-Api-Key /List-Api-Keys     restJson
OIDC POST /token  /client/register  /device_authorization    SSO OIDC (CreateToken/RegisterClient/StartDeviceAuthorization)
AUTH POST https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken          social refresh
AUTH POST https://prod.us-east-1.auth.desktop.kiro.dev/oauth/{device/authorization,device/poll,token}
TEL  POST https://telemetry.{region}.kiro.dev/v1/{traces,metrics}            OTLP
TEL  POST <endpoint>/agents/activity                                         activity publish (UA: KiroAgent)
Rust POST / (x-amz-target AmazonCodeWhisperer[Streaming]Service.*, KiroControlPlaneBearerService.*)  awsJson1.0 (v1/v2 engine only)
```

## Appendix B: enums worth hard-coding in Kiro-Go

* `ValidationExceptionReason` (KRS): CONTENT_LENGTH_EXCEEDS_THRESHOLD, PROMPT_TOO_LONG, INVALID_MODEL_ID, TOOL_SCHEMA_INVALID, TOOL_USE_RESULT_MISMATCH, THINKING_SIGNATURE_INVALID, REQUEST_BODY_INVALID_JSON, IMAGE_*/DOCUMENT_* (see §4.3).
* `ThrottlingExceptionReason`: USER_REQUEST_RATE_EXCEEDED, SERVICE_REQUEST_RATE_EXCEEDED, DAILY/MONTHLY_REQUEST_COUNT, INSUFFICIENT_MODEL_CAPACITY, CREDIT_CONSUMPTION_RATE_EXCEEDED.
* `ServiceQuotaExceededExceptionReason`: MONTHLY_REQUEST_COUNT, OVERAGE_REQUEST_LIMIT_EXCEEDED, CONVERSATION_LIMIT_EXCEEDED.
* `AccessDeniedExceptionReason`: FEATURE_NOT_SUPPORTED, TEMPORARILY_SUSPENDED, UNAUTHORIZED_{CUSTOMIZATION_RESOURCE,WORKSPACE_CONTEXT_FEATURE}_ACCESS.
* `StopReason`: END_TURN, TOOL_USE, MAX_TOKENS, CONTENT_FILTERED, MODEL_CONTEXT_WINDOW_EXCEEDED, PAUSE_TURN.

(End of report.)
