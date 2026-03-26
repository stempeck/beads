# Implementation Plan Outline: Fix bd Daemon Crash Loop on Docker Desktop Fakeowner Filesystems

**Date**: 2026-03-26
**Source**: `.designs/daemon-fakeowner-fix/design-doc.md`
**Purpose**: Self-contained phase extraction guide for creating focused IMPLREADME_PHASE{X}.md files
**Usage**: Extract any single phase section below and provide it to an LLM with: "Run `/design-plan-impl .designs/daemon-fakeowner-fix/implementation-plan/implementation_plan_outline.md` to extract Phase X"

---

## How To Use This Document

Each phase below is a **self-contained extraction unit**. Workflow:

1. `/design-plan-impl .designs/daemon-fakeowner-fix/design-doc.md` -- produces this outline (Mode A)
2. `/clear`
3. `/peer-review .designs/daemon-fakeowner-fix/implementation-plan/implementation_plan_outline.md` -- validates outline against codebase
4. `/clear`
5. `/design-plan-impl .designs/daemon-fakeowner-fix/implementation-plan/implementation_plan_outline.md` -- extract Phase X into IMPLREADME (Mode B)
6. `/clear`
7. Use the phase's **Recommended Skill** to implement (e.g., `/ultra-implement`)
8. Repeat steps 5-7 for each phase

**Phase dependency chain:**
```
Phase 1: Non-Fatal Chmod ──────────────────►
                                            │
Phase 2: Startup Backoff ──────────────────►│──► ALL COMPLETE
                                            │
Phase 3: Distinct Exit Codes ──────────────►│
```

All three phases are **fully independent** and can be implemented in any order or in parallel. Phase 1 alone fixes the immediate bug. Phases 2 and 3 are defense-in-depth hardening.

## Deployment Coverage

| Target | Scripts/Config | Covered By Phase | Gap? |
|--------|---------------|-----------------|------|
| CI (Linux/macOS) | `.github/workflows/ci.yml` | All phases (existing tests) | No |
| CI (Windows) | `.github/workflows/ci.yml` (smoke) | Phase 1 skip (GOOS != windows) | No |
| Release | `.goreleaser.yml`, `scripts/release.sh` | No release-specific changes | No |
| Install | `scripts/install.sh`, `Makefile` | No install changes needed | No |
| Nightly | `.github/workflows/nightly.yml` | All phases (integration tests) | No |

No infrastructure, terraform, or environment-specific changes required. This is a CLI-only change.

---

## Phase 1: Non-Fatal Chmod

### Objective
Make `os.Chmod()` failure on the Unix domain socket non-fatal so the daemon stays running on fakeowner filesystems (Docker Desktop host mounts) instead of crash-looping.

### Prerequisites
None -- this is the root fix.

### Recommended Skill
`/ultra-implement` -- straightforward backend Go code change with clear acceptance criteria.

### Design References
| Document | Section | Lines | What It Specifies |
|----------|---------|-------|-------------------|
| design-doc.md | Phase 1: Non-Fatal Chmod | L123-158 | Core fix: log warning instead of fatal error on chmod failure |
| api.md | Option 1: Silent fallback | L15-22 | Rationale for non-fatal approach (no flags, no env vars) |
| security.md | Option 1: Accept default socket permissions | L15-22 | Threat model: Docker containers are single-user, relaxed perms acceptable |
| ux.md | Option 1: Single-line warning | L15-21 | Warning format: single line to daemon.log |
| dependencies.md | Component Dependencies | L26-31 | Non-fatal chmod -> daemon stays running -> health checks pass |
| design-doc.md | Risk Registry | L109-117 | Risks: multi-user socket security (Low), empty-DB-over-JSONL gaps (Medium) |

### Current State (files to read for context)
| File | Lines | What's There |
|------|-------|-------------|
| `internal/rpc/server_lifecycle_conn.go` | L34-40 | Fatal chmod: returns error, closes listener, daemon exits |
| `internal/rpc/server_lifecycle_conn.go` | L111 | Existing warning pattern: `fmt.Fprintf(os.Stderr, "Warning: ...")` |
| `internal/rpc/server_core.go` | L27-66 | Server struct definition -- NO logger field; uses stderr for warnings |
| `internal/rpc/server_export_import_auto.go` | L199 | Another stderr warning pattern: `fmt.Fprintf(os.Stderr, "Warning: failed to set file permissions: %v\n", ...)` |
| `cmd/bd/daemon_server.go` | L14-34 | `startRPCServer()` -- creates Server, starts in goroutine, errors go to `serverErrChan` |
| `internal/rpc/server_routing_validation_diagnostics.go` | L340-395 | Health endpoint: returns "healthy"/"degraded"/"unhealthy" based on DB ping |
| `internal/rpc/protocol.go` | L416-427 | `HealthResponse` struct -- no `chmod_supported` field (not needed per design) |
| `cmd/bd/integrity.go` | L144-188 | `validatePreExport()` -- blocks empty-DB-over-JSONL export |
| `cmd/bd/daemon_sync.go` | L86-99 | Daemon-side empty-DB protection -- blocks empty export over non-empty JSONL |

### Required Changes
**File 1: `internal/rpc/server_lifecycle_conn.go`**
- **L34-40**: Remove fatal error handling for chmod. Replace with stderr warning using existing codebase pattern. Remove the `_ = listener.Close()` and `return fmt.Errorf(...)` lines. Replace with `fmt.Fprintf(os.Stderr, "Warning: could not set socket permissions to 0600 (filesystem may not support chmod on sockets): %v\n", err)`.

