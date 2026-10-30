# Configuration reference

waker reads one YAML file. It takes the first of these that is set:

1. `--config <path>`;
2. `$WAKER_CONFIG`;
3. `./waker.yaml`.

Unknown keys are errors, so typos fail loudly. `waker check` validates the file and reports every problem at once, each with its field path, e.g. `apps[1] (api).priority: 12 must be a number from 1 to 10`. A bad value inherited from `defaults` is reported once, at its real path (`defaults.idle: must be positive`). `waker check --effective` prints every app's resolved config, defaults applied. Relative paths (`state_dir`, `control_socket`, `branding.logo`, `pages.*`, `cwd`) resolve against the config file's directory, and `~/` is expanded.

**Sizes** are written like `512MiB`, `2GiB` or `768M`. A unit is required, and the single-letter units are binary too (`1G` = `1GiB`). **Durations** are Go durations: `10s`, `5m`, `1h30m`.

## Top level

| Key | Default | Meaning |
|---|---|---|
| `state_dir` | `$XDG_STATE_HOME/waker`, else `~/.local/state/waker` | Holds `modes.json` (runtime mode overrides) and `peaks.json` (learned memory peaks). Created 0700. |
| `control_socket` | `$XDG_RUNTIME_DIR/waker/control.sock`, else `<state_dir>/control.sock` | The CLI's unix socket. Its directory is 0700 and the socket 0600. |
| `resources` | see below | Memory policy. |
| `defaults` | – | App keys every app inherits; see below. |
| `apps` | required | One entry per app. |

## `defaults`

Any `apps[]` key except `name`, `listen`, `upstream`, `start` and `stop` (they identify one app) can be set once here:

```yaml
defaults:
  idle: 10m
  priority: 4
  health: {path: /healthz}
  branding: {accent: "#5ad1ff", name: Example Co}
```

**Precedence:** the app's own value, then `defaults`, then the built-in default.

- **Scalars** (`idle`, `mode`, `priority`, …): the app's value wins.
- **Maps** (`health`, `branding`, `env`, `pages`) merge per key: an app with `branding: {accent: "#f36"}` keeps the default `name` and `logo`. In `health`, `path` and `command` are one choice: an app that sets either inherits neither. In `pages`, `bare` and a map don't merge: the app's value wins whole.
- **Lists** (`idle_ignore`, `depends_on`): the app's list replaces the default one; `[]` clears it.

## `apps[]`

