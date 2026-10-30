# `secrets/` — waker's encrypted secrets

age-encrypted and committed next to the code; decrypted with the waker age key
(`~/.config/age/waker.key`). Pattern and commands: devbox `docs/repo-secrets.md`.

| File | Holds | Read by |
|---|---|---|
| `recipients.txt` | the owners' age public keys | `repo-secrets` (encrypt target) |
| `waker-terraform-state.env.age` | `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` of the R2 token `waker-terraform-state` (Object Read & Write on bucket `waker-backup`) | `mise run tf:github` (in memory, via `repo-secrets exec`) |
