# Changelog

All notable changes to waker. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow [SemVer](https://semver.org/) (pre-1.0: minor versions may break config).

## [Unreleased]

### Added
- CI on GitHub Actions for every PR (`lint`, `test`, `e2e`, `build`), and tagged releases for linux/darwin × amd64/arm64 with `SHA256SUMS`.
- `waker serve`: a reverse proxy per app that wakes it on the first request. It also provides:
  - modes `on` / `off` / `on-demand`;
  - a deadline-driven idle clock that counts activity from each request's end and never stops an app mid-request;
  - an `idle_ignore` list;
  - one boot at a time per app, with a 120 s failure cooldown.
- Visitor pages (starting, offline, busy, failed, down) with an embedded Geist font, plus per-app `branding.logo` / `accent` / `name`. The starting page polls `/__waker/status` and opens the app by itself.
- `asleep` state: an on-demand app stopped for idleness shows `asleep` in `waker status` and `/__waker/status`; `down` now means an app that should be running but isn't.
- `pages:` per app (or in `defaults`): your own HTML file per visitor page (`{{name}}`, `{{logo}}`, `{{accent}}` filled in; `/__waker/wait.js` injected into a custom starting page), or `pages: bare` for status and headers only.
- Programs (non-browser requests) are held through a cold boot and forwarded once the app is healthy; `waiting: hold` holds browsers too, `hold_timeout` bounds the wait.
- Heartbeat injection into HTML pages, so a reader keeps an on-demand app awake.
- Memory-aware boots: need (declared or learned peak) + headroom by priority + reserve. Also:
  - linear / table / fixed headroom curves;
  - float priorities;
  - eviction of lower-priority idle apps (policy, order, protect list);
  - peaks learned from the process tree or a systemd unit.
- macOS support for memory-aware boots: `resources.available_source: vm_stat` (the default on macOS; `meminfo` stays the Linux default). `memory_source` defaults to `none` there, and `waker check` rejects `proctree` and `systemd`.
- A top-level `defaults:` block every app inherits (app > defaults > built-in), and `waker check --effective` to print the resolved config.
- `depends_on:` per app (or in `defaults`): dependencies are woken first and kept up (no idle stop, no eviction) while a dependent is up; cycles and unknown names fail `waker check`.
- `schedule:` per app (or in `defaults`): cron windows (`{cron: "0 9 * * 1-5", for: 9h}`) during which the app is `on` whatever its declared mode; overrides still win.
- Runtime mode overrides (`waker mode`, `waker reset`), persisted atomically.
- `waker status` (text or `--json`), `waker check`, `waker version`. The control socket is a 0600 unix socket.
