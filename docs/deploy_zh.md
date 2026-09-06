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

#### 可选的 Release 资产

针对特殊用途，Release 还会发布：

- **只内嵌前端的 Manager**（不内嵌 machine 二进制）：
  `laelia-linux-amd64-frontend-only`、`laelia-windows-amd64-frontend-only.exe`、
  `laelia-darwin-arm64-frontend-only`。它们提供 UI/API，但不提供 `/machine/*` 安装端点。
- **内嵌 pi 的独立 machine 二进制**：
  `laelia-machine-linux-x64`、`laelia-machine-windows-x64.exe`、
  `laelia-machine-darwin-arm64`（以及 `.gz` 和 `manifest.json`）。
- **不内嵌 pi 的独立 machine 二进制**（builtin-pi 不可用，但用户自行安装的 pi 仍可用）：
  `laelia-machine-linux-x64-no-pi`、`laelia-machine-windows-x64-no-pi.exe`、
  `laelia-machine-darwin-arm64-no-pi`（以及 `.gz` 和 `manifest.json`）。

以上完全集成的 Manager 仍然是默认和推荐选项。

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

### 3b. 使用 Helm 部署 Manager（Kubernetes）

仓库提供可选的 Helm chart（`charts/manager`）将 Manager 部署进集群。chart 不内置数据库——请自行提供可达的 PostgreSQL 连接串：

```bash
cd /path/to/repo
helm install laelia-manager charts/manager \
  --namespace <ns> --create-namespace \
  --set pg.url='postgresql://laelia:<password>@<db-host>:5432/laelia'
```

chart 会创建 Deployment、Service 以及承载 `LAELIA_PG_URL` 的 Secret。Manager 默认在 8181 端口提供纯 HTTP 服务；TLS 请自行配置 ingress/反向代理（见 §5），仅当位于可信代理之后时才启用 `--trust-proxy`：

```bash
helm upgrade laelia-manager charts/manager --namespace <ns> \
  --set 'extraEnv[0].name=LAELIA_ALLOWED_ORIGINS' \
  --set 'extraEnv[0].value=https://laelia.example.com' \
  --set trustProxy=true
```

与 Docker 镜像一致，首个注册用户成为工作区管理员。该 chart 是可选方案：上述 Docker/二进制部署方式仍是受支持的非集群安装方式。

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

> **Windows 注意：** pi 代理在 Windows 上无需 Git Bash。Laelia 会安装一个 pi 扩展，把 `bash` 工具替换为原生 PowerShell 5.1 后端，因此 agent 使用 PowerShell 语法（不要使用 Bash heredoc 或 Unix-only 命令）。

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

Manager 默认在 8181 端口提供明文 HTTP。生产环境建议在前面放置带 HTTPS 的反向代理。**HTTPS 也是 PWA 生效的前提**（Web 应用安装、Service Worker、离线应用壳都需要安全源；浏览器仅在 HTTPS 或 localhost 下启用 Service Worker）。当 machine 流量也经过公共端点时，请使用 Caddy——它的 `h2c` upstream 可以保持后端为 HTTP/2：

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

## 8. 机器 Provisioner（可选）

*Provisioner* 是按需创建 machine 工作负载的企业级 worker。管理员在 Manager UI 中注册一次之后，任何持有 `laelia.provisioners.provision` 权限的用户在创建 Machine 页面的“预置机器”标签页中点击创建，机器即可自动上线——无需安装命令，也无需设备码审批。它与 Manager 的连接方向和 machine 一样是**出站**连接，因此 Manager 或集群除了现有 Manager 端点之外不需要再暴露任何东西。

各组件的分工：

- **Manager** 持有 provisioner 注册表、machine 行和任务状态。它从不直接访问集群——任务通过一条长连的 provisioner 流下发，状态再回传上来。
- **Provisioner**（`laelia-provisioner`）运行在客户的基础设施中（当前为 Kubernetes 集群）。它把任务转换成 `LaeliaMachine` CR 及其 Secret/Service/StatefulSet 子资源，并把 pod 驱动的进度上报。任务在重连时会重放，因此 provisioner 在预置过程中被杀掉是安全的。
- **machine pod** 就是一台普通的 laelia machine：运行时镜像只提供 agent 环境；machine 二进制由 init 容器在 pod 启动时从 Manager 下载到 PVC 中，之后的升级全部原地完成。

