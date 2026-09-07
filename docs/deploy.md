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
- To use the prebuilt manager binary from GitHub Releases: none — just
  download and run it.
- To build/run the manager as a Docker image: Docker with BuildKit enabled
  (Docker 20.10+; recent Docker Desktop/Engine enable it by default).
- To build the manager binary yourself: Go toolchain, pnpm, and outbound
  access for Go modules, pnpm, and the pi download (or a build proxy
  `LAELIA_BUILD_PROXY`).
- Each machine host needs outbound access to the manager and to the hosted LLM
  providers used by its agents.

## 1. Build the manager

### 1a. Download the prebuilt manager binary (recommended)

Prebuilt manager binaries are published on GitHub Releases — no build toolchain
needed:

| Platform | Asset |
| --- | --- |
| Linux (amd64) | `laelia-linux-amd64` |
| macOS (Apple Silicon) | `laelia-darwin-arm64` |
| Windows (amd64) | `laelia-windows-amd64.exe` |

```bash
# Linux (amd64)
curl -fsSL -o laelia https://github.com/Ranxy/laelia/releases/latest/download/laelia-linux-amd64
chmod +x laelia

# macOS (Apple Silicon)
curl -fsSL -o laelia https://github.com/Ranxy/laelia/releases/latest/download/laelia-darwin-arm64
chmod +x laelia
```

```powershell
# Windows (PowerShell)
curl.exe -fsSL -o laelia.exe https://github.com/Ranxy/laelia/releases/latest/download/laelia-windows-amd64.exe
```

The prebuilt binary is the same self-contained manager produced by
`scripts/build_laelia.sh`: it embeds the frontend and the per-platform machine
binaries, and serves the same `/machine/install.sh`, `/machine/install.ps1`,
and `/machine/manifest.json` endpoints, so machine hosts can be installed
directly from it.

#### Optional release assets

For special use cases, the release also publishes:

- **Frontend-only managers** (no embedded machine binaries):
  `laelia-linux-amd64-frontend-only`, `laelia-windows-amd64-frontend-only.exe`,
  `laelia-darwin-arm64-frontend-only`. These serve the UI/API but do not
  provide the `/machine/*` install endpoints.
- **Standalone machine binaries with pi**:
  `laelia-machine-linux-x64`, `laelia-machine-windows-x64.exe`,
  `laelia-machine-darwin-arm64` (plus `.gz` and `manifest.json`).
- **Standalone machine binaries without pi** (builtin-pi unavailable; a
  user-installed pi on PATH still works):
  `laelia-machine-linux-x64-no-pi`, `laelia-machine-windows-x64-no-pi.exe`,
  `laelia-machine-darwin-arm64-no-pi` (plus `.gz` and `manifest.json`).

The fully integrated managers above remain the default and recommended option.

### 1b. Build the manager Docker image

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

### 1c. Build the manager binary

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

### 3b. Deploy the manager with Helm (Kubernetes)

An optional Helm chart (`charts/manager`) deploys the manager into a cluster.
It ships no database — provide a reachable PostgreSQL URL:

```bash
cd /path/to/repo
helm install laelia-manager charts/manager \
  --namespace <ns> --create-namespace \
  --set pg.url='postgresql://laelia:<password>@<db-host>:5432/laelia'
```

The chart creates a Deployment, Service, and a Secret carrying
`LAELIA_PG_URL`. The manager serves plain HTTP on 8181; for TLS, either
enable the chart's optional Ingress (below) or wire up your own reverse
proxy (see §5):

```bash
helm upgrade laelia-manager charts/manager --namespace <ns> \
  --set 'extraEnv[0].name=LAELIA_ALLOWED_ORIGINS' \
  --set 'extraEnv[0].value=https://laelia.example.com'
```

`--trust-proxy` (see §5) is enabled automatically while the chart's Ingress
is enabled; set `trustProxy=true` explicitly when the manager sits behind
your own trusted proxy, or `trustProxy=false` to force it off.

#### Optional Ingress

Set `ingress.enabled=true` and the chart creates an `Ingress` for the
manager. The web UI, the API, and the `/machine/*` install endpoints all live
on the one 8181 port, so a single catch-all path is enough. Deployments that
bring their own gateway keep it disabled (the default):

