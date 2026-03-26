# Dimension 2: Data Model

## Summary

The data model concerns here are about protecting existing data (SQLite DB, JSONL) from corruption caused by rapid daemon restarts — not about adding new data structures. The key question is whether the existing empty-DB-over-JSONL protections are sufficient, and whether additional state tracking is needed for restart backoff.

## Constraint Check

- [x] C1 (Backward compatible): No schema changes, no migration needed
- [x] C2 (Works on fakeowner): Data protections apply regardless of filesystem
- [x] C3 (Works on native): Same protections, no behavioral regression

## Options Explored

### A. JSONL Overwrite Protection

#### Option A1: Rely on existing triple-redundant checks

- **Description**: The codebase already has empty-DB-over-JSONL protections at three layers: `export.go:329-347`, `sync_export.go:179-193`, and `daemon_sync.go:86-99`. Additionally, `integrity.go:144-188` has pre-export validation. Trust these.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: No code changes needed in the data layer. The checks already exist.
- **Cons**: The incident report says routes.jsonl WAS destroyed, meaning either: (a) these checks didn't exist at v0.49.1, or (b) there's a code path that bypasses them.
- **Effort**: None (verification only)
- **Reversibility**: N/A

#### Option A2: Add a JSONL backup before any export

- **Description**: Before writing `issues.jsonl`, copy the existing file to `issues.jsonl.bak`. Rotate backups (keep last 3).
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Belt-and-suspenders protection. Even if all three checks fail, data is recoverable.
- **Cons**: Extra disk I/O on every export. Backup files in `.beads/` could confuse users. Git might track `.bak` files if not gitignored.
- **Effort**: Low
- **Reversibility**: Easy

#### Option A3: Refuse to open DB if corruption detected from prior rapid restarts

- **Description**: Track daemon start timestamps. If N starts within M seconds, refuse to open the database and log a circuit-breaker message. This prevents the corruption from happening in the first place.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Prevents the root cause (rapid open/close cycles causing corruption). No data loss scenario.
- **Cons**: Requires persisting start timestamps somewhere. If the daemon can't start, it can't serve CLI requests either (though `--no-daemon` fallback exists).
- **Effort**: Medium
- **Reversibility**: Easy

### Recommendation

**Option A1 (trust existing checks) + Option A3 (circuit breaker on rapid restarts).**

The existing empty-DB checks are solid and cover all export paths. The incident likely occurred on v0.49.1 before these checks were added (or on a code path that has since been fixed). Verify this by checking git blame on the protection code.

Option A3 addresses the root cause: if the daemon never crash-loops, the DB never corrupts, and JSONL is never at risk.

### B. Restart State Tracking

#### Option B1: Use existing sync-state.json pattern

- **Description**: Extend `.beads/sync-state.json` (already used for sync backoff) to also track daemon start attempts. Add fields: `daemon_start_count`, `daemon_last_start`, `daemon_backoff_until`.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Reuses existing infrastructure. Same file, same read/write pattern.
- **Cons**: Mixing sync state with daemon state in one file. Sync state is cleared on success; daemon state has different lifecycle.
- **Effort**: Low
- **Reversibility**: Easy

#### Option B2: New `.beads/daemon-state.json`

- **Description**: Separate file for daemon lifecycle state: start count, last start time, crash timestamps, backoff until.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Clean separation of concerns. Can evolve independently.
- **Cons**: Another file in `.beads/`. Must be gitignored.
- **Effort**: Low
- **Reversibility**: Easy

#### Option B3: No persistent state — use in-process tracking only

- **Description**: Don't persist restart tracking. The daemon is a single process — if it crashes, the next invocation has no memory of prior crashes.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Simplest. No new files.
- **Cons**: Cannot implement cross-process circuit breaker. Each daemon start is independent. Only works if the daemon itself handles the backoff (never applies to external restarters like gastown).
- **Effort**: None
- **Reversibility**: N/A

### Recommendation

**Option B2: New `.beads/daemon-state.json`.** Daemon lifecycle state is distinct from sync state. The file should track:

```json
{
  "starts": [
    {"timestamp": "2026-03-25T10:00:00Z", "version": "0.49.1", "exit_reason": "chmod_failed"},
    {"timestamp": "2026-03-25T10:00:05Z", "version": "0.49.1", "exit_reason": "chmod_failed"}
  ],
  "backoff_until": "2026-03-25T10:05:00Z",
  "circuit_open": true
}
```

At startup, the daemon reads this file. If it sees N starts within M seconds (e.g., 5 starts in 60 seconds), it backs off exponentially and eventually trips a circuit breaker.

## Dependencies Produced

- `daemon-state.json` format → Integration dimension (gastown must understand backoff state)
- Circuit breaker decision → API dimension (daemon start command must communicate "backing off" state)

## Risks Identified

- **Risk: daemon-state.json on fakeowner filesystem has write issues**: Severity: Low. Mitigation: This file uses regular file I/O (not Unix sockets), which works on fakeowner. Only `chmod()` on sockets fails.
- **Risk: Stale daemon-state.json blocks daemon start permanently**: Severity: Medium. Mitigation: Auto-clear state older than 1 hour (same pattern as sync-state.json's 24-hour TTL).

## Constraints Identified

- `.beads/daemon-state.json` must be gitignored (add to `.beads/.gitignore` if not already covered by `beads.db` pattern)
- Start history should be bounded (keep last 20 entries, roll old ones off)
