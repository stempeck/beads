# Design: Fix bd Daemon Crash Loop on Docker Desktop Fakeowner Filesystems

## Executive Summary

The bd daemon crashes on Docker Desktop host mounts because `os.Chmod()` on Unix domain sockets returns `EINVAL` on fakeowner (VirtioFS/gRPC-FUSE) filesystems. The daemon treats this as fatal, exits, and external restarters re-launch it every ~5 seconds. After ~34 minutes of crash-looping (692+ restarts), rapid SQLite open/close cycles corrupt the database. The corrupted DB then rebuilds empty and overwrites `routes.jsonl` (now `issues.jsonl`) with empty data, silently destroying all issue data.

The fix is straightforward: make `chmod()` failure on the socket non-fatal. The daemon logs a warning and continues running. Defense-in-depth additions include startup backoff (circuit breaker) and distinct exit codes for "retry won't help" failures. All fixes are in the beads repo; no gastown changes are required for the immediate fix.

The core code change is 5 lines in `internal/rpc/server_lifecycle_conn.go`.

## Constraints Respected

All proposals in this design respect the following constraints:

- ✓ **C1: Backward compatible** — No existing behavior changes on native filesystems. Chmod still applies where it works.
- ✓ **C2: Works on fakeowner** — Daemon starts and stays running on Docker Desktop host mounts.
- ✓ **C3: Works on native** — Identical behavior to today on Linux ext4, macOS APFS, etc.
- ✓ **C4: No gastown changes required** — The immediate fix is entirely within the beads repo.
- ✓ **C5: No new flags/env vars required** — The daemon just works. No user configuration needed.

## Problem Statement

The bd daemon's RPC server startup sequence in `internal/rpc/server_lifecycle_conn.go:34-40` calls `os.Chmod(socketPath, 0600)` after binding the Unix socket. On fakeowner filesystems (Docker Desktop host bind mounts), this syscall returns `EINVAL` for Unix domain sockets. The error is treated as fatal, killing the daemon.

The cascading failure chain:
1. `chmod()` fails → daemon exits
2. External restarter re-launches daemon every ~5 seconds
3. Each restart opens SQLite, fails at chmod, exits
4. After 692+ rapid open/close cycles, SQLite database corrupts
5. Daemon detects corruption, rebuilds empty DB
6. Empty DB exports to JSONL, overwriting all existing issue data

## Proposed Design

### Overview

Make `os.Chmod()` failure on the Unix socket non-fatal. Log a warning. Continue serving. Add startup backoff as defense-in-depth against any future crash-loop scenario.

### Key Components

1. **Non-fatal chmod** — The core fix. `server_lifecycle_conn.go` logs a warning instead of returning an error when `os.Chmod()` on the socket fails.
2. **Startup backoff** — `daemon.go` reads `daemon-state.json` to detect rapid restarts and backs off exponentially before retrying.
3. **Distinct exit codes** — `daemon.go` exits with code 2 for "retry won't help" failures (filesystem limitations) so external restarters can make informed decisions.

### Component Dependency Graph

```
[Non-fatal chmod] ──► [Daemon stays running] ──► [Gastown health checks pass]
                                                         │
[Startup backoff] ──► [Crash-loop prevention] ──────────►│
                                                         │
[Exit codes] ──────► [External restarter decisions] ────►│
                                                         ▼
                                                    DAEMON HEALTHY
```

### Interface

No new CLI flags or environment variables. The daemon's existing interface is unchanged:

- `bd daemon start` — starts the daemon (now succeeds on fakeowner)
- `bd daemon status` — shows status (now shows "healthy" on fakeowner)
- `bd daemon health` — health check (now returns "healthy" on fakeowner)

Warning messages go to `daemon.log` only. No user-visible output changes.

### Data Model

**New file: `.beads/daemon-state.json`** (gitignored, internal only)

```json
{
  "starts": [
    {"timestamp": "2026-03-25T10:00:00Z", "version": "0.49.1", "exit_reason": "chmod_failed"}
  ],
  "backoff_until": "2026-03-25T10:05:00Z",
  "circuit_open": false
}
```

- Tracks last N daemon start attempts (bounded at 20 entries)
- Auto-clears entries older than 1 hour
- Read at startup, written at startup and on fatal exit

## Cross-Dimension Trade-offs

