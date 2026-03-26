# Dimension 1: API & Interface Design

## Summary

The daemon's failure on fakeowner filesystems exposes no new user-facing API. The fixes are primarily behavioral changes in existing codepaths. However, two interface decisions matter: (1) how the daemon communicates filesystem limitations to the user, and (2) whether new flags or environment variables are needed to control fallback behavior.

## Constraint Check

- [x] C1 (Backward compatible): No existing CLI flags or env vars change meaning
- [x] C2 (Works on fakeowner): API changes specifically enable fakeowner operation
- [x] C3 (Works on native): No behavioral change on filesystems where chmod succeeds

## Options Explored

#### Option 1: Silent fallback (chmod failure → log warning, continue)

- **Description**: When `os.Chmod()` on the socket fails, log a warning with filesystem diagnostic info and continue. No new flags. No user action required.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Zero user friction. Daemon just works. No configuration sprawl.
- **Cons**: User might not notice reduced socket security. Warning could be noisy if it logs every startup.
- **Effort**: Low
- **Reversibility**: Easy — can always make it fatal again

#### Option 2: New `--skip-chmod` flag

- **Description**: Add `--skip-chmod` flag to `bd daemon start` that skips the chmod call entirely. Default behavior remains fatal.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Explicit opt-in. User knows what they're choosing.
- **Cons**: Users must discover and set this flag. Every Docker deployment must pass it. Adds cognitive load per CLI design principles in AGENT_INSTRUCTIONS.md. Doesn't help the gastown code that calls `bd daemon --start` without knowing about fakeowner.
- **Effort**: Low
- **Reversibility**: Easy

#### Option 3: Environment variable `BD_SOCKET_CHMOD=warn`

- **Description**: New env var `BD_SOCKET_CHMOD` with values `fatal` (default), `warn`, `skip`. Controls chmod behavior.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Configurable without CLI changes. Works in Docker compose files.
- **Cons**: Another env var to document. Same problem as Option 2 — requires user action. Over-engineered for what is a single `if err != nil` branch.
- **Effort**: Low
- **Reversibility**: Easy

#### Option 4: Auto-detect fakeowner and adapt

- **Description**: At daemon startup, detect the filesystem type of `.beads/`. If it's `fakeowner`/VirtioFS, automatically treat chmod failures as warnings. On native filesystems, keep chmod fatal.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Zero user action. Correct behavior on both filesystem types. Self-documenting via log messages.
- **Cons**: Filesystem detection adds complexity. `fakeowner` detection may be fragile across Docker Desktop versions. Syscall (`statfs`) may have its own edge cases.
- **Effort**: Medium
- **Reversibility**: Easy

### Recommendation

**Option 1: Silent fallback.** The chmod is defense-in-depth for multi-user systems. Docker containers are typically single-user. Making chmod failure non-fatal (with a clear warning) is the right default for ALL environments, not just fakeowner. If chmod fails on a native filesystem, the user still gets a working daemon plus a warning — which is strictly better than a crash loop.

Option 4 (auto-detect) adds complexity for no real benefit over Option 1. If chmod fails, it fails — the reason doesn't matter for the daemon's decision to continue.

## Dependencies Produced

- Warning message format → UX dimension (how to communicate filesystem limitation)

## Risks Identified

- **Risk: Users on multi-user systems lose chmod protection without knowing**: Severity: Low. Mitigation: Warning message clearly states "socket permissions could not be tightened" so admin can investigate.

## Constraints Identified

- The daemon should never require user intervention to start on a supported platform. Flags/env vars that must be set are anti-patterns for a tool designed for AI agents.
