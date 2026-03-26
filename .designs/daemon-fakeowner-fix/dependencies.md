# Dependency Graph

## File Dependencies

```
server_lifecycle_conn.go ──► daemon.go (chmod result determines daemon start/stop)
         │
         └──► daemon_server.go (RPC server health reflects chmod status)

daemon.go ──► daemon-state.json (reads/writes restart tracking)
    │
    └──► daemon_sync.go (sync backoff pattern reused for startup backoff)

daemon_sync_state.go ──► daemon.go (backoff pattern reference implementation)

export.go ──► sync_export.go (empty DB protection)
    │
    └──► integrity.go (pre-export validation)

sync_export.go ──► daemon_sync.go (daemon-side export uses same protection)
```

## Component Dependencies

| Component | Depends On | Provides To |
|-----------|------------|-------------|
| Non-fatal chmod | `server_lifecycle_conn.go` socket binding | Daemon stays running on fakeowner |
| Startup backoff | `daemon-state.json` (new), daemon lock | Crash-loop prevention |
| Exit codes | Daemon main loop error handling | External restarters (gastown) |
| JSONL protection | Existing empty-DB checks | Data integrity on all filesystems |
| Warning messages | Daemon logger (`daemon.log`) | User/admin visibility into limitations |

## Implementation Order (Critical Path)

```
Phase 1: Non-fatal chmod ──────────────────────────────►
         (server_lifecycle_conn.go)                     │
                                                        │
Phase 2: Startup backoff ──────────────────────────────►│
         (daemon.go + daemon-state.json)                │
                                                        │
Phase 3: Exit codes ───────────────────────────────────►│
         (daemon.go exit paths)                         │
                                                        ▼
                                                   ALL COMPLETE
```

Phases 1, 2, and 3 are independent and can be implemented in parallel. Phase 1 alone fixes the immediate problem. Phases 2 and 3 are defense-in-depth.

## Circular Dependency Check

No circular dependencies. Each component has a clear direction:

- `server_lifecycle_conn.go` → change flows outward to daemon behavior
- `daemon-state.json` → read at startup, written at shutdown/failure
- Exit codes → consumed downstream by external callers

## Minimum Viable Fix

**Phase 1 alone** (non-fatal chmod in `server_lifecycle_conn.go:34-40`) is sufficient to fix the reported issue. Everything else is hardening.
