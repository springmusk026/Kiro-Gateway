# Security policy

## Reporting a vulnerability

Please report security issues **privately** through GitHub's
[private vulnerability reporting](https://github.com/springmusk026/Kiro-Gateway/security/advisories/new)
(Security → Report a vulnerability). Do not open a public issue.

Include what you found, how to reproduce it, the version you tested and the impact you expect. We aim to
acknowledge reports within a few days and to publish a fix and an advisory once one is available. We are happy
to credit you in the advisory.

## Supported versions

Security fixes are made on the latest release and the `main` branch. Please upgrade before reporting.

## Deployment checklist

Kiro Gateway holds credentials for your Kiro accounts. When you run it:

- **Require API keys** (Settings → Security → *Require an API key*) before the gateway is reachable from other
  machines, and give each person or tool its own key with sensible limits.
- **Use HTTPS** through a reverse proxy, and forward `X-Forwarded-Proto` so admin cookies are marked secure.
- **Set a strong admin password** (`ADMIN_PASSWORD`). Admin sign-in uses HttpOnly, SameSite=Strict session
  cookies and is rate-limited.
- **Encrypt credentials at rest** with `KIRO_ENCRYPTION_KEY`, and store that key outside the server.
- **Keep request bodies off** unless you need them; when enabled, prompts and responses are stored in the
  database.
- **Restrict `/metrics`** with `KIRO_METRICS_TOKEN` if you enable it.
- **Back up the database** and keep backups as protected as the server itself.

## Scope

In scope: the gateway, its admin console and API, and the published Docker image. Out of scope: Kiro's own
services, and issues that require an attacker to already have admin access or access to the server.
