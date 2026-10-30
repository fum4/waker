# waker

[![ci](https://github.com/fum4/waker/actions/workflows/ci.yml/badge.svg)](https://github.com/fum4/waker/actions/workflows/ci.yml)

Scale-to-zero for self-hosted apps: waker starts an app on its first request and stops it when nobody uses it.

Install one static binary (about 11 MB for linux/amd64), point a few lines of YAML at your app, and run it. No containers, no database, no external services: any app that starts from a command works.

## Quickstart

**1. Install**

```sh
curl -fsSL https://releases.nomadware.io/waker/install.sh | sh
```

**2. Configure** `waker.yaml`

```yaml
apps:
  - name: blog
    listen: 127.0.0.1:8080          # waker answers here; point your reverse proxy at it
    upstream: http://127.0.0.1:3000 # where the app itself serves
    start: [python3, -m, http.server, "3000", --bind, 127.0.0.1]
    idle: 10m                       # stop after 10 minutes without a request
```

**3. Run**

```sh
waker serve
```

**4. Keep it running**

```sh
waker service install               # a systemd user unit (Linux) or a LaunchAgent (macOS)
```

It runs `waker serve --config <this file>`, restarts on failure and starts at boot ([details](#as-a-service)).

## Configuration

The [minimal config](#quickstart) above is a whole, valid file: an app needs `name`, `listen`, `upstream` and `start`, and everything else has a default.

### Every key, with its default

Each key below shows its default in the comment; leave out any key you don't change. This file passes `waker check` (with `logo.svg` and `pages/starting.html` next to it).

```yaml
# ── top level ────────────────────────────────────────────────────────────
state_dir: ~/.local/state/waker     # default $XDG_STATE_HOME/waker, else ~/.local/state/waker
control_socket: ~/.local/state/waker/control.sock # default $WAKER_CONTROL_SOCKET, else $XDG_RUNTIME_DIR/waker/control.sock, else <state_dir>/control.sock

# ── defaults: any apps[] key except name, listen, upstream, start, stop ──
defaults:                           # default: none · app value > defaults > built-in
  idle: 15m
  branding: {name: Example Co}      # maps merge per key; lists are replaced

# ── apps[] ───────────────────────────────────────────────────────────────
apps:
  - name: web                       # required · lowercase letters, digits, dashes; unique
    listen: 127.0.0.1:8080          # required · where waker serves this app; unique
    upstream: http://127.0.0.1:3000 # required · where the app serves
    start: [npm, run, start]        # required · argv, no shell, own process group
    stop: []                        # default [] · only SIGTERM, then SIGKILL
    stop_timeout: 10s               # default 10s
    cwd: ./web                      # default waker's working directory
    env: {PORT: "3000"}             # default {} · added to waker's environment
    health:
      path: /                       # default / · GET; any status below 400 is up
      # command: [./healthy]        # optional · argv instead of path; exit 0 is up
      interval: 1s                  # default 1s
      timeout: 2s                   # default 2s
    boot_timeout: 600s              # default 600s
    waiting: page                   # default page · page | hold (browsers; programs are always held)
    hold_timeout: 60s               # default 60s, or boot_timeout if shorter
    mode: on-demand                 # default on-demand · on | off | on-demand
    idle: 30m                       # default 30m (here it would inherit 15m from defaults)
    idle_ignore: [/api/version]     # default [] · exact paths that never count or wake
    heartbeat: true                 # default true
    priority: 5                     # default 5 · 1 (lowest) to 10, fractions allowed
    memory: 512MiB                  # no default · need = max(memory, learned peak), else 1024MiB
    memory_source: proctree         # default proctree (none on macOS) · proctree | systemd | none
    # systemd_unit: web.service     # no default · required with memory_source: systemd
    # headroom: 1GiB                # no default · replaces the curve for this app
    evictable: true                 # default true
    depends_on: [api]               # default [] · woken first, kept up while web is

    # ── branding (per app or in defaults) ──
    branding:
      logo: ./logo.svg              # default: a built-in mark · .svg or .png
      accent: "#5ad1ff"             # default #c6ff4a
      name: Example Co              # no default · the logo's alt text
      contact: hello@example.com    # optional: email or https URL · linked on offline/failed/down

    # ── pages (per app or in defaults) ──
    pages:                          # default: the built-in pages · or `pages: bare`
      starting: ./pages/starting.html # starting | offline | busy | failed | down; omitted ones stay built-in

    # ── schedule (per app or in defaults) ──
    schedule:                       # default [] · windows that keep the app on
      - {cron: "0 9 * * 1-5", for: 9h} # no defaults · 5-field cron (or CRON_TZ=…) · for ≤ 168h

  - name: api
    listen: 127.0.0.1:8081
    upstream: http://127.0.0.1:4000
    start: [./bin/api, --port, "4000"]

# ── resources (all optional) ─────────────────────────────────────────────
resources:
  headroom:
    curve: linear                   # default linear · linear | table | fixed
    max: 5120MiB                    # default 5120MiB · linear: headroom at priority 1
    min: 512MiB                     # default 512MiB · linear: the floor
    # table: {1: 4GiB, 2: 3GiB, …}  # curve table: required, all ten priorities
    # value: 1GiB                   # curve fixed: required
  reserve: 0                        # default 0 · kept free on top of every headroom
  available_source: meminfo         # default meminfo on Linux · vm_stat on macOS
  eviction:
    enabled: true                   # default true
    policy: lower-priority          # default lower-priority · lower-or-equal | never
    order: priority-then-idle       # default priority-then-idle · idle-then-priority
    protect: [api]                  # default [] · never evicted
```

Sizes take a unit (`512MiB`, `2GiB`, `768M`; `0` needs none). Durations are Go durations (`10s`, `5m`, `1h30m`). Relative paths resolve against the config file's directory.

### At a glance

**[Top level](docs/configuration.md#top-level)**

| Key | Default | Meaning |
|---|---|---|
| `state_dir` | `$XDG_STATE_HOME/waker`, else `~/.local/state/waker` | Runtime mode overrides and learned memory peaks. |
| `control_socket` | `$WAKER_CONTROL_SOCKET`, else `$XDG_RUNTIME_DIR/waker/control.sock`, else `<state_dir>/control.sock` | The CLI's unix socket ([`--system`](#as-a-service): `/run/waker/control.sock`). |
| `resources` | see below | Memory policy. |
| `defaults` | – | App keys every app inherits. |
| `apps` | required | One entry per app. |

**[defaults](docs/configuration.md#defaults)**: any `apps[]` key except `name`, `listen`, `upstream`, `start`, `stop`. Scalars: the app wins. Maps (`health`, `branding`, `env`, `pages`): merge per key. Lists (`idle_ignore`, `depends_on`, `schedule`): the app's replaces the default; `[]` clears it.

**[apps[]](docs/configuration.md#apps)**

| Key | Default | Meaning |
|---|---|---|
| `name` · `listen` · `upstream` · `start` | required | Identity, waker's address, the app's address, the start argv. |
| `stop` | `[]` | Stop argv; SIGTERM → SIGKILL follows either way. |
| `stop_timeout` | `10s` | Bounds the stop command and SIGTERM → SIGKILL. |
| `cwd` · `env` | waker's | Working directory and extra environment. |
| `health.path` · `health.command` | `/` · – | Up = GET below 400, or the command exits 0. |
| `health.interval` · `health.timeout` | `1s` · `2s` | Poll rate while booting; one probe's limit. |
| `boot_timeout` | `600s` | Not healthy by then = failed (then 120 s cooldown). |
| `waiting` | `page` | Browsers during a boot: starting page, or `hold`. |
| `hold_timeout` | `60s`, or `boot_timeout` if shorter | How long a held request waits. |
| `mode` | `on-demand` | `on` · `off` · `on-demand`. |
| `idle` | `30m` | On-demand: stop after this long without activity. |
| `idle_ignore` | `[]` | Paths that never count as activity or wake. |
| `heartbeat` | `true` | Inject the keep-awake script into HTML pages. |
| `priority` | `5` | 1–10, fractions allowed; headroom and eviction rank. |
| `memory` | – | Declared need; the learned peak raises it. |
| `memory_source` · `systemd_unit` | `proctree` (macOS: `none`) · – | Where the peak is learned: `proctree`, `systemd` (both Linux only), `none`. |
| `headroom` | – (the curve) | Absolute headroom for this app. |
| `evictable` | `true` | `false` = never evicted. |
| `depends_on` | `[]` | Apps woken first and kept up while this one is ([details](docs/configuration.md#depends_on-per-app-or-in-defaults)). |

**[resources](docs/configuration.md#resources)**

| Key | Default | Meaning |
|---|---|---|
| `headroom.curve` | `linear` | `linear` · `table` · `fixed`. |
| `headroom.max` · `headroom.min` | `5120MiB` · `512MiB` | Linear: headroom at priority 1, and the floor. |
| `headroom.table` · `headroom.value` | – | Table: all ten priorities. Fixed: the one value. |
| `reserve` | `0` | Kept free on top of every headroom. |
| `available_source` | `meminfo` (macOS: `vm_stat`) | Where free memory is read; one per platform. |
| `eviction.enabled` | `true` | Evict at all. |
| `eviction.policy` | `lower-priority` | `lower-or-equal` · `never`. |
| `eviction.order` | `priority-then-idle` | `idle-then-priority`. |
| `eviction.protect` | `[]` | App names never evicted. |

**[branding](docs/configuration.md#branding-per-app-or-in-defaults)** (per app, or in `defaults`)

| Key | Default | Meaning |
|---|---|---|
| `logo` | built-in mark | SVG or PNG on the visitor pages. |
| `accent` | `#c6ff4a` | Indicator colour. |
| `name` | – | Your brand: the logo's alt text. |
| `contact` | – | An email or `https://` URL, linked from the offline, failed and down pages. |

**[pages](docs/configuration.md#pages-per-app-or-in-defaults)** (per app, or in `defaults`)

| Key | Default | Meaning |
|---|---|---|
| `starting` · `offline` · `busy` · `failed` · `down` | built-in | Your own HTML file; `{{name}}`, `{{logo}}`, `{{accent}}`, `{{contact}}` filled in. |
| `pages: bare` | – | No bodies: status and headers only. |

**[schedule](docs/configuration.md#schedule-per-app-or-in-defaults)** (per app, or in `defaults`): a list, default `[]`

| Key | Default | Meaning |
|---|---|---|
| `cron` | required | 5-field cron or `@daily` etc.; local time unless `CRON_TZ=…`. Each fire opens a window. |
| `for` | required | Window length, at most `168h`. The app is `on` inside it. |

## How it compares

[Sablier](https://github.com/sablierapp/sablier) is the closest well-known project, and it's excellent at what it targets. It scales **containers** (Docker, Swarm, Kubernetes, Podman) to zero. It does this through **plugins for an existing reverse proxy** (Traefik, Caddy, Nginx, …), and it supports groups of instances and waiting-page themes.

waker makes different trade-offs:

- **Process-level, no container runtime.** An app is any start/stop command: a binary, `npm start`, `systemctl --user start x`, `docker compose up`. waker supervises it in its own process group.
- **Its own proxy.** No proxy plugin is needed; waker is the hop in front of the app. Put any reverse proxy (or a tunnel) in front of waker.
- **Waiting per request, not per route.** Sablier picks one strategy per route: a waiting page (dynamic) or holding the request (blocking). waker decides per request: browsers get the starting page, programs are held, on the same address.
- **Memory-aware priorities.** Boots are admitted against available memory, with per-app headroom by priority, learned peaks and eviction. Sablier has no memory model.
- **Dependencies and schedules** are built in: wake the database first, keep the CRM up on weekdays.
- **One binary, one YAML file**, with no other moving parts.

Choose Sablier if your apps are containers behind Traefik/Caddy, or you need Kubernetes or multi-replica groups. Choose waker for processes on a single host where memory is the constraint. waker runs on Linux and macOS. Available memory comes from `/proc/meminfo` on Linux and from `vm_stat` on macOS. Learning an app's peak is Linux only, so on macOS you declare `memory` per app. It also manages one host only.

## CLI

The same binary is the daemon and its control client. Every command takes `--config <path>`, else `$WAKER_CONFIG`, else `./waker.yaml`.

```sh
waker serve                          # run the daemon
waker check                          # validate the config (--effective: print it with every default filled in)
waker status                         # what is up, what it needs, when it sleeps (--json)
waker mode blog off                  # take it offline (visitors get an "offline" page)
waker mode blog on-demand --idle 30s
waker reset blog                     # back to the declared mode
waker service install                # run as a service (--dry-run: print it; --system: system-wide)
waker service status                 # state, pid, where the logs are (exit 3 when not running)
waker service uninstall              # remove everything install wrote
waker version
```

From source: `go install github.com/fum4/waker/cmd/waker@latest`.

### As a service

`waker service install` is non-interactive, so an agent or a script can run it. It writes one unit and starts it:

| | Linux (systemd) | macOS (launchd) |
|---|---|---|
| default | user unit, `~/.config/systemd/user/waker.service` | LaunchAgent, `~/Library/LaunchAgents/io.nomadware.waker.plist` |
| `--system` | `/etc/systemd/system/waker.service`, `User=` you | LaunchDaemon, `/Library/LaunchDaemons/…`, `UserName` you |
| logs | `journalctl --user -u waker.service -f` | `~/Library/Logs/waker/waker.log` |

- **PATH.** A supervisor's PATH is minimal: `npm` or `python3` from your shell wouldn't be found at the first wake. The unit captures the PATH you install from (`--path` overrides it). `waker check` warns about any `start` or `stop` command missing from the installed service's PATH.
- **Working directory.** It's the config file's directory, so `start: [./bin/api]` works as it does from there.
- **Restarts.** waker restarts after a crash; `systemctl stop` / `launchctl bootout` leave it stopped.
- **Linger (Linux).** A user unit stops when your last session ends unless linger is on. install and status print the exact `sudo loginctl enable-linger <you>` line; waker never runs sudo itself.
- **Root.** `--system` needs root. Without it, install prints the `sudo …` line to run, with your PATH and user filled in.
- **Control socket (`--system`, Linux).** A system service has no `XDG_RUNTIME_DIR` to share with your shell, so it listens on `/run/waker/control.sock` (`RuntimeDirectory=waker`). The CLI falls back to that socket when nothing listens on its default, and `waker status` ends with the socket it used.
- **Ownership.** Every file carries a "Written by `waker service install`" marker. install never overwrites a unit you wrote yourself, and uninstall never removes one, without `--force`.
- **Elsewhere** (no systemd, other OSes) install refuses and prints the command, PATH and restart policy to set up by hand.

## What it does

- **On a visit** to the quickstart's http://127.0.0.1:8080, waker starts the app. A browser sees a starting page that opens the app once its health check passes; a program (curl, an API client, a webhook) is held and answered by the app itself.
- **When idle**, 10 minutes after the last request *ended*, waker stops it. Nothing is stopped mid-request.

| | |
|---|---|
| **Modes** | Each app is `on` (always up), `off` (offline page) or `on-demand` (woken by traffic, stopped when idle). `waker mode` overrides the mode at runtime. Overrides persist across restarts until `waker reset`. |
| **Idle clock** | Deadline-driven and exact to well under a second. Activity counts from a request's end, and nothing is stopped while a request is in flight. `idle_ignore` paths (pollers, health checks) never count as activity and never wake the app. |
| **Waiting** | Programs are held through a cold boot and forwarded, body included, once the app is healthy. Browsers get the starting page, or are held too with `waiting: hold`. `hold_timeout` bounds the wait. |
| **Heartbeat** | A tiny script is injected into the app's HTML pages. It keeps an on-demand app awake while someone is actually reading. Non-browser clients keep it awake with [`POST /__waker/active`](#keeping-an-app-awake-from-a-program). Opt out per app. |
| **Memory** | An app boots only when `need + headroom(priority) + reserve` is available. Need is the declared size or the learned peak, whichever is higher. The peak is learned from the process tree's RSS, or from a systemd unit's MemoryPeak (Linux; on macOS declare `memory`). When short, waker evicts lower-priority, idle, on-demand apps it started, one at a time. It never evicts pointlessly. |
| **Dependencies** | `depends_on: [db]` wakes the database before the app that needs it, and keeps it up (no idle stop, no eviction) while that app is. |
| **Schedules** | `schedule: [{cron: "0 9 * * 1-5", for: 9h}]` keeps an app on during working hours, whatever its mode, and hands it back to its mode after. |
| **Defaults** | A `defaults:` block sets any per-app key once; apps override it. `waker check --effective` prints what each app ends up with. |
| **Pages** | starting, offline, busy, failed and down. They have a dark design, embedded font, your logo and accent colour, an optional contact link, and make no external requests. They are `noindex` and never cached. Visitors only ever learn their app's state. Swap any of them for your own HTML file (the starting page still opens the app by itself, via `/__waker/wait.js`), or go `bare` (status and headers only). |
| **Proxy** | Streams both ways (full duplex). Handles hop-by-hop headers correctly, keeps multi-value headers, and tunnels WebSocket upgrades. `/__waker/*` is reserved and never forwarded. |
| **Control** | A unix socket readable only by waker's user (0600 in a 0700 dir). There is no network admin surface. |

### All traffic must go through waker

waker only knows about requests that reach its `listen` address. A caller that talks to the app's `upstream` port directly is invisible to it: it doesn't wake the app, doesn't count as activity, and the app can be idle-stopped under it. Point every client, reverse proxy and tunnel at `listen`, and keep the upstream bound to `127.0.0.1`.

### Keeping an app awake from a program

`POST /__waker/active` on the app's `listen` address counts as activity and answers `204`. It is the endpoint the heartbeat script uses. A long-running client (a worker, a dashboard, a CLI session) that needs an on-demand app to stay up between requests pings it more often than `idle`, e.g. once a minute:

```sh
while sleep 60; do curl -fsS -X POST http://127.0.0.1:8080/__waker/active; done
```

It only resets the idle clock. It never wakes a stopped app (send a real request for that), and it answers `204` in every mode and state, so it isn't a health check either: use `GET /__waker/status` for that.

## Docs

- [docs/configuration.md](docs/configuration.md): every key, its default and its validation.
- [docs/behaviour.md](docs/behaviour.md): what happens when — requests, states, idle, memory, dependencies, schedules.
- [examples/](examples/): a sample config.

## Status

Pre-1.0. Config keys may still change, and the [CHANGELOG](CHANGELOG.md) records every change.

**CI is the gate.** Every PR must pass the [`ci`](.github/workflows/ci.yml) checks (`lint`, `test`, `e2e`, `build`) before it merges. The `mise run pre-push` / `pre-merge` tasks run the same checks locally, for fast feedback. The required checks are declared as code, a repository ruleset in [`infra/github/`](infra/github/). A `v*` tag publishes a release ([`release.yml`](.github/workflows/release.yml)).

## Licence

[MIT](LICENSE). The embedded Geist font is © The Geist Project Authors, under the [SIL Open Font License 1.1](internal/pages/assets/geist-OFL.txt).
