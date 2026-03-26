# Dimension 3: User Experience

## Summary

UX here is about what the user sees when the daemon encounters fakeowner limitations. Today: silence followed by mysterious failures. Goal: clear diagnostic messages that explain what happened and what (if anything) the user should do.

## Constraint Check

- [x] C1 (Backward compatible): Warning messages are additive; no existing output changes
- [x] C2 (Works on fakeowner): Messages explain fakeowner-specific limitations
- [x] C3 (Works on native): No new output on native filesystems

## Options Explored

#### Option 1: Single-line warning on chmod failure

- **Description**: When chmod fails, log: `warning: could not set socket permissions (fakeowner/VirtioFS filesystem?): <error>. Continuing with default permissions.`
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Concise. Non-alarming. Provides actionable context (filesystem type).
- **Cons**: May not be visible if daemon runs in background (goes to daemon.log only).
- **Effort**: Low
- **Reversibility**: Easy

#### Option 2: Structured diagnostic with remediation hints

- **Description**: Log a multi-line diagnostic block:
  ```
  ⚠ Socket chmod failed: EINVAL on .beads/bd.sock
    Filesystem: fakeowner (Docker Desktop host mount)
    Impact: Socket accessible to all users in container (usually single-user, safe)
    Action: None required. Daemon running normally.
  ```
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Self-documenting. User understands impact and doesn't need to search docs.
- **Cons**: Verbose for a warning that appears every daemon start. Multi-line in daemon.log.
- **Effort**: Low
- **Reversibility**: Easy

#### Option 3: Log warning + `bd daemon status` shows filesystem info

- **Description**: Warning in daemon.log (Option 1 style) plus `bd daemon status` output includes a line: `Socket security: reduced (chmod not supported on this filesystem)`.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Warning is quiet (log only), but actively queryable via status command.
- **Cons**: Slightly more code to plumb chmod status through to status response.
- **Effort**: Medium
- **Reversibility**: Easy

### Recommendation

**Option 1: Single-line warning.** The user is an AI agent or developer inside a Docker container. They don't need a multi-line explainer every startup. One clear line in daemon.log is sufficient. If someone investigates daemon issues, the warning is there.

Option 3 is nice-to-have but adds complexity for a rare edge case that most users never query.

### Circuit Breaker UX

When the daemon detects rapid restart patterns and trips the circuit breaker:

```
error: daemon circuit breaker tripped (5 starts in 47 seconds)
  Last failure: chmod failed on .beads/bd.sock (EINVAL)
  Next attempt: in 2m30s (backoff active)
  Workaround: use bd --no-daemon for direct database access
```

This is critical — without this message, external restarters (gastown) will keep hammering the daemon with no feedback.

## Dependencies Produced

- Warning message text → API dimension (must not break `--json` output parsing)
- Circuit breaker message → Integration dimension (gastown parses daemon start output)

## Risks Identified

- **Risk: Warning in daemon.log not seen because daemon.log is noisy**: Severity: Low. Mitigation: Use structured log level (WARN) so it's filterable. The existing `!BADKEY` logging bug already makes the log noisy — this is a separate fix.

## Constraints Identified

- Warnings go to daemon.log (stderr), never to stdout (which carries RPC responses)
- `--json` output from `bd daemon status` should include a `chmod_supported` or `socket_security` field if we go with Option 3