```go
// BEFORE (L34-40):
// Set socket permissions to 0600 for security (owner only)
if runtime.GOOS != "windows" {
    if err := os.Chmod(s.socketPath, 0600); err != nil {
        _ = listener.Close()
        return fmt.Errorf("failed to set socket permissions: %w", err)
    }
}

// AFTER:
// Set socket permissions to 0600 for security (owner only).
// Non-fatal: on fakeowner filesystems (Docker Desktop host mounts),
// chmod on Unix sockets returns EINVAL. The daemon continues with
// default permissions. Docker containers are single-user so this
// is acceptable. See .designs/daemon-fakeowner-fix/security.md.
if runtime.GOOS != "windows" {
    if err := os.Chmod(s.socketPath, 0600); err != nil {
        fmt.Fprintf(os.Stderr, "Warning: could not set socket permissions to 0600 (filesystem may not support chmod on sockets): %v\n", err)
    }
}
```

**File 2: New test in `internal/rpc/` (e.g., `server_lifecycle_chmod_test.go`)**
- Test that `Server.Start()` succeeds even when the socket chmod would fail. This can be tested by verifying the server starts and accepts connections on a tmpfs/regular directory (chmod succeeds) and that the warning path is exercised.

### Acceptance Criteria
```bash
# 1. Code compiles
go build ./internal/rpc/
# Expected: no errors

# 2. Chmod error handling is non-fatal (no listener.Close or return on chmod error)
grep -A3 'os.Chmod(s.socketPath' internal/rpc/server_lifecycle_conn.go | grep -c 'listener.Close'
# Expected: 0

# 3. Warning message is present
grep -c 'could not set socket permissions' internal/rpc/server_lifecycle_conn.go
# Expected: 1

# 4. Existing RPC tests pass
go test ./internal/rpc/...
# Expected: PASS

# 5. Full test suite passes
go test -short ./...
# Expected: PASS
```

### Gotchas (from codebase investigation)
- **Server struct has no logger field.** The design doc's code sketch uses `s.logger.Warn(...)` but the `Server` struct in `server_core.go:27-66` has no logger. The correct approach is to use `fmt.Fprintf(os.Stderr, ...)` which is the established pattern throughout the RPC code (see `server_lifecycle_conn.go:111`, `server_export_import_auto.go:199`, `server_routing_validation_diagnostics.go:89`). The daemon-level logger captures stderr, so warnings will appear in `daemon.log`.
- **The `listener` is assigned to `s.listener` twice.** Lines 32 and 44 both set `s.listener = listener`. The first assignment at L32 is before the chmod; the second at L44 is under lock. If chmod was fatal and closed the listener, the first assignment would leave a closed listener in `s.listener`. Now that chmod is non-fatal, this double-assignment is harmless but worth noting.
- **`ensureSocketDir()` at line 144-152 also does a best-effort chmod.** Line 150 does `_ = os.Chmod(dir, 0700)` on the directory and already ignores errors. The socket chmod fix follows this same pattern.
- **The health endpoint (`handleHealth`) checks DB responsiveness, not chmod status.** It will correctly return "healthy" when the daemon is running with relaxed socket permissions, because health is defined by DB availability, not socket permissions.

---

## Phase 2: Startup Backoff / Circuit Breaker

### Objective
Add persistent startup state tracking so the daemon detects rapid restart patterns and backs off exponentially, preventing crash-loop resource exhaustion and SQLite corruption regardless of the failure cause.

### Prerequisites
None -- independent of Phase 1, though Phase 1 makes this less critical.

### Recommended Skill
`/ultra-implement` -- backend Go code following an established codebase pattern (`daemon_sync_state.go`).

### Design References
| Document | Section | Lines | What It Specifies |
|----------|---------|-------|-------------------|
| design-doc.md | Phase 2: Startup Backoff / Circuit Breaker | L160-179 | Deliverables, acceptance criteria, backoff schedule |
| design-doc.md | Data Model | L69-83 | `daemon-state.json` schema: starts array, backoff_until, circuit_open |
| data.md | Option A3: Refuse to open DB on rapid restarts | L36-43 | Circuit breaker rationale: prevent corruption at source |
| data.md | Option B2: New daemon-state.json | L63-97 | Separate file, start tracking, bounded at 20 entries |
| scale.md | Option 1: Exponential backoff | L15-21 | Schedule: 5s, 15s, 30s, 1m, 5m, 15m (capped) |
| ux.md | Circuit Breaker UX | L56-65 | Error message format when circuit breaker trips |
| integration.md | Option 2: Backoff state file | L24-31 | daemon-state.json is internal to bd, not a cross-repo contract |
| design-doc.md | Risk Registry | L115 | Risk: stale daemon-state.json blocks daemon permanently (mitigated by TTL + --force) |

