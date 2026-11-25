# Upgrading

## From Kiro-Go to Kiro Gateway

The project was renamed from **Kiro-Go** to **Kiro Gateway** and moved to
[springmusk026/Kiro-Gateway](https://github.com/springmusk026/Kiro-Gateway). Your data, settings, accounts and
API keys carry over unchanged; only names changed.

| What | Before | Now |
|---|---|---|
| Repository | `springmusk026/Kiro-Go` | `springmusk026/Kiro-Gateway` (the old URL redirects) |
| Docker image | `ghcr.io/springmusk026/kiro-go` | `ghcr.io/springmusk026/kiro-gateway` |
| Compose service | `kiro-go` | `kiro-gateway` |
| Binary and CLI | `kiro-go` | `kiro-gateway` |
| Go module | `kiro-go` | `github.com/springmusk026/Kiro-Gateway` |
| Fallback response header | `x-kiro-go-fallback-model` | `x-kiro-gateway-fallback-model` |

Environment variables (`KIRO_*`, `DATABASE_URL`, `ADMIN_PASSWORD`, …), the `data/` folder, the database and the
console address (`/admin`) are unchanged.

**Docker Compose**

```bash
git remote set-url origin https://github.com/springmusk026/Kiro-Gateway.git   # optional; GitHub redirects
git pull
docker compose up -d --build --remove-orphans
```

`--remove-orphans` stops the old `kiro-go` container, which would otherwise keep port 8080. The PostgreSQL
container and `./data` are reused as they are.

**Plain Docker** — replace the image name with `ghcr.io/springmusk026/kiro-gateway` and recreate the container
with the same volume and environment.

**Scripts** — rename `kiro-go migrate` / `kiro-go export` to `kiro-gateway migrate` / `kiro-gateway export`, and
read `x-kiro-gateway-fallback-model` if you used the fallback header.

## From the original Kiro-Go (Quorinex) or a JSON-only setup

1. Keep your `data/` folder (it holds `config.json`).
2. Start Kiro Gateway with the bundled `docker-compose.yml`. On first start against an empty database the
   gateway imports `config.json` and any stored responses automatically.
3. Open the console. A **migration prompt** compares `config.json` with the database, imports anything
   missing, verifies every account and key, and offers to delete the JSON files.

Things that work differently from the original project:

- **Admin password**: the old default password is never used. If none is set, one is generated on first start
  and printed in the log.
- **Thinking**: clients turn on thinking through their own settings (Claude Code's thinking setting, OpenAI
  `reasoning_effort`, …). The `-thinking` model suffix still works.
- **Admin sign-in** uses a session cookie; scripts can still send the `X-Admin-Password` header.

## API keys are hashed

From this version the gateway stores only a SHA-256 hash of each API key (plus a display hint such as
`sk-ab12…9f3c`). On the first start after upgrading, every stored key — plaintext, or encrypted with
`KIRO_ENCRYPTION_KEY` — is hashed and the configuration is saved again, in `config.json` or in PostgreSQL,
so no key value remains at rest. The deprecated single `apiKey` setting is converted into a key entry the
same way. Clients keep working with the keys they already have.

What changes for you:

- The console can no longer show or copy an existing key. Copy a new key when it is created (it is shown
  once), or use *Regenerate key* in the key's menu to replace a lost one.
- `kiro-gateway export` writes key hashes (`keyHash`, `keyHint`), not keys. Keep your own record of keys you
  hand out.
- The admin API's key views have `keyHint` instead of `keyMasked`; scripts that read `keyMasked` need updating.
- Downgrading to an older version after the migration loses the keys (older versions cannot read hashes):
  back up `config.json` or the database first if you might roll back.

## Encryption key

Setting `KIRO_ENCRYPTION_KEY` for the first time encrypts stored credentials on the next start. From then on
the same key is required to start the gateway. To change the key, export with the old key
(`kiro-gateway export --decrypt`), then import with the new one (`kiro-gateway migrate --force`).

## Version notes

See [CHANGELOG.md](../CHANGELOG.md) for what changed in each version.
