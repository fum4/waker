# Configuration reference

waker reads one YAML file. It takes the first of these that is set:

1. `--config <path>`;
2. `$WAKER_CONFIG`;
3. `./waker.yaml`.

Unknown keys are errors, so typos fail loudly. `waker check` validates the file and reports every problem at once, each with its field path, e.g. `apps[1] (api).priority: 12 must be a number from 1 to 10`. Relative paths (`state_dir`, `control_socket`, `branding.logo`, `cwd`) resolve against the config file's directory, and `~/` is expanded.

**Sizes** are written like `512MiB`, `2GiB` or `768M`. A unit is required, and the single-letter units are binary too (`1G` = `1GiB`). **Durations** are Go durations: `10s`, `5m`, `1h30m`.

## Top level

| Key | Default | Meaning |
|---|---|---|
| `state_dir` | `$XDG_STATE_HOME/waker`, else `~/.local/state/waker` | Holds `modes.json` (runtime mode overrides) and `peaks.json` (learned memory peaks). Created 0700. |
| `control_socket` | `$XDG_RUNTIME_DIR/waker/control.sock`, else `<state_dir>/control.sock` | The CLI's unix socket. Its directory is 0700 and the socket 0600. |
| `resources` | see below | Memory policy. |
| `branding` | see below | The look of visitor pages. |
| `apps` | required | One entry per app. |

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
| `mode` | `on-demand` | `on`, `off` or `on-demand`. Quote `"on"` and `"off"`, or YAML may read them as booleans. |
| `idle` | `30m` | On-demand only: stop after this long without activity. |
| `idle_ignore` | `[]` | Exact paths (query excluded) that are neither activity nor a reason to wake, e.g. a version poller. |
| `heartbeat` | `true` | Inject the heartbeat script into the app's HTML pages. |
| `priority` | `5` | A number from 1 (lowest) to 10 (highest); fractions are allowed (`7.5`). It decides headroom and who may evict whom. |
| `memory` | – | The declared need. The learned peak raises it. |
| `memory_source` | `proctree` | Where the peak is learned from: `proctree` (RSS of the started process tree, sampled every 5 s), `systemd` (needs `systemd_unit`; reads the unit's `MemoryPeak`), or `none`. |
| `headroom` | – | An absolute headroom for this app, replacing the curve. |
| `evictable` | `true` | `false` means this app is never evicted. |

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

1. waker picks candidates: apps it started that are `on-demand`, up, with nothing in flight, not protected, and of a lower priority (or equal, under `lower-or-equal`).
2. If stopping all of them couldn't cover the shortfall, nothing is stopped and the visitor sees "busy".
3. Otherwise it stops them one at a time in the configured order, re-measuring after each, and boots as soon as there's room.

`on`-mode apps are never evicted.

## `branding`

| Key | Default | Meaning |
|---|---|---|
| `logo` | a neutral built-in mark | An SVG or PNG file, read at start and served at `/__waker/logo`. |
| `accent` | `#c6ff4a` | The `#rgb` / `#rrggbb` colour of the starting indicator. |
| `name` | – | Your brand: the logo's alt text. |
