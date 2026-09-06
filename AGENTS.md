# AGENTS.md

This file provides guidance to AI coding assistants (Claude Code, Codex, Copilot) when working with code in this repository. `CLAUDE.md` is a symlink to this file — edit this one, never the symlink.

## What Laelia Is

Laelia is a self-hosted AI agent collaboration platform: humans and LLM-driven agents talk and collaborate in channels and DMs, turn messages into tasks and scheduled reminders, and delegate work to each other. A deployment consists of:

- **Manager** — the web UI + API service. All state lives in PostgreSQL; the binary embeds the built frontend and per-platform machine binaries.
- **Machine** — an agent host. It runs one or more agents, embeds the LLM runtime (pi), and makes **outbound-only** connections to the manager; no ports need to be published.
- **Provisioner** (optional) — provisions and deprovisions machines on Docker or Kubernetes backends.

Feature design documents live in `docs/plan/`, Read the matching design doc before reworking one of these subsystems, and add one there for new subsystem-scale features.

## Project Architecture

### Services

| Path | Owns |
| --- | --- |
| `backend/manager/` | Manager service: ConnectRPC API (`api/v1`), Postgres store (`store/`), background components (`component/`: dispatcher, roomhub, scheduler, iam, mailer, presence, webpush, mcp, provision, s3client, machinebuild, device), config, migrations (`migration/`), IdP plugins (`plugin/idp`), embedded frontend (`server/dist/`) and machine binaries (`server/embedded_machine/`) |
| `backend/agent/` | Machine-side daemon: ACP executor (`executor/` — the stdio `acp` path and the `acp2` v2 thread path), embedded pi runtime (`pi/embedded/`), providers, supervisor, workspace, chattools |
| `backend/provisioner/` | Provisioner binary and its Docker/Kubernetes backends |
| `backend/common/` | Shared error codes (`common.Errorf`/`Wrap`/`Wrapf` with a `Code`), CEL, resource names, logging, permissions |
| `backend/generated-go/` | Generated protobuf + Connect code (`v1`, `v1connect`, `store`) — never hand-edit |

The frontend calls the manager over ConnectRPC (`@connectrpc/connect-web`); transports and auth interceptors live in `frontend/src/connect/`.

### Protocol

- `proto/v1/` defines the public ConnectRPC services; `proto/store/` defines the row shapes stored in database JSONB columns.
- `cd proto && buf generate` regenerates Go code into `backend/generated-go/`, frontend types into `frontend/src/types/proto-es/` (only `laelia.v1`), and API docs into `proto/gen/grpc-doc/`. Generated output is committed but never hand-edited.

### Database Schema and Migrations

- `backend/manager/migration/migration/LATEST.sql` is the cumulative schema applied to fresh installs.
- Incremental migrations live in `backend/manager/migration/migration/{MAJOR.MINOR}/{NNNN}##{desc}.sql` and run automatically on server startup (forward-only, semver-ordered, advisory-locked so only one HA replica migrates).
- **Dual-maintenance rule**: every schema change must BOTH append idempotent DDL to `LATEST.sql` AND add an incremental file under the current `{MAJOR.MINOR}/` directory. Existing deployments never re-run `LATEST.sql`; they only execute the incremental file.
- Batched data reshapes that are awkward in pure SQL go in the `goMigrations` registry (`migrator.go`); a Go migration runs before the SQL migration of the same version and retries together with it on failure.
- Schema-invariant guards (required tables, indexes, constraints) live in `backend/manager/migration/migration_test.go`; extend them when a migration introduces an invariant worth locking.

### Store Layer

- `backend/manager/store/` maps database tables to Go. JSONB columns hold `protojson.Marshal` output of the `proto/store` message named in the column's SQL comment.
- Store unit tests are hermetic — they need no live database. Many lock query shape with string guards on the SQL (see `conversation_test.go` for the pattern). When a query's shape is itself the invariant — race-free `ON CONFLICT` clauses, index-hinted ordering, scoping predicates — add a guard test in the same package.

## Testing

The default Go suite is hermetic: `go test ./...` needs no PostgreSQL and no external service. Infrastructure-dependent suites are gated by env vars and skip when unset:

| Gate | Scope | Requires |
| --- | --- | --- |
| `LAELIA_RUN_MIGRATION_TESTS=1` + `LAELIA_TEST_PG_URL=<url>` | `backend/manager/migration` — migrator end-to-end (fresh install, upgrade, rollback) | Postgres URL whose user can `CREATEDB` |
| `LAELIA_RUN_PROVISIONER_TESTS=1` + `LAELIA_TEST_PG_URL=<url>` | `backend/manager/api/v1` — provisioner control-plane flow against a real manager stack (auth + IAM interceptors, real Postgres, real dispatcher) | Postgres URL whose user can `CREATEDB` |
| `LAELIA_RUN_OPENCODE_ACP_TESTS=1` | `backend/agent/executor` — real local `opencode acp` execution | local `opencode` CLI |
| `LAELIA_RUN_CODEX_ACP_TESTS=1` + `CODEX_HOME=<home>` | `backend/agent/executor` — `TestThreadExecutorCodex` against real local `codex` (app-server) | Writable codex home (`config.toml` + auth `models.json`); the test copies it into a hermetic temp home, the real one is never touched |

Run the matching gate when your change touches that path: migrations → migration gate; provisioner job flow → provisioner gate; ACP stdio/runtime integration → opencode gate; acp2/codex/thread executor → codex gate.

Frontend tests are Vitest, colocated with source as `*.test.ts(x)`; run `pnpm --dir frontend test`.

For manual end-to-end testing use the one-click test server (`scripts/test-server.sh`, below) instead of hand-rolling a database.

## Development Workflow

**ALWAYS follow these steps after making code changes:**

### Go Code Changes

1. **Format**: Run `gofmt -w` on modified files
2. **Lint**: Run `golangci-lint run --allow-parallel-runners` to catch issues
   - **Important**: Run golangci-lint repeatedly until there are no issues. The linter has a max-issues limit and may not show all issues in a single run.
3. **Auto-fix**: Use `golangci-lint run --fix --allow-parallel-runners` to fix issues automatically
4. **Test**: Run relevant tests before committing, plus the env-gated suites above when your change touches their path (see Testing)
5. **Build**: `go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`
6. **Tidy**: After changing Go dependencies, run `go mod tidy` to clean up `go.mod` and `go.sum`

### Frontend Code Changes

1. **Format + fix** — Run `pnpm --dir frontend biome:check` (Biome: format, lint, organize imports over `src/`) or `cd frontend && pnpm biome check --write <path>` for specific files. There is no ESLint in this repo; Biome is the only linter.
2. **Scanners** — Run `pnpm --dir frontend check`: store-write surface, React overlay layering, i18n completeness, and locale key order. These are conservative textual guardrails, not static analysis — fix the findings, don't bypass them.
3. **Type check** — Run `pnpm --dir frontend type-check`
4. **Test** — Run `pnpm --dir frontend test`

### Proto Changes

1. **Format**: Run `buf format -w proto`
2. **Lint**: Run `buf lint proto`
3. **Generate**: Run `cd proto && buf generate`
4. Commit the regenerated `backend/generated-go/` and `frontend/src/types/proto-es/` output together with the proto change

## Build/Test Commands

### Backend

```bash
# Build
go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go

# Start manager backend (default port 8181 matches the frontend vite proxy)
go run ./backend/manager/bin/server/main.go --port 8181 --debug

# Run single test
go test -v -count=1 github.com/Ranxy/laelia/backend/manager/path/to/tests -run ^TestFunctionName$

# Run multiple tests
go test -v -count=1 github.com/Ranxy/laelia/backend/manager/path/to/tests -run ^(TestFunctionName|TestFunctionNameTwo)$

# Env-gated integration suites (see Testing for preconditions)
LAELIA_RUN_MIGRATION_TESTS=1 LAELIA_TEST_PG_URL=postgresql://dev:dev@localhost/laelia go test ./backend/manager/migration -count=1
LAELIA_RUN_PROVISIONER_TESTS=1 LAELIA_TEST_PG_URL=postgresql://dev:dev@localhost/laelia go test ./backend/manager/api/v1 -count=1
LAELIA_RUN_OPENCODE_ACP_TESTS=1 go test ./backend/agent/executor -count=1
LAELIA_RUN_CODEX_ACP_TESTS=1 CODEX_HOME=/path/to/codex-home go test ./backend/agent/executor -run TestThreadExecutorCodex -count=1

# Lint
golangci-lint run --allow-parallel-runners
```

### Frontend