| Value | Purpose |
| --- | --- |
| `ingress.enabled` | Create the Ingress (default `false`). Also enables `--trust-proxy` (see above). |
| `ingress.className` | IngressClass of the target controller (`nginx`, `traefik`, ...). Empty lets the cluster's default controller claim the Ingress. |
| `ingress.annotations` | Free-form controller annotations; see the tuning example below. |
| `ingress.hosts` | Hosts and paths; each host needs at least one `path` (`pathType` defaults to `Prefix`). |
| `ingress.tls` | TLS sections (`secretName` + `hosts`). |
| `service.annotations` | Annotations on the chart's Service — controllers such as Traefik keep per-service options there. |

```bash
helm upgrade laelia-manager charts/manager --namespace <ns> \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set 'ingress.hosts[0].host=laelia.example.com' \
  --set 'ingress.hosts[0].paths[0].path=/' \
  --set ingress.tls[0].secretName=laelia-tls \
  --set 'ingress.tls[0].hosts[0]=laelia.example.com'
```

> **Machines need HTTP/2 through the ingress.** Machine and provisioner
> channels are bidirectional ConnectRPC streams and require HTTP/2
> end-to-end, so if machine traffic goes through the Ingress, the controller
> must forward HTTP/2 to the plain-HTTP backend (h2c upstream). Browser
> UI/API traffic has no such requirement:
>
> | Controller | h2c upstream | How |
> | --- | --- | --- |
> | Traefik | yes | annotate the chart's Service with `traefik.ingress.kubernetes.io/service.serversscheme: h2c` (via `service.annotations`) |
> | Envoy Gateway, Istio, Contour | yes | h2c upstreams supported natively |
> | ingress-nginx | no | `backend-protocol` has no H2C and `proxy-http-version` caps at 1.1 — expose the manager to machines another way that preserves HTTP/2 (e.g. L4/TCP or TLS passthrough) |
>
> nginx core itself gained HTTP/2 upstream support (`proxy_http_version 2`)
> in 1.29.4, but ingress-nginx does not expose it yet; check your
> controller's docs.

For an nginx ingress serving browser traffic, add the tuning annotations —
long timeouts keep command output streams open, buffering must be off, and
the body size covers the 100 MiB upload limit:

```yaml
ingress:
  enabled: true
  className: nginx
  annotations:
    nginx.ingress.kubernetes.io/proxy-body-size: "110m"
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-buffering: "off"
  hosts:
    - host: laelia.example.com
      paths:
        - path: /
  tls:
    - secretName: laelia-tls
      hosts:
        - laelia.example.com
```

As with the Docker image, the first user to sign up becomes the workspace
admin. The chart is optional: the Docker/binary flows above remain the
supported non-cluster installs.

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

> **Windows note:** pi agents work on Windows without Git Bash. Laelia installs
> a pi extension that overrides the `bash` tool with a native PowerShell 5.1
> backend, so the agent uses PowerShell syntax (no Bash heredocs or Unix-only
> commands).

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
`http://laelia-manager:8181` on a shared Docker network. On Kubernetes with
the chart's Ingress, see §3b for which controllers forward HTTP/2.

### Stop the machine

`setup` leaves the machine running in the background (a detached supervisor
process that watches the worker). To shut it down:

```bash
laelia-machine stop
```

The supervisor stops the worker gracefully and exits; the saved login is kept,
so running `laelia-machine --manager <url> setup` again starts the machine
without re-authenticating. `stop` reports an error if no machine is running on
this computer.

After the machine shows online, create agents on it from the UI. Configure the
API providers (for example DeepSeek or OpenRouter) that the agents should use.

## 5. External access

The manager serves plain HTTP on 8181 by default. For production, put a reverse
proxy with HTTPS in front of it. **HTTPS is also required for the PWA** (web app
install, service worker, offline app shell): browsers only enable service
workers on secure origins (or localhost). Use Caddy when machine traffic also
goes through the public endpoint — its `h2c` upstream keeps the backend leg on
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

## 8. Machine provisioners (optional)

