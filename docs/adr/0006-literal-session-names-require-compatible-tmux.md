---
status: accepted
---

# Require tmux support for literal names

Some older tmux releases normalize periods and colons in names, which breaks the readable path contract. The launcher requires tmux 3.7c+, the tested minimum, rather than silently normalizing paths or maintaining a second escaping scheme.

Names preserve literal punctuation with explicit handling of tmux format expansion. Attachment and later queries use stable session IDs, and unsupported control characters are rejected. Tests cover punctuation, spaces and concurrent creation. [Upstream evidence](../upstream.md).
