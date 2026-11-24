# Troubleshooting

Start with the gateway's log (`docker compose logs -f kiro-gateway`) and the **Request logs** page, which shows
the error, the account used and the upstream attempts for every request.

## Setup and access

**I don't know the admin password.**
It is printed once on first start: `docker compose logs kiro-gateway | grep -i password`. To set a new one,
put `ADMIN_PASSWORD=…` in `.env` and restart; that value always wins.

**The console still shows an old version after updating.**
Rebuild the image: `docker compose up -d --build --remove-orphans`, then reload the page.

**Port 8080 is already in use after upgrading from Kiro-Go.**
The old `kiro-go` container is still running. Run `docker compose up -d --remove-orphans`.

**The gateway stops at start-up with an encryption error.**
The stored credentials are encrypted and `KIRO_ENCRYPTION_KEY` is missing or different from the key used
before. Set the original key. See [upgrading.md](upgrading.md#encryption-key).

**`could not connect to database`.**
Check `DATABASE_URL` and that PostgreSQL is running (`docker compose ps`). The gateway retries for a minute at
start-up while the database comes up.

## Requests

**`overloaded_error` / HTTP 529 "No available accounts".**
No account can serve the request: all are disabled, cooling down after rate limits, or out of quota. Check the
Accounts page; quota resets at the start of the billing period. Turning on *Keep using accounts over quota*
(Settings → Routing) allows overage, which may be billed.

**HTTP 401 `authentication_error`.**
Key verification is on and the API key is missing, wrong, disabled or expired. Clients send it as `Authorization: Bearer <key>` or
`x-api-key: <key>`.

**HTTP 403 `model_not_allowed`.**
The key is restricted to certain models. Edit the key in **API & Keys**.

**HTTP 429 with a daily budget message.**
A global or per-key daily credit cap was reached. It resets at midnight in the budget time zone (Settings →
Budgets & alerts).

**"prompt is too long".**
The conversation is larger than the model's context window. Claude Code compacts automatically when it sees
this error; in other clients, start a new conversation or reduce attachments.

**A model is missing or "model not found".**
Refresh the catalogue (Models → Refresh from Kiro) and check that the model is enabled. Some models are only
available to certain account types or regions.

**Streaming responses stop after about 60–100 seconds behind a proxy.**
Your reverse proxy is buffering or timing out. Turn buffering off and raise the read timeout (see
[installation.md](installation.md#running-in-production)). The gateway sends keep-alive events every
15 seconds to keep connections open.

**An account keeps getting marked unhealthy.**
Open the account's details: an expired refresh token needs the account to be re-added; repeated rate limits
mean the account is overused — add accounts or lower the load.

## Getting more detail

- Set `LOG_LEVEL=debug` to log upstream requests and responses (remove it afterwards; it is verbose).
- Turn on body capture (Settings → Logging & storage) to see request and response bodies in Request logs.

If you are still stuck, see [SUPPORT.md](../SUPPORT.md).
