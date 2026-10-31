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

## Startup self-heal on launchd · 2026-10-31 · M

- **What:** on macOS an edited `service:` block applies only through `waker restart` or `waker service install`; a crash restart or a `launchctl kickstart` runs the old job. `waker serve` self-heals systemd units only.
- **Why deferred:** launchd caches the job at `bootstrap`, and reloading it (`bootout` + `bootstrap`) from inside the job kills the process doing it; rewriting just the plist would also hide the drift warning while the old job runs.
- **First step:** prototype a detached helper (`launchctl bootout` + `bootstrap` run via `launchctl submit` or a double-fork) and check that `KeepAlive` doesn't race it.

## Service tests run `systemd-analyze verify` on the generated unit in CI · 2026-10-31 · S

- **What:** the service tests compare the rendered unit as text, so a unit systemd itself rejects still passes. v0.4.0 quoted `WorkingDirectory`, systemd reported `bad-setting`, and the box's switch-over restart failed (fixed in #45, v0.4.1). Render a user unit and a system unit in CI and run `systemd-analyze verify` on each (`--user` for the user one).
- **Why deferred:** found during devbox#170's live apply; #45 shipped the fix alone. `verify` needs systemd on the runner (ubuntu-latest has it) and a unit whose `ExecStart` binary exists, so the test must point it at a real file.
- **First step:** a CI-only test (build tag or env guard, skipped when `systemd-analyze` is absent) that writes both units to a temp dir with `ExecStart` set to the built binary, then fails on any `verify` output.

