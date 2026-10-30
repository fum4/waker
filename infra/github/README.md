# infra/github — the repo's merge gate

One ruleset on **`fum4/waker`**, `default-branch`, targeting the default branch
(`master`). waker is public, so its CI is GitHub Actions and this ruleset is the
merge gate: `wt merge` and the web UI both read its required checks (devbox
`docs/ci.md` § Public repos).

| Rule | Setting | Why |
|---|---|---|
| `pull_request` | 0 approvals | every change is a PR; agents and the owner are one account, and GitHub never lets an author approve their own PR |
| `required_status_checks` | `lint`, `test`, `e2e`, `build`, from GitHub Actions only; not strict | the job names of [`ci.yml`](../../.github/workflows/ci.yml); `wt pr` already rebases, and strict mode re-runs every PR after each merge |
| `deletion`, `non_fast_forward` | on | `master` can't be deleted or rewritten |

No bypass actors, the admin role included: a web-UI merge is gated like `wt merge`.

**Renaming a job in `ci.yml` is a change to `required_checks` in `ruleset.tf`, in
the same PR**, or every PR waits on a check that never reports. Never add
`paths` / `paths-ignore` to `ci.yml` for the same reason.

## Depends on

- **GitHub auth**: the devbox's own `gh` identity. `mise run tf:github` exports
  `GITHUB_TOKEN=$(gh auth token)` in memory (needs scope `repo`, admin on the repo).
  No secret of this repo's.
- **R2 state**: bucket `waker-backup` (EU), key `terraform/github.tfstate`, with the
  `AWS_*` keys of the R2 token `waker-terraform-state` from
  `secrets/waker-terraform-state.env.age` (devbox `docs/repo-secrets.md` § Infra
  state). Until that bucket and token exist, see the root `TODO.md`.

## Run

```bash
mise run tf:check              # fmt + validate, offline (pre-push runs it)
mise run tf:github -- init     # once per checkout
mise run tf:github -- plan
mise run tf:github -- apply
```

Recovery that needs a rewrite of `master` (a force-push): set `enforcement =
"disabled"`, apply, do the recovery, set it back to `"active"`, apply. Never a
console edit: the next apply reverts it.
