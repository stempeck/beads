# bd daemon crashes on Docker Desktop host mounts (fakeowner filesystem)

## Investigation Notes

The bd daemon cannot run inside Docker containers that use Docker Desktop host bind mounts. The daemon starts, opens the SQLite database, creates its RPC Unix socket at `.beads/bd.sock`, then calls `chmod()` on the socket to tighten permissions. On `fakeowner` filesystems (Docker Desktop's VirtioFS/gRPC-FUSE translation layer for macOS host mounts), `chmod()` on Unix domain sockets returns `EINVAL`. The daemon treats this as fatal, exits, and restarts every ~5 seconds with no backoff — eventually corrupting the SQLite database through rapid open/close cycles.

This means the daemon is completely unusable in the primary gastown deployment target (Docker containers with host-mounted `.beads/` directories). Health checks, RPC-based operations, and any daemon-dependent functionality are all dead.

## Affected Layers & Files

**bd daemon (external Go binary — not in this repo)**
- `/home/dev/go/bin/bd` (v0.49.1) — The daemon binary. Contains the RPC server startup, Unix socket creation, `chmod()` call, and restart loop logic. This is where the fix needs to happen.

**Beads database (data files on the host mount)**
- `.beads/beads.db` — SQLite database. Opened on every daemon start attempt. After ~34 minutes of crash-looping (692+ restarts), the rapid open/close cycles corrupt it ("database disk image is malformed").
- `.beads/beads.db.corrupt.bak` — Created when the daemon detects corruption and rebuilds a fresh empty DB.
- `.beads/routes.jsonl` — JSONL export. Overwritten with empty data when the freshly-rebuilt DB does its first `bd sync --flush-only`, destroying the previous export that contained all issue data.
- `.beads/bd.sock` — The Unix domain socket that triggers the crash. Created by the daemon, then `chmod()` fails on it.
- `.beads/daemon.log` — Daemon log. In the observed incident: 6168 lines, 692 daemon starts, 9 "malformed" errors over 47 minutes.

**Filesystem layer**
- `/home/dev/gt` is mounted as `fakeowner` type (Docker Desktop host bind mount via `/run/host_mark/Users`). Regular file operations (read, write, create, delete) work. `chmod()` on regular files may silently no-op. `chmod()` on Unix domain sockets returns `EINVAL`.

**Gastown daemon management code (consumers of bd daemon)**
- `internal/beads/daemon.go` — `EnsureBdDaemonHealth()` (called from `cmd/status.go:195`), `CheckBdDaemonHealth()`, `restartBdDaemons()`, `StartBdDaemonIfNeeded()`. This code assumes the daemon can run. On fakeowner, it can't, so health checks always report failure or trigger more restarts.
- `internal/doctor/bd_daemon_check.go` — Doctor check that calls `bd daemon --status`, `bd daemon --health`, and tries `bd daemon --start`. Detects corruption in error output but only suggests manual repair. On fakeowner, the auto-start just feeds the crash loop.

### Additional Context

- **The socket location is the root cause.** Unix domain sockets should not live on network/translated filesystems. PostgreSQL, MySQL, and Redis all put their sockets in `/tmp/`, `/var/run/`, or a tmpfs — never in the data directory. The bd daemon co-locates its socket with its data in `.beads/`, which is on the host mount.
- **`chmod()` on the socket is a hardening step, not a functional requirement.** The daemon works without it — `bd --no-daemon` and `bd --sandbox` both bypass the RPC server entirely and function correctly. The socket permissions are defense-in-depth for multi-user systems, not a correctness requirement.
- **Go binaries bypass libc.** `LD_PRELOAD` interception of `chmod()` does not work on Go binaries because Go makes syscalls directly via assembly, never calling libc. There is no practical way to intercept this from outside the bd binary.
- **No backoff on restart.** The daemon retries every ~5 seconds unconditionally. There is no exponential backoff, circuit breaker, or max-restart limit. On fakeowner, this means it will crash-loop indefinitely.
- **`routes.jsonl` destruction is silent.** When the daemon rebuilds a fresh empty DB after corruption, it runs `bd sync --flush-only` which overwrites `routes.jsonl` with the empty DB's contents. There is no backup, no rename, and no log message indicating data was lost. In the observed incident, all 12 molecule steps from a live `gherkin-soldesign-plan` execution were silently destroyed.
- **The `!BADKEY` in daemon log messages** (`"Daemon started (interval: %v, auto-commit: %v, auto-push: %v)" !BADKEY=5s`) indicates the daemon uses a structured logger (key-value) but passes positional `%v` format args. This is a separate logging bug.
- **353 bare `exec.Command("bd", ...)` calls** exist across gastown production code without `--no-daemon`. These all hit the daemon when it's running (and crash-looping). The `internal/beads/beads.go` wrapper adds `--no-daemon` at line 478, but `internal/mail/bd.go:45` and most of `internal/cmd/` do not.

### Acceptance Criteria

- bd daemon starts and serves RPC requests inside Docker containers with fakeowner host mounts
- If `chmod()` on the Unix socket fails, the daemon logs a diagnostic message identifying the filesystem limitation and continues running (does not exit)
- Alternatively: the daemon places its socket on a filesystem that supports `chmod()` (e.g., `/tmp/bd-<workspace-hash>.sock`) and clients can locate it
- Rapid daemon restarts (N+ within M seconds) trigger exponential backoff or a circuit breaker
- Database corruption from rapid open/close cycles does not occur — either the restart loop is prevented, or the DB is not opened when the RPC server is known to be non-functional
- `routes.jsonl` is never overwritten by a freshly-rebuilt empty database without first preserving the existing export (rename, backup, or refusal to overwrite)
- Gastown's daemon management code (`internal/beads/daemon.go`, `internal/doctor/bd_daemon_check.go`) does not trigger or worsen crash loops on fakeowner filesystems
