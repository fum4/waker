# Ideas

Roadmap ideas: neither a decision nor agreed engineering yet. Heading: `## <title> · <YYYY-MM-DD>`.

## Live config reload (file watch or SIGHUP) · 2026-10-31

- **What:** apply an edited `waker.yaml` to a running daemon, without a restart: app keys, pages and branding at once, new apps started listening, removed apps drained.
- **Why it's only an idea:** the owner chose one rule instead: waker reads the file only at start, and every key applies on its next start, however that start happens (`waker restart`, a crash restart, a reboot). Nothing restarts waker because the file changed. Live reload would bring back a second way for config to take effect, and with it the question of which keys apply when.
- **Revisit when:** a user needs an edit to apply without the visitor-facing cost of a restart (the apps waker started stop with it).
