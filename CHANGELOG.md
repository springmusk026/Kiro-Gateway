# Changelog

## Unreleased

### Alerts
- Account health alerts alongside the budget alerts (console banner, `GET /admin/api/alerts`, webhook, once per
  day and level): token expiring within 24 h without a successful refresh, 3+ failed refreshes in a row,
  banned or suspended account, account cooling down or parked for more than an hour, and no account available.

### Security
- API keys are stored as SHA-256 hashes with a display hint (`sk-ab12…9f3c`) and shown only once, when created
  or regenerated (new *Regenerate key* action). Existing plaintext or encrypted keys are hashed on the first
  start and saved, in `config.json` and PostgreSQL alike; `kiro-gateway export` writes hashes. The admin API
  returns `keyHint` instead of `keyMasked`. See [docs/upgrading.md](docs/upgrading.md#api-keys-are-hashed).

## 2.1.0 — Kiro Gateway

The project is now **Kiro Gateway** ([springmusk026/Kiro-Gateway](https://github.com/springmusk026/Kiro-Gateway)).
Data and settings carry over; see [docs/upgrading.md](docs/upgrading.md) for the renamed image, compose service
and binary.

### Project
- Renamed to Kiro Gateway: image `ghcr.io/springmusk026/kiro-gateway`, compose service and binary
  `kiro-gateway`, Go module `github.com/springmusk026/Kiro-Gateway`, response header
  `x-kiro-gateway-fallback-model`. Environment variables are unchanged.
- New README with screenshots, installation, upgrading and troubleshooting guides, contributing guide,
  security policy, code of conduct and support page; CI runs formatting, vet, tests (with PostgreSQL), race
  tests and the console build on every push.

### Console
- Analytics page: KPIs with change vs the previous period, trends, breakdowns by model, account, key, client,
  endpoint, protocol and status, error classes, activity heatmap and slowest requests.
- Every table and list is paginated; request logs page through the history on the server with a total count.
- Models page with Catalog, Aliases (including fallbacks) and Thinking tabs.
- Migration prompt for moving `config.json` into PostgreSQL.

### Upstream
- Requests aligned with a wire capture of the official Kiro CLI 3.5.0 (API-key accounts use the runtime
  service's own operation and headers; `/v1/responses` headers, metadata and conversation ids match).
- `response.incomplete`, refusals and in-stream error events are handled.

### APIs
- Documents: Anthropic `document` blocks (base64, `text`, `content` and `url` sources, also inside
  `tool_result`), OpenAI Chat `file` parts and Responses `input_file` items are forwarded to Kiro as documents
  (pdf, csv, doc, docx, html, md, txt, xls, xlsx). Unsupported formats, files over 4.5 MB and more than 5
  documents in one message are rejected with a clear 400 instead of being dropped; across a conversation the
  5 newest documents are sent and older ones are replaced by a note.
- Image URLs: `http(s)` images (Anthropic `url` sources, OpenAI `image_url`, Responses `input_image`) are
  downloaded by the gateway with SSRF protection (https only, public addresses only on every redirect, 10 s,
  5 MB, image types checked by header and bytes, outbound proxy honoured). `KIRO_FETCH_IMAGE_URLS=0` turns
  this off; `KIRO_FETCH_ALLOW_HTTP=1` allows plain http.
- Responses: `reasoning` output items with the official `reasoning_summary_*` stream events, in the final
  and stored response, and `usage.output_tokens_details.reasoning_tokens`; replayed `reasoning` input items
  and `include: ["reasoning.encrypted_content"]` are accepted.
- Anthropic: thinking blocks carry a `signature` (`signature_delta` in streams). It is an opaque marker,
  not a verifiable signature.
- OpenAI Chat: `usage.completion_tokens_details.reasoning_tokens` when reasoning is returned.
- Responses echo the model name the client sent (Anthropic, OpenAI and Responses bodies, stream events and
  stored responses); request logs keep the resolved Kiro model id.
- Inline images and documents no longer count against the text payload budget, so a large attachment no
  longer truncates the conversation history.

### Security and operations
- API keys can be restricted to a list of models (exact ids or `prefix-*`, matched after alias resolution),
  a requests-per-minute limit and an expiry date. Expired keys get 401, other models 403
  (`permission_error` / `model_not_allowed`), RPM overruns 429 with `retry-after`; `/v1/models` lists only
  the models a presented key may use. Set in the key dialog; badges in the keys list.
- `KIRO_ENCRYPTION_KEY` encrypts account tokens, client secrets, Kiro API keys, proxy URLs, API key values
  and the admin password at rest (AES-256-GCM) in `config.json` and PostgreSQL. Plaintext data is encrypted on
  the first start with a key; a missing or wrong key stops the start-up instead of losing accounts.
  `kiro-gateway export` keeps encrypted values unless `--decrypt` is given.
- `config.json` is always written `0600` and a symlinked config file is refused.
- The console signs in with HttpOnly, SameSite=Strict session cookies (`/admin/api/session`) instead of
  sending the admin password with every call and keeping it in browser storage. Sessions expire after 12 h
  idle / 7 days (30 days with *keep me signed in*), survive restarts with PostgreSQL and end when the admin
  password changes. The `X-Admin-Password` header still works for scripts.
- Prometheus metrics at `/metrics` (`KIRO_METRICS=1`, optional `KIRO_METRICS_TOKEN`).
- Optional nightly `pg_dump` backups: `docker compose --profile backup up -d` (`BACKUP_KEEP`, default 14).

### Cost and reliability
- **Usage-aware routing**: accounts close to their usage limit (default 97 %, `usageRoutingThreshold` /
  `KIRO_USAGE_THRESHOLD`) are used only when nothing else is available, lower usage is preferred, and a
  conversation moves off its account once it crosses the threshold. Usage is refreshed per account every
  10–15 minutes in the background and after quota errors. The Accounts page shows credits left per account
  and for the pool.
- **Model fallback**: per-model fallback lists (Models → Aliases). A request whose model is unavailable or
  overloaded is retried once on the first fallback before anything is streamed, within the attempt budget;
  reported in `x-kiro-gateway-fallback-model` and the request log (schema migration 3 adds
  `kiro_request_logs.fallback_model`).
- **Budgets**: a global and a per-key daily credit cap, reset at midnight in a configurable time zone;
  requests over a cap get `429` (`rate_limit_error` / `insufficient_quota`) without reaching Kiro.
- **Alerts** at 80 % / 100 % of daily caps and key limits and at 90 % of the pool's account limits, shown in
  the console and posted once per day to an optional webhook (Slack/Discord-compatible `text`).
- **Prompt-cache points** (experimental, off by default, `KIRO_CACHE_POINTS=1`): cache points on
  `generateAssistantResponse` requests for models that support prompt caching. Unverified against Kiro.

## 2.0.0 — independent project

Kiro Gateway began as a fork of [Quorinex/Kiro-Go](https://github.com/Quorinex/Kiro-Go) at version 1.1.5. Since
then the gateway, its storage and its console have been rebuilt, so it is now maintained as its own project.
Configuration from the original (`data/config.json`) is still read and can be migrated into PostgreSQL from
the console.

### APIs
- Upstream errors mapped to the official status codes and types: "prompt is too long" (Claude Code
  auto-compaction), `context_length_exceeded` (Codex), `model_not_found`, `rate_limit_exceeded`,
  `insufficient_quota`, `overloaded_error` with `retry-after`; client errors are no longer retried on other
  accounts.
- SSE heartbeats (`ping` / `: keep-alive`) so proxies don't cut long requests.
- OpenAI Chat: `role` in the first chunk, `tool_choice`, `parallel_tool_calls`, `stop`,
  `max_completion_tokens`, `stream_options.include_usage`, `n > 1` rejected, text kept next to tool calls.
- Anthropic: `stop_sequences`, `tool_result.is_error`.
- Responses: official event sequence with `sequence_number` and `.done` events, full response objects
  (including on `response.failed` with `error.code`), `GET` / `DELETE /v1/responses/{id}`, Codex multi-agent
  tools, `additional_tools` and namespaces.
- `/v1/models` with `created`, Anthropic fields and `GET /v1/models/{id}`; request ids on every response;
  CORS for browser SDKs; JSON 404/405 and 413 errors.
- Long tool names shortened for Kiro and restored in responses on every API.

### Thinking
- Thinking follows the request (Anthropic `thinking` / `output_config.effort`, OpenAI `reasoning_effort`,
  Responses `reasoning`) instead of requiring a `-thinking` model; the suffix still works.
- Effort forwarded natively to models that declare it, clamped to their supported levels.
- *When to think* policy (follow request / always / never) and a configurable default thinking length.

### Upstream and accounts
- `/v1/responses` transport alongside `generateAssistantResponse`, chosen per account with fallback.
- Request shapes, headers and conversation ids aligned with the official Kiro CLI.
- Per-request upstream attempt budget, Kiro `retry-after` hints honoured, exponential back-off, throttle
  cooldowns, quota parking, forced token refresh before marking an account unhealthy, optional per-account
  concurrency limit.
- Conversation affinity for prompt-cache reuse, real cache-token accounting, Claude Code start-up probe
  answered locally.
- Live model catalogue from Kiro with enable/disable per model and configurable aliases.

### Storage and logging
- PostgreSQL backend for settings, accounts, keys and stored responses, with automatic schema migrations.
- Automatic first-start import of `config.json` and a console prompt to verify the transfer and delete the
  file; `kiro-gateway migrate` / `kiro-gateway export` commands.
- Full request history (timing, routing, tokens, cache, credits, attempts, optional bodies) and an admin
  audit log, written asynchronously so inference is never slowed down.

### Console
- New admin console (TanStack Start + shadcn/ui): Overview with traffic and usage breakdowns, Accounts,
  Models, Playground, API & Keys with client setup snippets, Request logs, Settings; English, 中文, Tiếng
  Việt; light/dark; mobile layouts.

### Security
- Generated admin password instead of a default, login rate limiting, constant-time comparisons, security
  headers, request size limits, validated upstream URLs, atomic config writes.

## 1.1.5 and earlier

See the history of [Quorinex/Kiro-Go](https://github.com/Quorinex/Kiro-Go).