### 8.1 Manager 侧配置

1. **注册 provisioner** — 设置 → Provisioners → *添加 provisioner*。一次性 provisioner token 会在“仅展示一次”的对话框中显示；请把它粘贴到 provisioner 的配置文件中（见下文）。轮换 token 会让旧 token 在下次使用时失效；删除 provisioner 时若仍有机器绑定会被拒绝。
2. **配置运行时镜像** — 设置 → 通用 → *机器运行时镜像*（例如 `registry.example.com/laelia/machine-runtime:1.2.3`）。未配置该项时预置机器会快速失败。
3. **开放自助（可选）** — 通过 设置 → 角色 / 访问控制，将预定义的 `machineProvisioner` 角色（或 `laelia.provisioners.provision` 权限）绑定给用户/用户组。工作空间管理员自动持有。只有这些用户才能看到创建 Machine 页面的“预置机器”标签页。

### 8.2 安装 provisioner（kubernetes 后端）

要求 **kubernetes ≥ 1.27**（StatefulSet PVC 自动删除为 beta 特性）——1.32+ 转正。本版本仅支持 amd64 节点。

构建并加载镜像：

```bash
LAELIA_BUILD_PROXY=http://host:port scripts/build_laelia_provisioner_docker.sh
# -> laelia/provisioner:latest（推送/传输到集群的 registry）
```

或构建 linux/amd64 原生二进制（用于非容器安装）：

```bash
GOOS=linux GOARCH=amd64 scripts/build_laelia_provisioner.sh   # -> build/laelia-provisioner
```

应用清单（CRD 由集群管理员一次性应用；其余均为 namespace 级）：

```bash
cd backend/provisioner/backend/kubernetes/deploy
kubectl apply -f laelia.sh_laeliamachines.yaml   # CRD（集群级，仅一次）
kubectl apply -f rbac.yaml                       # SA + Role + RoleBinding
kubectl apply -f deployment.yaml                 # namespace/secret/config/deployment
```

`deployment.yaml` 中包含 `laelia-provisioner-token` Secret——请把 8.1 步的一次性 token 粘贴进去（`stringData.token`）；配置文件通过 `LAELIA_PROVISIONER_TOKEN` 环境变量读取 token，因此密钥不会落入受版本控制的文件。若以裸进程代替 Deployment 运行：

```bash
KUBECONFIG=/path/to/kubeconfig ./build/laelia-provisioner run \
  --config /etc/laelia-provisioner/provisioner.yaml
```

#### 使用 Helm 安装 provisioner（推荐）

提供 Helm chart（`charts/provisioner`）封装上面的清单：其 `crds/` 目录携带 CRD，Helm 会在渲染 chart 模板之前应用 CRD。RBAC、ConfigMap（由 `values.yaml` 渲染）、Secret 与 Deployment 全部部署到 release namespace——chart 不创建 namespace，请用 `-n` 指定（可配合 `--create-namespace`）：

```bash
cd /path/to/repo
helm install laelia-provisioner charts/provisioner \
  --namespace laelia-machines --create-namespace \
  --set token=llprov_... \
  --set managerUrl=https://laelia.example.com
```

常用 values（详见 `charts/provisioner/values.yaml`）：

| Value | 用途 |
| --- | --- |
| `token` | Settings → Provisioners 中的一次性 token（必填；以 `LAELIA_PROVISIONER_TOKEN` 注入）。 |
| `managerUrl` | provisioner 连接的 Manager 地址。 |
| `namespace` | 机器工作负载落地的 namespace；缺省为 release namespace。 |
| `managerUrlOverride` | 供机器 pod 连接的集群内 Manager 服务地址（集群出口受限时）。 |
| `retainData`/`autoUpgrade`/`storage`/`resources`/`extraEnv` | 透传的 provisioner 配置项。 |

