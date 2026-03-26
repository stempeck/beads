# Dimension 6: Integration

## Summary

The fix must integrate with three systems: (1) the bd daemon itself (this repo), (2) gastown's daemon management code (separate repo), and (3) Docker Desktop's filesystem behavior. The critical integration concern is that gastown's health checks and restart loops must not worsen the situation.

## Constraint Check

- [x] C1 (Backward compatible): Existing daemon management code continues to work unchanged
- [x] C2 (Works on fakeowner): Daemon starts and stays running on fakeowner
- [x] C3 (Works on native): No behavioral change on native filesystems

## Options Explored

#### Option 1: Fix bd daemon only (this repo), gastown adapts naturally

- **Description**: Make chmod non-fatal in bd. The daemon starts and stays running. Gastown's health checks see a healthy daemon. No gastown changes needed.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Single-repo fix. Gastown's existing code works because the daemon is now healthy. No coordination needed.
- **Cons**: Doesn't address the general problem of gastown restarting a failing daemon without backoff. If a DIFFERENT failure causes crash loops in the future, the same pattern repeats.
- **Effort**: Low (bd changes only)
- **Reversibility**: Easy

#### Option 2: Fix bd daemon + add backoff state file that gastown reads

- **Description**: bd writes `daemon-state.json` with backoff/circuit-breaker state. Gastown reads this before attempting restarts.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Defense-in-depth. Even if a new failure mode appears, the backoff prevents crash loops.
- **Cons**: Requires gastown changes to read the state file. Cross-repo coordination. The state file format becomes a contract between two repos.
- **Effort**: Medium (bd) + Medium (gastown)
- **Reversibility**: Moderate — removing the state file contract requires coordinated changes

#### Option 3: Fix bd daemon + add exit codes for different failure modes

- **Description**: bd daemon exits with specific codes: 0 (clean), 1 (generic error), 2 (filesystem limitation — retry won't help), 3 (version mismatch). Gastown uses exit codes to decide whether to retry.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Standard Unix pattern. Exit codes are a stable contract. No state files.
- **Cons**: Gastown must be updated to check exit codes. Only useful if the daemon actually exits — if chmod is non-fatal, exit code 2 is never reached (which is the goal).
- **Effort**: Low (bd) + Low (gastown)
- **Reversibility**: Easy

### Recommendation

**Option 1 primarily, with elements of Option 3 as cheap insurance.**

If chmod is non-fatal (the core fix), the daemon starts and runs. Gastown sees a healthy daemon. Problem solved at the source. No cross-repo coordination needed for the immediate fix.

Option 3 (exit codes) is worth adding as a cheap improvement: when the daemon DOES exit fatally, the exit code should distinguish "retry might help" (exit 1) from "retry won't help" (exit 2). This is a one-line change that pays dividends if future failures emerge.

Option 2 (state file contract) is premature. The daemon-state.json is useful WITHIN bd for its own backoff, but making it a cross-repo API is over-engineering until there's a second consumer.

### Gastown-Side Recommendations (separate repo, not designed here)

These are notes for whoever addresses the gastown side:

1. **`EnsureBdDaemonHealth()` should have a backoff**: Don't restart the daemon more than 3 times in 5 minutes. After that, log an error and stop trying.
2. **`restartBdDaemons()` should check exit code**: If bd exits with code 2 (filesystem limitation), don't retry.
3. **353 bare `exec.Command("bd", ...)` calls**: These should use `--no-daemon` or go through the `internal/beads/beads.go` wrapper that already adds `--no-daemon` (line 478). This is a separate cleanup task.

## Dependencies Produced

- Non-fatal chmod → daemon stays running → gastown health checks pass (no integration work needed)
- Exit codes → gastown can be updated independently later
- daemon-state.json → internal to bd only; not a cross-repo contract

## Risks Identified

- **Risk: Gastown restarts daemon that is running but degraded (chmod warning)**: Severity: Low. Mitigation: The daemon reports "healthy" status via RPC. Gastown's health checks see "healthy". No restart triggered.
- **Risk: Future bd changes break gastown assumptions**: Severity: Medium. Mitigation: Exit codes provide a stable contract. Health response format is already a de facto API.

## Constraints Identified

- The fix must work WITHOUT gastown changes. Gastown changes are nice-to-have improvements for future robustness.
- The daemon health response must report "healthy" even when chmod failed — because the daemon IS healthy, just with relaxed socket permissions.