```bash
# Install dependencies
pnpm --dir frontend i

# Dev server (proxies API calls to localhost:8181)
pnpm --dir frontend dev

# Format + lint + organize imports (Biome over src/)
pnpm --dir frontend biome:check

# Lint only (Biome; no ESLint in this repo)
pnpm --dir frontend lint

# Policy scanners: store writes, React layering, i18n, key sort
pnpm --dir frontend check

# Sort locale keys after editing locale files
pnpm --dir frontend sort:i18n

# Type check
pnpm --dir frontend type-check

# Test
pnpm --dir frontend test
pnpm --dir frontend test:watch
pnpm --dir frontend test:coverage
```

### Proto

```bash
# Format
buf format -w proto

# Lint
buf lint proto

# Generate
cd proto && buf generate
```

### Build & Docker

```bash
# Local monolithic build: frontend + per-platform machine binaries -> embedded into manager
scripts/build_laelia.sh                             # outputs build/laelia + build/laelia-machine (dev mode)
RELEASE=true scripts/build_laelia.sh                # release-mode manager (adds the release build tag)
LAELIA_BUILD_PROXY=http://host:port scripts/build_laelia.sh  # route the pi GitHub download through a proxy

# Docker images (manager image embeds frontend + machine binaries; machine image embeds pi)
LAELIA_BUILD_PROXY=http://host:port scripts/build_laelia_manager_docker.sh  # -> laelia/manager:local (dev mode)
RELEASE=true LAELIA_BUILD_PROXY=http://host:port scripts/build_laelia_manager_docker.sh  # -> release mode
LAELIA_BUILD_PROXY=http://host:port scripts/build_laelia_machine_docker.sh  # -> laelia/machine:local

# Provisioner (image carries only the provisioner binary + CA certs; deploy manifests in
# backend/provisioner/backend/kubernetes/deploy/)
scripts/build_laelia_provisioner_docker.sh          # -> laelia-provisioner:local
scripts/build_laelia_provisioner.sh                 # bare-process binary -> build/laelia-provisioner
```

Notes:

- `scripts/build-pi.sh` downloads and checksum-verifies the standalone pi
  distribution (binary + runtime assets) into
  `backend/agent/pi/embedded/dist-<goos>-<goarch>` before any
  `go build -tags release`. It is idempotent (recorded version/platform in
  `pi.meta`); use `PI_FORCE=1` to re-download.
- Each `backend/agent/pi/embedded/dist-*/pi` is a tracked 0-byte placeholder.
  A release build replaces it with the real (large) binary; restore it with
  `git restore backend/agent/pi/embedded/dist-*/pi` before committing.
- `scripts/build_laelia.sh` cross-compiles linux-x64 / windows-x64 /
  darwin-arm64 machine binaries, gzips them, and embeds them into the manager
  (`backend/manager/server/embedded_machine/`, gitignored).
- The manager image needs `LAELIA_PG_URL`; the machine image needs
  `LAELIA_MANAGER_URL` and `LAELIA_TOKEN` (its entrypoint maps these env vars
  to CLI flags, adding `--allow-http` for `http://` URLs automatically).
- The machine image is an agent runtime: node/npm (base image) plus
  python3/pip, build-essential (make/gcc), git, curl, wget, jq, unzip, zip,
  ripgrep, and the codex CLI (`npm install -g @openai/codex`, version pinned
  via `CODEX_NPM_SPEC`) for the codex ACP v2 provider. Pass
  `APT_MIRROR=http://mirrors.aliyun.com/debian` (or your local Debian mirror)
  to speed up the apt steps in restricted networks.
- Codex login/config is never baked into the image: mount a writable CODEX_HOME
  volume (config.toml + auth/models.json) and point the machine entrypoint at
  it with `LAELIA_CODEX_HOME` (it exports CODEX_HOME for the daemon). Without
  it codex falls back to `~/.codex` under the container home.
- `LAELIA_BUILD_PROXY` is the single build proxy (pi download + docker Go
  stages). Do not use a global `HTTPS_PROXY` for docker builds: BuildKit
  auto-injects standard proxy args into every stage, including the final
  runtime images.

### Database

```bash
# Connect to Postgres
psql -h localhost -p 5432 -U dev -d laelia -c "sql"
```

### Test Server (one-click test environment)

