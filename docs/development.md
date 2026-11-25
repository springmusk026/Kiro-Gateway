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

## Conventions

- Keep upstream traffic indistinguishable from the official Kiro clients: don't add headers, fields or
  request patterns they don't send, and don't add upstream calls to the request path.
- Nothing on the request path may wait on the database; record asynchronously (see `storage/recorder.go`).
- Errors returned to clients use the API's own envelope via `proxy/api_errors.go`.
- New settings go through `config/` (persisted by whichever store is active) and get an admin API route,
  audited automatically for non-GET calls.

## Continuous integration

`.github/workflows/ci.yml` runs on every push and pull request: `gofmt`, `go vet`, all Go tests (the storage
tests against a PostgreSQL service container), the race detector on `proxy/`, and the console type check and
build. Run the same locally before pushing:

```bash
gofmt -l $(git ls-files '*.go'); go vet ./... && go test ./... && go test -race ./proxy/
(cd frontend && npx tsc --noEmit && npm run build:only)
```

## Releases

Pushing to `main` builds and publishes `ghcr.io/springmusk026/kiro-gateway:latest` (amd64 + arm64) through
`.github/workflows/docker.yml`, which also publishes versioned images for each tag.

To release version X.Y.Z:

1. Bump `config.Version` and `version.json`, and rename the `## Unreleased` section of `CHANGELOG.md` to
   `## X.Y.Z — <title>` (keep an empty `## Unreleased` above it). Commit.
2. Tag and push: `git tag vX.Y.Z && git push origin vX.Y.Z`.

`.github/workflows/release.yml` then:

- takes the release notes from the `## X.Y.Z` section of `CHANGELOG.md` and **fails** if that section is
  missing or empty (fix the changelog, delete the tag and push it again);
- builds the console (`npm run build:only`) and the binaries with `CGO_ENABLED=0` for linux/amd64,
  linux/arm64, darwin/arm64 and windows/amd64;
- publishes a GitHub Release (a prerelease for tags such as `v2.2.0-rc.1`) with one archive per platform
  (`.tar.gz`, `.zip` for Windows) and `SHA256SUMS.txt`.

Each archive holds the binary, the console in `web/`, `README.md`, `LICENSE` and `CHANGELOG.md`. The binary
serves the console from `./web` relative to its working directory (the console is not embedded, so the
Docker image and source builds behave as before): extract the archive and start the binary from that
folder, e.g. `cd kiro-gateway-vX.Y.Z-linux-amd64 && ADMIN_PASSWORD=… ./kiro-gateway`.

Dependabot (`.github/dependabot.yml`) opens weekly pull requests for Go modules, the console's npm
packages and GitHub Actions, with minor and patch updates grouped into one pull request per ecosystem.