这些 values 会渲染进 ConfigMap 中的 `provisioner.yaml`（`managerUrl`→`manager_url`、`namespace`→`namespace`、`managerUrlOverride`→`manager_url_override`、`retainData`→`retain_data`、`autoUpgrade`→`auto_upgrade`、`storage.*`→`storage.*`、`resources.*`→`resources.*`、`extraEnv`→`extra_env`；`backend` 固定为 `kubernetes`）。token **不会**写入配置文件——运行时通过 `LAELIA_PROVISIONER_TOKEN` Secret 读取。未设置的可选 value（如空的 `managerUrlOverride`）不会出现在生成的 YAML 中，对应原来 `deployment.yaml` 里被注释掉的可选 knobs。

可通过命令行 `--set` 设置，或（推荐）对两个必填字段之外的配置使用独立的 values 文件：

```bash
# --set 形式
helm install laelia-provisioner charts/provisioner -n laelia-machines \
  --set token=llprov_... \
  --set managerUrl=https://laelia.example.com \
  --set managerUrlOverride=http://laelia-manager.laelia-machines.svc:8181 \
  --set retainData=true \
  --set 'storage.size=20Gi' \
  --set 'extraEnv.LAELIA_INSECURE=true'

# values 文件形式（推荐）：字段名与之上的 value 名一一对应
helm install laelia-provisioner charts/provisioner -n laelia-machines \
  -f my-provisioner-values.yaml
```

```yaml
# my-provisioner-values.yaml
token: llprov_...
managerUrl: https://laelia.example.com
namespace: laelia-machines          # 缺省为 release namespace
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

这些 value 仅在安装/升级时读取。修改后请运行 `helm upgrade laelia-provisioner charts/provisioner -n laelia-machines -f my-provisioner-values.yaml`（若 ConfigMap 值已变而 pod 未被重新调度，可加 `--recreate-pods`）让 Deployment 拿到新的 ConfigMap。

`helm uninstall` 会删除 namespaced 资源，但由于 CRD 位于 `crds/`，它**不会**删除集群级的 CRD，需手动清理：

```bash
helm uninstall laelia-provisioner --namespace laelia-machines
kubectl delete crd laeliamachines.laelia.sh   # Helm 不管理 CRD 生命周期
```

### 8.3 provisioner 配置参考

```yaml
manager_url: https://laelia.example.com   # 或 --manager 参数
token: llprov_...                          # 一次性 token；--token 参数或 LAELIA_PROVISIONER_TOKEN 环境变量
backend: kubernetes                        # 工作负载后端（当前为 kubernetes）
namespace: laelia-machines                 # 机器工作负载落地的 namespace
# 可选项：
manager_url_override: http://laelia-manager.laelia-machines.svc:8181
                                           # pod 改用该地址连接 Manager（集群出口受限时）
retain_data: false                         # 删除时保留机器数据 PVC（StatefulSet Retain）
auto_upgrade: false                        # Manager 自动为该 provisioner 的机器触发升级
storage: { size: 10Gi, storage_class: "" } # 每台机器的 PVC 大小/存储类（storage_class 缺省为集群默认）
resources:
  requests: { cpu: "1", memory: "2Gi" }
  limits: { memory: "4Gi" }
extra_env:                                 # 透传到机器容器的环境变量
  LAELIA_INSECURE: "true"                  # Manager 使用自签名 https 证书时
