> **语言 / Language:** [English](deploy.md) | [中文](deploy_zh.md)

# 部署

Laelia 包含两个可部署组件：

- **Manager** — Web UI 与 Manager API。所有状态存储在 PostgreSQL 中，并内嵌前端以及各平台的 machine 二进制。可以以 Docker 镜像（`laelia/manager`）方式运行，也可以使用 `scripts/build_laelia.sh` 构建为原生二进制运行。
- **Machine** — 代理宿主机。它连接 Manager，运行一个或多个代理，并内嵌 pi 运行时。Machine 通过 Manager 的 *创建 Machine* 页面提供的脚本安装到宿主机上；不再有独立的 machine Docker 镜像。

Manager 镜像从本仓库构建；目前尚未发布预构建的 registry 镜像。

## 前置条件

- PostgreSQL 13+（推荐 14+），Manager 需要能够访问。
- 使用 GitHub Releases 上的预编译 Manager 二进制：无需任何构建工具链，下载即可运行。
- 以 Docker 镜像方式构建/运行 Manager：需要启用 BuildKit 的 Docker（Docker 20.10+；新版 Docker Desktop/Engine 默认已启用）。
- 自行构建 Manager 二进制：需要 Go 工具链、pnpm，以及访问 Go modules、pnpm 和 pi 下载的网络（或使用构建代理 `LAELIA_BUILD_PROXY`）。
- 每台 machine 宿主机需要能够访问 Manager，以及其代理所使用的托管 LLM 提供商。

## 1. 构建 Manager

### 1a. 下载预编译的 Manager 二进制（推荐）

每个平台的预编译 Manager 二进制发布在 GitHub Releases 上，无需构建工具链：

| 平台 | 文件 |
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
# Windows（PowerShell）
curl.exe -fsSL -o laelia.exe https://github.com/Ranxy/laelia/releases/latest/download/laelia-windows-amd64.exe
```

预编译二进制与 `scripts/build_laelia.sh` 产出的自包含 Manager 一致：内嵌前端和各平台 machine 二进制，并同样提供 `/machine/install.sh`、`/machine/install.ps1` 和 `/machine/manifest.json` 端点，可以直接从它安装 machine 宿主机。

### 1b. 构建 Manager Docker 镜像

```bash
scripts/build_laelia_manager_docker.sh   # -> laelia/manager:local + laelia/manager:latest
```

构建选项：

| 选项 | 用途 |
| --- | --- |
| `VERSION` | 镜像标签版本（默认：`local`） |
| `LAELIA_BUILD_PROXY` | 构建时用于 Go module 下载和 pi 下载的代理 |

示例：

```bash
VERSION=1.2.0 LAELIA_BUILD_PROXY=http://proxy.example.com:8080 scripts/build_laelia_manager_docker.sh
```

不要为 `docker build` 导出全局 `HTTPS_PROXY`：BuildKit 会将其注入到每个构建阶段，包括最终运行时镜像。`LAELIA_BUILD_PROXY` 只作用于需要它的构建阶段。

### 1c. 构建 Manager 二进制

如果希望以原生二进制而不是容器方式运行 Manager，请使用 `scripts/build_laelia.sh`。它会构建前端、交叉编译并内嵌各平台的 machine 二进制，最终生成一个自包含的 Manager 二进制：

```bash
scripts/build_laelia.sh                 # -> build/laelia（Manager 二进制）
LAELIA_BUILD_PROXY=http://proxy.example.com:8080 scripts/build_laelia.sh
```

输出 `build/laelia` 是内嵌了前端和 machine 二进制的 Manager 二进制。它与 Docker 镜像一样提供 `/machine/install.sh`、`/machine/install.ps1` 和 `/machine/manifest.json` 端点，因此可以直接从它安装 machine 宿主机。

## 2. 准备 PostgreSQL

Manager 启动时会自动执行 schema 迁移，因此只需要一个具有相应权限的空数据库。创建数据库用户和 UTF-8 数据库：

```sql
CREATE USER laelia WITH PASSWORD '<strong-password>';
CREATE DATABASE laelia OWNER laelia ENCODING 'UTF8';
```

对于已有数据库：

```sql
ALTER DATABASE laelia OWNER TO laelia;
```

数据库所有权是让迁移所需权限（创建表以及 `pg_trgm` 扩展）最简单的方式。在无法更改所有权的托管 PostgreSQL 上，请由管理员预先创建扩展并授予 schema 访问权限：

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
GRANT CREATE ON SCHEMA public TO laelia;
```

