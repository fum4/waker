# Behaviour

## A request, step by step

1. **`/__waker/*` is answered by waker** in every mode and state, and never forwarded:

   | Path | Answer |
   |---|---|
   | `POST active` | The heartbeat: counts as activity, 204. |
   | `status` | `{"state": …}` for this app only. |
   | `font.woff2`, `logo`, `heartbeat.js` | The page assets, cacheable for an hour. |
   | anything else | 404. |

2. **Mode `off`:** the offline page, with 503.
3. **Otherwise the request is forwarded** to the upstream, streaming both ways. If the connection is refused, the app is down:
   - an `idle_ignore` path gets the down state and doesn't wake it;
   - anything else wakes it, and the visitor gets the starting page. A boot that is refused for memory gives busy, and one inside the failure cooldown gives failed.
   - If the app turns out to be up already, a bodiless request is forwarded again.

**Browsers get HTML pages, everything else gets JSON.** A request counts as a browser when it has `Sec-Fetch-Mode: navigate`, or is a GET accepting `text/html`. Every other request gets `{"state": "starting"}` and the like. Both use 503 + `Retry-After: 10`, `Cache-Control: no-store` and `X-Robots-Tag: noindex`. An upstream error after connecting gives the down page, with 502.

**The starting page polls** `/__waker/status` every 3 s:

- it reloads when the state is `up`;
- it switches in place to the busy or failed copy if the boot ends that way.

**Connection cap:** each app's listener accepts at most 64 concurrent connections. Beyond that, a connection gets an immediate 503 `{"state":"busy"}`.

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

## Heartbeat

For an app with `heartbeat: true`:

1. **The request.** A GET that accepts HTML is forwarded with `Accept-Encoding: identity`.
2. **The injection.** A whole, uncompressed `200 text/html` response of up to 5 MiB gets `<script src="/__waker/heartbeat.js" defer>` before its last `</body>`. Its Content-Length is fixed and its ETag dropped.
3. **Pass-through.** HEAD, 304, compressed and larger responses pass through byte-for-byte.
4. **The script.** At most once a minute, while the tab is visible and the visitor gave input within the last minute, it sends `POST /__waker/active`.

## Memory

Before every boot, waker compares `/proc/meminfo` MemAvailable with the app's requirement; see [configuration.md § resources](configuration.md#resources).

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
