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
3. Load cached project directories or discover them beneath search roots, or accept `--directory` with an explicit profile. Revalidate a selected project before creating a session. Canonicalize directory aliases before computing session identity.
4. Lock that profile/directory identity on the canonical tmux socket path and check again for an existing session. Concurrent launches through equivalent server connections reuse one session.
5. For a new session, start the same executable as a short-lived runner. It reads a restricted launch snapshot, removes the snapshot and changes directory. Direct profiles replace it with the native agent process. Explicit shell profiles replace it with a login/interactive Zsh and a private startup shim that enters the configured command once after the first prompt initializes.
6. Attach by stable tmux session ID, retaining the selected tmux socket. A new terminal receives the attachment command as arguments.

Agent exit ends a direct profile's session. A shell profile returns to its interactive shell and ends when that shell exits. Closing a terminal detaches its client without ending either kind of live session. There is no saved-conversation index or agent-team orchestration.

Shell startup lives in `internal/launcher/shell.go` behind the per-profile `shell` setting. The launcher owns the startup hook; user dotfiles are never edited. A one-use 0600 `.zshenv` in a private 0700 directory restores the original `ZDOTDIR` before loading the user's startup chain. A readable-descriptor ZLE callback waits until the main editor starts reading, after startup, prompt hooks and user line-init widgets. Startup `vared` editors suspend the callback until they finish. This entry point survives later prompt-hook array and special-widget assignments.

At the main prompt, the callback closes its descriptor and temporarily binds one ASCII wakeup byte, preserving that binding and any longer sequences. It sends that byte through tmux to its own stable pane using the binary/socket captured before startup. The acceptance widget restores the bindings, assigns the quoted command as editor buffer data, and invokes builtin acceptance; configured bytes never pass through user key bindings. No callback or launcher binding remains active when the command executes. Nested shells and reattachment remain unaffected. Simple non-reserved command names permit normal alias resolution; arguments stay literal. The initial environment snapshot remains authoritative relative to the old tmux server, while the user's shell startup may intentionally modify it. Shell profiles check shell availability before startup and let the initialized shell resolve the agent.

## Session identity and names

Identity is the pair of profile ID and canonical project directory. Names retain `/{profile}/absolute/project/path/`; distinct profiles can run the same agent independently in one project. tmux 3.7c+ is required to preserve literal punctuation. Format-sensitive characters are escaped when passing names to tmux; later operations target session IDs.

Session-local ownership metadata identifies launcher sessions even after profiles are disabled or removed. Legacy `/codex/.../` and `/claude/.../` sessions are recognized without renaming. Inherited global metadata does not make ordinary sessions belong to the launcher.

## Discovery and adapters

Git discovery recognizes working trees, nested repositories, submodules and linked worktrees beneath configured roots. It also permits the search roots themselves and fully empty directories. The `all` mode permits every ordinary directory. Launcher exclusions apply independently of Git ignore files. Optional `exclude_hidden` pruning skips dot-prefixed directories and their descendants below each explicit root; it defaults to false. Explicit symlink roots are resolved, directory aliases are deduplicated, and directory symlinks are not recursively followed. Git metadata is never traversed; Git mode also prunes bare repository storage so its empty metadata directories cannot be selected.

An optional depth limit includes its boundary directories and prunes their children; each explicit root starts at depth 0. Unlimited depth remains the default. Project lists are cached by search settings and expanded absolute roots. A cache hit does not touch search roots. Lists older than five minutes trigger a detached, bounded refresh through the same executable; each picker uses a fixed snapshot. Cold searches populate the cache synchronously, and `refresh` explicitly rebuilds it without opening a UI. Per-cache locks prevent duplicate scans during concurrent cold launches; atomic replacement preserves complete snapshots for readers. Cache failures fall back to discovery. Selected cached paths must still be canonical, accessible directories and eligible for the selected discovery mode.

Rofi, Fuzzel, Wofi and fzf adapters validate selection identity instead of resolving ambiguous labels. Cancellation returns without launching anything. For desktop fzf launches, one terminal hosts both selection and attachment; cancellation closes that window.

Kitty, Foot, Alacritty, WezTerm and custom terminal templates preserve argument boundaries. `float` and `tile` select window identities, while the compositor controls actual placement. Configuration arrays never implicitly invoke a shell.

## Build and installation

Release binaries include explicit version, public source revision, runtime-source digest and local-change state. The digest includes runtime files, module metadata and embedded configuration, including their relative names. Builds use `-trimpath`, `-buildvcs=false` and disabled cgo.

Installation is user-level and explicitly versioned. Ownership hashes protect existing files; updates require `--update`, retain the previous executable/notices for rollback and preserve configuration. Uninstallation removes only unchanged owned files. System packages, agent CLIs and compositor configuration are managed separately.

The architectural choices and their trade-offs are recorded in [the ADRs](adr/).
