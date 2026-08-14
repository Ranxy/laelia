# 一键启动测试服务脚本设计方案

> 目标：提供一个自动化脚本，编译前端 + 用 `-tags embed_frontend` 编译后端，初始化数据库并写入预设测试数据，然后启动一个可被浏览器访问、可分享给其他用户/Agent 的测试服务。脚本以 `workdir` 为工作目录，所有运行时状态（数据库、日志、PID）都放在该目录内，测试结束后删除该目录即可完全清理，且多个实例可同时运行互不干扰。

---

## 1. 现状梳理（基于代码）

| 事实 | 代码依据 |
| --- | --- |
| 后端是 Go 单体，入口 `backend/manager/bin/server/main.go`，用 cobra 解析 `--port`（默认 8181）、`--debug` 等 | `cmd/root.go` |
| 后端**必须**连接 PostgreSQL，通过环境变量 `LAELIA_PG_URL` 传入连接串；缺失则启动失败 | `cmd/profile.go`、`store/db_connection.go` |
| 后端启动时**自动执行 schema 迁移**（嵌入式 SQL 迁移），无需手动建表 | `server/server.go` 中 `migration.MigrateSchema` |
| 前端是 React/Vite，构建产物在 `frontend/dist`；用 `-tags embed_frontend` 编译时把 `backend/manager/server/dist` 内嵌进二进制 | `server/server_frontend_embed.go`、`scripts/build_laelia.sh` |
| 内嵌模式下前端 API 走**同源**（`VITE_API_BASE_URL` 生产为空 → 同源），所以一个端口同时服务页面和 API | `frontend/src/connect/index.ts` |
| 就绪探针：`GET /healthz` 返回 `OK`，仅在服务完全启动后可用 | `server/echo_routes.go` |
| 第一个 end-user 会被自动授予 `workspaceAdmin`（通过 workspace IAM policy） | `api/v1/user_service.go` 约 471 行 |
| 用户创建走 `store.CreateUser`（bcrypt 密码哈希 + `email_verified_at`），管理员绑定走 `store.PatchWorkspaceIamPolicy` | `store/principal.go`、`store/policy.go` |
| 现有 `scripts/build_laelia.sh` 同时构建 manager 和 machine（含 pi，较重）；测试服务只需要 manager | `scripts/build_laelia.sh` |
| `backend/manager/server/dist/` 已被 gitignore，构建不会污染 git | `.gitignore` |

---

## 2. 总体架构

两个组件：

1. **构建脚本** `scripts/build_test_server.sh` —— 只构建 manager（前端内嵌），产物放入**共享缓存目录**（默认 `~/.cache/laelia-test/`），带构建锁，支持并发安全。
2. **启动器** `scripts/test-server.sh`（薄壳）+ **Go 启动器** `tools/testserver/`（核心逻辑）—— 在指定 `workdir` 内启动嵌入式 PostgreSQL、拉起 laelia 服务、等待就绪、写入种子数据、打印访问 URL，并负责优雅停机。

关键设计决策：

- **数据库用嵌入式 PostgreSQL**（`github.com/fergusstrange/embedded-postgres`），数据目录放在 `workdir/pgdata`。这样 `workdir` 完全自包含，删除即清理，且每个实例有独立的 PG 实例和端口，天然互不干扰。
- **构建产物放共享缓存**而非每个 workdir 各建一份：前端构建 + Go 编译耗时且产物大（二进制约几十 MB），共享缓存避免重复构建；workdir 只放运行时状态，删除 workdir 不影响缓存复用。
- **随机端口**：HTTP 端口和 PG 端口都随机选取并校验空闲，避免冲突。

---

## 3. workdir 目录布局

`workdir` 由启动器创建，结构如下：

```
<workdir>/
├── pgdata/            # 嵌入式 PostgreSQL 数据目录（自包含，删除即清）
├── logs/
│   ├── server.log     # laelia 服务日志
│   ├── postgres.log   # PostgreSQL 日志
│   └── launcher.log   # 启动器自身日志
├── run/
│   ├── server.pid     # laelia 进程 PID
│   ├── postgres.pid   # PG 进程 PID
│   └── port           # 实际使用的 HTTP 端口（供脚本/Agent 读取）
├── info.txt           # 访问 URL、账号密码、停止方法（人类可读）
├── stop.sh            # 一键停止脚本（调用启动器 stop）
└── .meta.json         # 启动器元数据（端口、PG URL、PID、创建时间等）
```

