# AGENTS.md

This file provides guidance to Copilot (codex/claude.ai/code) when working with code in this repository.


## Development Workflow
**ALWAYS follow these steps after making code changes:**

### Go Code Changes
1. **Format**: Run `gofmt -w` on modified files
2. **Lint**: Run `golangci-lint run --allow-parallel-runners` to catch issues
   - **Important**: Run golangci-lint repeatedly until there are no issues. The linter has a max-issues limit and may not show all issues in a single run.
3. **Auto-fix**: Use `golangci-lint run --fix --allow-parallel-runners` to fix issues automatically
4. **Test**: Run relevant tests before committing
  - If you change ACP stdio integration or anything that could break real local ACP execution, also run `LAELIA_RUN_OPENCODE_ACP_TESTS=1 go test ./backend/agent/executor -count=1` on a machine with local `opencode acp` available.
  - If you change the ACP v2 thread path (acp2, codex provider, thread executor), also run `LAELIA_RUN_CODEX_ACP_TESTS=1 CODEX_HOME=<writable codex home> go test ./backend/agent/executor -run TestThreadExecutorCodex -count=1` on a machine with local `codex` (with `app-server`) available.
5. **Build**: `go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`

### Frontend Code Changes

1. **Format** — Run `pnpm --dir frontend biome:format` (formats all files) or `cd frontend && pnpm biome format --write <path>` for specific files
2. **Lint** — Run `pnpm --dir frontend lint --fix` (ESLint) and `pnpm --dir frontend biome:lint` (Biome linter)
3. **Type check** — Run `pnpm --dir frontend type-check`
4. **Test** — Run `pnpm --dir frontend test`

**Recommended**: Use `pnpm --dir frontend biome:check` to format, lint, and organize imports in one command

### Proto Changes
1. **Format**: Run `buf format -w proto`
2. **Lint**: Run `buf lint proto`
3. **Generate**: Run `cd proto && buf generate`


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

# Run ACP executor integration tests against local opencode ACP when stdio/runtime integration changes
LAELIA_RUN_OPENCODE_ACP_TESTS=1 go test ./backend/agent/executor -count=1

# Run ACP v2 thread executor integration tests against local codex (needs a
# writable CODEX_HOME with config.toml + models.json; the tests copy it into a
# hermetic temp home, so the real home is never touched)
LAELIA_RUN_CODEX_ACP_TESTS=1 CODEX_HOME=/path/to/codex-home go test ./backend/agent/executor -run TestThreadExecutorCodex -count=1

# Lint
golangci-lint run --allow-parallel-runners
```


### Frontend

```bash
# Install dependencies
pnpm --dir frontend i

# Dev server
pnpm --dir frontend dev

# Format (Biome)
pnpm --dir frontend biome:format

# Lint
pnpm --dir frontend lint

# Lint (Biome)
pnpm --dir frontend biome:lint

# Format + Lint + Organize imports (recommended)
pnpm --dir frontend biome:check

# Type check
pnpm --dir frontend type-check

# Test
pnpm --dir frontend test
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
# Local monolithic build: frontend -> embedded into manager, pi -> embedded into machine
scripts/build_laelia.sh                             # outputs build/laelia + build/laelia-machine
LAELIA_BUILD_PROXY=http://host:port scripts/build_laelia.sh  # route the pi GitHub download through a proxy

# Docker images (manager image embeds the frontend; machine image embeds pi)
LAELIA_BUILD_PROXY=http://host:port scripts/build_laelia_manager_docker.sh  # -> laelia/manager:local
LAELIA_BUILD_PROXY=http://host:port scripts/build_laelia_machine_docker.sh  # -> laelia/machine:local
```

Notes:

- `scripts/build-pi.sh` downloads and checksum-verifies the standalone pi
  distribution (binary + runtime assets) into `backend/agent/pi/embedded/dist`
  before any `go build -tags release`. It is idempotent (recorded
  version/platform in `pi.meta`); use `PI_FORCE=1` to re-download.
- `backend/agent/pi/embedded/dist/pi` is a tracked 0-byte placeholder. A
  release build replaces it with the real (large) binary; restore it with
  `git restore backend/agent/pi/embedded/dist/pi` before committing.
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
  stages). `PI_PROXY` is a legacy alias that overrides just the pi download.
  Do not use a global `HTTPS_PROXY` for docker builds: BuildKit auto-injects
  standard proxy args into every stage, including the final runtime images.

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
  - **Go**: https://google.github.io/styleguide/go/
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
