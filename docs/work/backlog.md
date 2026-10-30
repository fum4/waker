# Backlog

Engineering follow-ups, newest last. Heading: `## <title> · <YYYY-MM-DD> · S|M|L`.

## `waker status` warns when an upstream is busy but waker saw no traffic · 2026-10-30 · M

- **What:** flag an app whose upstream shows work (process-tree CPU, or open connections on its upstream port) while waker's idle clock saw no requests: the sign of callers bypassing `listen` (docs/behaviour.md § Traffic must go through `listen`).
- **Why deferred:** not cheap. It needs a new sampler (per-process CPU deltas from `/proc/<pid>/stat`, or a `/proc/net/tcp*` scan matched to the upstream port), thresholds that don't false-alarm on background jobs, and a systemd-managed app's tree isn't the one waker started.
- **First step:** prototype the connection check only (established sockets on the upstream port not owned by waker), surfaced as a `warning:` line in `waker status`.

## Learned peaks on macOS (`memory_source: proctree` via `ps`) · 2026-10-30 · S

- **What:** on macOS waker learns no peaks: `proctree` reads `/proc`, so `waker check` rejects it there and `memory_source` defaults to `none`. Every app must declare `memory`, or it counts as 1024 MiB.
- **Why deferred:** the darwin-memory PR kept to available memory (`vm_stat`). A `ps -A -o pid=,ppid=,rss=` sampler is a second subprocess every 5 s per running app, and it can't be tested on a real Mac from the box.
- **First step:** factor `ProcTreeRSSMiB` into "build the pid → (ppid, rss) table" (procfs on Linux, `ps` on darwin) plus the shared tree sum, then let `config.PlatformFor("darwin")` accept `proctree`. Verify on a Mac against Activity Monitor.

## CI: install only the Go tools, and check infra/github · 2026-10-30 · S

- **What:** `terraform` joined `.mise.toml [tools]` for `infra/github`, so `jdx/mise-action` in every `ci.yml` job now downloads it too, unused. And nothing in CI checks the Terraform: only the local `pre-push` (`tf:check`).
- **Why deferred:** the ruleset PR keeps off `.github/` workflows.
- **First step:** `install_args: go staticcheck` on the four jobs' `mise-action` step; add `mise run tf:check` to the `lint` job (installing terraform there only).

