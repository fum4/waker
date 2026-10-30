# TODO — owner

Things only the owner can do. Clear and delete each one when done.

## Apply the GitHub ruleset (infra/github) · 2026-10-30

Until it's applied, GitHub reports no required checks and `wt merge` refuses every PR
to `master`. Needed once, in this order:

1. **R2 bucket** — Cloudflare dashboard → R2 Object Storage → **Create bucket** →
   name `waker-backup`, location **Specify jurisdiction → European Union (EU)** →
   Create. (Out-of-band by doctrine: it holds Terraform's own state.)
2. **waker age key** — the repo's first secret needs its own key: the agent runs
   `age-keygen` into `~/.config/age/waker.key`, scaffolds `secrets/` and escrows the
   key in devbox; you save the private key in Bitwarden as **"waker age key"**.
3. **R2 token** — R2 Object Storage → **Manage API tokens** → **Create Account API
   token** → name `waker-terraform-state`, permission **Object Read & Write**,
   **Apply to specific buckets only** → `waker-backup`, TTL forever → Create. Leave
   the page open on the Access Key ID / Secret Access Key (shown once) and tell the
   agent; it opens `mise run secrets:edit secrets/waker-terraform-state.env.age`
   (tmpfs) and you paste them as `AWS_ACCESS_KEY_ID=…` / `AWS_SECRET_ACCESS_KEY=…`.
   Never Bitwarden.
4. Then the agent runs `mise run tf:github -- init`, `plan` (expect 1 to add),
   `apply`, and checks `gh pr checks <n> --required` on an open PR to `master`.
