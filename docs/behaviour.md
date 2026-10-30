# Behaviour

## A request, step by step

1. **`/__waker/*` is answered by waker** in every mode and state, and never forwarded:

   | Path | Answer |
   |---|---|
   | `POST active` | Counts as activity, 204; never wakes the app. The heartbeat and [program keepalives](#keepalive-for-programs) use it. |
   | `status` | `{"state": …}` for this app only. |
   | `font.woff2`, `logo`, `heartbeat.js`, `wait.js` | The page assets, cacheable for an hour. |
   | anything else | 404. |

2. **Mode `off`:** the offline page, with 503.
3. **Otherwise the request is forwarded** to the upstream, streaming both ways. If the connection is refused, the app isn't running:
   - an `idle_ignore` path gets the `asleep` state (or `down`, below) and the down page, and doesn't wake it;
   - anything else wakes it. A boot that is refused for memory gives busy, and one inside the failure cooldown gives failed. While it boots:
     - **a browser** gets the starting page at once (`waiting: page`, the default), or is held like a program (`waiting: hold`);
     - **a program** (anything that isn't a browser, see below) is always **held**: the request waits until the app is healthy, then is forwarded, body included. If the boot fails or is refused it gets that state. After `hold_timeout` (default 60 s), a held browser gets the failed page and a program gets 503 `{"state":"starting"}`; the boot carries on.
     - A held request counts as in flight: the app is neither idle-stopped nor evicted under it, and its connection counts against the 64-connection cap.
   - If the app turns out to be up already, the request is forwarded again, as long as none of its body was sent yet.

**Browsers get HTML pages, everything else gets JSON.** A request counts as a browser when it has `Sec-Fetch-Mode: navigate`, or is a GET accepting `text/html`. Every other request gets `{"state": "starting"}` and the like. Both use 503 + `Retry-After: 10`, `Cache-Control: no-store` and `X-Robots-Tag: noindex`. An upstream error after connecting gives the down page, with 502.

`/__waker/status` is cached per app for 1 s, with one health probe in flight, so visitor polls never multiply probes (or spawn health commands).

**The starting page polls** `/__waker/status` every 3 s, through `/__waker/wait.js` (the built-in page loads it; a [custom starting page](configuration.md#pages-per-app-or-in-defaults) gets it injected):

- it reloads when the state is `up`;
- if the boot ends busy or failed, the built-in page switches in place to that copy, and a custom page reloads once to show that state's page.

With [`pages: bare`](configuration.md#pages-per-app-or-in-defaults), every page and JSON answer above is the status and headers alone.

**Connection cap:** each app's listener accepts at most 64 concurrent connections. Beyond that, a connection gets an immediate 503 `{"state":"busy"}` (with `pages: bare`, the 503 alone).

## States

`state` is what visitors and `waker status` see. The first match wins:

1. **starting:** a boot is running.
2. **up:** the health check passes.
3. **failed:** the last boot failed. Its error is in `waker status`, never on the page.
4. **busy:** the last boot was refused for memory.
5. **offline:** mode is off.
6. **asleep:** an on-demand app that isn't running (e.g. after an idle stop); the next request wakes it. Visitors on an `idle_ignore` path get the down page.
7. **down:** otherwise: an app that should be running (mode `on`, or inside a schedule window) but isn't.

Boots are serialised per app: any number of concurrent requests share one boot. A failed boot answers "failed" for 120 s before a request may retry. Switching an app to `off` while it boots wins: when the boot ends, the app is stopped.

## Idle

- **Arming.** On-demand apps have a deadline: last activity + `idle`. waker's loop sleeps exactly until the nearest deadline, capped at 60 s.
- **What counts as activity:** forwarded requests (non-ignored paths), measured from the request's **end**. Also heartbeats, the end of a boot, and mode changes.
- **No deadline** exists while a request is in flight (long downloads and open WebSockets keep the app up), while the app boots, or while a [dependent](#dependencies) is up. An app inside a [schedule](#schedules) window has none either: it is `on`.
- **The stop fires once**, then stays disarmed until the next activity. It doesn't consult health: an unhealthy but running app is stopped too.
- **A stop is committed atomically** with the in-flight check. A request that arrives while the app is stopping waits for the stop to finish, then wakes the app again; it is never forwarded to a process that is going down. A new boot waits until the previous process has fully stopped.

## Dependencies

For an app with [`depends_on`](configuration.md#depends_on-per-app-or-in-defaults):

- **Wake order.** Waking the app wakes every dependency first, in parallel, each as if a request had woken it: its own memory check and eviction, its own boot. A dependency that is already up isn't booted. The app's own memory check and start wait until all of them are healthy; it shows `starting` throughout. Its `boot_timeout` covers the whole boot, dependencies included.
- **Failure.** If a dependency ends failed or busy, is in mode `off` (it is never woken then), or isn't up within the app's `boot_timeout`, the app's boot fails without starting it, and `waker status` names the dependency: `failed — dependency db: offline (mode off)`. The usual 120 s cooldown applies.
- **Keep-up.** While a dependent that waker started is up, booting or stopping, the dependency has no idle deadline and is never evicted. `waker status` shows it as `needed by: api`. `waker mode db off` still stops it (an operator's explicit call), and warns: `warning: db is off while api (up) still need it`.
- **Release.** When the last dependent stops (idle, eviction, mode `off`, shutdown), the dependency's idle clock restarts from that moment, so it stops after its own `idle`.
- **Shutdown** stops dependents before their dependencies.

## Schedules

For an app with a [`schedule`](configuration.md#schedule-per-app-or-in-defaults):

- **Precedence.** The effective mode is a `waker mode` override, else `on` inside a window, else the declared mode. An override beats the schedule for as long as it stands, even one set to the declared mode, and `waker reset` hands the app back to the schedule.
- **A window opens:** the app is woken as if switched to `on`, along with its dependencies. While it lasts there is no idle stop and no eviction.
- **A window closes:** the app falls back to its declared mode. An `on-demand` app gets one full `idle` from the close, so it stops after `idle` without traffic. An `off` app is stopped.
- **Timing.** The idle loop wakes at every window boundary. A boundary it slept past (a busy host) is handled at its next wake-up, since it compares the mode with the last one it acted on.
- **Startup.** A window open when waker starts boots the app at once, like an `on` app.
- **Status.** `waker status` shows where the app stands:

  ```
  mode:      on (schedule; declared on-demand, idle 30m0s)
  schedule:  on until 18:00 (window 0 9 * * 1-5 for 9h)
  ```

  Outside a window it shows `schedule:  next window Mon 09:00 (…)`. While an override outranks an open window, the line ends `· the override wins`. `--json` has `schedule: {active, until | next, window}`.

## Heartbeat

For an app with `heartbeat: true`:

1. **The request.** A GET that accepts HTML is forwarded with `Accept-Encoding: identity`.
2. **The injection.** A whole, uncompressed `200 text/html` response of up to 5 MiB gets `<script src="/__waker/heartbeat.js" defer>` before its last `</body>`. Its Content-Length is fixed and its ETag dropped.
3. **Pass-through.** HEAD, 304, compressed and larger responses pass through byte-for-byte.
4. **The script.** At most once a minute, while the tab is visible and the visitor gave input within the last minute, it sends `POST /__waker/active`.

## Keepalive for programs

A client that isn't a browser page (a worker, a dashboard, a long CLI session) keeps an on-demand app awake between its requests with the heartbeat's endpoint: `POST /__waker/active` on the app's `listen` address, more often than `idle` (e.g. every minute).

- **What it does:** records activity now, exactly like the end of a forwarded request, so the idle deadline moves to now + `idle` (and a disarmed clock re-arms). Answers 204, no body, `no-store`.
- **What it doesn't:** it never wakes a stopped app and is never forwarded. It answers 204 in every mode and state, `off` and down included, so it isn't a health check: wake the app with a real request, and read its state from `GET /__waker/status`.
- Any other method on `/__waker/active` is a 404.

## Traffic must go through `listen`

waker sees only requests that reach its `listen` address. A caller that talks to the app's `upstream` port directly bypasses it: no wake, no activity, no in-flight count. The app can then be idle-stopped (or evicted) while that caller is still using it. Point every client, reverse proxy and tunnel at `listen`, and bind the upstream to `127.0.0.1` so nothing outside the host can reach it.

## Memory

Before every boot, waker compares available memory (`/proc/meminfo` MemAvailable on Linux, `vm_stat` on macOS) with the app's requirement, minus what other admitted boots still starting have reserved, so two concurrent boots never count the same free memory; see [configuration.md § resources](configuration.md#resources).

**The learned peak** is measured before each stop and stored in `peaks.json`. It replaces the previous figure, so an app that slims down gets cheaper. It comes from the started process tree's RSS (sampled every 5 s), or from the systemd unit's `MemoryPeak`. Both are Linux only; on macOS nothing is learned (`memory_source: none`), so set `memory` per app or each counts as 1024 MiB.

`waker status` prints the policy, then each app's breakdown:

```
memory:    need 900 + headroom(linear p7.5) 1422 = 2322 MiB required to boot (need = max(declared 800, learned 900))
```

## Runtime state and restarts

- **Overrides.** `waker mode` overrides persist in `modes.json`, written atomically (temp file, fsync, rename).
  - A corrupt file is logged and ignored, so the waker starts with the declared modes instead of crash-looping.
  - Overrides for apps no longer configured are dropped.
  - Setting the declared mode removes the override, except on an app with a `schedule`: there it pins that mode through windows until `waker reset`.
- **Startup.** `on` apps that aren't healthy are booted, apps inside a schedule window included. A listener that can't bind (port in use) is skipped and reported by `waker status`; the other apps still run.
- **Shutdown.** On SIGTERM/SIGINT, waker drains connections for up to 5 s, then stops every app it started, dependents before their dependencies.
