# Architecture

Kiro-Go is a single Go binary (plus an optional PostgreSQL database) that serves the public APIs, the admin
console and its API.

```
client ──► auth & limits ──► translate (Anthropic / OpenAI / Responses ─► Kiro payload)
                                   │
                                   ▼
                     pick account (affinity, health, concurrency)
                                   │
                                   ▼
             upstream call: /v1/responses (SSE) or generateAssistantResponse (event stream)
                                   │      retries within the request budget, failover
                                   ▼
          translate stream back ─► client          request summary ─► async recorder ─► PostgreSQL
```

## Layout

| Path | Contents |
|---|---|
| `main.go`, `cli.go` | start-up, database connection and first-start import, `migrate` / `export` commands, graceful shutdown |
| `config/` | configuration model, the `Store` interface (JSON file or PostgreSQL), debounced counter writes |
| `auth/` | Builder ID / IAM Identity Center (OIDC), Microsoft SSO, SSO-token import, token refresh |
| `pool/` | account pool: selection, cooldowns, throttle back-off, earliest-cooldown for `retry-after` |
| `proxy/` | HTTP handlers, API translators, upstream clients, admin API |
| `storage/` | PostgreSQL backend: migrations, config store, stored responses, request recorder and reader, JSON migrator |
| `telemetry/` | interfaces and record types shared by the proxy and the storage backend |
| `rtk/` | compression of large tool results |
| `frontend/` | the admin console (TanStack Start SPA + shadcn/ui), built into `web/` |

Key files in `proxy/`: `handler.go` (routing and the three APIs), `translator.go` (request/response
translation), `kiro.go` (legacy transport and endpoints), `kiro_responses.go` (`/v1/responses` transport),
`upstream_protocol.go` and `upstream_budget.go` (protocol selection, retries), `account_failover.go`
(error classification and account health), `session_affinity.go`, `thinking_request.go`, `api_errors.go`,
`sse_heartbeat.go`, `stop_sequences.go`, `models_dynamic.go`, `request_trace.go`.

## Upstream transports

Kiro serves chat through two transports:

- **`/v1/responses`** on `runtime.<region>.kiro.dev` — Server-Sent Events, the transport current Kiro
  clients use. It carries native reasoning controls and reports context usage and credits.
- **`generateAssistantResponse`** — the older AWS event-stream API on the Kiro IDE / CodeWhisperer / Amazon Q
  endpoints. It reports cache read/write tokens in its metadata events.

In `auto` mode Kiro-Go tries the preferred transport for each account and remembers what works, falling
back when the upstream rejects the protocol (for example "profileArn is required"). Kiro API key accounts
use the Kiro CLI runtime directly.

See [kiro-protocol.md](kiro-protocol.md) for the full map of Kiro's APIs and how Kiro-Go compares.

Requests are shaped like the official clients': on `/v1/responses` the exact raw header set (origin, profile, `TokenType`,
the IDE's own user agent — or the CLI's for API-key accounts — attempt and fallback counters, metadata in
the official key order), conversation ids that stay stable across turns, `agentTaskType: "vibe"`, tool names shortened
with a hash when they exceed Kiro's limit (and restored in responses), and no client-side invented ids.

## Account selection

1. Accounts that are disabled, cooling down, out of quota or banned are skipped.
2. **Conversation affinity** (default on): a conversation is identified from its first messages and sent to
   the account that served it before, so Kiro's per-account prompt cache stays warm. Bindings expire after an
   hour of inactivity. Optional API-key affinity pins each key to one account.
3. Otherwise accounts are used in rotation, preferring **usage headroom**: accounts at or above the usage
   threshold (default 97 % of their limit) are a last resort, and lower usage bands win over higher ones
   (`pool/usage_routing.go`). Usage comes from Kiro's usage API, refreshed per account every 10–15 minutes
   in the background and after quota errors (`proxy/usage_refresh.go`), plus a local estimate of the
   credits spent since. A bound conversation moves once its account crosses the threshold. An optional
   per-account concurrency limit queues extra requests.

## Retries and account health

Every client request gets a budget of upstream attempts (`KIRO_MAX_UPSTREAM_ATTEMPTS`, default 4) shared by
retries and account switches, so one request can never fan out into a burst of upstream calls.

| Upstream result | What happens |
|---|---|
| Client error (bad request, prompt too long, invalid model) | returned immediately with the matching error; no retry, no penalty for the account |
| Throttling (429) | the account backs off (honouring Kiro's `retry-after` / `retryAfterMilliseconds`), the request moves to another account |
| Quota or monthly limit | the account is parked until its limit resets (unless overage is allowed) |
| Expired or rejected token | a forced refresh is tried before the account is marked unhealthy |
| Transient (5xx, network) | exponential back-off with jitter (250 ms·2ⁿ, throttling 1 s·2ⁿ, capped at 20 s) |

Retries only happen before any content has reached the client; after that, errors are reported in-stream.

**Model fallback** (`proxy/model_fallback.go`): when the model itself is unavailable (Kiro's
temporarily-unavailable / overloaded errors, a 503/529-class result after the account loop, or an invalid
model id) and a fallback is configured, the request is retried once on the first fallback model, inside the
same attempt budget and only before any content was streamed. Explicit model-unavailable errors stop the
account loop early so the budget is not spent on accounts that would fail the same way.

## Budgets and alerts

Daily credit caps (global and per API key) are checked in memory right after authentication, before any
upstream call; exceeding one returns `429` in the API's own shape. Daily counters are updated from the
credits each request reports and persisted with the debounced config writer. A background loop evaluates
alerts every 30 s and posts new ones to the optional webhook (`proxy/cost_controls.go`).

## Streaming

Streams are translated event by event. A heartbeat (`ping` for Anthropic, `: keep-alive` comments for
OpenAI) is written after 15 s of silence so proxies such as Cloudflare or nginx don't cut long-thinking
requests. Streams are abandoned after `KIRO_STREAM_IDLE_TIMEOUT` seconds without upstream data. Stop sequences
are matched across chunk boundaries and cancel the upstream call when hit.

## Caching and cost

- Kiro caches prompt prefixes **server-side, per account**; the official clients do not send cache markers.
  `KIRO_CACHE_POINTS=1` adds experimental (unverified) cache points to `generateAssistantResponse` requests
  (`proxy/cache_points.go`).
  Conversation affinity and byte-stable prompts (deterministic ids, nothing time-based injected) are what
  make those caches hit. Cache read/write tokens are recorded per request when Kiro reports them.
- Billing is in credits per request, scaled by the model's multiplier (shown on the Models page).
- Local answers cost nothing upstream: `count_tokens`, Claude Code's start-up probe, model lists.
- Large tool results are compressed before being sent (`KIRO_GO_RTK`).

## Request recording

A lightweight trace wraps each API request (status, time to first byte, bytes, account, tokens, cache
tokens, attempts). After the response is finished the summary is put on a bounded channel; a background
writer batch-inserts with `COPY`. A full queue drops records and counts them instead of blocking. Admin
changes are recorded as audit events the same way. Measured overhead is a few microseconds per request.

## Console

The console is a static single-page app served from `web/` under `/admin` (hashed assets are cached
immutably, the shell is never cached). It signs in through `POST /admin/api/session` and then talks to
`/admin/api/*` with an HttpOnly session cookie (scripts can still send the admin password header); login
attempts are rate-limited per client. See [configuration.md](configuration.md#security).
