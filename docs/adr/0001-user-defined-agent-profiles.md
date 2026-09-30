---
status: accepted
---

# Extend agent support through profiles

A fixed list of agents would require launcher changes whenever another CLI is added. The launcher accepts user-defined profiles and supplies Claude, Codex, Pi and OpenCode presets. A profile contains a display label, native command arguments and optional environment overrides. Different profiles may run the same CLI independently.

This keeps agent authentication and model settings with the native CLI. Agent-team orchestration remains outside the launcher's scope.
