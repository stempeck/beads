# Dimension 4: Scalability

## Summary

Scale concerns here are narrow: (1) the daemon crash-looping 692 times in 47 minutes is itself a scale/resource problem, and (2) any fix must not introduce per-request overhead. The daemon serves a single workspace — there are no multi-tenancy or throughput scaling concerns.

## Constraint Check

- [x] C1 (Backward compatible): No performance regression on native filesystems
- [x] C2 (Works on fakeowner): Eliminates crash-loop resource exhaustion
- [x] C3 (Works on native): Chmod succeeds immediately; no new overhead

## Options Explored

#### Option 1: Exponential backoff on daemon startup failures

- **Description**: Track failed starts in `daemon-state.json`. Schedule: 5s, 15s, 30s, 1m, 5m, 15m (capped). Circuit breaker at 5 consecutive failures.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Prevents resource exhaustion. Bounded retry behavior. Familiar pattern.
- **Cons**: Adds ~1 file read + 1 file write per daemon start attempt. Negligible overhead.
- **Effort**: Medium
- **Reversibility**: Easy

#### Option 2: No backoff — just fix chmod to be non-fatal

- **Description**: If chmod failure is the only cause of the crash loop, making it non-fatal eliminates the loop entirely. No backoff needed.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Simplest fix. Addresses the root cause directly.
- **Cons**: Only protects against this specific failure mode. Other fatal errors during startup could still cause crash loops if an external restarter is involved.
- **Effort**: Low
- **Reversibility**: Easy

### Recommendation

**Both.** Option 2 (non-fatal chmod) fixes this specific bug. Option 1 (backoff) is defense-in-depth against future startup failures causing the same crash-loop pattern. The backoff is cheap to implement given the existing `sync-state.json` pattern.

## Dependencies Produced

- Backoff schedule → Data dimension (stored in daemon-state.json)
- Circuit breaker state → Integration dimension (gastown health checks need to read it)

## Risks Identified

- **Risk: Backoff delays legitimate daemon restarts**: Severity: Low. Mitigation: Backoff only triggers after N consecutive failures. A successful start resets the counter. Manual `bd daemon start --force` bypasses backoff.

## Constraints Identified

- The backoff state file must be on the same filesystem as `.beads/` — it cannot use `/tmp/` because it needs to survive container restarts
- Backoff schedule should be short enough that a fixed daemon restarts within minutes, not hours