A *provisioner* is an enterprise worker that creates machine workloads on
demand. An admin registers it once in the manager UI; afterwards any user with
the `laelia.provisioners.provision` permission clicks *Create machine* on the
Provisioned tab, and the machine appears online with no install command and no
device-code approval. It connects **outbound** to the manager (same direction
as machines), so nothing must be exposed on the manager or the cluster beyond
the existing manager endpoint.

How the pieces fit:

- The **manager** owns the provisioner registry, machine rows, and job state.
  It never talks to the cluster directly — jobs flow down a long-lived
  provisioner stream, status flows back up.
- The **provisioner** (`laelia-provisioner`) runs inside the customer's
  infrastructure (today: a Kubernetes cluster). It turns jobs into
  `LaeliaMachine` CRs plus their Secret/Service/StatefulSet children and
  reports pod-driven progress back. Jobs are replayed on reconnect, so killing
  the provisioner mid-provision is safe.
- The **machine pod** is a plain laelia machine: the runtime image provides the
  agent environment only; the machine binary is downloaded from the manager
  into a PVC by an init container at pod start, and every machine feature
  (agents, IAM, in-place upgrades) works identically.

### 8.1 Manager-side setup

1. **Register the provisioner** — Settings → Provisioners → *Add provisioner*.
   The one-time provisioner token is shown in a copy-once dialog; paste it into
   the provisioner's config (below). Rotating the token kills the old one at
   its next use; deleting a provisioner is refused while machines are still
   bound to it.
2. **Configure the runtime image** — Settings → General → *Machine runtime
   image* (for example `registry.example.com/laelia/machine-runtime:1.2.3`).
   Provisioning fails fast until this is set.
3. **Grant self-service** (optional) — bind the predefined `machineProvisioner`
   role (or `laelia.provisioners.provision`) to users/groups via Settings →
   Roles / Access. Workspace admins hold it automatically. The *Provisioned*
   tab on the Create-machine page appears only for these callers.

### 8.2 Install the provisioner (kubernetes backend)

Requires **kubernetes ≥ 1.27** (StatefulSet PVC auto-delete beta) — 1.32+ GA.
amd64 nodes only in this release.

Build and load the image:

```bash
LAELIA_BUILD_PROXY=http://host:port scripts/build_laelia_provisioner_docker.sh
# -> laelia/provisioner:latest (push/transfer it to your cluster's registry)
```

Or build a plain linux/amd64 binary for non-container installs:

```bash
GOOS=linux GOARCH=amd64 scripts/build_laelia_provisioner.sh   # -> build/laelia-provisioner
```

Apply the manifests (cluster admin applies the CRD once; the rest is
namespace-scoped):

```bash
cd backend/provisioner/backend/kubernetes/deploy
kubectl apply -f laelia.sh_laeliamachines.yaml   # CRD (cluster-scoped, once)
kubectl apply -f rbac.yaml                       # SA + Role + RoleBinding
kubectl apply -f deployment.yaml                 # namespace/secret/config/deployment
```

`deployment.yaml` carries a `laelia-provisioner-token` Secret — paste the
one-time token from step 8.1 into it (`stringData.token`); the config file
reads the token from the `LAELIA_PROVISIONER_TOKEN` environment variable, so
the secret never lands in a committed file. For a bare process instead of the
Deployment:

```bash
KUBECONFIG=/path/to/kubeconfig ./build/laelia-provisioner run \
  --config /etc/laelia-provisioner/provisioner.yaml
```

#### Install the provisioner with Helm (recommended)

A Helm chart (`charts/provisioner`) wraps the manifests above: its `crds/`
directory carries the CRD, which Helm applies before the chart's templates.
The RBAC, ConfigMap (rendered from `values.yaml`), Secret, and Deployment all
deploy into the release namespace — no namespace is created by the chart, so
pass one with `-n` (it should already exist or use `--create-namespace`):

```bash
cd /path/to/repo
helm install laelia-provisioner charts/provisioner \
  --namespace laelia-machines --create-namespace \
  --set token=llprov_... \
  --set managerUrl=https://laelia.example.com
```

Key values (see `charts/provisioner/values.yaml`):