Manager 使用标准 PostgreSQL URI 连接：

```
postgresql://laelia:<password>@<db-host>:5432/laelia
```

## 3. 启动 Manager

```bash
docker run -d --name laelia-manager \
  --restart unless-stopped \
  -p 8181:8181 \
  -e LAELIA_PG_URL='postgresql://laelia:<password>@<db-host>:5432/laelia' \
  laelia/manager:local
```

如果构建的是原生二进制，请使用相同的环境变量运行：

```bash
LAELIA_PG_URL='postgresql://laelia:<password>@<db-host>:5432/laelia' \
  ./build/laelia --port 8181
```

镜像以非特权用户运行，并提供 `/healthz` 健康检查。验证方式：

```bash
curl -fsS http://localhost:8181/healthz
```

打开 http://localhost:8181 并注册。第一个用户将成为工作区管理员。登录后，在 Settings 中配置 API 提供商，然后创建 machine（见下一节）。

Manager 环境变量：

| 变量 | 说明 |
| --- | --- |
| `LAELIA_PG_URL` | PostgreSQL 连接 URL（必填）。 |
| `LAELIA_ALLOWED_ORIGINS` | 允许跨域携带凭据调用 API 的额外来源列表（逗号分隔，例如 `https://front.example.com`）。同源请求始终允许；为空表示禁用跨域浏览器访问。 |
| `LAELIA_COOKIE_SAMESITE` | 访问令牌 cookie 的 SameSite 策略：`lax`（默认）、`strict` 或 `none`。`none` 仅用于前端与 API 在不同站点部署的情况（仅在 HTTPS 下生效，并且需要 `LAELIA_ALLOWED_ORIGINS` 以保持 CSRF 安全）。 |

前端与 API 位于同一站点的不同子域（例如 UI 在 `https://laeliapage.example.com`，API 在 `https://laelia.example.com`）：设置 `LAELIA_ALLOWED_ORIGINS=https://laeliapage.example.com`，并使用 `VITE_API_BASE_URL=https://laelia.example.com` 构建前端。默认的 `lax` cookie 策略仍然有效，因为同一注册域的子域属于 same-site；只有当前端位于完全不同的域名时才需要 `LAELIA_COOKIE_SAMESITE=none`。

注意事项：

- PostgreSQL 与 Manager 在同一主机：Linux 上使用 `--network host` 并去掉 `-p`；Docker Desktop 上使用 `host.docker.internal` 作为数据库主机。Linux Docker 也可以添加 `--add-host=host.docker.internal:host-gateway` 并保留端口映射。
- Manager 默认不保留本地状态；数据库是唯一数据源，因此应备份数据库而不是容器。如果启用了内置 TLS（见下文），请用 volume 持久化其证书目录。
- Manager 每次启动都会应用待执行的迁移；升级前请备份数据库。

## 4. 启动 machine 宿主机

Machine 通过 OAuth2 风格的 **设备码流程** 与 Manager 进行认证——没有注册令牌。在 Manager UI 中，进入 Machines 并点击 *创建 Machine*。页面会显示两条需要在宿主机上执行的命令：

1. **安装（Install）** — 从 Manager 安装 `laelia-machine` 二进制。
2. **设置（Setup）** — 运行 `laelia-machine --manager <url> setup` 完成认证并启动 machine。

