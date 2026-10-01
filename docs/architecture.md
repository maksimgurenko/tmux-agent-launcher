# Architecture

tmux-agent-launcher selects a project and agent profile, creates or reuses their live tmux session, and attaches it in a fresh terminal window. It runs on Linux. Desktop integration is optional; agent authentication, model settings and saved conversations belong to each agent's own CLI.

## Repository layout

| Path | Responsibility |
|---|---|
| `cmd/tmux-agent-launcher/` | Executable entry point |
| `internal/launcher/` | CLI, configuration, discovery, session management, process execution and adapter tests |
| `internal/launcher/default-config.toml` | Embedded example emitted by `config init` |
| `scripts/` | Build, release and installation tools; installer tests |
| `examples/` | Optional desktop integration examples |
| `docs/` | Configuration reference, architecture, upstream facts and decision records |

The implementation stays in one internal Go package because these components serve one executable and are not a public library API. Tests sit beside the behavior they exercise. See [CONTRIBUTING.md](../CONTRIBUTING.md) for commands and [CONTEXT.md](../CONTEXT.md) for terminology.

## Launch flow

1. Merge built-in defaults, the selected TOML file and explicit CLI options; reject unknown keys and invalid values.
2. Offer managed live sessions and new-session actions for enabled profiles with available executables; defer project-dependent executable checks until a project is selected. Selecting a live session skips discovery and does not require its original profile or executable.
3. Discover eligible directories beneath search roots, or accept `--directory` with an explicit profile. Canonicalize directory aliases before computing session identity.
4. Lock that profile/directory identity on the canonical tmux socket path and check again for an existing session. Concurrent launches through equivalent server connections reuse one session.
5. For a new session, start the same executable as a short-lived runner. It reads a restricted launch snapshot, removes the snapshot, changes directory and replaces itself with the native agent process.
6. Attach by stable tmux session ID, retaining the selected tmux socket. A new terminal receives the attachment command as arguments.

Agent exit ends the session. Closing a terminal detaches its client without ending a running agent. There is no extra shell kept alive after agent exit, saved-conversation index or agent-team orchestration.

## Session identity and names

Identity is the pair of profile ID and canonical project directory. Names retain `/{profile}/absolute/project/path/`; distinct profiles can run the same agent independently in one project. tmux 3.7c+ is required to preserve literal punctuation. Format-sensitive characters are escaped when passing names to tmux; later operations target session IDs.

Session-local ownership metadata identifies launcher sessions even after profiles are disabled or removed. Legacy `/codex/.../` and `/claude/.../` sessions are recognized without renaming. Inherited global metadata does not make ordinary sessions belong to the launcher.

## Discovery and adapters

Git discovery recognizes working trees, nested repositories, submodules and linked worktrees beneath configured roots. The `all` mode permits ordinary directories. Launcher exclusions apply independently of Git ignore files. Explicit symlink roots are resolved, directory aliases are deduplicated, and directory symlinks are not recursively followed. Git metadata is never traversed.

Rofi, Fuzzel, Wofi and fzf adapters validate selection identity instead of resolving ambiguous labels. Cancellation returns without launching anything. For desktop fzf launches, one terminal hosts both selection and attachment; cancellation closes that window.

Kitty, Foot, Alacritty, WezTerm and custom terminal templates preserve argument boundaries. `float` and `tile` select window identities, while the compositor controls actual placement. Configuration arrays never implicitly invoke a shell.

## Build and installation

Release binaries include explicit version, public source revision, runtime-source digest and local-change state. The digest includes runtime files, module metadata and embedded configuration, including their relative names. Builds use `-trimpath`, `-buildvcs=false` and disabled cgo.

Installation is user-level and explicitly versioned. Ownership hashes protect existing files; updates require `--update`, retain the previous executable/notices for rollback and preserve configuration. Uninstallation removes only unchanged owned files. System packages, agent CLIs and compositor configuration are managed separately.

The architectural choices and their trade-offs are recorded in [the ADRs](adr/).
