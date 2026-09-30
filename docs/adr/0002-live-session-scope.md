---
status: accepted
---

# Manage live tmux sessions

The launcher starts coding agents and reattaches active tmux sessions. Discovering saved conversations would require separate history integrations for each agent, with different resume semantics and storage formats. Saved-conversation selection therefore remains with each agent's own interface or a user-defined launch command.

Closing a terminal leaves a running agent available for reattachment. When the agent exits, its live session ends.