在你批准登录后，页面会等待 machine 出现。

### 安装 machine 二进制

在宿主机上运行页面显示的安装命令。它会从 Manager 下载预构建的 `laelia-machine` 二进制，根据 manifest 校验 SHA-256，并安装到 `~/.local/bin`：

```bash
# Linux / macOS
curl -fsSL https://laelia.example.com/machine/install.sh | sh

# Windows（PowerShell）
irm https://laelia.example.com/machine/install.ps1 | iex
```

安装脚本由 Manager 提供，并且已经包含 Manager URL，因此无需设置环境变量。可选覆盖项：`LAELIA_MACHINE_INSTALL_DIR`（安装目录，默认 `~/.local/bin`）和 `LAELIA_MACHINE_FORCE=1`（即使已安装也重新安装）。

### 运行 `laelia-machine setup`

安装完成后，运行页面显示的 setup 命令：

```bash
laelia-machine --manager https://laelia.example.com setup
```

`setup` 会启动设备码流程：打印批准 URL（例如 `https://laelia.example.com/login/device?user_code=XXXX-XXXX`）和用户码，等待已登录用户打开并批准，然后在前台运行 machine。之后重启时会自动验证已保存的登录状态（“already logged in”）并直接启动 machine。

CLI 选项：

| 选项 | 说明 |
| --- | --- |
| `--manager <url>` | Manager 基础 URL（默认 `https://localhost:8181`）。对于 `http://` URL 需要添加 `--allow-http`。 |
| `--insecure` | 跳过 TLS 证书校验（自签名环境；仅开发用）。 |
| `--allow-http` | 允许明文 HTTP 连接（仅开发用）。 |
| `--debug` | 启用调试日志。 |
| `--force` | 清除本地 machine 状态并注册一台全新 machine（仅 setup）。 |
| `--no-browser` | 不自动打开浏览器中的批准 URL（仅 setup）。 |

machine 数据根目录由 `LAELIA_HOME` 环境变量控制（请使用绝对路径）。设置后，`machine.json`、`daemon.sock`、代理工作区以及物化的 pi 运行时都位于该目录下。默认为 `~/.laelia`。

machine 只发起出站连接；无需发布任何端口。请将 `$LAELIA_HOME` 放在持久化文件系统上，以便代理工作区、已保存的登录状态（`machine.json`）和物化的 pi 运行时在重启后仍然保留。

如果本地状态丢失，machine 会重新执行设备码流程并注册一台全新 machine（旧 machine 记录仍保留在 Manager 上，处于离线状态）。如果要重新认证已有 machine，请保留 `$LAELIA_HOME`；如果其登录已被吊销，请在宿主机上再次运行 `laelia-machine --manager <url> setup`，并由 machine 的所有者或工作区管理员批准。

machine 与 Manager 之间的通道是双向的，并且需要 HTTP/2。当 Manager 位于反向代理之后时，代理必须转发 HTTP/2（见下文）；否则请将 `--manager` 直接指向 Manager，例如共享 Docker 网络中的 `http://laelia-manager:8181`。

### 停止 machine

`setup` 会让 machine 在后台运行（一个分离的 supervisor 进程负责监控 worker）。要关闭它：

```bash
laelia-machine stop
```

supervisor 会优雅地停止 worker 并退出；已保存的登录状态会保留，因此再次运行 `laelia-machine --manager <url> setup` 即可重新启动 machine，无需重新认证。如果本机没有正在运行的 machine，`stop` 会报错。

machine 显示在线后，可以在 UI 中为其创建代理。请配置代理要使用的 API 提供商（例如 DeepSeek 或 OpenRouter）。

## 5. 外部访问

Manager 默认在 8181 端口提供明文 HTTP。生产环境建议在前面放置带 HTTPS 的反向代理。当 machine 流量也经过公共端点时，请使用 Caddy——它的 `h2c` upstream 可以保持后端为 HTTP/2：

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

