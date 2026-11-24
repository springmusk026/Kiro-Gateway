# Kiro-Go

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?style=flat&logo=docker)](docker-compose.yml)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?style=flat&logo=postgresql&logoColor=white)](docs/configuration.md#storage)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

**A self-hosted gateway that turns a pool of Kiro accounts into Anthropic- and OpenAI-compatible APIs** —
built to run Claude Code, Codex, IDE agents and SDKs on Kiro, with an admin console, request history and
careful upstream behaviour.

[English](README.md) | [中文](README_CN.md) | [Tiếng Việt](README_VI.md)

> Kiro-Go started from [Quorinex/Kiro-Go](https://github.com/Quorinex/Kiro-Go) (MIT) and has since been
> rebuilt into an independent project: new console, PostgreSQL storage, request-driven thinking, API
> conformance work and Kiro-client-aligned upstream behaviour. See [CHANGELOG.md](CHANGELOG.md).

## Highlights

**APIs your tools already speak**
- Anthropic Messages (`/v1/messages`, `count_tokens`), OpenAI Chat Completions and OpenAI Responses
  (`/v1/responses`, including stored responses and `previous_response_id`), `/v1/models`.
- Official error codes (Claude Code auto-compacts on "prompt is too long", Codex sees `context_length_exceeded`),
  SSE heartbeats, `tool_choice`, stop sequences, request ids, CORS for browser SDKs.
- **Thinking follows the request** — Anthropic `thinking` / `output_config.effort`, OpenAI
  `reasoning_effort`, Responses `reasoning` — and the effort level is forwarded natively to models that
  support it. The legacy `-thinking` model suffix still works.
- Web search (`web_search` tool) relayed through Kiro's MCP endpoint.

**An account pool that behaves**
- Builder ID, IAM Identity Center / enterprise SSO, Microsoft SSO, SSO tokens, credential JSON and Kiro API
  keys; automatic token refresh.
- Conversation affinity keeps each conversation on one account so Kiro's prompt cache stays warm.
- Per-request retry budget, honours Kiro's `retry-after` hints, no replays of client errors, cooldowns
  for throttled or exhausted accounts, optional per-account concurrency limit.
- Upstream protocol chosen per account (`/v1/responses` or `generateAssistantResponse`), request shapes
  and headers aligned with the official Kiro CLI.

**A console you'd actually use** (`/admin`)
- Overview, and an **Analytics** page: requests, success rate, credits, tokens, cache share, latency and
  time-to-first-token percentiles with change vs the previous period; trends; breakdowns by model, account,
  key, client, endpoint, protocol and status; error classes; an activity heatmap; the slowest requests.
- Accounts with quota and health; Models with the live catalog (enable/disable), aliases and thinking
  policy; playground; API keys with per-key quotas and copy-paste client setup.
- Full request history and admin audit log in PostgreSQL, with optional body capture — recorded off the
  request path so it never slows inference.
- English, 中文, Tiếng Việt; light and dark themes; works on a phone.

## Quick start

### Docker Compose (recommended)

```bash
git clone https://github.com/springmusk026/Kiro-Go.git
cd Kiro-Go
docker compose up -d
docker compose logs kiro-go | grep -i password   # first start prints a generated admin password
```

This starts Kiro-Go on port `8080` with a PostgreSQL 16 container. Open `http://localhost:8080/admin`, log
in, add an account, and create an API key. Set `ADMIN_PASSWORD` (and `POSTGRES_PASSWORD`) in a `.env` file
to choose your own.

Update later with:

```bash
git pull && docker compose up -d --build
```

Coming from a `config.json` setup? It is imported on first start and the console walks you through
verifying and removing the file — see [migration](docs/configuration.md#migrating-from-configjson).

### Docker image

Multi-arch images (amd64, arm64) are published to `ghcr.io/springmusk026/kiro-go` — `latest` from `main`,
plus a tag per version:

```bash
docker run -d --name kiro-go -p 8080:8080 \
  -e ADMIN_PASSWORD=change-me \
  -e DATABASE_URL="postgres://kiro:kiro@db-host:5432/kiro?sslmode=disable" \
  -v "$PWD/data:/app/data" \
  --restart unless-stopped ghcr.io/springmusk026/kiro-go:latest
```

Without `DATABASE_URL` everything is kept in `data/config.json` (no request history).

### From source

Requires Go 1.25+ and Node 22+ (for the console).

```bash
git clone https://github.com/springmusk026/Kiro-Go.git && cd Kiro-Go
(cd frontend && npm ci && npm run build)   # builds the console into ./web
go build -o kiro-go .
./kiro-go
```

## Connect a client

The **API & Keys** page generates these snippets for your server, key and models.

**Claude Code**

```bash
export ANTHROPIC_BASE_URL=http://localhost:8080
export ANTHROPIC_AUTH_TOKEN=<your-api-key>
export ANTHROPIC_MODEL=claude-sonnet-4.5
export ANTHROPIC_SMALL_FAST_MODEL=claude-haiku-4.5
claude
```

**Codex CLI** (`~/.codex/config.toml`)

```toml
model = "claude-sonnet-4.5"
model_provider = "kiro"
model_reasoning_effort = "medium"

[model_providers.kiro]
name = "Kiro-Go"
base_url = "http://localhost:8080/v1"
env_key = "KIRO_API_KEY"
wire_api = "responses"
```

**OpenAI / Anthropic SDKs** — point `base_url` at `http://localhost:8080/v1` (OpenAI) or
`http://localhost:8080` (Anthropic) and use a Kiro-Go API key.

More clients (Cline, Continue, LiteLLM, cURL) and thinking options: [docs/clients.md](docs/clients.md).

## Documentation

| | |
|---|---|
| [docs/clients.md](docs/clients.md) | Setting up Claude Code, Codex, SDKs and other tools |
| [docs/api.md](docs/api.md) | Endpoints, compatibility notes, thinking, models, errors |
| [docs/configuration.md](docs/configuration.md) | Environment variables, settings, storage, migration from JSON, logging |
| [docs/architecture.md](docs/architecture.md) | How a request flows, upstream protocols, account pool, caching and cost |
| [docs/development.md](docs/development.md) | Building, testing, the console, releasing |
| [CHANGELOG.md](CHANGELOG.md) | What changed, including the divergence from the original project |

## Security

- The server listens on `0.0.0.0`. **Create an API key** (or enable API key verification in Settings) before
  exposing it — otherwise anyone who can reach the port can use your accounts. The console warns while it is
  open.
- The admin password is generated on first start if unset; admin logins are rate-limited and compared in
  constant time; the console uses HttpOnly, SameSite=Strict session cookies (the password is never stored in
  the browser) and sends strict security headers.
- Set `KIRO_ENCRYPTION_KEY` (`openssl rand -base64 32`) to encrypt account tokens, API keys and passwords at
  rest; keep the key safe, the data cannot be read without it.
- API keys can be limited to certain models, a number of requests per minute and an expiry date.
- Optional: Prometheus metrics (`KIRO_METRICS=1`) and nightly database backups
  (`docker compose --profile backup up -d`). See [docs/configuration.md](docs/configuration.md#security).
- Request/response body capture is off by default because prompts can be sensitive.
- Put TLS in front (Caddy, nginx, Cloudflare) when exposing it beyond localhost.

## Disclaimer

For educational and research purposes. Not affiliated with Amazon, AWS or Kiro. You are responsible for
complying with the terms of service of the accounts you use and with applicable law.

## License

[MIT](LICENSE). Portions derived from [Quorinex/Kiro-Go](https://github.com/Quorinex/Kiro-Go), also MIT.