| Conflict | Resolution | Rationale |
|----------|------------|-----------|
| Security vs Fakeowner compatibility | Accept relaxed socket permissions on fakeowner | Docker containers are single-user. Chmod still applies on native filesystems. Threat model doesn't justify blocking daemon startup. |
| Simplicity vs Defense-in-depth | Core fix (5 lines) + optional hardening (backoff) | Phase 1 alone fixes the bug. Phases 2-3 prevent the class of bugs. |
| bd-only fix vs cross-repo fix | bd-only for immediate fix | Gastown changes are recommended but not required. No cross-repo coordination needed. |

## Trade-offs and Decisions

### Decisions Made

| Decision | Options Considered | Chosen | Rationale | Reversibility |
|----------|-------------------|--------|-----------|---------------|
| Chmod failure handling | Fatal error, Non-fatal warning, Skip-chmod flag, Auto-detect filesystem | Non-fatal warning | Chmod is defense-in-depth, not correctness. Crash is worse than relaxed permissions. No user action required. | Easy |
| Socket location | Keep in `.beads/` | Keep in `.beads/` | Socket creation and I/O work fine on fakeowner. Only `chmod()` fails. No need to move the socket to fix this. | Easy |
| Backoff mechanism | In-process only, Persistent state file, Reuse sync-state.json | New daemon-state.json | Daemon lifecycle state is distinct from sync state. Persistent state enables cross-process circuit breaking. | Easy |
| Gastown integration approach | Fix bd only, State file contract, Exit codes | Fix bd only (+ exit codes as cheap bonus) | Non-fatal chmod eliminates the root cause. Gastown sees healthy daemon. No coordination needed. | Easy |

### Open Questions

- [ ] **Q1**: Should `daemon-state.json` use the same backoff schedule as `sync-state.json` (30s, 1m, 2m, 5m, 10m, 30m) or a shorter schedule? Daemon restarts are faster than sync operations.
- [ ] **Q2**: Should the daemon log the fakeowner filesystem name specifically, or just log the `chmod()` error generically? Generic is simpler and covers unknown future filesystems.

## Risk Registry

| Risk | Severity | Likelihood | Mitigation | Phase |
|------|----------|------------|------------|-------|
| Multi-user container loses socket security | Low | Very Low | Warning logged. Chmod still works on native. Docker containers are single-user. | 1 |
| daemon-state.json write fails on fakeowner | Low | Very Low | Regular file I/O works on fakeowner (only socket chmod fails). | 2 |
| Stale daemon-state.json blocks daemon permanently | Medium | Low | Auto-clear entries older than 1 hour. `bd daemon start --force` bypasses backoff. | 2 |
| Gastown doesn't check exit codes (status quo) | Low | High (today) | bd fix alone resolves the fakeowner issue. Exit codes are bonus for future robustness. | 3 |
| Empty-DB-over-JSONL protection gaps | Medium | Low | Triple-redundant checks already exist at export.go, sync_export.go, daemon_sync.go. Verify they cover the crash-loop scenario. | 1 |

## Implementation Plan

### Phase Acceptance Criteria

### Phase 1: Non-Fatal Chmod (Effort: Low — ~30 minutes)

**Deliverables:**
1. Modified `internal/rpc/server_lifecycle_conn.go` — chmod failure logged as warning, daemon continues
2. Test: daemon starts successfully when chmod returns error

**Acceptance Criteria:**
- [ ] `os.Chmod()` failure on socket does NOT cause daemon to exit
- [ ] Warning message logged to daemon.log with error details
- [ ] On native filesystems, chmod still succeeds and applies 0600 permissions
- [ ] `bd daemon status` reports "healthy" when running with chmod warning
- [ ] Existing RPC tests pass unchanged

**Dependencies:** None — this is the root fix.

**Risks addressed:** Multi-user socket security (mitigated by warning). Empty-DB-over-JSONL (eliminated by preventing crash loop).

**Code change:**

```go
// internal/rpc/server_lifecycle_conn.go, lines 34-40
// BEFORE:
if runtime.GOOS != "windows" {
    if err := os.Chmod(s.socketPath, 0600); err != nil {
        _ = listener.Close()
        return fmt.Errorf("failed to set socket permissions: %w", err)
    }
}

// AFTER:
if runtime.GOOS != "windows" {
    if err := os.Chmod(s.socketPath, 0600); err != nil {
        s.logger.Warn("could not set socket permissions to 0600 (filesystem may not support chmod on sockets): %v", err)
    }
}
```

