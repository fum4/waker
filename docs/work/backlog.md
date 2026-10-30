# Backlog

Engineering follow-ups, newest last. Heading: `## <title> · <YYYY-MM-DD> · S|M|L`.

## `waker status` warns when an upstream is busy but waker saw no traffic · 2026-10-30 · M

- **What:** flag an app whose upstream shows work (process-tree CPU, or open connections on its upstream port) while waker's idle clock saw no requests: the sign of callers bypassing `listen` (docs/behaviour.md § Traffic must go through `listen`).
- **Why deferred:** not cheap. It needs a new sampler (per-process CPU deltas from `/proc/<pid>/stat`, or a `/proc/net/tcp*` scan matched to the upstream port), thresholds that don't false-alarm on background jobs, and a systemd-managed app's tree isn't the one waker started.
- **First step:** prototype the connection check only (established sockets on the upstream port not owned by waker), surfaced as a `warning:` line in `waker status`.
