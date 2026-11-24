# Installation

Kiro Gateway runs as a single service. PostgreSQL is optional but recommended: it adds request history,
analytics and the audit log.

- [Docker Compose (recommended)](#docker-compose-recommended)
- [Prebuilt image](#prebuilt-image)
- [From source](#from-source)
- [Running in production](#running-in-production)

## Docker Compose (recommended)

```bash
git clone https://github.com/springmusk026/Kiro-Gateway.git
cd Kiro-Gateway
docker compose up -d
```

This starts two containers: `kiro-gateway` on port 8080 and `postgres` (PostgreSQL 16). Data lives in
`./data` next to the compose file.

Optional settings go in a `.env` file in the same folder:

```dotenv
ADMIN_PASSWORD=choose-a-strong-password
POSTGRES_PASSWORD=another-strong-password
KIRO_ENCRYPTION_KEY=   # openssl rand -base64 32 — keep a copy somewhere safe
```

If `ADMIN_PASSWORD` is not set, a password is generated on first start and printed once:

```bash
docker compose logs kiro-gateway | grep -i password
```

Open **http://localhost:8080/admin**, sign in, add an account and create an API key.

**Updating**

```bash
git pull
docker compose up -d --build --remove-orphans
```

**Nightly database backups** (optional) are provided as a compose profile; dumps go to `./data/backups`:

```bash
docker compose --profile backup up -d
```

## Prebuilt image

Multi-architecture images (amd64, arm64) are published to the GitHub Container Registry: `latest` from the
main branch and a tag for each release.

```bash
docker run -d --name kiro-gateway -p 8080:8080 \
  -e ADMIN_PASSWORD=choose-a-strong-password \
  -e DATABASE_URL="postgres://kiro:password@db-host:5432/kiro?sslmode=disable" \
  -v "$PWD/data:/app/data" \
  --restart unless-stopped \
  ghcr.io/springmusk026/kiro-gateway:latest
```

Without `DATABASE_URL` the gateway stores its configuration in `data/config.json` and keeps only recent
request logs in memory.

## From source

Requirements: Go 1.25 or newer and Node.js 22 or newer.

```bash
git clone https://github.com/springmusk026/Kiro-Gateway.git
cd Kiro-Gateway
(cd frontend && npm ci && npm run build)   # builds the admin console into ./web
go build -o kiro-gateway .
ADMIN_PASSWORD=choose-a-strong-password ./kiro-gateway
```

Run the binary from the repository folder so it can find `./web`. Command-line tools:

```text
kiro-gateway                 start the gateway
kiro-gateway migrate         import a config.json into PostgreSQL
kiro-gateway export          write the configuration back to a JSON file
kiro-gateway help
```

## Running in production

Before exposing the gateway beyond your own machine:

1. **Require API keys.** Create keys in **API & Keys** and turn on **Require an API key** (Settings →
   Security); until then the endpoints are open to anyone who can reach the port. Give each person or tool
   its own key so usage and limits are tracked separately.
2. **Use HTTPS.** Put a reverse proxy in front of the gateway. Streaming responses need buffering turned off
   and generous timeouts.

   Caddy:

   ```caddyfile
   gateway.example.com {
       reverse_proxy 127.0.0.1:8080 {
           flush_interval -1
       }
   }
   ```

   nginx:

   ```nginx
   location / {
       proxy_pass http://127.0.0.1:8080;
       proxy_http_version 1.1;
       proxy_set_header Host $host;
       proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
       proxy_set_header X-Forwarded-Proto $scheme;
       proxy_buffering off;
       proxy_read_timeout 600s;
       client_max_body_size 64m;
   }
   ```

   Forward `X-Forwarded-Proto` so admin session cookies are marked secure.
3. **Encrypt credentials at rest** with `KIRO_ENCRYPTION_KEY`, and store the key separately from the server.
4. **Set budgets and alerts** under Settings → Budgets & alerts, so a runaway client cannot use up your
   accounts.
5. **Back up the database** (compose `backup` profile or your own `pg_dump` schedule).
6. **Monitor it.** Set `KIRO_METRICS=1` (and `KIRO_METRICS_TOKEN`) to expose Prometheus metrics at
   `/metrics`.

The database grows with request history; 30 days are kept by default (Settings → Logging & storage).

All settings and environment variables are described in [configuration.md](configuration.md).
