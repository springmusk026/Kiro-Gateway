# Development

## Requirements

- Go 1.25+
- Node 22+ and npm (console)
- PostgreSQL 16 for the storage integration tests (optional)

## Build and run

```bash
(cd frontend && npm ci && npm run build)   # type-checks, builds and publishes the console into ./web
go build -o kiro-gateway .
ADMIN_PASSWORD=dev-password ./kiro-gateway       # serves http://localhost:8080 and /admin
```

Run the binary from the repository root: the console is served from `./web` relative to the working
directory.

### Console development

```bash
cd frontend
npm run dev          # Vite on http://localhost:5173/admin, proxying /admin/api and /v1 to :8080
KIRO_GATEWAY_URL=http://other-host:8080 npm run dev
npm run typecheck
```

The console is a TanStack Start SPA (file routes in `src/routes`, data hooks in `src/lib/queries*.ts`,
components in `src/components`), styled with shadcn/ui on Tailwind v4. Read
[`frontend/DESIGN.md`](../frontend/DESIGN.md) before adding UI. English strings in `src/locales/en*.ts` are the
source of truth; `zh.ts` and `vi.ts` may lag and fall back to English.

`npm run build` writes the static build to `web/`, which is committed so `go build` alone produces a working
server. Commit `frontend/` sources and the rebuilt `web/` together; the Docker build rebuilds the console from
source regardless.

## Tests

```bash
go vet ./...
go test ./...
go test -race ./proxy/
```

Storage tests run against a real database when `KIRO_TEST_DATABASE_URL` is set (they truncate the tables):

```bash
docker run -d --name kiro-test-pg -p 55432:5432 -e POSTGRES_USER=kiro -e POSTGRES_HOST_AUTH_METHOD=trust \
  -e POSTGRES_DB=kirogo_test postgres:16-alpine
KIRO_TEST_DATABASE_URL="postgres://kiro@localhost:55432/kirogo_test?sslmode=disable" go test ./storage/
```

`KIRO_LIVE=1 KIRO_LIVE_CONFIG=path/to/config.json go test -run TestLiveResponsesTransport ./proxy/` exercises
the real `/v1/responses` upstream with an account from that config (uses credits).

### Live check (opt-in, uses Kiro credits)

`proxy/live_check_test.go` builds the gateway, starts it with a temporary config, adds a Kiro API-key account
through the admin API (`POST /admin/api/auth/credentials`) and sends one minimal request each to
`/v1/messages` (non-streaming and streaming), `/v1/chat/completions` and `/v1/responses` with the newest
haiku-class model from `/v1/models`. It checks the response shapes: HTTP 200, text content, usage counts, and
the Anthropic SSE event order (`message_start`, content blocks, `message_delta`, `message_stop`). It is skipped
unless `KIRO_LIVE_API_KEY` is set:

```bash
KIRO_LIVE_API_KEY=ksk_... KIRO_LIVE_REGION=us-east-1 go test ./proxy -run '^TestLiveGateway$' -count=1 -v
```

`KIRO_LIVE_MODEL` forces a model, `KIRO_LIVE_PROXY_URL` sets the gateway's outbound proxy and
`KIRO_LIVE_GATEWAY_BIN` uses a binary you already built. The key is never printed; on failure the gateway log
is shown with the key masked.

`.github/workflows/live-check.yml` runs it daily (and on demand from the Actions tab). To enable it, add the
repository secret `KIRO_LIVE_API_KEY` (Settings → Secrets and variables → Actions) and, if the key's account
is not in `us-east-1`, `KIRO_LIVE_REGION`. Without the secret the workflow ends with a notice and does nothing.
Each run costs four tiny requests a day (a one-line prompt, at most 16 output tokens each) on the cheapest
model, plus the model-list call the gateway makes when an account is added.
Use a dedicated key so it can be revoked on its own.

### Console tests

Unit tests (Vitest, jsdom) sit next to the code they cover as `*.test.ts(x)`: pagination, the API client,
i18n and locale integrity (translations may only use English keys and must keep the same `{placeholders}`),
and the format helpers.

```bash
cd frontend
npm test             # vitest run, a few seconds
npm run test:watch
```

End-to-end smoke tests (Playwright, `frontend/e2e/`) run against a real gateway binary serving the published
console, so build the console first. Global setup builds the gateway (`go build` from the repository root),
starts it on a free port with a temporary `CONFIG_PATH` and `ADMIN_PASSWORD`, and signs in once. With
`E2E_DATABASE_URL` set, a second project runs the same tests against a gateway on PostgreSQL; its settings,
accounts and API keys are replaced at the start of each run (request logs are kept).

```bash
cd frontend
npm run build                                    # publishes the console into ../web (don't commit it)
npx playwright install chromium                  # once
npx playwright test                              # JSON backend, about 20 s
E2E_DATABASE_URL="postgres://kiro@localhost:55432/kirogo_e2e?sslmode=disable" npx playwright test
```

`PW_CHROMIUM=/path/to/chrome` uses an already installed Chromium, `E2E_GATEWAY_BIN` skips the Go build, and
`E2E_KEEP_TMP=1` keeps the temporary directory with each gateway's log. The tests never contact Kiro: they
add no accounts and the gateway's outbound proxy points at a closed local port. Tests fail on any console
error or uncaught exception; a test that expects one (a 401 on the login page) lists it with
`test.use({ allowConsoleErrors: [...] })`.

## Conventions

- Keep upstream traffic indistinguishable from the official Kiro clients: don't add headers, fields or
  request patterns they don't send, and don't add upstream calls to the request path.
- Nothing on the request path may wait on the database; record asynchronously (see `storage/recorder.go`).
- Errors returned to clients use the API's own envelope via `proxy/api_errors.go`.
- New settings go through `config/` (persisted by whichever store is active) and get an admin API route,
  audited automatically for non-GET calls.

## Continuous integration

`.github/workflows/ci.yml` runs on every push and pull request: `gofmt`, `go vet`, all Go tests (the storage
tests against a PostgreSQL service container), the race detector on `proxy/`, the console type check and
build, the console unit tests, and the Playwright suite against both storage backends (the HTML report is
uploaded as an artifact when it fails). Run the same locally before pushing:

```bash
gofmt -l $(git ls-files '*.go'); go vet ./... && go test ./... && go test -race ./proxy/
(cd frontend && npx tsc --noEmit && npm test && npm run build && npx playwright test)
```

## Releases

Pushing to `main` builds and publishes `ghcr.io/springmusk026/kiro-gateway:latest` (amd64 + arm64) through
`.github/workflows/docker.yml`. Tag `vX.Y.Z` to publish versioned images; bump `config.Version`,
`version.json` and `CHANGELOG.md` together.
