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

## Release site upload and signing · 2026-10-30 · M

- **What:** `release.yml` publishes to GitHub Releases only. The README install line (`curl -fsSL https://releases.nomadware.io/waker/install.sh | sh`) needs the artifacts, `SHA256SUMS` and an `install.sh` on releases.nomadware.io (R2), plus signed artifacts (e.g. cosign keyless or minisign).
- **Why deferred:** waits on the owner's go for the releases site (bucket, domain, upload credential): `TODO(release-site)` in `.github/workflows/release.yml`.
- **First step:** decide signing (cosign keyless needs `id-token: write`; minisign needs a key in `secrets/`), then add a goreleaser `blobs` (S3-compatible R2) publisher and an `install.sh` that verifies `SHA256SUMS`.
