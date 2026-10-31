# Changelog

All notable changes to waker. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow [SemVer](https://semver.org/) (pre-1.0: minor versions may break config).

## [Unreleased]

### Added
- `waker update [--version vX.Y.Z] [--check]` installs a signed release over the running binary, verified the way install.sh verifies it: the SSH signature (checked natively, with no OpenSSH needed), RELEASE `waker <tag>`, and the checksum. The new binary must then run and report its tag. The swap is atomic, the old binary is kept as `waker.prev`, and the version is recorded where install.sh records it. It prints old → new, the CHANGELOG link and "restart to apply: waker restart", and never restarts. It never downgrades without `--version`, and prints the `sudo` line when the binary isn't writable.
- A daily release check, `update: {check: true}` (the default). At most once a day (`<state_dir>/update.json`, shared by serve and the CLI), one GET of `https://releases.nomadware.io/waker/latest` with a 5 s timeout, silent on failure, no identifiers. A newer release is logged by `waker serve` (at start and daily), printed by `waker status`, and shown as one dim stderr line after `check`, `mode`, `reset`, `restart`, `service` and `version` on a terminal. `update.check: false` or `WAKER_NO_UPDATE_CHECK=1` turns it off; dev builds never check. `waker init` writes the block commented out.
- A `service:` block in waker.yaml (`restart`, `path`, `system`, `start`, `env`, `user`; all optional) that `waker service install` and `uninstall` read. Flags override it for one run; `--restart` is new. Validated with field paths (`service.restart: "sometimes" must be always, on-failure or never`) and shown by `waker check --effective`. `waker init` writes it commented out, with every default visible.
- Drift warning: when the installed waker-written unit differs from the one waker.yaml describes (an edited `service:` block, or a waker upgrade that writes units differently), `waker check`, `waker status` and the startup log say so and print the `waker restart` line.
- docs/configuration.md § When changes apply.
- `waker restart`: applies waker.yaml now. It validates the file, rewrites a waker-written unit that the `service:` block (or this waker version) would write differently, then restarts the service (`systemctl restart`; launchd `bootout` + `bootstrap`), starting it if stopped. It takes install's service flags and `--dry-run`, and prints the `sudo` line for a system service. It refuses `--no-start`, and service flags for a unit waker didn't write; a unit found only in the other scope is named.
- Startup self-heal (systemd): every `service:` key applies at waker's next start, however it restarts (`waker restart`, `systemctl restart`, a crash, a reboot). When `waker serve` runs as the service and its waker-written unit is stale, it rewrites the unit and runs `daemon-reload` + `enable` before binding (putting the old unit back if that fails), then exits once (code 75) so the supervisor restarts it when `path`, `env` or `user` changed (`restart: never`: it logs "restart to apply"). At most once per 10 minutes (`<state_dir>/self-heal.json`). It leaves the unit alone and logs the `waker restart` line when the new `path` loses an app command or a system unit runs as a non-root `user`; it never touches a unit waker didn't write, a `waker serve` that isn't the unit's main process, or launchd.
- Flags are saved to waker.yaml: `service install`, `init` and `setup` write each service flag that changed the service (`--restart`, `--path`, `--system`, `--no-start`, `--run-as`) into the `service:` block, editing only those keys (comments, order and alignment stay). `--no-save` keeps them to one run; `--dry-run` prints what it would save. A file whose header says `# managed: <tool>` is never written: a flag that would change it is refused. `init` and `setup` take the service flags.

### Changed
- **Breaking:** the service's PATH defaults to `~/.local/bin` + the system PATH (macOS: plus `/opt/homebrew/bin`), no longer the installing shell's PATH. A start command found only via your shell's PATH (nvm, pyenv) needs its directory in `service.path`; `waker check` and install name it.
- `service uninstall` takes `--config` and removes the scope that `service.system` names, unless `--system` says otherwise.
- The `--system` sudo hint repeats your flags and adds `--run-as` and `--config`.

## [0.3.0] - 2026-10-31

### Added
- `waker service install --no-start`: write and enable the service without starting or restarting it, for provisioning scripts that decide when to restart. On macOS it writes the plist and prints the `launchctl bootstrap` line.

### Changed
- `waker service install` writes a service that stays up: systemd `Restart=always` (was `on-failure`), `StartLimitIntervalSec=0` and `Type=exec`; launchd `KeepAlive` true (was only after an unsuccessful exit). waker now comes back after any exit its supervisor didn't ask for, and a crash loop no longer stops it for good. `systemctl stop` / `launchctl bootout` still leave it stopped. **Existing installs:** run `waker service install` again to get the new unit.

## [0.2.0] - 2026-10-31

### Added
- `waker check`: a `mode` of `true`/`yes` or `false`/`no` says to write `on` or `off`, since a YAML 1.1 tool may have rewritten them. Bare `on`/`off` were always fine (YAML 1.2); the docs no longer say to quote them.
- `waker init`: writes a `waker.yaml` for one app. On a terminal it prompts for name, listen address (default `0.0.0.0:8080`), app port, start command, health path and idle time, then runs `waker check` and offers to install the service. `--yes` plus flags runs it without a terminal. It never overwrites a config without `--force`.
- `waker setup`: `init` when there is no config yet, then `waker service install`. Prompts on a terminal; `--yes` for scripts.
- `waker service install|uninstall|status`: run waker as a systemd user unit (Linux) or a LaunchAgent (macOS); `--system` for a system unit or LaunchDaemon. The unit runs `waker serve --config <absolute path>` from the config's directory, with the installer's PATH, and restarts on failure. `--dry-run` prints the unit and the commands. install prints the `sudo loginctl enable-linger` line when linger is off, and the `sudo` line for `--system`; waker never runs sudo. install and uninstall never touch a unit they didn't write without `--force`. `status` shows state, pid and where the logs are, and exits 3 when not running. Replaces `examples/waker.service`.
- `$WAKER_CONTROL_SOCKET` overrides the default control socket (the file's `control_socket` still wins). A `--system` systemd service listens on `/run/waker/control.sock`; the CLI falls back to it when the file sets no socket and nothing listens on the default. `waker status` ends with the socket it used.
- `waker check` warns when a `start` or `stop` command isn't an executable on the installed service's PATH (else this shell's).
- CI on GitHub Actions for every PR (`lint`, `test`, `e2e`, `build`), and tagged releases for linux/darwin × amd64/arm64 with `SHA256SUMS`.
- `waker serve`: a reverse proxy per app that wakes it on the first request. It also provides:
  - modes `on` / `off` / `on-demand`;
  - a deadline-driven idle clock that counts activity from each request's end and never stops an app mid-request;
  - an `idle_ignore` list;
  - one boot at a time per app, with a 120 s failure cooldown.
- Visitor pages (starting, offline, busy, failed, down) with an embedded Geist font, plus per-app `branding.logo` / `accent` / `name`. The starting page polls `/__waker/status` and opens the app by itself.
- `branding.contact` (per app, or in `defaults`): an email address or an `https://` URL that the offline, failed and down pages link to ("drop us a line at …"); `{{contact}}` in custom pages. Page copy reworded (starting: "Powering up the servers").
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
