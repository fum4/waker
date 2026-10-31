# internal/

waker's packages. Dependencies point down this list. Nothing here is importable from outside the module.

| Package | What it is | Depends on |
|---|---|---|
| `schedule` | Pure cron-window math: `Active`, `At` (current window's end, merged, or the next start), `Boundary`. Wraps robfig/cron. | – |
| `yamledit` | `Set`: one key of a top-level mapping, edited in the document's text (every other byte stays), then re-parsed and checked to change only that key. | – |
| `config` | The YAML model: load, defaults, field-named validation, `Size`/`Duration` types; `EditService` / `WriteFile` save flags to the `service:` block, refusing a `# managed: <tool>` file. | schedule, yamledit |
| `service` | `waker service`: plans a systemd unit or launchd plist install/uninstall as files + commands (`Plan`, printed for `--dry-run`, else `Apply`), `Status`, `CheckDrift` (installed unit vs the one install would write), `LookPath` against a given PATH. A `Host` (fake in tests, with a `Root` prefix) and a `Runner` keep it off the real machine in tests. | – |
| `prompt` | The terminal prompt `waker init` uses: `Ask` (label, dim default, inline validation, a ✔ redraw), `Confirm`, `IsTerminal`; ANSI off with `NO_COLOR`. Line-based, no dependency. | – |
| `scaffold` | The starter config: `Answers`, their validators, `SplitArgs` (shell-like words → argv) and `Render` (the house-style YAML: header, fixed key order, aligned values and comments; golden-tested). | config |
| `clock` | Real and fake clocks (the fake `Sleep` advances time, for tests). | – |
| `version` | The build version (stamped by `-ldflags`). | – |
| `idle` | The activity `Tracker` (in-flight counter, deadline from request end) and the deadline `Loop`. | clock |
| `state` | Atomic `modes.json` / `peaks.json`. | config |
| `boot` | The start/stop/health process supervisor (own process group, SIGTERM → SIGKILL). | config |
| `memory` | The headroom curve, the need/required `Budget`, available memory (`Meminfo` on Linux, `VMStat` on macOS, picked by `ForSource`), process-tree RSS, systemd `MemoryPeak`. | config |
| `app` | One app's runtime: effective mode (override > schedule window > declared), the boot state machine, dependencies (wake first, keep up), `Wake`/`Tick`/`Stop`/`SetMode`, `Snapshot`. | idle, state, clock, memory, schedule |
| `evict` | `Gate` (admission + eviction plan) implementing `app.Gate`. | app, memory |
| `pages` | The visitor pages (built-in, or the owner's files with placeholders filled) and assets (embedded Geist, logo, heartbeat.js, wait.js); `WithTag`, the one script-tag injector. | config |
| `proxy` | The per-app HTTP front: forwarding, waking, `/__waker/*`, heartbeat injection, connection cap. | app, pages |
| `control` | The unix-socket admin API and its client. | app |
| `daemon` | `waker serve`: wires every app (and its `depends_on`), listener, the gate, the sampler and the idle loop. | everything above |
| `testapp` | A fake upstream for tests; `testapp/fakeapp` is its binary, for the e2e test. | – |

**Extending:**
- A new available-memory source implements `memory.Available` and is a case in `memory.ForSource`. Name it in `config.PlatformFor` for the OSes it runs on; that sets defaults and `waker check`. Keep the parsing in an untagged file with tests that run on Linux, and put only the OS call behind a build tag (see `vmstat.go` / `vmstat_darwin.go`).
- A platform's memory sources (which `memory_source` values work there, and the default) live only in `config.PlatformFor`.
- A new peak source is a case in `daemon.recordPeak`.
- A new visitor page is an entry in `pages.copyText` and `config.PageKeys` (a test keeps them equal), plus the state that shows it.
