# Deploy with Docker

Laelia has two deployable components, both packaged as Docker images:

- `laelia/manager` — the web UI and manager API. It stores all state in
  PostgreSQL and embeds the frontend.
- `laelia/machine` — an agent host. It connects to the manager, runs one or
  more agents, and embeds the pi runtime.

The images are built from this repository; no prebuilt registry images are
published yet.

## Prerequisites

- Docker with BuildKit enabled (Docker 20.10+; recent Docker Desktop/Engine
  enable it by default).
- PostgreSQL 13+ (14+ recommended), reachable from the manager.
- Outbound network access from the build machine for Go modules, pnpm, and the
  pi download, or a build proxy (`LAELIA_BUILD_PROXY`).
- Each machine host needs outbound access to the manager and to the hosted LLM
  providers used by its agents.

## 1. Build the images

The two images are built independently:

```bash
scripts/build_laelia_manager_docker.sh   # -> laelia/manager:local + laelia/manager:latest
scripts/build_laelia_machine_docker.sh   # -> laelia/machine:local + laelia/machine:latest
```

Build options:

| Option | Purpose |
| --- | --- |
| `VERSION` | Image tag version (default: `local`) |
| `LAELIA_BUILD_PROXY` | Build-time proxy for Go module downloads and the pi download |
| `PI_PROXY` | Override for the pi download only (machine image) |
| `APT_MIRROR` | Debian mirror used for the machine image's apt packages |
| `CODEX_NPM_SPEC` | Codex CLI version spec installed in the machine image |

Example:

```bash
VERSION=1.2.0 LAELIA_BUILD_PROXY=http://proxy.example.com:8080 scripts/build_laelia_manager_docker.sh
VERSION=1.2.0 LAELIA_BUILD_PROXY=http://proxy.example.com:8080 scripts/build_laelia_machine_docker.sh
```

Do not export a global `HTTPS_PROXY` for `docker build`: BuildKit injects it
into every stage, including the final runtime images. `LAELIA_BUILD_PROXY` is
scoped to the build stages that need it.

## 2. Prepare PostgreSQL

The manager runs schema migrations automatically on startup, so it only needs
an empty database with the right privileges. Create a database user and a
UTF-8 database:

```sql
CREATE USER laelia WITH PASSWORD '<strong-password>';
CREATE DATABASE laelia OWNER laelia ENCODING 'UTF8';
```

For an existing database:

```sql
ALTER DATABASE laelia OWNER TO laelia;
```

Database ownership is the simplest way to give the user what the migrations
need: creating tables and the `pg_trgm` extension. On managed PostgreSQL where
you cannot change ownership, pre-create the extension and grant schema access
as an admin:

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
GRANT CREATE ON SCHEMA public TO laelia;
```

The manager connects with a standard PostgreSQL URI:

```
postgresql://laelia:<password>@<db-host>:5432/laelia
```

## 3. Start the manager

```bash
docker run -d --name laelia-manager \
  --restart unless-stopped \
  -p 8181:8181 \
  -e LAELIA_PG_URL='postgresql://laelia:<password>@<db-host>:5432/laelia' \
  laelia/manager:local
```

The image runs as an unprivileged user and checks `/healthz`. Verify with:

```bash
curl -fsS http://localhost:8181/healthz
```

Open http://localhost:8181 and sign up. The first user becomes the workspace
admin. After signing in, configure API providers under Settings, then create a
machine (next section).

Manager environment variables:

| Variable | Description |
| --- | --- |
| `LAELIA_PG_URL` | PostgreSQL connection URL (required). |
| `LAELIA_ALLOWED_ORIGINS` | Comma-separated list of extra origins (e.g. `https://front.example.com`) allowed to call the API cross-origin with credentials. Same-origin requests are always allowed; empty means cross-origin browser access is disabled. |
| `LAELIA_COOKIE_SAMESITE` | Access-token cookie SameSite policy: `lax` (default), `strict`, or `none`. `none` is only for deployments that serve the frontend from a different site than the API (it is only honored over HTTPS and requires `LAELIA_ALLOWED_ORIGINS` to stay CSRF-safe). |

Frontend on a different subdomain of the same site (e.g. UI at
`https://laeliapage.example.com`, API at `https://laelia.example.com`): set
`LAELIA_ALLOWED_ORIGINS=https://laeliapage.example.com` and build the frontend
with `VITE_API_BASE_URL=https://laelia.example.com`. The default `lax` cookie
policy still works because subdomains of the same registrable domain are
same-site; `LAELIA_COOKIE_SAMESITE=none` is only needed when the frontend is
on a completely different domain.

Notes:

- PostgreSQL on the same host: on Linux use `--network host` and drop `-p`; on
  Docker Desktop use `host.docker.internal` as the database host. On Linux
  Docker you can also add `--add-host=host.docker.internal:host-gateway` and
  keep the port mapping.
- The manager keeps no local state by default; the database is the source of
  truth, so back it up rather than the container. If you enable the built-in
  TLS (below), persist its certificate directory with a volume.
- The manager applies pending migrations on every startup; make a database
  backup before upgrading.

## 4. Start machine hosts

Machines authenticate with the manager through an OAuth2-style **device code
flow** — there are no registration tokens. In the manager UI, go to Machines
and click *Create Machine*: the page shows the command to run on the host and
waits for the machine to appear. On the host, run:

```bash
docker run -d --name laelia-machine \
  --restart unless-stopped \
  -e LAELIA_MANAGER_URL='https://laelia.example.com' \
  -v laelia-machine-data:/home/laelia \
  laelia/machine:local
```

