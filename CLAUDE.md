# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**beads** (command: `bd`) is a distributed, git-backed issue tracker designed for AI agents. SQLite for speed, JSONL for git sync, hash-based IDs for collision-free multi-agent workflows. We dogfood this tool for all task tracking.

## Build & Test Commands

```bash
# Build
make build                    # or: go build -o bd ./cmd/bd

# Test
make test                     # runs scripts/test.sh with coverage
go test -short ./...          # fast local tests (skips slow integration)
go test ./...                 # full suite before committing

# Lint (ignore baseline warnings - see docs/LINTING.md)
golangci-lint run ./...

# Install locally
make install                  # installs to $GOPATH/bin
```

## Running Single Tests

```bash
go test -v -run TestMyFunction ./cmd/bd/
go test -v -run TestMyFunction ./internal/storage/sqlite/
go test -v -tags integration -run TestDualMode ./cmd/bd/  # daemon mode tests
```

## Architecture

**Three-Layer Design:**

1. **CLI Layer** (`cmd/bd/`) - Cobra commands, one file per command. All support `--json` for programmatic use.
2. **Storage Layer** (`internal/storage/sqlite/`) - SQLite implementation with interface in `internal/storage/storage.go`
3. **RPC Layer** (`internal/rpc/`) - Unix sockets for daemon communication. CLI tries daemon first, falls back to direct DB.

**Distributed Database Pattern:**
```
SQLite (.beads/beads.db, gitignored)
    ↕ auto-sync (5s debounce)
JSONL (.beads/issues.jsonl, git-tracked)
    ↕ git push/pull
Remote (shared across machines)
```

**Key paths:**
- Core types: `internal/types/types.go`
- CLI entry: `cmd/bd/main.go`
- Export: `cmd/bd/export.go`, `cmd/bd/autoflush.go`
- Import: `cmd/bd/import.go`, `internal/importer/`

## Critical Workflows

### Testing Without Pollution
```bash
# Use isolated test database (NEVER pollute production)
BEADS_DB=/tmp/test.db ./bd init --quiet --prefix test
BEADS_DB=/tmp/test.db ./bd create "Test issue" -p 1
```

### Agent Session End (MANDATORY)
```bash
bd sync       # Export + commit + push
git push      # Verify pushed
git status    # Must show "up to date with origin"
```

### DO NOT use `bd edit` - it opens an interactive $EDITOR
Use `bd update` instead:
```bash
bd update <id> --description "new description"
bd update <id> --title "new title"
```

## Adding Features

**New Command:**
1. Create file in `cmd/bd/`
2. Add to root command in `main.go`
3. Add `--json` flag
4. Add tests in `*_test.go`
5. Update docs

**Storage Changes:**
1. Update schema in `internal/storage/sqlite/schema.go`
2. Add migration if needed
3. Update `internal/types/types.go`
4. Update export/import in `cmd/bd/`

## Version Bumping

```bash
./scripts/bump-version.sh 0.9.3 --commit  # Updates all version files atomically
git push origin main
```

## Key Documentation

- **AGENT_INSTRUCTIONS.md** - Complete development guide (READ THIS)
- **docs/ARCHITECTURE.md** - Full architecture details
- **CONTRIBUTING.md** - Contribution workflow, dual-mode testing
- **docs/CLI_REFERENCE.md** - Command reference
