IMPLREADME_PHASE1.md IMPORTANT! DO NOT modify the IMPLREADME! It should be
used as the source-of-truth for implementation details and can only be modified
if I explicitly ask.

## Requirements

The `beads` CLI (`bd`) includes a daemon mode that communicates over Unix domain sockets via an RPC layer (`internal/rpc/`). On startup, the daemon creates a Unix socket and attempts to `chmod` it to `0600`. On Docker Desktop host-mounted filesystems (fakeowner), `os.Chmod` on Unix sockets returns `EINVAL`, which the current code treats as a fatal error — closing the listener and returning an error that kills the daemon. This causes a crash loop.

---

### Scenario

**What currently exists:**

In `internal/rpc/server_lifecycle_conn.go`, the `Start()` method (specifically lines 34-40) treats `os.Chmod` failure on the socket as fatal:

```go
// Set socket permissions to 0600 for security (owner only)
if runtime.GOOS != "windows" {
    if err := os.Chmod(s.socketPath, 0600); err != nil {
        _ = listener.Close()
        return fmt.Errorf("failed to set socket permissions: %w", err)
    }
}
```

When `os.Chmod` returns `EINVAL` (as it does on fakeowner filesystems), the listener is closed and the daemon exits. The daemon restarter then tries again, creating a crash loop.

**What needs to change:**

The `os.Chmod` failure must become non-fatal. Instead of closing the listener and returning an error, log a warning to stderr and continue. This follows the established pattern used elsewhere in the same codebase:

- `server_lifecycle_conn.go:111` — `fmt.Fprintf(os.Stderr, "Warning: failed to close default storage: %v\n", closeErr)`
- `server_export_import_auto.go:199` — `fmt.Fprintf(os.Stderr, "Warning: failed to set file permissions: %v\n", err)`
- `server_lifecycle_conn.go:150` — `_ = os.Chmod(dir, 0700)` (best-effort, error discarded)

**Why it matters:**

Docker Desktop users running `bd` against host-mounted directories cannot use daemon mode at all. The fix is a 3-line change (remove 2 lines, modify 1) that eliminates the crash loop while maintaining the chmod attempt for filesystems that support it.

**End state:**

The daemon starts successfully on all filesystems. On fakeowner, a single warning line appears in daemon.log via stderr. The socket has default permissions (typically 0755 on fakeowner). This is acceptable because Docker containers are single-user environments.

---

### Design Context

This phase is part of a larger architecture change documented in:
- **Design doc**: `.designs/daemon-fakeowner-fix/design-doc.md`
- **Full outline**: `.designs/daemon-fakeowner-fix/implementation-plan/implementation_plan_outline.md`
- **Phase**: 1 of 3 — "Non-Fatal Chmod"
- **Prerequisites**: None — this is the root fix.

**Key design references** (read these for context on design decisions):
| Document | Section | Lines | What It Specifies |
|----------|---------|-------|-------------------|
| design-doc.md | Phase 1: Non-Fatal Chmod | L123-158 | Core fix: log warning instead of fatal error on chmod failure |
| api.md | Option 1: Silent fallback | L15-22 | Rationale for non-fatal approach (no flags, no env vars) |
| security.md | Option 1: Accept default socket permissions | L15-22 | Threat model: Docker containers are single-user, relaxed perms acceptable |
| ux.md | Option 1: Single-line warning | L15-21 | Warning format: single line to daemon.log |
| dependencies.md | Component Dependencies | L26-31 | Non-fatal chmod -> daemon stays running -> health checks pass |

---

### Files to Modify

#### `internal/rpc/server_lifecycle_conn.go`

**Current state** (verified 2026-03-26):
```go
// Lines 34-40:
	// Set socket permissions to 0600 for security (owner only)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(s.socketPath, 0600); err != nil {
			_ = listener.Close()
			return fmt.Errorf("failed to set socket permissions: %w", err)
		}
	}
```

**Required change**: Remove the `listener.Close()` and `return fmt.Errorf(...)` lines. Replace the error handling body with a stderr warning using the established codebase pattern. The `if runtime.GOOS != "windows"` guard and the `os.Chmod` call remain — only the error handling changes.

Replace lines 34-40 with:
```go
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

---

### Acceptance Criteria

The ONLY successful outcome is that ALL of the following pass:

1. **Code compiles**
   ```bash
   go build ./internal/rpc/
   # Expected: no errors
   ```

2. **Chmod error handling is non-fatal (no listener.Close on chmod error path)**
   ```bash
   grep -A3 'os.Chmod(s.socketPath' internal/rpc/server_lifecycle_conn.go | grep -c 'listener.Close'
   # Expected: 0
   ```

3. **Warning message is present**
   ```bash
   grep -c 'could not set socket permissions' internal/rpc/server_lifecycle_conn.go
   # Expected: 1
   ```

4. **No fatal return on chmod error path**
   ```bash
   grep -A3 'os.Chmod(s.socketPath' internal/rpc/server_lifecycle_conn.go | grep -c 'return fmt.Errorf'
   # Expected: 0
   ```

5. **Existing RPC tests pass**
   ```bash
   go test ./internal/rpc/...
   # Expected: PASS
   ```

6. **Full test suite passes**
   ```bash
   go test -short ./...
   # Expected: PASS
   ```

### Environment

- **Build**: `make build` or `go build -o bd ./cmd/bd`
- **Test (fast)**: `go test -short ./...`
- **Test (full)**: `go test ./...`
- **Test (single package)**: `go test -v ./internal/rpc/...`
- **Lint**: `golangci-lint run ./...` (ignore baseline warnings per `docs/LINTING.md`)

### Gotchas

- **Server struct has no logger field.** The design doc's code sketch uses `s.logger.Warn(...)` but the `Server` struct in `server_core.go:27-66` has no logger — it's a 40-field struct that uses `os.Stderr` for all warnings. The correct approach is `fmt.Fprintf(os.Stderr, ...)`. The daemon-level logger captures stderr, so warnings appear in `daemon.log`.
- **The `listener` is assigned to `s.listener` twice.** Line 32 assigns before chmod; line 44 assigns under lock. With the fatal chmod removed, both assignments are to the same valid listener. This double-assignment is harmless but explains why removing `listener.Close()` is safe — the listener at L32 is the same object used at L44.
- **`ensureSocketDir()` at L144-152 already does best-effort chmod.** Line 150: `_ = os.Chmod(dir, 0700)` on the directory, error discarded. The socket chmod fix follows this exact same pattern — the codebase already treats directory chmod as non-fatal.
- **The health endpoint checks DB responsiveness, not chmod status.** `handleHealth` at L340-395 returns "healthy"/"degraded"/"unhealthy" based on `GetStatistics` DB ping. It will correctly return "healthy" when the daemon runs with relaxed socket permissions, because health is defined by DB availability.
- **All required imports (`fmt`, `os`, `runtime`) already exist** in `server_lifecycle_conn.go` lines 3-15. No import changes needed.

### Peer Review Corrections

- **Claim 5 (`daemon_server.go` L14-34)**: Original outline said function spans L14-34. Peer review found it extends to L40. The function at L14-40 creates the Server, starts it in a goroutine, and routes errors to `serverErrChan`. This is context-only for Phase 1 — no modifications to this file are required.
