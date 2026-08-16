> **Language / 语言:** [English](deploy.md) | [中文](deploy_zh.md)

# Deploy

Laelia has two deployable components:

- **Manager** — the web UI and manager API. It stores all state in PostgreSQL
  and embeds the frontend plus the per-platform machine binaries. It can be
  run as a Docker image (`laelia/manager`) or as a native binary built with
  `scripts/build_laelia.sh`.
- **Machine** — an agent host. It connects to the manager, runs one or more
  agents, and embeds the pi runtime. Machines are installed on hosts with the
  script shown on the manager's *Create Machine* page; there is no separate
  machine Docker image.

The manager image is built from this repository; no prebuilt registry images
are published yet.

## Prerequisites

- PostgreSQL 13+ (14+ recommended), reachable from the manager.
- To build/run the manager as a Docker image: Docker with BuildKit enabled
  (Docker 20.10+; recent Docker Desktop/Engine enable it by default).
- To build the manager binary: Go toolchain, pnpm, and outbound access for Go
  modules, pnpm, and the pi download (or a build proxy `LAELIA_BUILD_PROXY`).
- Each machine host needs outbound access to the manager and to the hosted LLM
  providers used by its agents.

## 1. Build the manager

### 1a. Build the manager Docker image

```bash
scripts/build_laelia_manager_docker.sh   # -> laelia/manager:local + laelia/manager:latest
```

Build options:

| Option | Purpose |
| --- | --- |
| `VERSION` | Image tag version (default: `local`) |
| `LAELIA_BUILD_PROXY` | Build-time proxy for Go module downloads and the pi download |

Example:

```bash
VERSION=1.2.0 LAELIA_BUILD_PROXY=http://proxy.example.com:8080 scripts/build_laelia_manager_docker.sh
```

Do not export a global `HTTPS_PROXY` for `docker build`: BuildKit injects it
into every stage, including the final runtime images. `LAELIA_BUILD_PROXY` is
scoped to the build stages that need it.

### 1b. Build the manager binary

To run the manager as a native binary instead of a container, use
`scripts/build_laelia.sh`. It builds the frontend, cross-compiles and embeds
the per-platform machine binaries, and produces a single self-contained
manager binary:

```bash
scripts/build_laelia.sh                 # -> build/laelia (manager binary)
LAELIA_BUILD_PROXY=http://proxy.example.com:8080 scripts/build_laelia.sh
```

The output `build/laelia` is the manager binary with the frontend and machine
binaries embedded. It serves the same `/machine/install.sh`,
`/machine/install.ps1`, and `/machine/manifest.json` endpoints as the Docker
image, so machine hosts can be installed directly from it.

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

If you built the native binary instead, run it with the same environment:

```bash
LAELIA_PG_URL='postgresql://laelia:<password>@<db-host>:5432/laelia' \
  ./build/laelia --port 8181
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
and click *Create Machine*. The page shows two commands for the host:

1. **Install** — installs the `laelia-machine` binary from the manager.
2. **Setup** — runs `laelia-machine --manager <url> setup` to authenticate and
   start the machine.

The page waits for the machine to appear after you approve the login.

### Install the machine binary

On the host, run the install command shown on the page. It downloads the
prebuilt `laelia-machine` binary from the manager, verifies its SHA-256 against
the manifest, and installs it to `~/.local/bin`:

```bash
# Linux / macOS
curl -fsSL https://laelia.example.com/machine/install.sh | sh

# Windows (PowerShell)
irm https://laelia.example.com/machine/install.ps1 | iex
```

The install script is served by the manager and already contains the manager
URL, so no environment variables are needed. Optional overrides:
`LAELIA_MACHINE_INSTALL_DIR` (install directory, default `~/.local/bin`) and
`LAELIA_MACHINE_FORCE=1` (reinstall even if already present).

### Run `laelia-machine setup`

After installation, run the setup command shown on the page:

```bash
laelia-machine --manager https://laelia.example.com setup
```

`setup` starts the device-code flow: it prints an approval URL (e.g.
`https://laelia.example.com/login/device?user_code=XXXX-XXXX`) and a user code,
waits for a logged-in user to open it and approve, then runs the machine in the
foreground. On later restarts the saved login is validated automatically
("already logged in") and the machine starts directly.

