# Development

## Requirements

- Go 1.25+
- Node 22+ and npm (console)
- PostgreSQL 16 for the storage integration tests (optional)

## Build and run

```bash
(cd frontend && npm ci && npm run build)   # type-checks, builds and publishes the console into ./web
go build -o kiro-go .
ADMIN_PASSWORD=dev-password ./kiro-go       # serves http://localhost:8080 and /admin
```

Run the binary from the repository root: the console is served from `./web` relative to the working
directory.

### Console development

```bash
cd frontend
npm run dev          # Vite on http://localhost:5173/admin, proxying /admin/api and /v1 to :8080
KIRO_GO_URL=http://other-host:8080 npm run dev
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

## Conventions

- Keep upstream traffic indistinguishable from the official Kiro clients: don't add headers, fields or
  request patterns they don't send, and don't add upstream calls to the request path.
- Nothing on the request path may wait on the database; record asynchronously (see `storage/recorder.go`).
- Errors returned to clients use the API's own envelope via `proxy/api_errors.go`.
- New settings go through `config/` (persisted by whichever store is active) and get an admin API route,
  audited automatically for non-GET calls.

## Releases

Pushing to `main` builds and publishes `ghcr.io/springmusk026/kiro-go:latest` (amd64 + arm64) through
`.github/workflows/docker.yml`. Tag `vX.Y.Z` to publish versioned images; bump `config.Version`,
`version.json` and `CHANGELOG.md` together.
