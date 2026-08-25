# 一键启动测试服务（test-server）

自动化脚本：编译前端 + 用 `-tags embed_frontend` 编译后端，初始化嵌入式 PostgreSQL 并写入预设测试数据，然后启动一个可被浏览器访问、可分享给其他用户/Agent 的测试服务。所有运行时状态都在 `--workdir` 内，删除该目录即完全清理；多实例可同时运行互不干扰。

## 快速开始

```bash
# 一键启动（首次会自动构建前端+后端，并下载嵌入式 PG 二进制）
scripts/test-server.sh run --workdir /tmp/laelia-test-1

# 输出示例
#   page:    http://127.0.0.1:32643
#   lan:     http://192.168.1.20:32643
#   admin:   admin@laelia.test / admin1234
#   user:    alice@laelia.test / alice1234
#   user:    bob@laelia.test / bob1234
#   stop:    bash /tmp/laelia-test-1/stop.sh
#   delete:  rm -rf /tmp/laelia-test-1
```

浏览器打开 `http://127.0.0.1:<port>`，用 `admin@laelia.test / admin1234` 登录（管理员），或用 alice/bob 登录（普通用户）。

`run` 命令在启动完成后会**立即返回**，服务在后台持续运行，不会占据终端；构建过程只显示简略进度（如 `frontend build complete`、`backend build complete`），详细构建日志在 worktree 缓存的 `build.log` 中。

## 停止与清理

```bash
# 方式一：workdir 内的一键停止脚本
bash /tmp/laelia-test-1/stop.sh

# 方式二：launcher 子命令
scripts/test-server.sh stop --workdir /tmp/laelia-test-1

# 查看状态
scripts/test-server.sh status --workdir /tmp/laelia-test-1

# 完全清理（先 stop，再删除目录）
rm -rf /tmp/laelia-test-1
```

## 常用选项

| 选项 | 说明 |
| --- | --- |
| `--workdir <dir>` | 工作目录（必填），所有运行时状态（PG 数据、日志、PID）都在这里 |
| `--port <n>` | HTTP 端口，默认随机空闲端口 |
| `--pg-port <n>` | PostgreSQL 端口，默认随机空闲端口 |
| `--no-seed` | 跳过预设测试数据 |
| `--build` | 强制重新构建 laelia 二进制 |
| `--keep` | 兼容保留；现在 `run` 总是让实例在后台持续运行 |
| `--admin-email / --admin-password` | 覆盖预设管理员账号 |
| `--cache <dir>` | 共享缓存目录（默认 `LAELIA_TEST_CACHE` 或 `~/.cache/laelia-test`） |

## 架构

- **构建**：`scripts/build_test_server.sh` 只构建 manager（前端内嵌），产物进入**按 worktree 隔离的缓存**（默认 `~/.cache/laelia-test/worktrees/<worktree-id>/`），用 flock 串行化并发构建 + 基于源码指纹的 stamp 跳过重复构建。
- **启动器**：`tools/testserver/`（独立 Go module，`replace` 指向主模块），负责嵌入式 PG、服务拉起、就绪轮询、种子写入、优雅停机。
- **数据库**：嵌入式 PostgreSQL（`github.com/fergusstrange/embedded-postgres`），数据目录在 `workdir/pgdata`，二进制下载到共享缓存 `<cache>/pg`。PG 密码持久化在 `workdir/pgpassword`，重启同一 workdir 时复用同一密码，避免因数据目录复用导致认证失败。
- **种子数据**：复用 `store` 包创建 admin/alice/bob 三个用户，并把 admin 绑定为 `workspaceAdmin`。

## 并发与隔离

- 每个实例独立 `workdir`、独立 PG 实例（独立数据目录 + 独立端口）、独立 HTTP 端口。
- 构建产物按 git worktree 隔离：每个 worktree 有独立的 `laelia` 二进制和 stamp，多个 agent 在多个 worktree 中启动互不覆盖。
- 构建 stamp 包含当前 worktree 的源码指纹（HEAD、已暂存/未暂存 diff、未跟踪文件），因此**当前 worktree 启动的服务一定由当前最新代码构建**；未提交的改动也会触发重新构建。
- 删除 `workdir` 即清理全部运行时状态。

## 注意事项

- 服务监听所有网卡（`0.0.0.0`），局域网内其他机器可通过 `http://<本机IP>:<port>` 访问；如需仅本机访问，请配合防火墙。
- 首次运行会从 Maven Central 下载 PG 16 二进制（约 50MB）到共享缓存；离线环境可用 `LAELIA_TEST_PG_BIN` 指向本地 PG 二进制（预留，未实现）。
- 预设密码为测试用途，生产环境请勿使用。