### Phase 2: Startup Backoff / Circuit Breaker (Effort: Medium — ~2 hours)

**Deliverables:**
1. New `cmd/bd/daemon_startup_state.go` — startup tracking and backoff logic
2. Modified `cmd/bd/daemon.go` — reads/writes startup state at daemon start
3. New `.beads/daemon-state.json` (gitignored) — persistent backoff state

**Acceptance Criteria:**
- [ ] 5+ daemon starts within 60 seconds triggers exponential backoff
- [ ] Backoff schedule: 5s, 15s, 30s, 1m, 5m, 15m (capped)
- [ ] Circuit breaker trips after 10 consecutive failures within 1 hour
- [ ] `bd daemon start` prints backoff message when circuit breaker is active
- [ ] State auto-clears after 1 hour of inactivity
- [ ] `bd daemon start --force` bypasses backoff (escape hatch)
- [ ] Successful daemon start resets all backoff state

**Dependencies:** None (independent of Phase 1, but Phase 1 makes this less critical).

**Risks addressed:** Stale state blocking daemon (mitigated by 1-hour TTL and --force flag).

### Phase 3: Distinct Exit Codes (Effort: Low — ~30 minutes)

**Deliverables:**
1. Modified `cmd/bd/daemon.go` — exit code 2 for filesystem/environment errors where retry won't help
2. Documentation of exit code semantics

**Acceptance Criteria:**
- [ ] Exit code 0: clean shutdown (signal, parent died)
- [ ] Exit code 1: generic error (retry may help)
- [ ] Exit code 2: environment error (filesystem limitation, missing binary, etc. — retry won't help)
- [ ] Exit codes documented in `docs/DAEMON.md`
- [ ] Existing daemon tests pass (exit code 0 and 1 paths already exist)

**Dependencies:** None (independent of Phases 1-2).

**Risks addressed:** Future gastown exit-code awareness (gastown changes are separate work).

## Appendix: Dimension Analyses

- [API Design](api.md)
- [Data Model](data.md)
- [User Experience](ux.md)
- [Scalability](scale.md)
- [Security](security.md)
- [Integration](integration.md)
- [Dependencies](dependencies.md)

## Appendix: Pre-Synthesis Constraint Audit

| Dimension | Recommendation | C1 | C2 | C3 | C4 | C5 | Status |
|-----------|---------------|----|----|----|----|----|----|
| API | Non-fatal chmod, no new flags | ✓ | ✓ | ✓ | ✓ | ✓ | PASS |
| Data | daemon-state.json for backoff | ✓ | ✓ | ✓ | ✓ | ✓ | PASS |
| UX | Single-line warning in daemon.log | ✓ | ✓ | ✓ | ✓ | ✓ | PASS |
| Scale | Backoff + non-fatal chmod | ✓ | ✓ | ✓ | ✓ | ✓ | PASS |
| Security | Accept default perms on fakeowner | ✓ | ✓ | ✓ | ✓ | ✓ | PASS |
| Integration | Fix bd only, gastown adapts naturally | ✓ | ✓ | ✓ | ✓ | ✓ | PASS |

## Appendix: Cross-Dimension Conflict Matrix

|              | API | Data | UX | Scale | Security | Integration |
|--------------|-----|------|----|-------|----------|-------------|
| **API**      | -   | ○    | ○  | ○     | ○        | ○           |
| **Data**     | ○   | -    | ○  | ○     | ○        | ○           |
| **UX**       | ○   | ○    | -  | ○     | ○        | ○           |
| **Scale**    | ○   | ○    | ○  | -     | ○        | ○           |
| **Security** | ○   | ○    | ○  | ○     | -        | ⚠           |
| **Integration** | ○ | ○   | ○  | ○     | ⚠        | -           |

Legend: ○ No conflict, ⚠ Tension (trade-off needed)

### Tension: Security vs Integration

- **Nature**: Security wants 0600 socket permissions. Integration (fakeowner) cannot provide them.
- **Resolution**: Accept relaxed permissions on fakeowner. Log warning. Chmod still applies on native.
- **Rationale**: Docker containers are single-user. The threat model doesn't justify blocking the daemon.
