---
status: accepted
---

# Use unattended agent presets

Presets enable each agent's supported unattended launch policy to keep session creation usable from a desktop hotkey. Commands remain configurable so users can select another policy without changing launcher code or native configuration files.

The flags have different meanings across agents: Pi's project-trust control differs from per-tool approval, and OpenCode's automatic approvals still honor explicit denies. Documentation records each actual behavior instead of treating all presets as an identical permission model. [Upstream references](../upstream.md).