| Key | Default | Meaning |
|---|---|---|
| `name` | required | Lowercase letters, digits and dashes; unique. It is shown under the indicator on visitor pages. |
| `listen` | required | `host:port` that waker serves the app on; unique. |
| `upstream` | required | `http(s)://host:port` where the app serves. |
| `start` | required | Argv list, run without a shell, in its own process group. It may stay in the foreground (it is the app), or exit 0 after handing off to a service manager. A non-zero exit before the app is healthy fails the boot. |
| `stop` | `[]` | Argv list run to stop the app. After it, whatever is left of the started process group gets SIGTERM, then SIGKILL after `stop_timeout`. With no `stop`, only the signals are used. |
| `stop_timeout` | `10s` | Bounds the stop command and the wait between SIGTERM and SIGKILL. |
| `cwd`, `env` | waker's | The working directory and extra environment for `start`, `stop` and the health command. |
| `health.path` | `/` | GET on the upstream. Any status below 400 means up; redirects aren't followed and count as up. |
| `health.command` | – | Argv instead of `path`; exit 0 means up. |
| `health.interval` / `health.timeout` | `1s` / `2s` | How often health is polled while booting, and how long one probe may take. |
| `boot_timeout` | `600s` | A boot that isn't healthy by then fails. A failed boot answers "failed" for 120 s before the next request may retry. |
| `waiting` | `page` | What a browser gets while the app boots: `page` (the starting page at once) or `hold` (the request waits for the app, like a program's). Programs are always held; see [behaviour.md](behaviour.md#a-request-step-by-step). |
| `hold_timeout` | `60s` (capped at `boot_timeout`) | How long a held request waits. Then a browser gets the failed page and a program 503 JSON. At most `boot_timeout`. |
| `mode` | `on-demand` | `on`, `off` or `on-demand`. Quote `"on"` and `"off"`, or YAML may read them as booleans. |
| `idle` | `30m` | On-demand only: stop after this long without activity. |
| `idle_ignore` | `[]` | Exact paths (query excluded) that are neither activity nor a reason to wake, e.g. a version poller. |
| `heartbeat` | `true` | Inject the heartbeat script into the app's HTML pages. |
| `priority` | `5` | A number from 1 (lowest) to 10 (highest); fractions are allowed (`7.5`). It decides headroom and who may evict whom. |
| `memory` | – | The declared need. The learned peak raises it. |
| `memory_source` | `proctree` | Where the peak is learned from: `proctree` (RSS of the started process tree, sampled every 5 s), `systemd` (needs `systemd_unit`; reads the unit's `MemoryPeak`), or `none`. |
| `headroom` | – | An absolute headroom for this app, replacing the curve. |
| `evictable` | `true` | `false` means this app is never evicted. |
| `branding` | see below | The look of this app's visitor pages; usually set once in `defaults`. |
| `pages` | built-in | Your own HTML file per visitor page, or `bare` for status and headers only; see below. |
| `depends_on` | `[]` | Apps woken before this one and kept up while it is; see below. |

## `resources`

All keys are optional. The defaults are the linear curve and lower-priority eviction.

```yaml
resources:
  headroom:
    curve: linear        # linear | table | fixed
    max: 5120MiB         # linear: headroom at priority 1
    min: 512MiB          # linear: the floor
    # table: {1: 4GiB, 2: 3GiB, …, 10: 256MiB}   # table: all ten keys
    # value: 1GiB                                # fixed
  reserve: 0             # kept free on top of every app's headroom
  available_source: meminfo
  eviction:
    enabled: true
    policy: lower-priority    # lower-priority | lower-or-equal | never
    order: priority-then-idle # priority-then-idle | idle-then-priority
    protect: [db]             # app names never evicted
```

**The boot rule:** an app boots only if available memory ≥ `need + headroom + reserve`.

- **`need`** is `max(memory, learned peak)`, or 1024 MiB when neither is known.
- **The linear curve** is `max(min, max × (10 − p) / 9)`, rounded down to MiB. With the defaults:

  | p1 | p2 | p3 | p4 | p5 | p6 | p7 | p8 | p9 | p10 |
  |---|---|---|---|---|---|---|---|---|---|
  | 5120 | 4551 | 3982 | 3413 | 2844 | 2275 | 1706 | 1137 | 568 | 512 |

  A fractional priority uses the formula directly, e.g. p7.5 gives 1422.
- **The table curve** interpolates linearly between neighbouring integer keys.

**Eviction**, when memory is short:

1. waker picks candidates: apps it started that are `on-demand`, up, with nothing in flight, not protected, not kept up by a [dependent](#depends_on-per-app-or-in-defaults), and of a lower priority (or equal, under `lower-or-equal`).
2. If stopping all of them couldn't cover the shortfall, nothing is stopped and the visitor sees "busy".
3. Otherwise it stops them one at a time in the configured order, re-measuring after each, and boots as soon as there's room.

`on`-mode apps are never evicted.

## `branding` (per app, or in `defaults`)

| Key | Default | Meaning |
|---|---|---|
| `logo` | a neutral built-in mark | An SVG or PNG file, read at start and served at `/__waker/logo`. |
| `accent` | `#c6ff4a` | The `#rgb` / `#rrggbb` colour of the starting indicator. |
| `name` | – | Your brand: the logo's alt text. |

## `pages` (per app, or in `defaults`)

Replace any of the built-in visitor pages with your own HTML file:

```yaml
pages:
  starting: ./pages/starting.html   # starting | offline | busy | failed | down
  offline: ./pages/offline.html     # pages you leave out stay built-in
```

Each file is read once at start. `waker check` fails if one is missing, unreadable, a directory or over 1 MiB, naming the key (`apps[0] (web).pages.starting: open …: no such file or directory`, or `defaults.pages.starting: …` when inherited). A custom page is served with the same status and headers as the built-in one (503 or 502, `text/html`, `no-store`, `noindex`).

Three placeholders are replaced, HTML-escaped; nothing else in the file is touched (it is not a template):

| Placeholder | Becomes |
|---|---|
| `{{name}}` | The app's name. |
| `{{logo}}` | `/__waker/logo`, the app's `branding.logo`. |
| `{{accent}}` | `branding.accent`, else `#c6ff4a`. |

```html
<!doctype html><html><head><meta charset="utf-8"><title>{{name}} is starting</title></head>
<body style="background:#111;color:#eee">
  <img src="{{logo}}" alt="" width="48">
  <h1 style="color:{{accent}}">Waking {{name}}…</h1>
</body></html>
```

**The starting page opens the app by itself:** waker inserts `<script src="/__waker/wait.js" defer></script>` before its last `</body>` (or at the end), unless the file already loads `/__waker/wait.js`. When the boot fails or is refused, the script reloads once, so the visitor gets that state's page. Other pages get no script.

**`pages: bare`** drops every page and JSON body for the app: visitors get the status code and headers alone (`Content-Length: 0`, `Cache-Control: no-store`, `X-Robots-Tag: noindex`, `Retry-After` on 503). `/__waker/*` still answers. Use it when something in front of waker renders its own error pages.

## `depends_on` (per app, or in `defaults`)

The apps this one needs, by name:

```yaml
apps:
  - name: api
    depends_on: [db, cache]
```

Waking `api` wakes `db` and `cache` first, and they stay up while `api` is; see [behaviour.md § Dependencies](behaviour.md#dependencies). Any configured app may be named, listening or not. `waker check` rejects, naming the key:

- an unknown app (`apps[0] (api).depends_on[1]: "ghost" is not a configured app`);
- the app itself, or a name listed twice;
- a cycle, reported once (`apps[0] (api).depends_on: cycle api → db → api`).

Set in `defaults`, the list applies to every app, and an app named in it drops itself from its inherited copy: `defaults: {depends_on: [db]}` makes every app but `db` need `db`. An app's own list naming itself is still an error.
