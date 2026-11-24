# Kiro-Go

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat&logo=docker)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

Convert Kiro accounts to OpenAI / Anthropic compatible API service.

[English](README.md) | [中文](README_CN.md) | [Tiếng Việt](README_VI.md)

If this project helps you, a Star would mean a lot.

## Features

- Anthropic `/v1/messages`, OpenAI `/v1/chat/completions` & OpenAI `/v1/responses`
- Multi-account pool with round-robin load balancing
- Auto token refresh, SSE streaming, Web admin panel
- Multiple auth: AWS Builder ID, IAM Identity Center (Enterprise SSO), Microsoft Enterprise SSO, SSO Token, local cache, credentials JSON, Kiro API Key
- Usage tracking, account import/export, i18n (CN / EN / VI)
- Configurable client model mappings, advertised through `/v1/models`
- Support configuring outbound proxy (SOCKS5 / HTTP)
- Live model catalog fetched from Kiro (context window, max output, vision, credit rate), refreshed automatically
- Dashboard with overview, traffic chart, model catalog and a built-in playground (streaming, reasoning, copy as cURL)
- Automatic upstream protocol selection (`/v1/responses` or `generateAssistantResponse`) per account
- Hardened admin access: generated password, login throttling, security headers

## Quick Start

### Docker Compose (Recommended)

```bash
git clone https://github.com/Quorinex/Kiro-Go.git
cd Kiro-Go
mkdir -p data
docker-compose up -d
```

### Docker Run

```bash
docker run -d \
  --name kiro-go \
  -p 8080:8080 \
  -e ADMIN_PASSWORD=your_secure_password \
  -v /path/to/data:/app/data \
  --restart unless-stopped \
  ghcr.io/quorinex/kiro-go:latest
```

### Build from Source

```bash
git clone https://github.com/Quorinex/Kiro-Go.git
cd Kiro-Go
go build -o kiro-go .
./kiro-go
```

### Deploy on Zeabur

The repo already includes a `Dockerfile`, so it builds and runs on Zeabur out of the box.

**Option 1: Dashboard (one-click)**

1. Fork this repo to your GitHub account.
2. In Zeabur, create a new service and choose **Deploy from GitHub**, then select your fork.
3. Zeabur auto-detects the `Dockerfile` and builds the image.
4. In the **Networking** tab, expose port `8080` and bind a domain.
5. In the **Variables** tab, set at least `ADMIN_PASSWORD` (admin panel password).
6. Mount a Volume at `/app/data` if you want accounts / config to survive redeploys.

**Option 2: CLI**

```bash
npm i -g zeabur
zeabur auth login
zeabur deploy
```

> Run the commands from the project root. The CLI writes `.zeabur/context.json` to remember the target project / service — it contains personal IDs, so don't commit it.

Once the service is up, open `https://<your-domain>/admin` to log in.

Config is auto-created at `data/config.json`. Mount `/app/data` for persistence. On first start (or if the password is still the old `changeme` default) a random admin password is generated, saved to `data/config.json` and printed once in the log. Set your own with the `ADMIN_PASSWORD` env var or in the admin panel.

## Usage

Open `http://localhost:8080/admin`, log in, add accounts, then call the API:

```bash
# Claude
curl http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"claude-sonnet-4.5","max_tokens":1024,"messages":[{"role":"user","content":"Hello!"}]}'

# OpenAI
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer any" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello!"}]}'
```

### Add a Kiro API Key account

In the admin panel, choose **API Key** when adding an account and paste `ksk_...` (or `ksk_...|region`).

You can also import via the credentials API:

```bash
curl -X POST http://localhost:8080/admin/api/auth/credentials \
  -H "Content-Type: application/json" \
  -H "Cookie: <admin-session>" \
  -d '{"kiroApiKey":"ksk_your_key|us-east-1","authMethod":"api_key","nickname":"cli-key"}'
```

API Key accounts call the Kiro CLI runtime (`https://runtime.{region}.kiro.dev/`) with `tokentype: API_KEY`. They skip OAuth refresh and do not use `profileArn`.

### Model mappings

Configure client-facing aliases under **Settings - Model Mappings**, for example `kiro-opus-5` → `claude-opus-5`. Mappings apply to the Anthropic, OpenAI Chat Completions, and OpenAI Responses APIs. Each alias and its configured thinking-suffix variant are included in `/v1/models`. Source IDs match case-insensitively and resolve directly to one upstream model ID; mapping chains are rejected.

## Thinking Mode

Append a suffix (default `-thinking`) to the model name, e.g. `claude-sonnet-4.5-thinking`. Claude-compatible requests that include a top-level `thinking` config such as `{"type":"enabled","budget_tokens":2048}` or `{"type":"adaptive"}` also enable thinking mode automatically. Configure output format in the admin panel under Settings - Thinking Mode.

## Outbound Proxy

For users in restricted network regions, configure an outbound proxy in the admin panel under **Settings - Outbound Proxy Settings**. Supports SOCKS5 and HTTP proxies.

The setting takes effect immediately without restarting.

## Prompt caching and cost

What Kiro does natively (from the official Kiro CLI 3.5.0 and its bundled agent core):

- **Caching is server-side and automatic.** The runtime schema has `cachePoint` markers (on user/assistant
  messages and tools) and a `clientCacheConfig`, but the official client never sets them; Kiro caches prompt
  prefixes on its own, **per account**, and reports `cacheReadInputTokens` / `cacheWriteInputTokens` in
  `metadataEvent.tokenUsage`. Per-model support (`supportsPromptCaching`, minimum tokens per checkpoint) is
  published by the control plane. The legacy streaming schema has no cache fields, so markers are not sent.
- Billing is per request in credits, scaled by each model's credit multiplier (shown on the Models page).