| Value | Purpose |
| --- | --- |
| `token` | One-time token from Settings → Provisioners (required; injected as `LAELIA_PROVISIONER_TOKEN`). |
| `managerUrl` | Manager the provisioner connects to. |
| `namespace` | Where machine workloads land; defaults to the release namespace. |
| `managerUrlOverride` | In-cluster manager service URL for machine pods (egress-restricted clusters). |
| `retainData`/`autoUpgrade`/`storage`/`resources`/`extraEnv` | Passthrough provisioner config knobs. |

These values map to the `provisioner.yaml` rendered into the ConfigMap
(`managerUrl`→`manager_url`, `namespace`→`namespace`, `managerUrlOverride`
→`manager_url_override`, `retainData`→`retain_data`, `autoUpgrade`
→`auto_upgrade`, `storage.*`→`storage.*`, `resources.*`→`resources.*`,
`extraEnv`→`extra_env`, `backend` is fixed to `kubernetes`). The token is
*not* written to the config file — it is read at runtime from the
`LAELIA_PROVISIONER_TOKEN` Secret. Unset optional values (e.g. an empty
`managerUrlOverride`) are omitted from the rendered YAML, matching the
commented-out optional knobs of `deployment.yaml`.

Set them on the command line with `--set`, or prefer a dedicated values file
for anything beyond the two required fields:

```bash
# --set form
helm install laelia-provisioner charts/provisioner -n laelia-machines \
  --set token=llprov_... \
  --set managerUrl=https://laelia.example.com \
  --set managerUrlOverride=http://laelia-manager.laelia-machines.svc:8181 \
  --set retainData=true \
  --set 'storage.size=20Gi' \
  --set 'extraEnv.LAELIA_INSECURE=true'

# values-file form (recommended): values mirror the field names above
helm install laelia-provisioner charts/provisioner -n laelia-machines \
  -f my-provisioner-values.yaml
```

```yaml
# my-provisioner-values.yaml
token: llprov_...
managerUrl: https://laelia.example.com
namespace: laelia-machines          # defaults to the release namespace
managerUrlOverride: http://laelia-manager.laelia-machines.svc:8181
retainData: true
autoUpgrade: false
storage:
  size: 20Gi
  storageClass: ""
resources:
  requests: { cpu: "1", memory: "2Gi" }
  limits: { memory: "4Gi" }
extraEnv:
  LAELIA_INSECURE: "true"
```