删除 `workdir` 前建议先执行 `stop.sh`（或启动器在收到信号时自动清理）；即使直接 `rm -rf`，残留的孤儿进程也可通过 `run/*.pid` 定位清理（见 §9 兜底）。

---

## 4. 构建脚本 `scripts/build_test_server.sh`

只构建 manager（前端内嵌），产物进共享缓存。

```bash
#!/usr/bin/env bash
# 用法: scripts/build_test_server.sh [--force]
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
. ./scripts/build_init.sh

CACHE_DIR="${LAELIA_TEST_CACHE:-$HOME/.cache/laelia-test}"
BIN="$CACHE_DIR/laelia"
STAMP="$CACHE_DIR/build.stamp"
mkdir -p "$CACHE_DIR"

# 构建锁：并发构建时只有一个真正构建，其余等待后复用
exec 9>"$CACHE_DIR/.build.lock"
flock 9

# 若产物已存在且未 --force，且 stamp 与当前 git HEAD 一致，则跳过
if [[ -f "$BIN" && -f "$STAMP" && "$(cat "$STAMP")" == "$GIT_COMMIT" && "${1:-}" != "--force" ]]; then
  echo "laelia 已构建 ($GIT_COMMIT)，跳过。"
  exit 0
fi

echo "构建前端..."
rm -rf backend/manager/server/dist
pnpm --dir frontend i --frozen-lockfile
pnpm --dir frontend build
cp -r frontend/dist backend/manager/server/dist

echo "构建 manager (embed_frontend)..."
CGO_ENABLED=0 go build -tags embed_frontend -ldflags "-w -s" -p=16 -o "$BIN" ./backend/manager/bin/server/main.go

echo "$GIT_COMMIT" > "$STAMP"
echo "构建完成: $BIN"
```

要点：
- **构建锁（flock）** 保证多人/多 Agent 同时触发构建时只有一个真正构建，其余等待后复用，避免写 `backend/manager/server/dist` 和缓存目录的竞态。
- **stamp 校验**：产物与当前 git HEAD 一致则跳过构建，加速重复启动。
- 缓存目录可用 `LAELIA_TEST_CACHE` 覆盖（例如 CI 或共享机器上放到公共位置）。

---

## 5. Go 启动器 `tools/testserver/`

独立 Go module（`replace` 指向主模块），这样不污染主 `go.mod`，且能复用 `backend/manager/store`、`backend/manager/migration` 做种子写入。

### 5.1 命令行接口

```
testserver run --workdir <dir> [--port <n>] [--pg-port <n>] [--host 127.0.0.1]
               [--no-seed] [--build] [--keep] [--admin-email ...] [--admin-password ...]
testserver stop --workdir <dir>
testserver status --workdir <dir>
```

- `--workdir`（必填）：工作目录，绝对路径。
- `--port`：HTTP 端口，默认随机（20000–40000 区间内选空闲）。
- `--pg-port`：PG 端口，默认随机。
- `--host`：绑定地址，默认 `127.0.0.1`（安全）；分享给局域网其他用户/Agent 时用 `--host 0.0.0.0`。
- `--no-seed`：跳过种子数据。
- `--build`：启动前强制重新构建（否则用缓存产物）。
- `--keep`：退出时不自动清理（保留 PG 数据便于排查）。
- `--admin-email/--admin-password`：覆盖预设管理员账号。

### 5.2 启动流程（`run`）

1. **解析并创建 workdir**：`mkdir -p` 各子目录，写 `.meta.json`。
2. **确保二进制**：若 `--build` 或缓存无产物，调用 `scripts/build_test_server.sh`。
3. **选端口**：随机选 HTTP 端口和 PG 端口，用 `net.Listen` 探测空闲，冲突则重选。
4. **启动嵌入式 PostgreSQL**：
   - 用 `embedded-postgres` 在 `workdir/pgdata` 初始化并启动，监听 `127.0.0.1:<pg-port>`。
   - 首次运行会下载 PG 二进制（约 50MB）到 `~/.cache/laelia-test/pg`，之后复用；可用 `LAELIA_TEST_PG_BIN` 指向本地 PG 二进制以离线/加速。
   - 创建数据库 `laelia` 和用户 `laelia`（密码随机，写入 `.meta.json`）。
