# TODO — owner

Things only the owner can do. Clear and delete each one when done.

## Save the waker age key in Bitwarden · 2026-10-30

The key lives on the box (`~/.config/age/waker.key`) and in the devbox escrow
(fum4/devbox#162); its durable home is Bitwarden. Over SSH run
`cat ~/.config/age/waker.key` and save the whole output in Bitwarden as a secure
note named **"waker age key"**. Then delete this file.
