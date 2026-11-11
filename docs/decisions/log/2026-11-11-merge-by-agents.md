# waker merges by agents: [merge] by = "agents", the practice before cerebro config, with the standard approval_paths

decided: tier=flag reversible=yes veto-by=2026-11-13

waker had no factory.toml, so no old tier to map. Agents already merged here through wt merge once the ruleset's required checks passed, so by = "agents" keeps today's practice. The owner may want by = "owner" instead: a one-line change in cerebro/repo.toml. approval_paths is the standard list; note infra/terraform/** does not cover this repo's ruleset stack in infra/github/, left as the standard list pending the owner.

By: cerebro-layout migration builder, pending the owner