### Current State (files to read for context)
| File | Lines | What's There |
|------|-------|-------------|
| `cmd/bd/daemon_sync_state.go` | Full file (L1-184) | Reference pattern: `SyncState` type, Load/Save/Record/Reset with mutex, stale cleanup at 24h, backoff schedule |
| `cmd/bd/daemon.go` | L297-400 | `runDaemonLoop()` entry -- where startup state should be checked/written |
| `cmd/bd/daemon.go` | L390-393 | `ResetBackoffOnDaemonStart()` call -- existing sync state reset at daemon start |
| `cmd/bd/daemon_start.go` | L39-161 | `daemonStartCmd` Run function -- parent process, pre-fork checks |
| `cmd/bd/daemon_start.go` | L163-176 | Flag definitions -- where `--force` should be added |
| `cmd/bd/daemon_autostart.go` | L31-35, L594-616 | Existing parent-side backoff: `canRetryDaemonStart()` with 5s/10s/20s/40s/80s/120s schedule |
| `.beads/.gitignore` | L8-14 | Daemon runtime files ignored; `sync-state.json` at L13 -- `daemon-state.json` goes here |

### Required Changes
**File 1: New `cmd/bd/daemon_startup_state.go`**
- Create following `daemon_sync_state.go` pattern exactly:
  - `DaemonStartupState` struct with `Starts []StartEntry`, `BackoffUntil time.Time`, `CircuitOpen bool`
  - `StartEntry` struct: `Timestamp time.Time`, `Version string`, `ExitReason string`
  - Constants: `daemonStateFile = "daemon-state.json"`, `maxStartEntries = 20`, stale threshold = 1 hour
  - Backoff schedule: 5s, 15s, 30s, 1m, 5m, 15m (per design doc)
  - Functions: `LoadDaemonStartupState()`, `SaveDaemonStartupState()`, `RecordDaemonStart()`, `RecordDaemonStartSuccess()`, `ShouldBlockDaemonStart()`, `GetStartupBackoffMessage()`
  - Mutex for thread safety
  - Auto-clear entries older than 1 hour
  - Circuit breaker trips after 10 consecutive failures within 1 hour
  - Bound starts array to 20 entries

**File 2: `cmd/bd/daemon_start.go`**
- **L163-176 (init function)**: Add `--force` flag: `daemonStartCmd.Flags().Bool("force", false, "Bypass startup backoff (use after fixing the issue)")`
- **L39-161 (Run function)**: After `interval` validation (~L66), add backoff check:
  - Read `force` flag
  - If not force: call `ShouldBlockDaemonStart(beadsDir)`
  - If blocked: print backoff message from `GetStartupBackoffMessage()` and exit
  - Need to determine beadsDir early (before fork) -- use `beads.FindDatabasePath()` like existing code at L137-141

**File 3: `cmd/bd/daemon.go`**
- **~L390 (after ResetBackoffOnDaemonStart)**: Call `RecordDaemonStart(beadsDir, Version)` to log this start attempt
- **After successful server start (~L615-618)**: Call `RecordDaemonStartSuccess(beadsDir)` to clear backoff state
- **On fatal return paths**: Call `RecordDaemonStartFailure(beadsDir, reason)` with appropriate reason string

**File 4: `.beads/.gitignore`**
- **After L13 (sync-state.json)**: Add `daemon-state.json`

### Acceptance Criteria
```bash
# 1. New file exists and compiles
test -f cmd/bd/daemon_startup_state.go && go build ./cmd/bd/
# Expected: file exists, no compile errors

# 2. --force flag registered
grep -c 'force' cmd/bd/daemon_start.go
# Expected: >= 2 (flag definition + flag read)

# 3. daemon-state.json is gitignored
grep -c 'daemon-state.json' .beads/.gitignore
# Expected: 1

# 4. Backoff schedule matches design
grep -c '15 \* time.Minute' cmd/bd/daemon_startup_state.go
# Expected: >= 1

# 5. Circuit breaker threshold exists
grep -c 'circuit' cmd/bd/daemon_startup_state.go
# Expected: >= 1

# 6. Stale state cleanup (1 hour)
grep 'time.Hour' cmd/bd/daemon_startup_state.go | grep -v '_test'
# Expected: contains "1 * time.Hour" or similar

# 7. All tests pass
go test -short ./...
# Expected: PASS

# 8. Startup state follows sync state pattern (mutex, Load/Save/Record)
grep -c 'Mutex' cmd/bd/daemon_startup_state.go
# Expected: >= 1
```

### Gotchas (from codebase investigation)
- **Existing parent-side backoff in `daemon_autostart.go:594-616`.** There is ALREADY a backoff mechanism for auto-start (`canRetryDaemonStart()` with 5s-120s schedule), but it's in-process only (global variables, not persisted). Phase 2's `daemon-state.json` is the persistent, cross-process version. These are complementary, not conflicting -- the auto-start backoff prevents the parent from spawning too many children; the startup state prevents the child daemon from running if it knows it will fail.
- **`beadsDir` determination is duplicated.** `daemon_start.go` determines `dbPath` at L137-141, while `daemon.go:runDaemonLoop()` determines `daemonDBPath` at L338-347. For the backoff check in `daemon_start.go`, you need `beadsDir` BEFORE the fork. Use the same `beads.FindDatabasePath()` + `filepath.Dir()` pattern.
- **`daemon.go` uses `return` not `os.Exit()` for cleanup.** Fatal paths in `runDaemonLoop()` use `return` to allow defers (PID file removal, lock release). Recording failure state must happen BEFORE these returns, not in a defer (because you need the specific reason string).
- **Race condition with concurrent daemon starts.** The startup state file could be read/written by multiple processes simultaneously (e.g., two `bd daemon start` calls). The mutex only protects within a single process. File-level locking via `daemon.lock` (acquired at `daemon.go:349`) prevents the daemon itself from running concurrently, but the backoff check in `daemon_start.go` happens BEFORE the lock. Use atomic file writes (write to temp, rename) like `saveSyncStateUnlocked` does.
- **The design doc's backoff schedule (5s, 15s, 30s, 1m, 5m, 15m) differs from the sync state schedule (30s, 1m, 2m, 5m, 10m, 30m).** This is intentional -- daemon restarts are faster operations than sync, so shorter initial backoff makes sense.
- **Open Question Q1 from design doc**: The design doc asks whether to use the same schedule as sync-state.json. The answer is no -- use the shorter schedule specified in the design doc (5s, 15s, 30s, 1m, 5m, 15m).