CLI options:

| Option | Description |
| --- | --- |
| `--manager <url>` | Manager base URL (default `https://localhost:8181`). For `http://` URLs add `--allow-http`. |
| `--insecure` | Skip TLS certificate verification (self-signed setups; development only). |
| `--allow-http` | Allow plain HTTP connections (development only). |
| `--debug` | Enable debug logging. |
| `--force` | Wipe local machine state and register a brand-new machine (setup only). |
| `--no-browser` | Do not auto-open the approval URL in a browser (setup only). |

The machine data root is controlled by the `LAELIA_HOME` environment variable
(use an absolute path). When set, `machine.json`, `daemon.sock`, agent
workspaces, and the materialized pi runtime all live under this directory.
Defaults to `~/.laelia`.

The machine makes outbound connections only; no port needs to be published.
Keep `$LAELIA_HOME` on a persistent filesystem so agent workspaces, the
persisted login state (`machine.json`), and the materialized pi runtime survive
restarts.

If the local state is lost, the machine re-runs the device flow and registers
a brand-new machine (the old machine row stays on the manager, offline). To
re-authenticate an existing machine instead, keep `$LAELIA_HOME` and, if its
login was revoked, run `laelia-machine --manager <url> setup` on the host again
and approve with the machine's owner or a workspace admin.

Machine-manager channels are bidirectional and require HTTP/2. When the manager
is behind a reverse proxy, the proxy must forward HTTP/2 (see below); otherwise
point `--manager` directly at the manager, for example
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
2. Build or pull the new image (or rebuild the binary with
   `scripts/build_laelia.sh`).
3. Stop and remove the container, then start it with the same `LAELIA_PG_URL`
   and the new image tag. Pending migrations apply automatically on startup.
   For a native binary, replace the old `build/laelia` and restart the process.

Machine:

1. Re-run the install command from the manager's *Create Machine* page (or
   re-run the install script) to update the `laelia-machine` binary.
2. Run `laelia-machine --manager <url> setup` again. The persisted refresh
   token lets it reconnect; you only need to re-authenticate if the local
   state was lost or the token was rotated/revoked.

## 7. Air-gapped environments

If the target host cannot reach a registry, transfer the manager image:

```bash
docker save laelia/manager:local | gzip > laelia-manager-image.tar.gz
```

Copy the archive to the target host and load it:

```bash
docker load < laelia-manager-image.tar.gz
```

For a native manager, copy the `build/laelia` binary instead. Machine hosts
install `laelia-machine` from the manager itself, so as long as they can reach
the manager they do not need a separate image or binary transfer.

## Troubleshooting

- `bind: address already in use` — port 8181 is taken on the host. Stop the
  conflicting process or map a different host port (`-p 8080:8181`).
- The manager logs `must set PG_URL environment variable` — `LAELIA_PG_URL` is
  missing or empty; pass it with `-e`.
- Database connection or migration errors — verify the URI, database encoding,
  and that the user can create tables and the `pg_trgm` extension (section 2).
- Machine cannot connect — check that the `--manager` URL is reachable and
  that HTTP/2 is preserved through any proxy; with self-signed certificates
  use `--insecure` (development only). If the local machine state was lost,
  re-run `laelia-machine --manager <url> setup` to re-authenticate.
- Command output stalls in the web UI behind Nginx — use `proxy_buffering off`
  and long `proxy_read_timeout`/`proxy_send_timeout`.
- 502 Bad Gateway — `proxy_pass` must point at the actual manager
  (`127.0.0.1:8181` or the container name), not at the public domain, or the
  proxy loops.