What the gateway does to keep costs down:

- **Conversation affinity (on by default).** Every turn of a conversation goes to the account that served the
  previous turn, so Kiro's per-account prompt cache stays warm; different conversations still spread across
  the pool. Disable with `KIRO_CONVERSATION_AFFINITY=0`. (`KIRO_SESSION_AFFINITY=1` additionally pins each API
  key to one account.)
- **Stable prompt prefixes.** Conversation IDs are deterministic and nothing random or time-based is injected
  into the prompt, so repeated turns share a byte-identical prefix.
- **Real cache accounting.** Upstream cache reads/writes are recorded per request (database backend) next to
  tokens and credits, so hit rates can be measured instead of guessed.
- **No wasted requests.** Claude Code's start-up quota probe is answered locally (`KIRO_LOCAL_PROBES=0` to
  disable); `count_tokens` is computed locally; per-request retry budget, no 4xx replays, exponential backoff
  and account cooldowns avoid paying for doomed retries; large tool results are compressed (`KIRO_GO_RTK`).
- Disable models you don't want clients to use (Models page) — e.g. keep expensive models off for a team key.

## Storage

Kiro-Go stores its data in one of two backends:

| Backend | When | What is stored |
|---|---|---|
| **PostgreSQL** (recommended) | `DATABASE_URL` is set — the default `docker-compose.yml` runs a `postgres:16` service for you | settings, accounts, API keys, stored `/v1/responses` conversations, **full request history** and an **audit log** of admin actions |
| JSON file | no `DATABASE_URL` | `data/config.json` and `data/responses/*.json`; request logs are kept in memory only |

**Migrating from `config.json`.** Point Kiro-Go at an empty database and start it: the existing
`data/config.json` and stored responses are imported automatically on the first start
(`KIRO_AUTO_IMPORT=false` turns that off). While a `config.json` is still on disk, the admin console shows a
migration prompt (also under **Settings → Logging & storage**) that compares the file with the database,
imports anything missing (merging accounts and API keys without overwriting the database, or replacing it
wholesale if you choose), verifies every account, key and stored response, and then deletes the JSON files.
Nothing else in the data folder, such as the PostgreSQL volume, is touched. You can also migrate by hand,
and export back to JSON at any time:

```bash
kiro-go migrate --from data/config.json --to "postgres://kiro:kiro@localhost:5432/kiro?sslmode=disable"
kiro-go export --from "$DATABASE_URL" --out config.backup.json
```

In Docker: `docker compose run --rm kiro-go ./kiro-go export --out /app/data/config.backup.json`.

**Request history without slowing inference.** Each request is summarised (endpoint, model, account, API key,
client, protocol, tokens, credits, latency, time to first byte, upstream attempts, errors) after the response is
sent and handed to a background writer that batch-inserts into PostgreSQL. If the database is slow or
unavailable, records are dropped and counted instead of delaying requests; usage counters are flushed in the
background at most once per second. Recording request and response **bodies** is off by default (they can
contain sensitive prompts) — enable it with a size cap under Settings → Logging. Logs are kept for 30 days by
default (configurable).

Building from source requires Go 1.25+.

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CONFIG_PATH` | Config file path (JSON backend, and the source for the first-start import) | `data/config.json` |
| `DATABASE_URL` | PostgreSQL connection URL; enables the database backend | – |
| `KIRO_LOG_BODIES_KB` | Record up to this many KB of each request/response body (overrides the setting) | `0` (off) |
| `ADMIN_PASSWORD` | Admin panel password (overrides config) | generated on first start |
| `LOG_LEVEL` | `debug`, `info`, `warn`, `error` | `info` |
| `KIRO_UPSTREAM_PROTOCOL` | `auto`, `responses` or `legacy` (Settings → Endpoint overrides it) | `auto` |
| `KIRO_CONVERSATION_AFFINITY` | `0` disables keeping each conversation on one account (prompt-cache reuse) | on |
| `KIRO_LOCAL_PROBES` | `0` sends Claude Code's start-up quota probe upstream instead of answering locally | on |
| `KIRO_SESSION_AFFINITY` | `1` keeps each API key on the account that last served it (better prompt-cache reuse) | off |
| `KIRO_MAX_UPSTREAM_ATTEMPTS` | Maximum upstream chat calls per client request, across all retries and account switches | `4` |
| `KIRO_MAX_CONCURRENT_PER_ACCOUNT` | Queue requests beyond this many in flight per account (`0` = unlimited) | `0` |
| `KIRO_STREAM_IDLE_TIMEOUT` | Seconds without upstream data before a stream is abandoned | `180` |
| `KIRO_MAX_BODY_MB` | Maximum client request body size | `64` |
| `KIRO_MAX_STORED_RESPONSES` / `KIRO_MAX_STORED_RESPONSES_MB` | Limits for stored `/v1/responses` objects | `20000` / `2048` |
| `KIRO_KEEP_STRUCTURED_TOOL_HISTORY` | `1` keeps tool calls in history as structured data instead of text | off |
| `KIRO_GO_RTK` | `false` disables compression of large tool results | on |

> **Security:** the server listens on `0.0.0.0` by default. Turn on **API key verification** (Settings → API Settings) before exposing it; otherwise anyone who can reach the port can use your accounts. The dashboard shows a warning while it is off.

## Contributing

Friendly discussion is welcome. If you run into issues, try asking Claude Code, Codex, or similar tools for help first — most problems can be solved that way. PRs are even better.

## Friend Links

- [LINUX DO](https://linux.do)

## Disclaimer

For educational and research purposes only. Not affiliated with Amazon, AWS, or Kiro. Users are responsible for complying with applicable terms of service and laws. Use at your own risk.

## License

[MIT](LICENSE)
