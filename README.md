# waker

Scale-to-zero for self-hosted apps. waker sits in front of each app and:

- **wakes** it on the first request, showing visitors a starting page that opens the app by itself;
- **stops** it after real inactivity, measured from the end of the last request, never in the middle of one;
- **checks memory** before booting, using per-app priorities, and stops lower-priority idle apps to make room.

It's a single static Go binary. It is both the daemon (`waker serve`) and the CLI that controls it (`status`, `mode`, `reset`, `check`). Apps are plain processes started from an argv list, so any language, framework or service manager works.

## Quickstart

```sh
go install github.com/fum4/waker/cmd/waker@latest
```

Write a `waker.yaml`:

```yaml
apps:
  - name: blog
    listen: 127.0.0.1:8080          # waker answers here; point your reverse proxy at it
    upstream: http://127.0.0.1:3000 # where the app itself serves
    start: [python3, -m, http.server, "3000", --bind, 127.0.0.1]
    idle: 10m                       # stop after 10 minutes without a request
```

Then run it:

```sh
waker check       # validate the config
waker serve       # run (see examples/waker.service for systemd)
waker status      # what is up, what it needs, when it sleeps
waker mode blog off            # take it offline (visitors get an "offline" page)
waker mode blog on-demand --idle 30s
waker reset blog               # back to the declared mode
```

Open http://127.0.0.1:8080. The first visit shows the starting page; the app opens once it answers its health check.

- [docs/configuration.md](docs/configuration.md) is the full config reference.
- [docs/behaviour.md](docs/behaviour.md) explains what happens when.
- [examples/](examples/) has a sample config and a systemd unit.

## What it does

| | |
|---|---|
| **Modes** | Each app is `on` (always up), `off` (offline page) or `on-demand` (woken by traffic, stopped when idle). `waker mode` overrides the mode at runtime. Overrides persist across restarts until `waker reset`. |
| **Idle clock** | Deadline-driven and exact to well under a second. Activity counts from a request's end, and nothing is stopped while a request is in flight. `idle_ignore` paths (pollers, health checks) never count as activity and never wake the app. |
| **Heartbeat** | A tiny script is injected into the app's HTML pages. It keeps an on-demand app awake while someone is actually reading. Opt out per app. |
| **Memory** | An app boots only when `need + headroom(priority) + reserve` is available. Need is the declared size or the learned peak, whichever is higher. The peak is learned from the process tree's RSS, or from a systemd unit's MemoryPeak. When short, waker evicts lower-priority, idle, on-demand apps it started, one at a time. It never evicts pointlessly. |
| **Dependencies** | `depends_on: [db]` wakes the database before the app that needs it, and keeps it up (no idle stop, no eviction) while that app is. |
| **Pages** | starting, offline, busy, failed and down. They have a dark design, embedded font, your logo and accent colour, and make no external requests. They are `noindex` and never cached. Visitors only ever learn their app's state. Swap any of them for your own HTML file, or go `bare` (status and headers only). |
| **Proxy** | Streams both ways (full duplex). Handles hop-by-hop headers correctly, keeps multi-value headers, and tunnels WebSocket upgrades. `/__waker/*` is reserved and never forwarded. |
| **Control** | A unix socket readable only by waker's user (0600 in a 0700 dir). There is no network admin surface. |

## How it compares

[Sablier](https://github.com/sablierapp/sablier) is the closest well-known project, and it's excellent at what it targets. It scales **containers** (Docker, Swarm, Kubernetes, Podman) to zero. It does this through **plugins for an existing reverse proxy** (Traefik, Caddy, Nginx, …), and it supports groups of instances and waiting-page themes.

waker makes different trade-offs:

- **Process-level, no container runtime.** An app is any start/stop command: a binary, `npm start`, `systemctl --user start x`, `docker compose up`. waker supervises it in its own process group.
- **Its own proxy.** No proxy plugin is needed; waker is the hop in front of the app. Put any reverse proxy (or a tunnel) in front of waker.
- **Memory-aware priorities.** Boots are admitted against available memory, with per-app headroom by priority, learned peaks and eviction. Sablier has no memory model.
- **One binary, one YAML file**, with no other moving parts.

Choose Sablier if your apps are containers behind Traefik/Caddy, or you need Kubernetes or multi-replica groups. Choose waker for processes on a single host where memory is the constraint. Today waker is Linux-first: memory availability is read from `/proc/meminfo`, and other sources plug in behind an interface. It also manages one host only.

## Status

Pre-1.0. Config keys may still change, and the [CHANGELOG](CHANGELOG.md) records every change.

## Licence

[MIT](LICENSE). The embedded Geist font is © The Geist Project Authors, under the [SIL Open Font License 1.1](internal/pages/assets/geist-OFL.txt).
