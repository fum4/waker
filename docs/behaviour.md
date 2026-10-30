# Behaviour

## A request, step by step

1. **`/__waker/*` is answered by waker** in every mode and state, and never forwarded:

   | Path | Answer |
   |---|---|
   | `POST active` | The heartbeat: counts as activity, 204. |
   | `status` | `{"state": …}` for this app only. |
   | `font.woff2`, `logo`, `heartbeat.js`, `wait.js` | The page assets, cacheable for an hour. |
   | anything else | 404. |

2. **Mode `off`:** the offline page, with 503.
3. **Otherwise the request is forwarded** to the upstream, streaming both ways. If the connection is refused, the app is down:
   - an `idle_ignore` path gets the down state and doesn't wake it;
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
6. **down:** otherwise.

Boots are serialised per app: any number of concurrent requests share one boot. A failed boot answers "failed" for 120 s before a request may retry. Switching an app to `off` while it boots wins: when the boot ends, the app is stopped.

## Idle

- **Arming.** On-demand apps have a deadline: last activity + `idle`. waker's loop sleeps exactly until the nearest deadline, capped at 60 s.
- **What counts as activity:** forwarded requests (non-ignored paths), measured from the request's **end**. Also heartbeats, the end of a boot, and mode changes.
- **No deadline** exists while a request is in flight (long downloads and open WebSockets keep the app up) or while the app boots.
- **The stop fires once**, then stays disarmed until the next activity. It doesn't consult health: an unhealthy but running app is stopped too.
- **A stop is committed atomically** with the in-flight check. A request that arrives while the app is stopping waits for the stop to finish, then wakes the app again; it is never forwarded to a process that is going down. A new boot waits until the previous process has fully stopped.

## Heartbeat

For an app with `heartbeat: true`:

1. **The request.** A GET that accepts HTML is forwarded with `Accept-Encoding: identity`.
2. **The injection.** A whole, uncompressed `200 text/html` response of up to 5 MiB gets `<script src="/__waker/heartbeat.js" defer>` before its last `</body>`. Its Content-Length is fixed and its ETag dropped.
3. **Pass-through.** HEAD, 304, compressed and larger responses pass through byte-for-byte.
4. **The script.** At most once a minute, while the tab is visible and the visitor gave input within the last minute, it sends `POST /__waker/active`.

## Memory

Before every boot, waker compares `/proc/meminfo` MemAvailable with the app's requirement, minus what other admitted boots still starting have reserved, so two concurrent boots never count the same free memory; see [configuration.md § resources](configuration.md#resources).

**The learned peak** is measured before each stop and stored in `peaks.json`. It replaces the previous figure, so an app that slims down gets cheaper. It comes from the started process tree's RSS (sampled every 5 s), or from the systemd unit's `MemoryPeak`.

`waker status` prints the policy, then each app's breakdown:

```
memory:    need 900 + headroom(linear p7.5) 1422 = 2322 MiB required to boot (need = max(declared 800, learned 900))
```

## Runtime state and restarts

- **Overrides.** `waker mode` overrides persist in `modes.json`, written atomically (temp file, fsync, rename).
  - A corrupt file is logged and ignored, so the waker starts with the declared modes instead of crash-looping.
  - Overrides for apps no longer configured are dropped.
  - Setting the declared mode removes the override.
- **Startup.** `on` apps that aren't healthy are booted. A listener that can't bind (port in use) is skipped and reported by `waker status`; the other apps still run.
- **Shutdown.** On SIGTERM/SIGINT, waker drains connections for up to 5 s, then stops every app it started.