To start a throwaway, browser-accessible laelia instance (manual testing, or
sharing a page with other users/agents), use `scripts/test-server.sh`. It
builds the frontend + backend (embedded), runs an isolated embedded PostgreSQL,
seeds preset users, and serves on a random port inside `--workdir`:

```bash
scripts/test-server.sh run --workdir /tmp/laelia-test-1
# ... prints the URL and preset accounts (admin@laelia.test / admin1234 etc.)
scripts/test-server.sh stop   --workdir /tmp/laelia-test-1
rm -rf /tmp/laelia-test-1   # run stop first; removes all instance state
```

Full usage, options, and caveats: see `docs/test-server.md`.

## Code Style

- **General**: Follow Google style guides for all languages
  - Go: https://google.github.io/styleguide/go/
- **Conciseness**: Write clean, minimal code; fewer lines is better. Prioritize simplicity for effective and maintainable software.
- **Comments**: Only include comments that are essential to understanding functionality or convey non-obvious information
- **Go**: Use standard Go error handling with detailed error messages
- **API and Proto**: Follow AIPs at https://google.aip.dev/general. When AIP and the proto guide conflict, AIP takes precedence. For example, use HELLO for enum names, not TYPE_HELLO.
- **Naming**: Use American English, avoid plurals like "xxxList" for simplicity and to prevent singular/plural ambiguity stemming from poor design
- **Git**: Follow conventional commit format
- **Imports**: Use organized imports (sorted by the import path)
- **Formatting**: Use linting/formatting tools before committing
- **Error Handling**: Be explicit but concise about error cases
- **Go Resources**: Always use `defer` for resource cleanup like `rows.Close()` (sqlclosecheck)
- **Go Defer**: Avoid using `defer` inside loops (revive) - use IIFE or scope properly
- **Frontend**: Follow [`frontend/AGENTS.md`](frontend/AGENTS.md) — the canonical frontend ownership map, shadcn-style component rules, i18n policy, and store write surface. In short: all user-facing display text goes through the i18n system in `src/locales/`, shared UI primitives from `src/components/ui/` come before hand-rolled markup, and components never call `useAppStore.setState` directly.

## Common Go Lint Rules

Always follow these guidelines to avoid common linting errors:

- **Unused Parameters**: Prefix unused parameters with underscore (e.g., `func foo(_ *Bar)`)
- **Modern Go Conventions**: Use `any` instead of `interface{}` (since Go 1.18)
- **Confusing Naming**: Avoid similar names that differ only by capitalization
- **Identical Branches**: Don't use if-else branches that contain identical code
- **Unused Functions**: Mark unused functions with `// nolint:unused` comment if needed for future use
- **Function Receivers**: Don't create unnecessary function receivers; use regular functions if receiver is unused
- **Proper Import Ordering**: Maintain correct grouping and ordering of imports
- **Consistency**: Keep function signatures, naming, and patterns consistent with existing code
- **Export Rules**: Only export (capitalize) functions and types that need to be used outside the package
- **Linting Command**: Always run `golangci-lint run --allow-parallel-runners` without appending filenames to avoid "function not defined" errors (functions are defined in other files within the package)

Project-specific rules enforced by `.golangci.yaml` (forbidigo — the linter rejects these outright):

- **Error construction**: Use `common.Errorf`/`common.Wrap`/`common.Wrapf` with an error `Code`, never `fmt.Errorf` — API errors must carry connect codes
- **protojson**: Use the `ProtojsonUnmarshaler` wrapper instead of calling `protojson.Unmarshal` directly
- **Sorting**: Use the `slices` package (`slices.Sort`, `slices.SortFunc`, `slices.IsSortedFunc`), never the pre-1.21 `sort` functions
- **revive** runs with enable-all-rules minus a short disable list; don't fight it, conform to it

## Miscellaneous

- The database JSONB columns store JSON marshalled by `protojson.Marshal` in Go code. `protojson.Marshal` produces camelCased proto field names rather than the snake_case keys suggested by the SQL column names: the `chat_preferences` column stores `{"enterToSend": ...}` because the `proto/store` message field is `enter_to_send`.
- `frontend/src/types/proto-es/` and `backend/generated-go/` are buf output — regenerate with `cd proto && buf generate`, never hand-edit; both must be regenerated and committed together with the proto change.
- When modifying multiple files, run file modification tasks in parallel whenever possible, instead of processing them sequentially.