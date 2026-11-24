# Configuration

Most settings live in the admin console (**Settings**) and are stored with the rest of the configuration.
Environment variables cover deployment concerns and a few operational switches; where both exist the
environment variable wins.

- [Environment variables](#environment-variables)
- [Console settings](#console-settings)
- [Storage](#storage)
- [Migrating from config.json](#migrating-from-configjson)
- [Request history and logging](#request-history-and-logging)
- [Command-line tools](#command-line-tools)

## Environment variables

### Deployment

| Variable | Description | Default |
|---|---|---|
| `ADMIN_PASSWORD` | Admin console password. Overrides the stored one on every start. | generated on first start and printed once |
| `DATABASE_URL` (or `KIRO_DATABASE_URL`) | PostgreSQL URL. Enables the database backend, request history and audit log. | – (JSON file) |
| `CONFIG_PATH` | `config.json` location: the JSON backend, and the source for the first-start import. | `data/config.json` |
| `KIRO_AUTO_IMPORT` | `false` stops the automatic import of `config.json` into an empty database (the console still offers it). | on |
| `LOG_LEVEL` | `debug`, `info`, `warn`, `error` | `info` |
| `HTTPS_PROXY` / `NO_PROXY` | Standard proxy variables for outbound traffic (the console's outbound proxy setting takes precedence). | – |

The listen host and port are console settings (`host`, `port`; default `0.0.0.0:8080`).

### Upstream behaviour

| Variable | Description | Default |
|---|---|---|
| `KIRO_UPSTREAM_PROTOCOL` | `auto`, `responses` (`/v1/responses`) or `legacy` (`generateAssistantResponse`). The console setting overrides it. | `auto` |
| `KIRO_MAX_UPSTREAM_ATTEMPTS` | Maximum upstream chat calls per client request, across retries and account switches. | `4` |
| `KIRO_MAX_CONCURRENT_PER_ACCOUNT` | Requests beyond this many in flight on one account wait for a slot (`0` = unlimited). | `0` |
| `KIRO_STREAM_IDLE_TIMEOUT` | Seconds without upstream data before a stream is abandoned. | `180` |
| `KIRO_CONVERSATION_AFFINITY` | `0` disables keeping each conversation on the account that served it (prompt-cache reuse). | on |
| `KIRO_SESSION_AFFINITY` | `1` additionally pins each API key to the account that last served it. | off |
| `KIRO_LOCAL_PROBES` | `0` forwards Claude Code's start-up quota probe upstream instead of answering it locally. | on |

### Requests and payloads

| Variable | Description | Default |
|---|---|---|
| `KIRO_MAX_BODY_MB` | Largest accepted client request body; larger requests get `413`. | `64` |
| `KIRO_MAX_STORED_RESPONSES` / `KIRO_MAX_STORED_RESPONSES_MB` | Limits for stored `/v1/responses` objects (also expire after 30 days). | `20000` / `2048` |
| `KIRO_KEEP_STRUCTURED_TOOL_HISTORY` | `1` sends earlier tool calls upstream as structured data instead of narrated text. | off |
| `KIRO_GO_RTK` | `false` disables compression of large tool results before they are sent upstream. | on |
| `KIRO_GO_RTK_MIN_BYTES` / `KIRO_GO_RTK_MAX_BYTES` | Size window in which tool results are compressed. | `500` / `10485760` |
| `KIRO_TOOL_LEAK_FIX` | `off` disables filtering of tool-call markup that leaks into text output. | on |
| `KIRO_TOOL_LEAK_DEBUG` | `1` logs what the tool-leak filter removes. | off |

### Logging

| Variable | Description | Default |
|---|---|---|
| `KIRO_LOG_BODIES_KB` | Record up to this many KB of each request and response body (overrides the console setting). | `0` (off) |

### Testing only

`KIRO_TEST_DATABASE_URL` (storage integration tests), `KIRO_LIVE=1` / `KIRO_LIVE_CONFIG` (live upstream test),
`KIRO_GO_RTK_ARTIFACT_DIR` (tool-compression test artifacts). See [development.md](development.md).

## Console settings

| Section | What it controls |
|---|---|
| Security | Admin password, API key verification for the public endpoints. |
| Routing & upstream | Preferred endpoint (Kiro IDE / CodeWhisperer / Amazon Q), endpoint fallback, upstream protocol, whether accounts stay in rotation after reaching their quota (overage may be billed). |
| Prompt filter | Removing Claude Code boilerplate, environment noise and custom patterns from system prompts before they are sent. |
| Network | Outbound HTTP/SOCKS5 proxy (applies immediately). |
| Logging & storage | Storage backend status, recorder health, body capture size, history retention, the `config.json` migration. |
| Maintenance | Resetting counters, version information and the server-side configuration view. |

The **Models** page holds everything model-related, in three tabs:

- **Catalog** — the live model list from Kiro; enable or disable models (disabled models disappear from
  `/v1/models` and requests for them are rejected with a clear error), see context window, output limit,
  credit multiplier, effort levels and prompt-cache support.
- **Aliases** — client-facing model names (below).
- **Thinking** — when to think (follow the request / always / never), the default thinking length, the
  legacy model suffix, and how reasoning is returned to OpenAI and Anthropic clients.

### Model aliases

Aliases (Models → Aliases) expose client-friendly names that resolve to one upstream model, for
example `kiro-opus` → `claude-opus-4.5`. They apply to all three APIs, appear in `/v1/models`, match
case-insensitively and cannot chain.

## Storage

| Backend | Enabled by | Stores |
|---|---|---|
| **PostgreSQL** (recommended) | `DATABASE_URL` — the default `docker-compose.yml` runs `postgres:16-alpine` | settings, accounts, API keys, stored `/v1/responses` objects, request history, request bodies (if enabled), admin audit log |
| JSON file | no `DATABASE_URL` | `data/config.json` and `data/responses/*.json`; request logs are kept in memory only |

Schema migrations run automatically at start-up under an advisory lock, so several replicas can start at
once. Configuration writes only touch rows that changed. High-frequency counters (request and token
totals) are flushed at most once per second, outside the request path.

## Migrating from config.json

1. Set `DATABASE_URL` (the bundled compose file already does) and start Kiro-Go against an empty database.
   `config.json` and stored responses are imported automatically (`KIRO_AUTO_IMPORT=false` turns this off).
   The file is left on disk.
2. Open the console. While a `config.json` is still on disk it shows a **migration prompt** (also under
   Settings → Logging & storage) comparing the file with the database — accounts, API keys and stored
   responses, with what is missing:
   - **Not migrated** (database has no accounts or keys): *Migrate now* imports everything.
   - **Needs import** (the file has items the database lacks): *Import N missing* adds only those; nothing
     in the database is overwritten. *Replace database with config.json* exists for the rare case you want
     the file to win, behind a confirmation.
   - **Verified**: everything is in the database; *Delete JSON files* removes them.
3. Files are deleted only after every account, key and stored response is confirmed in the database. Only
   `config.json`, its temporary files and `data/responses/*.json` are removed; the PostgreSQL volume and
   anything else in `data/` are untouched. Stored responses older than their 30-day retention don't need to
   be transferred.

By hand, or to go back to JSON:

```bash
kiro-go migrate --from data/config.json --to "$DATABASE_URL"          # add --force to replace existing data
kiro-go export --from "$DATABASE_URL" --out config.backup.json
docker compose run --rm kiro-go ./kiro-go export --out /app/data/config.backup.json
```

## Request history and logging

With PostgreSQL, every API request is summarised after the response is sent: endpoint, protocol, model,
account, API key, client IP and user agent, status and error class, input/output/cache tokens, credits,
duration, time to first byte, upstream attempts and body sizes. Records go to a bounded in-memory queue and
are batch-inserted by a background writer; if the database is slow or down, records are dropped and counted
(Settings shows written / queued / dropped / failed) rather than delaying requests.

- **Bodies** are off by default. Settings → Logging enables capture with a per-body size cap (16 KB – 1 MB).
  Captured bodies are stored separately and deleted with their request.
- **Retention**: 30 days by default, configurable; old rows are purged in the background.
- **Audit log**: every admin change (settings, accounts, keys, models, migrations) is recorded with time, client IP and the affected item.
- The **Request logs** page filters by status, endpoint, model, account, key and free text, and shows each
  request's timing, routing, usage and bodies. Without a database the page shows the in-memory log.

## Command-line tools

```text
kiro-go                 start the server
kiro-go migrate         import config.json into PostgreSQL   (--from, --to, --force)
kiro-go export          write the database configuration to a JSON file   (--from, --out)
kiro-go help
```