These values are read at install/upgrade time only. After changing them, run
`helm upgrade laelia-provisioner charts/provisioner -n laelia-machines -f
my-provisioner-values.yaml` (add `--recreate-pods` if the ConfigMap value
changed but the pod didn't need rescheduling) so the Deployment picks up the
new ConfigMap.

`helm uninstall` removes the namespaced resources but — because the CRD ships
in `crds/` — **not** the cluster-scoped CRD; delete it manually:

```bash
helm uninstall laelia-provisioner --namespace laelia-machines
kubectl delete crd laeliamachines.laelia.sh   # Helm does not manage CRDs
```

### 8.3 Provisioner config reference

```yaml
manager_url: https://laelia.example.com   # or --manager flag
token: llprov_...                          # one-time token; --token flag or LAELIA_PROVISIONER_TOKEN env
backend: kubernetes                        # workload backend (kubernetes today)
namespace: laelia-machines                 # where machine workloads land
# Optional:
manager_url_override: http://laelia-manager.laelia-machines.svc:8181
                                           # pods connect here instead of manager_url (egress-restricted clusters)
retain_data: false                         # keep machine data PVCs on delete (StatefulSet Retain)
auto_upgrade: false                        # manager auto-triggers upgrades for this provisioner's machines
storage: { size: 10Gi, storage_class: "" } # PVC size/class per machine (storage_class defaults to the cluster default)
resources:
  requests: { cpu: "1", memory: "2Gi" }
  limits: { memory: "4Gi" }
param_bounds:                              # optional bounds for user-settable machine parameters
  cpu:    { min: "250m",  max: "8"     }   # (design: provisioner-machine-params-design.md); an omitted
  memory: { min: "512Mi", max: "32Gi" }    # side is unbounded; users may override these values
  disk:   { min: "1Gi",   max: "500Gi" }   # per machine at create time, defaults keep applying
extra_env:                                 # passthrough env on the machine container
  LAELIA_INSECURE: "true"                  # for https managers with self-signed certs
```

Machine parameters (§8.3a): the values above are defaults users may override
per machine at create time; `resources`/`storage` stay the fallback for
machines whose user leaves a field empty.

`--allow-http` is required when `manager_url` is plain HTTP (dev only).

### 8.4 RBAC matrix

The provisioner's Role is namespace-scoped; no cluster-admin and no cluster-wide
list/watch (design §12). The CRD itself is applied once by a cluster admin.

| Resource | Verbs | Why |
|---|---|---|
| `laelia.sh/laeliamachines` (+`/status`, `/finalizers`) | get/list/watch/create/update/patch/delete | One CR per machine; the CR is the workload's desired state |
| `secrets` | get/list/watch/create/update/patch/delete | Bootstrap secret (`machine.json` + bootstrap script) |
| `services` | get/list/watch/create/update/patch/delete | Headless Service required by the StatefulSet |
| `apps/statefulsets` | get/list/watch/create/update/patch/delete | The machine's single-replica workload |
| `pods` | get/list/watch | Pod status drives the CR phase |
| `persistentvolumeclaims` | get/list/watch/delete | Explicit PVC cleanup on delete (retention fallback) |
| `events` | create/patch | `kubectl describe` diagnostics |

### 8.5 Runtime image contract

The runtime image provides the agent environment; it must NOT contain the
laelia machine binary — the pod downloads it from the manager into the PVC and
upgrades it in place afterwards. Reference image:
`scripts/docker/Dockerfile.machine-runtime` (node, python, build-essential,
git, curl, jq, ripgrep, codex CLI; non-root uid 1001). Any image satisfying the
contract works:

- POSIX `sh`, `curl`, `gzip`, `sha256sum` (the init container's bootstrap script)
- an entrypoint that execs `$LAELIA_MACHINE_BIN` (default `/data/bin/laelia-machine`)
  with `LAELIA_HOME=/data/laelia`, honoring `LAELIA_MANAGER_URL` (+ auto
  `--allow-http` for `http://`), `LAELIA_PROVISIONED=true` → `--provisioned
  --no-browser --foreground`, and `CODEX_HOME` (default
  `/data/laelia/codex`, on the PVC)
- runs as a non-root uid

### 8.6 What the provisioner creates per machine

All inside the configured namespace, owned by the `LaeliaMachine` CR
`laelia-machine-<machine-uuid-prefix>`: a bootstrap Secret (the machine's
credential — CRs never carry tokens), a headless Service, a single-replica
StatefulSet with `volumeClaimTemplates: [data]` and the amd64 nodeSelector, and
the data PVC. Deleting the machine in the UI deletes the CR; the finalizer
removes the Secret and (unless `retain_data: true`) the data PVC. `kubectl get
laeliamachines -n laelia-machines` is the operator's fleet view.

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

Provisioner-specific:

- ProvisionMachine fails with "runtime image not configured" — set the machine
  runtime image under Settings → General (section 8.1).
- A new machine stays at phase *Pending* — the provisioner is offline or not
  registered. The job is replayed automatically when the provisioner connects;
  check the provisioner's logs and its Status column on Settings →
  Provisioners.
- Machine pod stuck in `ImagePullBackOff` / `ErrImagePull` — the runtime image
  reference (Settings → General) is not pullable from the nodes (private
  registry credentials, wrong tag/arch). Fix the image or the pull secret, then
  delete and recreate the machine.
- Machine pod stuck in `Creating` — usually a Pending PVC: the configured
  `storage.storage_class` does not exist or no default StorageClass is
  bound (`kubectl get pvc -n laelia-machines`). Set `storage.storage_class`
  in the provisioner config and recreate the machine.
- Machine shows *Provisioned* but stays offline — the workload exists but the
  pod is not connecting (bootstrap download failing?). Check
  `kubectl logs` on the pod's init container and the manager's reachability
  from the pod (`manager_url` / `manager_url_override`).
- Deleted machine left a data PVC behind — the provisioner has
  `retain_data: true`; the PVC is kept on purpose. Remove it manually when no
  longer needed.