---

## Phase 3: Distinct Exit Codes

### Objective
Add distinct exit codes so external restarters (like gastown) can distinguish "retry might help" (exit 1) from "retry won't help" (exit 2), enabling smarter restart decisions.

### Prerequisites
None -- independent of Phases 1 and 2.

### Recommended Skill
`/ultra-implement` -- straightforward backend Go code change with documentation update.

### Design References
| Document | Section | Lines | What It Specifies |
|----------|---------|-------|-------------------|
| design-doc.md | Phase 3: Distinct Exit Codes | L180-195 | Exit code semantics: 0=clean, 1=generic, 2=environment |
| integration.md | Option 3: Exit codes for different failure modes | L33-41 | Exit codes as stable contract, standard Unix pattern |
| integration.md | Gastown-Side Recommendations | L52-58 | Future gastown exit code awareness (separate work) |
| design-doc.md | Risk Registry | L116 | Risk: gastown doesn't check exit codes today (status quo, not blocking) |

### Current State (files to read for context)
| File | Lines | What's There |
|------|-------|-------------|
| `cmd/bd/daemon_start.go` | L66-134 | 8x `os.Exit(1)` calls for various error conditions |
| `cmd/bd/daemon.go` | L297-699 | `runDaemonLoop()` -- uses `return` not `os.Exit()` for cleanup via defers |
| `cmd/bd/daemon.go` | L369-387 | Non-retryable errors: single-process backend, filesystem |
| `cmd/bd/daemon.go` | L337-347 | Non-retryable error: no beads database found |
| `cmd/bd/daemon_lifecycle.go` | L371-472 | `startDaemon()` -- forks child process, monitors via PID file |
| `docs/DAEMON.md` | L489-510 | "Common Daemon Issues" section -- exit codes documentation would fit here |

### Required Changes
**File 1: `cmd/bd/daemon.go` (or new `cmd/bd/daemon_exit_codes.go`)**
- Define exit code constants:
```go
const (
    ExitCleanShutdown   = 0 // Signal, parent died, requested stop
    ExitGenericError    = 1 // Retry may help
    ExitEnvironmentError = 2 // Filesystem limitation, missing binary, etc. -- retry won't help
)
```