5. **启动 laelia 服务**：
   ```
   LAELIA_PG_URL=postgresql://laelia:<pw>@127.0.0.1:<pg-port>/laelia \
     <cache>/laelia --port <http-port> --debug \
     >> <workdir>/logs/server.log 2>&1 &
   ```
   记录 PID 到 `run/server.pid`。
6. **等待就绪**：轮询 `http://127.0.0.1:<http-port>/healthz`，超时（默认 60s）则报错并清理。服务启动时自动完成 schema 迁移。
7. **写入种子数据**（除非 `--no-seed`，见 §6）。
8. **写 `info.txt` 和 `stop.sh`**，打印访问 URL 与账号。
9. **进入等待**：监听 SIGINT/SIGTERM，收到后执行优雅停机（停 laelia → 停 PG → 写日志）。`--keep` 时跳过停机。

### 5.3 停机流程（`stop` / 信号）

1. 读 `.meta.json` 拿到 PID 与 PG 端口。
2. 向 laelia 进程发 SIGTERM（服务自身有优雅停机逻辑，见 `server.go` 的 `Shutdown`）。
3. 等待进程退出（超时则 SIGKILL）。
4. 停掉嵌入式 PG（`embedded-postgres` 的 `Stop`）。
5. 更新 `.meta.json` 状态为 `stopped`。

---

## 6. 种子数据（`--seed`）

种子逻辑放在 Go 启动器内，**复用 store 包**，与 API 走完全相同的代码路径，保证一致性：

1. 连接 `postgresql://laelia:<pw>@127.0.0.1:<pg-port>/laelia`（迁移已由服务启动完成）。
2. 用 `store.CreateUser` 创建预设用户（bcrypt 密码哈希 + `EmailVerifiedAt` 置为当前时间，避免登录被"未验证邮箱"拦截）：
   - 管理员：`admin@laelia.test` / `admin1234`
   - 普通用户：`alice@laelia.test` / `alice1234`、`bob@laelia.test` / `bob1234`
   - 密码可用 `--admin-password` 等覆盖；账号密码写入 `info.txt`。
3. 用 `store.PatchWorkspaceIamPolicy` 把管理员绑定为 `workspaceAdmin`（`common.FormatRole(common.WorkspaceAdmin)`，member 用 `common.FormatUserHandle(handle)`）。
4. （可选，后续迭代）创建测试 Agent / 会话 / 群组等更丰富的演示数据。

> 说明：直接复用 store 而非走 HTTP API，是因为创建用户/绑定角色需要管理员鉴权，走 API 要先登录拿 token，链路更长且依赖邮件验证配置；直接写库最稳。

---

## 7. 并发与隔离保证

| 需求 | 方案 |
| --- | --- |
| 多实例同时运行 | 每个实例独立 `workdir`、独立 PG 实例（独立数据目录 + 独立端口）、独立 HTTP 端口，无共享可变状态 |
| 端口冲突 | HTTP/PG 端口随机 + 空闲探测 |
| 并发构建 | 构建脚本用 flock 串行化，产物进共享缓存，构建后只读复用 |
| 数据库互不干扰 | 嵌入式 PG 数据目录在各自 workdir 内，天然隔离 |
| 删除 workdir 即清理 | 所有运行时状态（PG 数据、日志、PID）都在 workdir 内 |

---

## 8. 分享访问 URL

- 本地浏览器：`http://127.0.0.1:<port>`。
- 局域网其他用户/Agent：`--host 0.0.0.0` 后打印 `http://<本机局域网IP>:<port>`（脚本用 `hostname -I` 探测）。
- 跨公网分享（可选扩展）：支持 `--tunnel` 调用 ngrok/cloudflared 生成公网 URL，写入 `info.txt`。v1 不做，留接口。

`info.txt` 内容示例：

```
Laelia 测试服务已启动
  页面:   http://127.0.0.1:38123
  局域网: http://192.168.1.20:38123
  管理员: admin@laelia.test / admin1234
  用户:   alice@laelia.test / alice1234
          bob@laelia.test / bob1234
停止:    bash <workdir>/stop.sh
删除:    rm -rf <workdir>   (建议先 stop)
```

---

