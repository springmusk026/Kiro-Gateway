# Contributing to Kiro Gateway

Thanks for taking the time to contribute. This guide explains how to report problems, propose changes and get
a pull request merged.

## Ways to help

- **Report a bug** — open an issue with the bug report form. Include the version (shown in the console
  footer), how you run the gateway, the client you use and the relevant log lines or Request log entry.
  Remove API keys, tokens and account emails first.
- **Suggest a feature** — open an issue with the feature request form and describe the problem you want to
  solve before the solution you have in mind.
- **Improve the documentation** — fixes and clarifications are always welcome.
- **Send code** — see below.

Please don't report security vulnerabilities in public issues; follow [SECURITY.md](SECURITY.md) instead.

## Development setup

You need Go 1.25+, Node.js 22+ and, for the storage tests, PostgreSQL 16 (a Docker container is fine).
[docs/development.md](docs/development.md) has the full instructions; the short version:

```bash
(cd frontend && npm ci && npm run build)
go build -o kiro-gateway . && ADMIN_PASSWORD=dev ./kiro-gateway
```

For console work, run `npm run dev` in `frontend/` while the gateway runs on port 8080.

## Before you open a pull request

1. **Keep it focused.** One change per pull request is easier to review and to revert if needed.
2. **Run the checks** that CI runs:

   ```bash
   gofmt -l $(git ls-files '*.go')        # must print nothing
   go vet ./... && go test ./... && go test -race ./proxy/
   (cd frontend && npx tsc --noEmit && npm run build:only)
   ```
3. **Add tests** for behaviour you change or add. Gateway behaviour is tested with Go tests in the package you
   touch; storage changes need a test that runs against PostgreSQL (`KIRO_TEST_DATABASE_URL`).
4. **Update the docs** when you add a setting, an environment variable, an endpoint or change behaviour users
   can see, and add a line to the *Unreleased* section of [CHANGELOG.md](CHANGELOG.md).
5. **Console changes** follow [frontend/DESIGN.md](frontend/DESIGN.md). Add English strings to
   `frontend/src/locales/en*.ts`; other languages fall back to English until translated. Check the page at
   phone width (390 px) and in dark mode.
6. **Don't commit the published console** (`web/`) unless the pull request is specifically a release build;
   maintainers rebuild it.

## Guidelines for gateway changes

- Requests sent to Kiro must look like the official Kiro clients' requests. Don't add headers, fields or extra
  upstream calls without evidence from the official clients ([docs/kiro-protocol.md](docs/kiro-protocol.md)).
- Nothing on the request path may wait for the database; record asynchronously.
- Errors returned to clients use each API's own error format (`proxy/api_errors.go`).
- Never log secrets. Mask emails and keys in anything shown to users.

## Commit messages

Use short, descriptive subjects in the form `area: what changed` (for example
`proxy: retry once on a fallback model`), and explain the reason in the body when it isn't obvious.

## License

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE).