Caddy 会自动获取并续期 Let's Encrypt 证书。较长的超时时间可以保持命令输出流不断开。如果 Caddy 本身运行在 Docker 中，请将其指向共享网络上的 Manager 容器，例如 `h2c://laelia-manager:8181`。

Nginx 适用于 Web UI。注意：传统的 `proxy_pass` 无法转发 HTTP/2 upstream，因此 machine 宿主机应直接连接 Manager，而不是通过 Nginx：

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

`proxy_buffering off` 可以将命令输出实时流式传输到浏览器；`client_max_body_size` 覆盖 100 MiB 的上传限制。

在可信反向代理之后，使用 `--trust-proxy` 启动 Manager，以便在限流和 IP 白名单中信任来自 `X-Forwarded-For`/`X-Real-IP` 的客户端 IP：

```bash
docker run -d --name laelia-manager \
  --restart unless-stopped \
  -p 8181:8181 \
  -e LAELIA_PG_URL='postgresql://laelia:<password>@<db-host>:5432/laelia' \
  laelia/manager:local --port 8181 --trust-proxy
```

Manager 还内置 TLS：`--tls-cert-dir` 会加载或生成自签名证书，`--tls-host` 列出其主机名。目前尚未实现自动 ACME 证书，因此推荐使用带可信证书的反向代理。如果使用内置 TLS，请将 volume 挂载到非特权用户可写的目录（例如 `/home/laelia`），并传入 `--tls-cert-dir /home/laelia/certs`。

## 6. 升级

Manager：

1. 备份 PostgreSQL。
2. 构建或拉取新镜像（或使用 `scripts/build_laelia.sh` 重新构建二进制）。
3. 停止并删除容器，然后使用相同的 `LAELIA_PG_URL` 和新镜像标签启动。待执行的迁移会在启动时自动应用。对于原生二进制，请替换旧的 `build/laelia` 并重启进程。

Machine：

1. 重新运行 Manager *创建 Machine* 页面中的安装命令（或重新运行安装脚本）以更新 `laelia-machine` 二进制。
2. 再次运行 `laelia-machine --manager <url> setup`。已保存的 refresh token 可以让它重新连接；只有在本地状态丢失或令牌被轮换/吊销时才需要重新认证。

## 7. 离线/隔离环境

如果目标主机无法访问 registry，请传输 Manager 镜像：

```bash
docker save laelia/manager:local | gzip > laelia-manager-image.tar.gz
```

将归档复制到目标主机并加载：

```bash
docker load < laelia-manager-image.tar.gz
```

对于原生 Manager，请改为复制 `build/laelia` 二进制。machine 宿主机从 Manager 本身安装 `laelia-machine`，因此只要它们能访问 Manager，就不需要单独传输镜像或二进制。

## 故障排查

- `bind: address already in use` — 主机上的 8181 端口已被占用。请停止冲突进程或映射不同的主机端口（`-p 8080:8181`）。
- Manager 日志显示 `must set PG_URL environment variable` — `LAELIA_PG_URL` 缺失或为空；请通过 `-e` 传入。
- 数据库连接或迁移错误 — 请检查 URI、数据库编码，以及用户是否可以创建表和 `pg_trgm` 扩展（第 2 节）。
- Machine 无法连接 — 请检查 `--manager` URL 是否可达，以及 HTTP/2 是否在代理中保留；对于自签名证书，请使用 `--insecure`（仅开发用）。如果本地 machine 状态丢失，请重新运行 `laelia-machine --manager <url> setup` 重新认证。
- Web UI 在 Nginx 后面命令输出卡住 — 请使用 `proxy_buffering off` 以及较长的 `proxy_read_timeout`/`proxy_send_timeout`。
- 502 Bad Gateway — `proxy_pass` 必须指向实际的 Manager（`127.0.0.1:8181` 或容器名），而不是公共域名，否则代理会循环。
