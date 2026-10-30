# Changelog

All notable changes to waker. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow [SemVer](https://semver.org/) (pre-1.0: minor versions may break config).

## [Unreleased]

### Added
- `waker serve`: a reverse proxy per app that wakes it on the first request. It also provides:
  - modes `on` / `off` / `on-demand`;
  - a deadline-driven idle clock that counts activity from each request's end and never stops an app mid-request;
  - an `idle_ignore` list;
  - one boot at a time per app, with a 120 s failure cooldown.
- Visitor pages (starting, offline, busy, failed, down) with an embedded Geist font, plus per-app `branding.logo` / `accent` / `name`. The starting page polls `/__waker/status` and opens the app by itself.
- Heartbeat injection into HTML pages, so a reader keeps an on-demand app awake.
- Memory-aware boots: need (declared or learned peak) + headroom by priority + reserve. Also:
  - linear / table / fixed headroom curves;
  - float priorities;
  - eviction of lower-priority idle apps (policy, order, protect list);
  - peaks learned from the process tree or a systemd unit.
- A top-level `defaults:` block every app inherits (app > defaults > built-in; maps merge per key, lists are replaced), and `waker check --effective` to print the resolved config.
- Runtime mode overrides (`waker mode`, `waker reset`), persisted atomically.
- `waker status` (text or `--json`), `waker check`, `waker version`. The control socket is a 0600 unix socket.