```

`manager_url` 为纯 HTTP 时需要 `--allow-http`（仅开发用）。

### 8.4 RBAC 矩阵

provisioner 的 Role 为 namespace 级；无需 cluster-admin，也没有任何集群范围的 list/watch（设计 §12）。CRD 本身由集群管理员一次性应用。

| 资源 | 动词 | 用途 |
|---|---|---|
| `laelia.sh/laeliamachines`（含 `/status`、`/finalizers`） | get/list/watch/create/update/patch/delete | 每台机器一个 CR；CR 即工作负载的期望状态 |
| `secrets` | get/list/watch/create/update/patch/delete | 引导 Secret（`machine.json` + 引导脚本） |
| `services` | get/list/watch/create/update/patch/delete | StatefulSet 所需的 headless Service |
| `apps/statefulsets` | get/list/watch/create/update/patch/delete | 机器的单副本工作负载 |
| `pods` | get/list/watch | Pod 状态驱动 CR 阶段 |
| `persistentvolumeclaims` | get/list/watch/delete | 删除时显式清理 PVC（保留策略兜底） |
| `events` | create/patch | `kubectl describe` 诊断信息 |

### 8.5 运行时镜像契约

运行时镜像提供 agent 运行环境；它**不得**包含 laelia machine 二进制——pod 启动时从 Manager 把二进制下载到 PVC，之后的升级在原地完成。参考镜像：`scripts/docker/Dockerfile.machine-runtime`（node、python、build-essential、git、curl、jq、ripgrep、codex CLI；非 root uid 1001）。任何满足以下契约的镜像均可使用：

- POSIX `sh`、`curl`、`gzip`、`sha256sum`（init 容器的引导脚本）
- entrypoint 以 exec 方式启动 `$LAELIA_MACHINE_BIN`（默认 `/data/bin/laelia-machine`），并设置 `LAELIA_HOME=/data/laelia`，正确处理 `LAELIA_MANAGER_URL`（`http://` 自动追加 `--allow-http`）、`LAELIA_PROVISIONED=true` → `--provisioned --no-browser --foreground`，以及 `CODEX_HOME`（默认 `/data/laelia/codex`，位于 PVC 上）
- 以非 root uid 运行

### 8.6 provisioner 为每台机器创建的对象

全部位于所配置的 namespace 中，属主为 `LaeliaMachine` CR `laelia-machine-<machine-uuid-prefix>`：引导 Secret（机器的凭据——CR 上绝不携带 token）、headless Service、带 `volumeClaimTemplates: [data]` 和 amd64 nodeSelector 的单副本 StatefulSet，以及数据 PVC。在 UI 中删除机器会删除 CR；finalizer 会移除 Secret，并（除非 `retain_data: true`）删除数据 PVC。`kubectl get laeliamachines -n laelia-machines` 是集群管理员的机队视图。

## 故障排查

- `bind: address already in use` — 主机上的 8181 端口已被占用。请停止冲突进程或映射不同的主机端口（`-p 8080:8181`）。
- Manager 日志显示 `must set PG_URL environment variable` — `LAELIA_PG_URL` 缺失或为空；请通过 `-e` 传入。
- 数据库连接或迁移错误 — 请检查 URI、数据库编码，以及用户是否可以创建表和 `pg_trgm` 扩展（第 2 节）。
- Machine 无法连接 — 请检查 `--manager` URL 是否可达，以及 HTTP/2 是否在代理中保留；对于自签名证书，请使用 `--insecure`（仅开发用）。如果本地 machine 状态丢失，请重新运行 `laelia-machine --manager <url> setup` 重新认证。
- Web UI 在 Nginx 后面命令输出卡住 — 请使用 `proxy_buffering off` 以及较长的 `proxy_read_timeout`/`proxy_send_timeout`。
- 502 Bad Gateway — `proxy_pass` 必须指向实际的 Manager（`127.0.0.1:8181` 或容器名），而不是公共域名，否则代理会循环。

Provisioner 相关：

- 创建机器时报 "runtime image not configured" — 请在 设置 → 通用 中配置机器运行时镜像（第 8.1 节）。
- 新机器一直停留在 *等待中（Pending）* 阶段 — provisioner 离线或未注册。provisioner 连接后任务会自动重放；请检查 provisioner 日志以及 设置 → Provisioners 中它的状态列。
- 机器 pod 卡在 `ImagePullBackOff` / `ErrImagePull` — 设置 → 通用 中的运行时镜像在节点上不可拉取（私有 registry 凭据、镜像标签或架构不对）。修正镜像或拉取凭据后删除并重建机器。
- 机器 pod 卡在 `Creating` — 通常是 PVC 一直 Pending：所配置的 `storage.storage_class` 不存在或集群没有默认 StorageClass（`kubectl get pvc -n laelia-machines`）。在 provisioner 配置中设置 `storage.storage_class` 并重建机器。
- 机器显示 *已创建（Provisioned）* 但一直离线 — 工作负载已存在但 pod 未接入（引导下载失败？）。请查看 pod init 容器日志，并确认 pod 能访问 `manager_url`（或 `manager_url_override`）。
- 已删除的机器留下数据 PVC — provisioner 配置了 `retain_data: true`，PVC 是有意保留的。不再需要时请手动清理。
