# tmux-agent-launcher

Language for designing an independently usable launcher for interactive coding agents and their working sessions.

## Language

**Coding agent**:
An interactive coding assistant operated through a command-line program.
_Avoid_: Model, wrapper

**Agent profile**:
A named definition of how to launch a coding agent. Different profiles may refer to the same coding agent with different launch choices.
_Avoid_: Agent, Codex profile

**Preset**:
An agent profile supplied with the launcher as a starting point for user configuration.
_Avoid_: Supported-agent allowlist

**Project directory**:
The directory selected as the starting location for a coding agent's work.
_Avoid_: Session

**Search root**:
A configured directory beneath which the launcher discovers eligible project directories.
_Avoid_: Project directory

**Discovery mode**:
The rule determining which directories beneath search roots can be selected as projects: Git repositories or all directories.
_Avoid_: Agent profile

**Live session**:
An existing, running workspace that remains available for reattachment after its terminal window closes.
_Avoid_: Saved conversation

**Session identity**:
The combination of an agent profile and a canonical project directory that identifies one live session.
_Avoid_: Session name, conversation ID

**Session name**:
The readable tmux name formed from a profile identifier and the project's canonical directory path, such as `/codex/home/example/project/`.
_Avoid_: Conversation ID

**Saved conversation**:
Interaction history retained by a coding agent that can survive the end of a live session.
_Avoid_: Live session, tmux session
