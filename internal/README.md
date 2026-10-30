# internal/

waker's packages. Dependencies point down this list. Nothing here is importable from outside the module.

| Package | What it is | Depends on |
|---|---|---|
| `config` | The YAML model: load, defaults, field-named validation, `Size`/`Duration` types. | – |
| `clock` | Real and fake clocks (the fake `Sleep` advances time, for tests). | – |
| `version` | The build version (stamped by `-ldflags`). | – |
| `idle` | The activity `Tracker` (in-flight counter, deadline from request end) and the deadline `Loop`. | clock |
| `state` | Atomic `modes.json` / `peaks.json`. | config |
| `boot` | The start/stop/health process supervisor (own process group, SIGTERM → SIGKILL). | config |
| `memory` | The headroom curve, the need/required `Budget`, `MemAvailable`, process-tree RSS, systemd `MemoryPeak`. | config |
| `app` | One app's runtime: effective mode, the boot state machine, dependencies (wake first, keep up), `Wake`/`Tick`/`Stop`/`SetMode`, `Snapshot`. | idle, state, clock, memory |
| `evict` | `Gate` (admission + eviction plan) implementing `app.Gate`. | app, memory |
| `pages` | The visitor pages (built-in, or the owner's files with placeholders filled) and assets (embedded Geist, logo, heartbeat.js, wait.js); `WithTag`, the one script-tag injector. | config |
| `proxy` | The per-app HTTP front: forwarding, waking, `/__waker/*`, heartbeat injection, connection cap. | app, pages |
| `control` | The unix-socket admin API and its client. | app |
| `daemon` | `waker serve`: wires every app (and its `depends_on`), listener, the gate, the sampler and the idle loop. | everything above |
| `testapp` | A fake upstream for tests; `testapp/fakeapp` is its binary, for the e2e test. | – |

**Extending:**
- A new memory source implements `memory.Available`.
- A new peak source is a case in `daemon.recordPeak`.
- A new visitor page is an entry in `pages.copyText` and `config.PageKeys` (a test keeps them equal), plus the state that shows it.
