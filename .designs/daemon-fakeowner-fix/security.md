# Dimension 5: Security

## Summary

The chmod on the Unix socket is a security hardening measure (0600 = owner-only access). Making it non-fatal means the socket may have broader permissions on some filesystems. This analysis evaluates the real-world threat model.

## Constraint Check

- [x] C1 (Backward compatible): On native filesystems, chmod still succeeds → no security change
- [x] C2 (Works on fakeowner): Socket runs with default permissions instead of 0600
- [x] C3 (Works on native): Identical security posture to today

## Options Explored

#### Option 1: Accept default socket permissions on fakeowner

- **Description**: When chmod fails, the socket has whatever permissions the `net.Listen("unix", path)` call assigned (typically 0755 or umask-dependent). Accept this.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: No code complexity. Socket works. Docker containers are typically single-user.
- **Cons**: In multi-user containers, other users could connect to the daemon and manipulate issues. This is unlikely in practice — Docker containers rarely have multiple human users.
- **Effort**: Low
- **Reversibility**: Easy

#### Option 2: Set umask before socket creation

- **Description**: Call `syscall.Umask(0077)` before `net.Listen("unix", ...)` to ensure the socket is created with 0600-ish permissions from the start. Then chmod is unnecessary.
- **Constraint Compliance**: ✓ C1, ✓ C2, ✓ C3
- **Pros**: Socket created with correct permissions. No post-creation chmod needed.
- **Cons**: `Umask` is process-global in Go. Could affect other goroutines creating files concurrently (daemon log, state files). Would need a mutex or careful ordering. On fakeowner, umask may also be ignored for sockets — the filesystem translation layer may not honor it.
- **Effort**: Medium
- **Reversibility**: Easy

### Recommendation

**Option 1: Accept default permissions, with logging.** The threat model does not justify Option 2's complexity:

1. **Docker containers are single-user.** The attack surface of "another user connects to the daemon socket" is near-zero.
2. **The daemon only manages issue tracking data.** Even if compromised, the blast radius is issue metadata — not credentials, not code, not infrastructure.
3. **The socket is in `.beads/` which is gitignored.** It's not accessible outside the container.
4. **The directory permissions (0700) may still be enforced**, limiting access even if the socket itself has broader permissions.

## Dependencies Produced

- Security posture decision → UX dimension (warning message must convey the security implication)

## Risks Identified

- **Risk: Security audit flags "socket without chmod"**: Severity: Low. Mitigation: Log message documents the decision. Code comment explains the threat model. The chmod still succeeds on native filesystems.
- **Risk: Future multi-user Docker use case**: Severity: Low. Likelihood: Very low.

## Constraints Identified

- On native filesystems, chmod must continue to succeed and apply 0600. The non-fatal behavior is only for the failure case.
- The security relaxation is logged, not silent. Auditors can grep daemon.log for the warning.
