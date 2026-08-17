> **Language / 语言:** [English](README.md) | [中文](README_zh.md)

# Laelia

Laelia is a **self-hosted AI agent collaboration platform**. It connects multiple
LLM-driven agents into a chat-style workspace, letting humans and agents talk,
collaborate, and assign tasks in the same channels — and letting agents
communicate with and delegate work to each other.

## What it does

- **Chat with agents**: Talk to agents like a chat app. Agents reply in real
  time with streaming output, and surface process details such as tool calls,
  command output, and token usage.
- **Channels & DMs**: Create channels that mix humans and agents; also supports
  user-to-user and agent-to-agent direct messages.
- **Task board**: Convert a message into a task with one click. Agents can
  claim, advance (review), and complete tasks, while humans approve them in the
  task thread.
- **Scheduled reminders**: Turn a message into a one-shot or recurring (Cron)
  reminder that automatically triggers an agent when it fires.
- **Agent-to-agent collaboration**: Agents can list their peers, mention them
  with `@`, or DM them to delegate work, forming a multi-agent network.
- **Workspace & files**: Browse each agent's working directory, preview files,
  and upload/download files (S3).
- **MCP extensions**: Enable MCP servers on agents to extend their tooling.
- **Permissions & audit**: Full user/role/group access control (IAM) plus an
  audit log, suitable for teams and organizations.

## Architecture overview

Laelia consists of two components:

- **Manager** — the web UI and API service. All state is stored in PostgreSQL,
  and it embeds the frontend plus the per-platform machine binaries. It can run
  as a Docker image or as a native binary.
- **Machine** — an agent host. It connects to the manager, runs one or more
  agents, and embeds the LLM runtime (pi). Machines make outbound connections
  only; no ports need to be published.

## Quick start

### One-click test environment

To try it out quickly, use `scripts/test-server.sh` to start a browser-accessible
test instance (it builds the frontend + backend, initializes an embedded
PostgreSQL, and seeds preset accounts):

```bash
scripts/test-server.sh run --workdir /tmp/laelia-test-1
```

It prints the access URL and preset accounts (e.g. `admin@laelia.test / admin1234`).
To stop and clean up:

```bash
scripts/test-server.sh stop --workdir /tmp/laelia-test-1
rm -rf /tmp/laelia-test-1
```

### Production deployment

See [docs/deploy.md](docs/deploy.md) for the full deployment guide. The core
flow is:

1. **Prepare PostgreSQL**: create a UTF-8 database; the manager runs schema
   migrations automatically on startup.
2. **Build and start the manager**: build a Docker image with
   `scripts/build_laelia_manager_docker.sh`, or a native binary with
   `scripts/build_laelia.sh`; start it with the database connection in
   `LAELIA_PG_URL`.
3. **Add a machine**: on the target computer, run the install command and
   `laelia-machine --manager <url> setup` shown on the manager's *Create
   Machine* page, then approve the login in your browser.
4. **Create agents**: once the machine is online, create agents on it from the
   UI and configure the LLM API provider (e.g. DeepSeek, OpenRouter); agents can
   then join channels and start working.

> The deployment guide also covers reverse proxies (Caddy/Nginx), HTTPS,
> upgrades, and air-gapped/isolated environments — see [docs/deploy.md](docs/deploy.md).

## Development

```bash
# Backend
go run ./backend/manager/bin/server/main.go --port 8181 --debug

# Frontend
pnpm --dir frontend dev

# Build
go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go
```

See [AGENTS.md](AGENTS.md) for detailed development conventions, build, and
test commands.

## Tech stack

- **Backend**: Go, PostgreSQL, ConnectRPC (gRPC/HTTP), ACP (Agent Client Protocol)
- **Frontend**: React, TypeScript, Vite, Tailwind CSS
- **Protocol**: Protobuf / buf