**File 2: `cmd/bd/daemon_start.go`**
- Classify existing `os.Exit(1)` calls:
  - **Keep as exit 1 (retryable)**: L66-69 (invalid interval), L71-75 (PID file error), L89-92 (daemon already running), L100-103 (daemon already running, can't check version)
  - **Change to exit 2 (non-retryable)**: L111-114 (`--auto-commit` with `--local`), L115-119 (`--auto-push` with `--local`), L123-127 (not in git repo), L130-134 (no upstream)

**File 3: `cmd/bd/daemon.go`**
- In `runDaemonLoop()`, the function uses `return` (not `os.Exit`) so exit codes flow through the calling function. The exit code needs to be communicated back to the caller.
- Option A: Change `runDaemonLoop()` to return an int exit code, and have the caller in `daemon_start.go` call `os.Exit(code)`.
- Option B: Use a package-level variable or error wrapper to carry the exit code.
- **Recommended: Option A** -- return int from `runDaemonLoop()`.
- Classify return paths:
  - **Exit 2 (non-retryable)**: L343-345 (no database found), L369-387 (single-process backend unsupported), L427-440 (multiple/non-canonical DB files), L505-506 (can't open store), L548-560 (fingerprint mismatch), L569-601 (version mismatch)
  - **Exit 0 (clean)**: normal event loop completion (signal/parent died)

**File 4: `docs/DAEMON.md`**
- Add new section "Exit Codes" before or after "Common Daemon Issues" (~L489):
```markdown
## Exit Codes

| Code | Meaning | Retry? | Examples |
|------|---------|--------|----------|
| 0 | Clean shutdown | N/A | Signal received, parent process died, `bd daemon stop` |
| 1 | Generic error | Yes | PID file error, daemon already running, transient failures |
| 2 | Environment error | No | Filesystem limitation, missing database, unsupported backend, no git repo |
```

### Acceptance Criteria
```bash
# 1. Exit code constants defined
grep -c 'ExitEnvironmentError' cmd/bd/daemon_start.go cmd/bd/daemon.go cmd/bd/daemon_exit_codes.go 2>/dev/null | grep -v ':0$'
# Expected: at least one file with matches

# 2. Exit code 2 is used somewhere
grep -c 'os.Exit(2)' cmd/bd/daemon_start.go
# Expected: >= 1

# 3. docs/DAEMON.md has exit code section
grep -c 'Exit Codes' docs/DAEMON.md
# Expected: >= 1

# 4. Exit code 0 still used for clean shutdown (implicit returns)
grep -c 'ExitCleanShutdown\|exit code 0\|clean shutdown' cmd/bd/daemon.go docs/DAEMON.md
# Expected: >= 1

# 5. All tests pass
go test -short ./...
# Expected: PASS

# 6. Code compiles
go build ./cmd/bd/
# Expected: no errors
```

### Gotchas (from codebase investigation)
- **`runDaemonLoop()` currently returns nothing (void function).** Changing its signature to return an `int` exit code requires updating ALL callers. There are at least two call sites: `daemon_start.go` (via `startDaemon` -> fork) and the deprecated flag-based path in `daemon.go`. The foreground path calls `runDaemonLoop()` directly; the background path forks a child process and the exit code propagates through the child's `os.Exit`.
- **103 files use `os.Exit` across the codebase.** Phase 3 only targets daemon-related files. Do NOT change exit codes in non-daemon commands.
- **No existing exit code tests.** The codebase has 24 daemon test files but none assert specific exit code values. Adding exit code tests requires spawning a subprocess and checking its exit status, which is more complex than unit tests. Consider testing the classification logic (which errors map to which codes) rather than the actual `os.Exit` calls.
- **`daemon_lifecycle.go:startDaemon()` spawns a child via `exec.Command()` and calls `cmd.Process.Release()` at L455.** The parent process does NOT wait for the child to exit, so it never sees the child's exit code. Exit codes only matter when the daemon runs in `--foreground` mode (where the caller IS the daemon) or when an external restarter (systemd, gastown) monitors the child process directly.
- **The deprecated flag-based daemon start path in `daemon.go:50-91` (the `daemonCmd` Run function) also calls `runDaemonLoop()`.** If `runDaemonLoop()` signature changes, this path must be updated too. However, this path sets `os.Exit(1)` for its own validation errors and would need the same exit code classification.
- **Open Question Q2 from design doc**: Whether to log the fakeowner filesystem name or just the chmod error generically. The Phase 1 change uses the generic approach (log the error message from `os.Chmod`), which covers all unknown future filesystem types. This is the right call -- no fakeowner-specific detection needed.

---

## Success Metrics

These map to the design doc's success criteria:

| Metric | Source | How to Verify |
|--------|--------|---------------|
| Daemon starts on fakeowner without crash | Phase 1 | Deploy to Docker Desktop host mount, verify `bd daemon status` shows "healthy" |
| No JSONL data loss from crash loops | Phase 1 (eliminates crash loop) + existing protections | Verify empty-DB checks at `integrity.go:178-181` and `daemon_sync.go:86-99` cover the scenario |
| Crash-loop resource exhaustion prevented | Phase 2 | Simulate 10 rapid daemon starts, verify circuit breaker trips |
| External restarters can make informed retry decisions | Phase 3 | Run daemon in --foreground on unsupported filesystem, verify exit code 2 |
| Zero regression on native filesystems | All phases | Full test suite passes on Linux and macOS CI |

---

# Peer Review

**Review Date**: 2026-03-26
**Reviewer**: Claude Code (rootcause-review skill)
**Original Analysis Date**: 2026-03-26

## Review Summary

**Overall Verdict**: PARTIALLY VALIDATED
**Confidence Level**: High

The implementation plan is well-researched with overwhelmingly accurate code references. All three phases are correctly scoped and the proposed changes are sound. However, there are factual errors in specific claims (file counts, call-site analysis, line ranges), and the "fully independent / parallel" claim for phases 2 and 3 is misleading due to shared file modifications.

## Claim-by-Claim Validation

### Phase 1 Claims

### Claim 1: `server_lifecycle_conn.go` L34-40 contains fatal chmod handling
**Status**: VALIDATED
**Verification Method**: Read file, verified line-by-line
**Evidence**: Lines 34-40 contain exactly the code shown: `os.Chmod` → `listener.Close()` → `return fmt.Errorf(...)`. The BEFORE block in the plan is a character-perfect match.
**Notes**: None.

### Claim 2: `server_lifecycle_conn.go` L111 has existing warning pattern
**Status**: VALIDATED
**Verification Method**: Read file at L111
**Evidence**: Line 111: `fmt.Fprintf(os.Stderr, "Warning: failed to close default storage: %v\n", closeErr)`
**Notes**: Good identification of the codebase pattern to follow.

### Claim 3: `server_core.go` L27-66 — Server struct has no logger field
**Status**: VALIDATED
**Verification Method**: Read full Server struct definition
**Evidence**: 40-field struct with no logger. Uses `os.Stderr` for warnings throughout.
**Notes**: The gotcha correctly identifies that the design doc's `s.logger.Warn(...)` sketch is wrong and `fmt.Fprintf(os.Stderr, ...)` is the correct pattern.

### Claim 4: `server_export_import_auto.go` L199 has stderr warning pattern
**Status**: VALIDATED
**Verification Method**: Read file at L199
**Evidence**: `fmt.Fprintf(os.Stderr, "Warning: failed to set file permissions: %v\n", err)` — exact match.

### Claim 5: `daemon_server.go` L14-34 — startRPCServer
**Status**: PARTIALLY VALIDATED
**Verification Method**: Read full file
**Evidence**: Function starts at L14 but extends to L40 (not L34). The function does what's described: creates Server, starts in goroutine, errors to `serverErrChan`.
**Notes**: Line range L14-34 cuts off mid-function. Should be L14-40.

### Claim 6: Health endpoint at L340-395 returns healthy/degraded/unhealthy
**Status**: VALIDATED
**Verification Method**: Read `server_routing_validation_diagnostics.go` L340-395
**Evidence**: `handleHealth` checks DB via `GetStatistics`, returns "healthy" (default), "degraded" (>500ms), or "unhealthy" (ping error). No chmod status check.

### Claim 7: `protocol.go` L416-427 — HealthResponse struct, no chmod_supported
**Status**: VALIDATED
**Verification Method**: Read protocol.go L416-427
**Evidence**: HealthResponse has fields for status, version, uptime, DB response, connections, memory — no chmod field.

### Claim 8: `integrity.go` L144-188 — validatePreExport blocks empty-DB-over-JSONL
**Status**: VALIDATED
**Verification Method**: Read integrity.go L144-188
**Evidence**: `validatePreExport` at L144-188. L178-181: `if dbCount == 0 && jsonlCount > 0 { return fmt.Errorf("refusing to export empty DB over %d issues...") }`

### Claim 9: `daemon_sync.go` L86-99 — daemon-side empty-DB protection
**Status**: VALIDATED
**Verification Method**: Read daemon_sync.go L86-99
**Evidence**: Safety check at L86-99: if `len(issues) == 0`, checks JSONL count, refuses if `existingCount > 0`.

### Phase 2 Claims

### Claim 10: `daemon_sync_state.go` provides reference pattern
**Status**: VALIDATED
**Verification Method**: Read full file (184 lines)
**Evidence**: Contains `SyncState` type, `LoadSyncState/SaveSyncState/RecordSyncFailure/RecordSyncSuccess/ResetBackoffOnDaemonStart`, mutex protection, stale cleanup at 24h, backoff schedule (30s, 1m, 2m, 5m, 10m, 30m).
**Notes**: Good reference pattern. The plan correctly notes the different backoff schedules.

### Claim 11: `daemon.go` L390-393 — ResetBackoffOnDaemonStart call
**Status**: VALIDATED
**Verification Method**: Read daemon.go L390-393
**Evidence**: `if !localMode { ResetBackoffOnDaemonStart(beadsDir) }` — exact match.

### Claim 12: `daemon_autostart.go` L594-616 — existing parent-side backoff
**Status**: VALIDATED
**Verification Method**: Read daemon_autostart.go L594-616
**Evidence**: `canRetryDaemonStart()` with schedule 5s, 10s, 20s, 40s, 80s, 120s (capped). Uses global variables `lastDaemonStartAttempt` and `daemonStartFailures`. In-process only, not persisted.

### Claim 13: `.beads/.gitignore` L13 has sync-state.json
**Status**: VALIDATED
**Verification Method**: Read .gitignore
**Evidence**: L13: `sync-state.json`. `daemon-state.json` is NOT present (correctly identified as needing addition).

### Claim 14: Race condition with concurrent daemon starts
**Status**: VALIDATED
**Verification Method**: Traced code flow
**Evidence**: Backoff check in `daemon_start.go` runs before fork. Lock acquired at `daemon.go:349` in the child process. Gap between check and lock allows concurrent reads. Plan correctly recommends atomic file writes.

### Phase 3 Claims

### Claim 15: `daemon_start.go` has 8x `os.Exit(1)` at L66-134
**Status**: VALIDATED
**Verification Method**: Grep for `os.Exit(1)` in daemon_start.go
**Evidence**: 8 matches at lines 68, 74, 91, 103, 113, 118, 126, 133. All within the L66-134 range.
**Notes**: Some individual line ranges in the exit code classification are off by 1-2 lines (e.g., plan says L111-114 but the `--auto-commit` error exit is at L113). Not a problem for implementation.

### Claim 16: `runDaemonLoop` is void (returns nothing)
**Status**: VALIDATED
**Verification Method**: Read function signature at daemon.go:297
**Evidence**: `func runDaemonLoop(interval time.Duration, autoCommit, autoPush, autoPull, localMode bool, logPath, pidFile, logLevel string, logJSON, federation bool, federationPort, remotesapiPort int)` — no return type.

### Claim 17: 103 files use `os.Exit` across the codebase
**Status**: INVALIDATED
**Verification Method**: Grep for `os.Exit` in `cmd/bd/`
**Evidence**: 100 files, 728 total occurrences. Not 103.
**Notes**: Minor factual error. The practical guidance ("only target daemon-related files") is correct.

### Claim 18: Deprecated path in `daemon.go:50-91` calls `runDaemonLoop()`
**Status**: INVALIDATED
**Verification Method**: Read daemon.go L50-253 (the full deprecated Run function)
**Evidence**: The deprecated `daemonCmd` Run function extends from L50 to L253, NOT L50-91. It calls `startDaemon()` at L252, not `runDaemonLoop()` directly. `startDaemon` (in `daemon_lifecycle.go:389`) is the sole direct call site for `runDaemonLoop`. Changing `runDaemonLoop`'s signature requires updating `startDaemon`, which serves both the deprecated and new paths.
**Notes**: The gotcha's underlying point is valid (both paths are affected), but the mechanism described is wrong.

### Claim 19: `daemon_lifecycle.go` L371-472 — startDaemon spawns child, Release at L455
**Status**: VALIDATED
**Verification Method**: Read daemon_lifecycle.go L371-472
**Evidence**: `startDaemon` at L371-472. `cmd.Process.Release()` at L455. Parent does NOT wait for child exit.

### Claim 20: All three phases are "fully independent" and can be parallelized
**Status**: PARTIALLY VALIDATED
**Verification Method**: Analyzed file modification sets
**Evidence**: Phases are logically independent. However, Phases 2 and 3 both modify `daemon_start.go` and `daemon.go`. Implementing in parallel would produce merge conflicts.
**Notes**: Sequential implementation or careful conflict resolution needed.

## Code Reference Verification

| Reference | Claimed | Actual | Status |
|-----------|---------|--------|--------|
| `server_lifecycle_conn.go:34-40` | Fatal chmod: close listener, return error | Exact match | VALIDATED |
| `server_lifecycle_conn.go:111` | Warning pattern with `fmt.Fprintf(os.Stderr, ...)` | Exact match | VALIDATED |
| `server_core.go:27-66` | Server struct, no logger field | Exact match | VALIDATED |
| `server_export_import_auto.go:199` | stderr warning pattern | Exact match | VALIDATED |
| `daemon_server.go:14-34` | startRPCServer creates Server, goroutine, errChan | Function starts L14, ends L40 (not L34) | PARTIALLY VALIDATED |
| `server_routing_validation_diagnostics.go:340-395` | Health endpoint, healthy/degraded/unhealthy | Exact match | VALIDATED |
| `protocol.go:416-427` | HealthResponse struct, no chmod_supported | Exact match | VALIDATED |
| `integrity.go:144-188` | validatePreExport, empty-DB-over-JSONL protection | Exact match | VALIDATED |
| `daemon_sync.go:86-99` | Daemon-side empty-DB protection | Exact match | VALIDATED |
| `daemon_sync_state.go:1-184` | Reference pattern: SyncState, Load/Save/Record/Reset | Exact match | VALIDATED |
| `daemon.go:297-400` | runDaemonLoop entry | Exact match | VALIDATED |
| `daemon.go:390-393` | ResetBackoffOnDaemonStart call | Exact match | VALIDATED |
| `daemon_start.go:39-161` | daemonStartCmd Run function | Exact match | VALIDATED |
| `daemon_start.go:163-176` | Flag definitions (init function) | Exact match | VALIDATED |
| `daemon_autostart.go:31-35` | Failure tracking vars | L32-35 (off by 1) | VALIDATED |
| `daemon_autostart.go:594-616` | canRetryDaemonStart with backoff | Exact match | VALIDATED |
| `.beads/.gitignore:8-14` | Daemon runtime files, sync-state.json at L13 | Exact match | VALIDATED |
| `daemon_lifecycle.go:371-472` | startDaemon, fork, Process.Release at L455 | Exact match | VALIDATED |
| `docs/DAEMON.md:489-510` | Common Daemon Issues section | Exact match | VALIDATED |
| `daemon.go:337-347` | No beads database found (non-retryable) | Exact match | VALIDATED |
| `daemon.go:369-387` | Single-process backend check | L369-388 (1 line off) | VALIDATED |
| `daemon.go:505-506` | Can't open store | L503-506 | VALIDATED |
| `daemon.go:548-560` | Fingerprint mismatch | Exact match | VALIDATED |
| `daemon.go:569-601` | Version mismatch | Exact match | VALIDATED |
| `daemon.go:50-91` | Deprecated path calls runDaemonLoop | Path extends to L253, calls startDaemon not runDaemonLoop | INVALIDATED |
| `server_routing_validation_diagnostics.go:89` | Another stderr warning pattern | L89: `fmt.Fprintf(os.Stderr, "Warning: Client request without database binding validation...` | VALIDATED |

## 5-Whys Logic Chain Review

This is an implementation plan, not a 5-whys analysis. However, the causal reasoning is:

| Step | Logic | Evidence | Status |
|------|-------|----------|--------|
| Problem: Daemon crash-loops on fakeowner | Chmod returns EINVAL on fakeowner sockets | Design doc specifies this; code at L36-39 treats it as fatal | VALIDATED |
| Root cause: Fatal error handling for chmod | Error handling closes listener and returns error, killing daemon | Lines 37-38: `_ = listener.Close(); return fmt.Errorf(...)` | VALIDATED |
| Fix: Make chmod non-fatal | Log warning, continue running | Uses established pattern from L111, L199 | VALIDATED |
| Defense: Startup backoff | Prevents resource exhaustion from any crash loop | Modeled on proven daemon_sync_state.go pattern | VALIDATED |
| Defense: Exit codes | Enables smarter restart decisions | Standard Unix pattern (0/1/2) | VALIDATED |

## Environment Verification

**Environment Tested**: Local macOS development environment
**Tests Performed**:
1. Verified all referenced files exist and contain the described code
2. Verified grep counts for os.Exit patterns
3. Verified call-site analysis for runDaemonLoop
4. Verified .gitignore does not already contain daemon-state.json

Note: Docker Desktop fakeowner filesystem not available for direct reproduction. The fix is straightforward enough (remove two lines, add one warning line) that environmental reproduction is not critical for review.

## Falsification Attempts

**Attempted to disprove**: "All three phases are fully independent and can be parallelized"
**Result**: Partially disproved. Phases 2 and 3 both modify `daemon_start.go` and `daemon.go`. While the changes target different lines, simultaneous branch work would produce merge conflicts. The claim is logically correct (no runtime dependency) but practically misleading.

**Attempted to disprove**: "runDaemonLoop has at least two call sites"
**Result**: Disproved. There is exactly one direct call site: `daemon_lifecycle.go:389`. The deprecated path calls `startDaemon`, which wraps `runDaemonLoop`.

**Attempted to disprove**: "Phase 1 fix is sufficient to solve the crash loop"
**Result**: Failed to disprove. Making chmod non-fatal eliminates the only fatal error in the daemon startup path that's triggered by fakeowner. The daemon would proceed to listener setup (which works on fakeowner), health checks (DB-based, unaffected), and event loop.

## Gaps Identified

1. **Phase 3 Required Changes omits the deprecated daemon path.** The gotchas section correctly notes it exists, but the Required Changes section doesn't include it. If `runDaemonLoop` returns an int, `startDaemon` must propagate it, AND the deprecated path's own validation `os.Exit(1)` calls at `daemon.go:103,109,142,150,169,183,193,198,206,224` need classification too.

2. **Phase 2's `RecordDaemonStartSuccess` placement is vague.** The plan says "After successful server start (~L615-618)" but the server start at L615 is followed by extensive setup (registry, config, sync, event loop). "Successful start" needs a precise definition — is it when the server is listening, or when the full daemon loop is running?

3. **No test strategy for Phase 3 exit codes.** The gotchas note "No existing exit code tests" and suggest testing classification logic, but Required Changes doesn't include a test file.

4. **Phase 2 doesn't address the deprecated path's interaction.** The deprecated `daemonCmd` path at `daemon.go:96-252` also calls `startDaemon`. If `--force` is added to `daemonStartCmd`, the deprecated path won't have it. Users on the deprecated path can't bypass the backoff.

## Errors Found

1. **"103 files use os.Exit"** — actual count is 100 files with 728 occurrences.
2. **"daemon.go:50-91 calls runDaemonLoop()"** — the deprecated Run function extends to L253 and calls `startDaemon()` at L252, not `runDaemonLoop()` directly.
3. **`daemon_server.go` L14-34** — function extends to L40, not L34.
4. **"At least two call sites" for runDaemonLoop** — there is exactly one direct call site (`daemon_lifecycle.go:389`). Both the deprecated and new paths go through `startDaemon`.

## Solution Assessment

**Proposed Fix Adequacy**: Adequate
**Systemic**: Yes
**Completeness**: Phase 1 fixes all instances (any chmod failure on any filesystem). Phases 2-3 provide defense-in-depth for any future crash cause.
**Risk Assessment**: Low risk. Phase 1 is a 3-line change (remove 2, add 1). Phases 2-3 are additive.

### Phase 1 — Non-Fatal Chmod
- **Systemic?** Yes. Handles ALL chmod failures on ALL filesystems generically. No hardcoded filesystem names.
- **Future-proof?** Yes. New filesystems that fail chmod work automatically.
- **Pattern consistent?** Yes. Follows `ensureSocketDir()` L150 which already does best-effort `_ = os.Chmod(dir, 0700)`.

### Phase 2 — Startup Backoff
- **Systemic?** Yes. Handles any crash cause, not just fakeowner.
- **Pattern consistent?** Yes. Modeled on `daemon_sync_state.go`.
- **Gap:** Deprecated path can't use `--force`. Minor — deprecated path is rarely used.

### Phase 3 — Distinct Exit Codes
- **Systemic?** Yes. Classifies all daemon error paths, not just fakeowner.
- **Gap:** Missing test file in Required Changes. Missing deprecated path classification.

### Enforcement Analysis

Answer each question explicitly:
1. **Can the original failure still occur after this fix?** NO. Phase 1 makes chmod non-fatal. The daemon continues regardless of chmod result. The crash loop is structurally impossible after this change.
2. **What type of enforcement does the fix use?**
   - [x] Mechanical interlock (code prevents the action)
   - [ ] Runtime guard (code detects and blocks at runtime)
   - [ ] Instruction/configuration (relies on correct interpretation)
   - [ ] Advisory only (comments, docs, warnings)
3. **Enforcement Score**: 9/10 — The code change makes it impossible for chmod failure to kill the daemon. Deducted 1 point because the fix relies on someone not re-introducing `listener.Close()` + `return` in the future (no compiler guard).
4. **What would a mechanical interlock look like for this problem?** The proposed fix IS the mechanical interlock — removing the fatal error path from the code. A stronger version would be a linter rule or test that asserts `Server.Start()` never returns an error from chmod, but that's over-engineering for a 3-line change.

**GATE**: Enforcement Score 9 >= 7. Passes.

## Final Verdict

**Root Cause Claim**: VALIDATED — Fatal chmod handling in `server_lifecycle_conn.go:34-40` causes daemon crash on fakeowner filesystems.
**Solution Claim**: VALIDATED — All three phases are correctly scoped, well-researched, and systemic.
**Recommendation**: Proceed with fix — with these corrections:
1. Fix the deprecated path omission in Phase 3's Required Changes
2. Add explicit test file to Phase 3's Required Changes
3. Clarify "fully parallel" to note merge conflict risk between Phases 2 and 3
4. Correct the 4 factual errors identified above

## Reviewer Notes

This is an unusually high-quality implementation plan. 24 out of 28 code references are exact matches. The gotchas sections are particularly valuable — they identify real pitfalls (no logger field, double listener assignment, race conditions, deprecated path) that would catch an implementer off guard. The pattern-matching approach (follow `daemon_sync_state.go`) is sound and reduces implementation risk.

The main concern is that Phases 2 and 3 advertise as parallelizable but would produce merge conflicts. Recommend implementing Phase 1 first (it's the root fix), then Phase 2, then Phase 3 — sequential, not parallel.
