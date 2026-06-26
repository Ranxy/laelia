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

# Start manager backend
go run ./backend/manager/bin/server/main.go --port 8111 --debug

# Run single test
go test -v -count=1 github.com/Ranxy/laelia/backend/manager/path/to/tests -run ^TestFunctionName$

# Run multiple tests
go test -v -count=1 github.com/Ranxy/laelia/backend/manager/path/to/tests -run ^(TestFunctionName|TestFunctionNameTwo)$

# Run ACP executor integration tests against local opencode ACP when stdio/runtime integration changes
LAELIA_RUN_OPENCODE_ACP_TESTS=1 go test ./backend/agent/executor -count=1

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

### Database

```bash
# Connect to Postgres
psql -h localhost -p 5432 -U dev -d laelia -c "sql"
```


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