## 9. 失败处理与兜底

- **启动失败**：任一环节失败即回滚——停掉已启动的 PG/服务，删除 workdir 内已创建内容（除非 `--keep`），返回非零退出码并打印日志路径。
- **孤儿进程兜底**：即使 `rm -rf` 前未 stop，`run/*.pid` 记录了 PID；提供 `testserver stop --workdir <dir>` 在目录被删前也能按 PID 清理。另提供 `testserver cleanup --stale` 扫描 `~/.cache/laelia-test/instances` 清理超时未用的实例（可选）。
- **PG 二进制下载失败**：支持 `LAELIA_TEST_PG_BIN` 指向本地 PG 二进制，或回退到共享 PG 后端（见 §11 备选）。

---

## 10. 实施步骤

1. **构建脚本**：新增 `scripts/build_test_server.sh`（§4）。
2. **Go 启动器**：新增 `tools/testserver/`（独立 module + replace），实现 `run/stop/status`、嵌入式 PG、服务拉起、就绪轮询、种子写入（§5、§6）。
3. **薄壳脚本**：新增 `scripts/test-server.sh`，转发参数到 Go 启动器，并负责先构建（`--build` 或缓存缺失时）。
4. **种子数据**：在启动器内实现 §6 的 store 复用逻辑。
5. **测试**：本地跑通"一键启动 → 浏览器访问 → 登录预设账号 → stop → 删除 workdir 无残留"；再并发启动 2–3 个实例验证互不干扰。
6. **文档**：在 `docs/` 补充使用说明（含分享 URL、清理、常见问题）。

---

## 11. 备选方案（供权衡）

### 备选 A：共享 PostgreSQL + 唯一数据库
- 不下载 PG 二进制，连一个已运行的 PG（如本机 5432），每个实例创建唯一数据库 `laelia_test_<随机>`，清理时 DROP。
- 优点：轻量、无需下载；缺点：依赖外部 PG，数据库不在 workdir 内（但 DROP 后同样无残留），且需保证共享 PG 有足够连接/权限。
- 适合：已有稳定 PG 的环境。可作为 `--pg-mode shared` 后端加入。

### 备选 B：Docker 起 PG
- 用 `docker run postgres` 起独立容器，数据卷挂到 workdir。
- 优点：无需下载二进制；缺点：依赖 Docker，且容器/卷清理比嵌入式稍繁琐。

**推荐**：v1 用嵌入式 PG（最自包含、最贴合"删除 workdir 即清理"），预留 `--pg-mode` 抽象以便后续加共享/Docker 后端。


---

## 12. 实现状态（已落地）

本方案已实现并通过本地端到端验证：

- **构建脚本** `scripts/build_test_server.sh`：只构建 manager（前端内嵌），产物进共享缓存，flock 串行化 + git stamp 跳过重复构建。
- **Go 启动器** `tools/testserver/`（独立 module + replace）：实现 `run/stop/status`、嵌入式 PG、服务拉起、就绪轮询、种子写入、优雅停机。
- **薄壳脚本** `scripts/test-server.sh`：转发参数并传入 `--repo`。
- **使用文档** `docs/test-server.md`。

实现中的关键修正（相对 §5 设计）：

1. **runtimePath 必须在 dataPath 之外**：embedded-postgres 的 `Start()` 会 `os.RemoveAll(dataPath)`，若 runtimePath 在 dataPath 内会被一并删除导致 initdb 失败。实际 `runtimePath=workdir/runtime`、`dataPath=workdir/pgdata`。
2. **stop 用 pg_ctl 直接停 PG**：embedded-postgres 的 `Stop()` 要求是启动它的同一实例；跨进程 stop 时新建实例会返回 `ErrServerNotStarted` 而不做任何事。实际用 `<cache>/pg/binaries/bin/pg_ctl stop -D <dataPath>`。
3. **服务监听所有网卡**：laelia 服务 `--port` 绑定 `:port`（所有接口），`--host` 仅影响展示；启动器始终打印 localhost 与局域网两个 URL。

验证结果：一键启动 → 浏览器访问（healthz OK、前端正常）→ 种子用户（admin/alice/bob + workspaceAdmin 绑定）→ 并发两实例互不干扰（独立端口/数据）→ stop 后服务与 PG 均停止 → 删除 workdir 无残留进程。
