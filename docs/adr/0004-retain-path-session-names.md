---
status: accepted
---

# Retain readable project paths in session names

Names use `/{profile}/absolute/project/path/` so users can identify sessions with ordinary tmux tools. Short generated names would need a second identity lookup to recover the project path. The leading segment is the profile ID; `codex` and `codex-work` stay distinct even when both launch the same CLI.

Paths are canonicalized for identity, while operations target stable tmux session IDs. Retaining literal names imposes the compatibility requirement recorded in [ADR 0006](0006-literal-session-names-require-compatible-tmux.md).