The entrypoint runs `laelia-machine setup --no-browser`: it prints an approval
URL (e.g. `https://laelia.example.com/login/device?user_code=XXXX-XXXX`) to the
container logs, waits for a logged-in user to open it and approve, then runs
the machine in the foreground. On later restarts the saved login is validated
automatically ("already logged in") and the machine starts directly.

Environment variables:

| Variable | Description |
| --- | --- |
| `LAELIA_MANAGER_URL` | Manager base URL. For `http://` URLs the entrypoint automatically adds `--allow-http`. |
| `LAELIA_INSECURE` | `true` to skip TLS certificate verification (self-signed setups; development only). |
| `LAELIA_DEBUG` | `true` for debug logging. |
| `LAELIA_HOME` | Laelia data root directory (use an absolute path). When set, `machine.json`, `daemon.sock`, agent workspaces, and the materialized pi runtime all live under this directory. Defaults to `~/.laelia`. |

The machine makes outbound connections only; no port needs to be published.
Mount a volume at `$LAELIA_HOME` so agent workspaces, the persisted login
state (`machine.json`), and the materialized pi runtime survive container
restarts. If you prefer a bind mount, create the directory and
`chown 1001:1001` it first.

If the container is recreated without the volume, the login state is lost:
the machine re-runs the device flow and registers a brand-new machine (the
old machine row stays on the manager, offline). To re-authenticate an
existing machine instead, keep the volume and, if its login was revoked, run
`laelia-machine setup` on the host again and approve with the machine's owner
or a workspace admin.

Machine-manager channels are bidirectional and require HTTP/2. When the manager
is behind a reverse proxy, the proxy must forward HTTP/2 (see below); otherwise
point `LAELIA_MANAGER_URL` directly at the manager, for example
`http://laelia-manager:8181` on a shared Docker network.

After the machine shows online, create agents on it from the UI. Configure the
API providers (for example DeepSeek or OpenRouter) that the agents should use.

## 5. External access

The manager serves plain HTTP on 8181 by default. For production, put a reverse
proxy with HTTPS in front of it. Use Caddy when machine traffic also goes
through the public endpoint — its `h2c` upstream keeps the backend leg on
HTTP/2:

```caddyfile
laelia.example.com {
    reverse_proxy 127.0.0.1:8181 {
        transport http {
            versions h2c
            read_timeout 3600s
            write_timeout 3600s
        }
    }
}
```

Caddy obtains and renews a Let's Encrypt certificate automatically. The long
timeouts keep command output streams open. If Caddy itself runs in Docker,
point it at the manager container on a shared network instead, for example
`h2c://laelia-manager:8181`.

Nginx works for the web UI. Note that classic `proxy_pass` cannot forward
HTTP/2 upstream, so machine hosts should connect to the manager directly
rather than through Nginx:

```nginx
server {
    listen 80;
    listen 443 ssl;
    server_name laelia.example.com;

    ssl_certificate /path/to/fullchain.pem;
    ssl_certificate_key /path/to/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8181;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_buffering off;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
        client_max_body_size 110m;
    }
}
```

`proxy_buffering off` streams command output to the browser as it arrives;
`client_max_body_size` covers the 100 MiB upload limit.

Behind a trusted reverse proxy, start the manager with `--trust-proxy` so
client IPs from `X-Forwarded-For`/`X-Real-IP` are trusted for rate limiting and
IP allowlists:

```bash
docker run -d --name laelia-manager \
  --restart unless-stopped \
  -p 8181:8181 \
  -e LAELIA_PG_URL='postgresql://laelia:<password>@<db-host>:5432/laelia' \
  laelia/manager:local --port 8181 --trust-proxy
```

The manager also has built-in TLS: `--tls-cert-dir` loads or generates a
self-signed certificate and `--tls-host` lists its hostnames. Automatic ACME
certificates are not implemented yet, so a reverse proxy with a trusted
certificate is the recommended setup. If you use the built-in TLS, mount a
volume on a directory the unprivileged user can write (for example
`/home/laelia`) and pass `--tls-cert-dir /home/laelia/certs`.

## 6. Upgrade

Manager:

1. Back up PostgreSQL.
2. Build or pull the new image.
3. Stop and remove the container, then start it with the same `LAELIA_PG_URL`
   and the new image tag. Pending migrations apply automatically on startup.

Machine:

1. Stop and remove the container.
2. Start it with the new image and the same `/home/laelia` volume. The
   persisted refresh token lets it reconnect; you only need a new registration
   token if the volume was lost or the token was rotated/revoked.

## 7. Air-gapped environments

If the target host cannot reach a registry, transfer the images:

```bash
docker save laelia/manager:local laelia/machine:local | gzip > laelia-images.tar.gz
```

Copy the archive to the target host and load it:

```bash
docker load < laelia-images.tar.gz
```

## Troubleshooting

- `bind: address already in use` — port 8181 is taken on the host. Stop the
  conflicting process or map a different host port (`-p 8080:8181`).
- The manager logs `must set PG_URL environment variable` — `LAELIA_PG_URL` is
  missing or empty; pass it with `-e`.
- Database connection or migration errors — verify the URI, database encoding,
  and that the user can create tables and the `pg_trgm` extension (section 2).
- Machine cannot connect — check that `LAELIA_MANAGER_URL` is reachable and
  that HTTP/2 is preserved through any proxy; with self-signed certificates
  set `LAELIA_INSECURE=true` (development only). If the machine container was
  recreated without its volume, rotate the token in the UI.
- Command output stalls in the web UI behind Nginx — use `proxy_buffering off`
  and long `proxy_read_timeout`/`proxy_send_timeout`.
- 502 Bad Gateway — `proxy_pass` must point at the actual manager
  (`127.0.0.1:8181` or the container name), not at the public domain, or the
  proxy loops.
