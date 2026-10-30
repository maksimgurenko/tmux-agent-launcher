# Configuration

The default file is `$XDG_CONFIG_HOME/tmux-agent-launcher/config.toml`, falling back to `~/.config/tmux-agent-launcher/config.toml` when XDG configuration home is unset, empty or relative. Defaults work without a file. Create the complete example with:

```sh
tmux-agent-launcher config init
```

Existing files are preserved. `--config FILE` selects another file and fails if that file is missing. Precedence is built-in defaults, TOML, then explicit CLI options. Configuration is read on each launch. Profile command changes affect newly created sessions; live sessions retain their running agent.

## Terminal and menu

Place top-level keys before any TOML table:

```toml
picker = "rofi"
terminal = "kitty"
presentation = "float"
```

| Setting | Values |
|---|---|
| `picker` | `rofi`, `fuzzel`, `wofi`, `fzf` |
| `terminal` | `kitty`, `foot`, `alacritty`, `wezterm`, `custom` |
| `presentation` | `float`, `tile` |

Install the selected programs independently. Menu styling uses the provider's own configuration. Additional menu providers require an adapter; arbitrary menu command templates are not supported. fzf needs a TTY; a desktop launch opens one terminal for selection and attachment.

Session and project searches are case insensitive, including queries containing uppercase letters: `new`, `New` and `NEW` all match `New`. Result ordering uses the provider's existing settings and scoring; matching capitalization does not receive a separate ranking bonus.

## Agent profiles

The presets are Codex (`codex --yolo`), Claude (`claude --dangerously-skip-permissions`), Pi (`pi --approve`) and OpenCode (`opencode --auto`). These select each CLI's supported unattended policy; their semantics differ. [Upstream references](upstream.md).

Override a preset or add another named profile:

```toml
[profiles.codex]
command = ["codex", "--yolo"]

[profiles.codex-work]
label = "Codex (work)"
command = ["codex", "--yolo"]
env = { EDITOR = "nvim" }

[profiles.pi]
enabled = false
```

IDs allow letters, digits, underscores and hyphens. An added profile requires a nonempty command array; its label defaults to its ID, and `enabled` defaults to true. Omitted preset fields remain intact. Command arrays replace the whole command, with each argument represented separately. Environment maps replace that profile's configured map and override inherited values for the agent process. Credentials and native agent settings stay with the agent CLI or inherited environment.

Each new agent receives the launcher's current environment snapshot plus its profile overrides and tmux connection variables. If the snapshot omits TERM, the agent receives tmux's terminal type. Unrelated variables removed from the launcher environment are not restored from an older tmux server. Relative commands and relative or empty PATH entries resolve against the selected project directory. The menu and `doctor` defer these executable checks until a project is selected.

`codex-work` gets distinct sessions from `codex`, even when both launch the same CLI. Disabling/removing a profile or uninstalling its executable does not hide its live sessions.

## Search roots

```toml
[search]
roots = ["~/projects", "~/src"]
mode = "git"
max_depth = 7
cache = true
exclude = [".git", ".cache", "node_modules", ".venv", "target"]
```

`mode = "git"` discovers Git working trees; `mode = "all"` makes ordinary directories eligible. Roots default to `["~"]`. `~` and `~/...` expand; other relative paths use the invocation's working directory. Arrays replace their defaults rather than concatenate.

`max_depth` limits traversal below each search root. The root is depth 0; `max_depth = 7` includes directories at depth 7 and does not read their children. The default, `0`, preserves unlimited depth. Add a deeper collection as another search root to search it independently. `--max-depth N` overrides the configured value; negative values are rejected.

Exclusions are globs matched against directory basenames, or paths relative to a root when the pattern contains `/`. Discovery recognizes nested repositories, submodules and worktrees. It ignores `.gitignore`, excludes bare repository storage in Git mode and never traverses `.git` metadata. Linked worktrees outside the roots are not added automatically. Canonical directories are deduplicated; directory symlinks are not recursively followed. Missing roots are diagnosed while other roots continue. Paths containing control characters are unsupported.

Project caching is enabled by default. The first search scans and saves its results. Later searches open the picker from that saved list without walking the roots. When the list is at least five minutes old, using it starts a detached refresh; the current picker keeps its original list and the next invocation sees the refreshed results. There is no persistent daemon or timer. Set `cache = false` to scan on every search.

To make a newly created project visible immediately, or prepare the cache before opening a picker, run:

```sh
tmux-agent-launcher refresh
tmux-agent-launcher refresh --max-depth 7
```

Refresh uses the same configuration and search overrides as a normal launch, without requiring tmux, a picker or a terminal. Changing roots, discovery mode, exclusions or depth selects a separate cache; relative roots are resolved using the invocation's directory. Use identical overrides for refresh and launch. A missing or corrupt cache is rebuilt; unavailable cache storage falls back to an uncached scan. The first scan after a settings change can still take time.

Cached paths are rechecked after selection. Deleted paths, paths redirected to another canonical directory, and Git projects that lost their metadata are rejected with a refresh hint. Existing live-session selection continues to skip discovery. Cancellation starts no agent; an already started background refresh can finish independently.

Private JSON lists and refresh locks live under `$XDG_CACHE_HOME/tmux-agent-launcher/projects` (default `~/.cache/tmux-agent-launcher/projects`). Refreshes serialize per search configuration and replace lists atomically, so readers can use the previous complete list during a refresh. These regenerable files can be removed when the launcher is not in use.

## Custom terminals and Ghostty

A custom terminal must accept a child executable and its arguments. Exactly one standalone `{command}` expands to that argument list; `{app_id}` expands to `tmux-floating` or `tmux-tiling`. Other placeholders are rejected. No shell evaluation is added by the launcher.

For Ghostty on Linux, this template matches its documented `-e` and class options:

```toml
terminal = "custom"

[terminals.custom]
command = ["ghostty", "--class=org.tmuxagentlauncher.{app_id}", "-e", "{command}"]
```

Set the top-level `terminal` field in its existing location, before tables. Ghostty requires a valid GTK application ID, hence the dotted prefix. Its `-e` mode passes arguments without shell expansion and starts a separate instance. See [Ghostty's class option](https://ghostty.org/docs/config/reference#class), [initial command](https://ghostty.org/docs/config/reference#initial-command) and [GTK ID rules](https://docs.gtk.org/gio/type_func.Application.id_is_valid.html).

Compositor rules must match `org.tmuxagentlauncher.tmux-floating` / `org.tmuxagentlauncher.tmux-tiling` for this template. The supplied Hyprland example uses the built-in adapters' unprefixed identities; adapt its class expressions and reload the compositor. The Ghostty template is documentation-based and has not had a real graphical workflow check. Kitty's real workflow is tested; the remaining built-in adapters have contract tests, as described in [README.md](../README.md#requirements).

## Overrides and checks

```sh
tmux-agent-launcher doctor
tmux-agent-launcher doctor --terminal foot --picker fuzzel
tmux-agent-launcher --terminal foot --picker fuzzel
tmux-agent-launcher --profile codex-work
tmux-agent-launcher --profile codex --directory ~/projects/example
tmux-agent-launcher --root ~/projects --root ~/src --discovery all
tmux-agent-launcher --max-depth 7
tmux-agent-launcher refresh --root ~/projects --max-depth 7
```

`--directory` requires `--profile` and bypasses discovery. Repeated `--root` flags replace configured roots. `--tmux-socket FILE` selects another server and preserves it across terminal attachment. `--help` lists all options. Unknown TOML keys, unsupported choices and invalid commands are errors. `doctor` checks configuration and dependencies without starting agents; optional missing agent executables are informational.
