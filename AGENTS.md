# waker: repo rules

Scale-to-zero for self-hosted apps: waker starts an app on its first request and stops it when
nobody uses it. One static Go binary, one YAML file. Overview: [README.md](README.md).

This file adds only what is specific to waker. The cross-repo rules are the owner's global agent
rules (devbox `agents/AGENTS.md`). This repo is **public**: GitHub Actions is its CI.

## Routing

| Occasion | Doc | Guardrail |
|---|---|---|
| Add or change a config key | [docs/configuration.md](docs/configuration.md), [README.md § Every key](README.md#every-key-with-its-default) | The file is read only at start; every key applies on the next start, never on a file change ([ledger/ideas.md](ledger/ideas.md) § Live config reload) |
| Change what happens on a request, idle, memory, dependencies or schedules | [docs/behaviour.md](docs/behaviour.md) | Traffic must go through `listen`; nothing is stopped mid-request |
| Change the CLI, `init` / `setup`, the service or updating | [README.md § CLI](README.md#cli) | Never runs sudo itself (prints the line); never overwrites a unit or file it didn't write without `--force` |
| Ship a user-facing change | [CHANGELOG.md](CHANGELOG.md) | The CHANGELOG records every change, under `## [Unreleased]` until a release (Keep a Changelog; pre-1.0 minors may break config) |
| Cut a release | [README.md § Status](README.md#status), `.github/workflows/release.yml` | A `v*` tag publishes; fum4/releases signs and serves it |
| Change the repo ruleset | [infra/github/README.md](infra/github/README.md) | The required checks are declared as code there |
| Run the checks | [README.md § Status](README.md#status) | CI is the gate; `mise run pre-push` / `pre-merge` are fast local feedback |
| File a follow-up or an idea | [ledger/backlog.md](ledger/backlog.md), [ledger/ideas.md](ledger/ideas.md) | `## <title> · <YYYY-MM-DD> · S\|M\|L`; owner asks are files in `ledger/asks/` |

## Asks and decisions

About to ask the owner anything, just chose between options, or just heard a decision → the
`cerebro-ledger` skill: it's a file in the repo (`ledger/asks/`, `docs/decisions/log/`), not a
chat message, even if the skill didn't fire.

## Merging

Agents may merge here (`[merge] by = "agents"` in [cerebro/repo.toml](cerebro/repo.toml)): PR,
then CI's required checks and `wt pre-merge` green, then `wt merge`. A PR touching an
`approval_paths` entry needs the owner's go.
