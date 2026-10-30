# TODO — owner

Things only the owner can do. Clear and delete each one when done.

## Apply the GitHub ruleset (infra/github) · 2026-10-30

Until it's applied, GitHub reports no required checks and `wt merge` refuses every PR
to `master`. Needed once, in this order:

1. **R2 bucket** `waker-backup` (EU) — created by the hub with `devbox-r2-bucket-create`;
   nothing to click.
2. **waker age key → Bitwarden** — the key exists (`~/.config/age/waker.key`, escrowed
   in devbox). Over SSH run `cat ~/.config/age/waker.key` and save the whole output
   in Bitwarden as **"waker age key"** (a secure note).
3. **R2 token** — R2 Object Storage → **Manage API tokens** → **Create Account API
   token** → name `waker-terraform-state`, permission **Object Read & Write**,
   **Apply to specific buckets only** → `waker-backup`, TTL forever → Create. Leave
   the page open on the Access Key ID / Secret Access Key (shown once) and tell the
   agent; it opens `mise run secrets:edit secrets/waker-terraform-state.env.age`
   (tmpfs) and you paste them as `AWS_ACCESS_KEY_ID=…` / `AWS_SECRET_ACCESS_KEY=…`.
   Never Bitwarden.
4. Then the agent runs `mise run tf:github -- init`, `plan` (expect 1 to add),
   `apply`, and checks `gh pr checks <n> --required` on an open PR to `master`.
