# tmux-agent-launcher

Pick a project, start a coding agent in tmux, and reattach its live session from a desktop hotkey or terminal.

```sh
tmux-agent-launcher --profile codex --directory ~/projects/example
tmux-agent-launcher --picker fzf --terminal foot --presentation tile
```

Built-in profiles run **Codex `--yolo`, Claude `--dangerously-skip-permissions`, Pi `--approve`, and OpenCode `--auto`**. These defaults enable the supported unattended modes. Pi normally has no per-tool approval prompts; its flag trusts project configuration for this process. OpenCode's mode honors explicit denies. Change the command arrays to choose different policies. Authentication and native agent settings stay with each CLI. [Upstream flag references](docs/upstream.md).

One live tmux session belongs to each profile and canonical project directory. Its name stays readable: `/codex/absolute/path/to/project/`. Repeated launches attach the same session in a fresh terminal. Profile edits affect future sessions; removed/disabled profiles and missing agent executables do not hide their live sessions. Existing `/codex/.../` and `/claude/.../` sessions are recognized. Sessions end when their agent exits. Saved conversations and agent orchestration are outside this tool's scope.

## Requirements

Linux; **tmux 3.7c or later** for literal punctuation in names; one selected picker (Rofi, Fuzzel, Wofi or fzf); one selected terminal (Kitty, Foot, Alacritty, WezTerm or custom argv). Git is required for Git discovery, the default. Install the agent CLIs you want independently. No private launch helper is required.

Hyprland is optional. Floating/tiling flags set `tmux-floating` / `tmux-tiling` window identities; placement requires compositor rules. Graphical pickers need their normal graphical session. fzf needs a TTY: a desktop launch opens one fresh terminal for selection and attachment; a launch from an existing TTY selects there and attaches in a fresh terminal.

Tested on Arch Linux x86_64 with tmux 3.7c, Go 1.26.0/1.27.1, Kitty 0.49.1, Rofi 2.0.0 and fzf 0.74.4, including fresh Kitty attachments, float/tile placement under Hyprland 0.56.2, and desktop fzf selection/cancellation. Both Linux amd64 and arm64 binaries are built. Fuzzel, Wofi, Foot, Alacritty and WezTerm have adapter contract tests; their full graphical workflows and native ARM64 runtime are untested. See [development checks](CONTRIBUTING.md). Agent providers can change their flags; overrides remain available.

## Install

Releases contain a compiled executable; Go is unnecessary for binary installation. The installer requires Bash, curl for downloads, GNU tar/coreutils and awk. It installs only the launcher plus its license/notices, keeps configuration, and requires an explicit version. Initial release candidates may exist locally before the first public release.

From an extracted source checkout of the selected tag:

```sh
bash install.sh --version v0.1.0 --repo maksimgurenko/tmux-agent-launcher
export PATH="$HOME/.local/bin:$PATH"
tmux-agent-launcher --version
tmux-agent-launcher doctor
```

For manual downloads, download the matching architecture archive, `checksums.txt` and this revision's `install.sh` from the chosen repository/release. Verify the archive against the published checksum, then supply that digest:

```sh
bash install.sh --archive ./tmux-agent-launcher-v0.1.0-linux-amd64.tar.gz --checksum SHA256_FROM_CHECKSUMS
```

Checksums detect corruption; use a repository and release you trust. The installer refuses existing unmanaged files and symlinks. `--prefix /absolute/path` changes the executable directory to `PREFIX/bin`; `--data-dir /absolute/path` changes notices and ownership state from `$XDG_DATA_HOME/tmux-agent-launcher` (default `~/.local/share/tmux-agent-launcher`). These options must be reused on update/removal.

To build from source, install Go 1.26 or later plus the installer utilities, and use a checked-out revision:

```sh
./build.sh
./build/tmux-agent-launcher doctor
./release.sh v0.1.0
# Supply the digest printed in dist/checksums.txt to install.sh --archive.
```

The first build downloads the declared TOML module through Go's module mechanism. `CGO_ENABLED=0`, `-trimpath` and `-buildvcs=false` are set. `--version` reports version, explicit public base, runtime-source digest, and local drift against `EXPECTED_SOURCE_DIGEST`. An ordinary development build reports `public-base=unpublished`; it does not invent a published revision.

For AI-assisted installation: **“Install this project using [INSTALL.agent.md](INSTALL.agent.md) from this release.”**

## Configure

Defaults work without a config file: Rofi, Kitty, floating windows, home search root, Git working trees, and four enabled profiles. Create a private example with:

```sh
tmux-agent-launcher config init
```

The file is `$XDG_CONFIG_HOME/tmux-agent-launcher/config.toml`, falling back to `~/.config/tmux-agent-launcher/config.toml`. An empty or relative XDG variable uses the fallback. `--config FILE` selects a file and errors when it is missing. Precedence: defaults, TOML, explicit CLI flags. Unknown keys and invalid values are errors. `config init` preserves existing files.

| TOML key | Type / behavior |
|---|---|
| `picker` | `"rofi"`, `"fuzzel"`, `"wofi"` or `"fzf"` |
| `terminal` | `"kitty"`, `"foot"`, `"alacritty"`, `"wezterm"` or `"custom"` |
| `presentation` | `"float"` or `"tile"` |
| `search.roots` | Directory array; default `["~"]`; `~` and `~/...` expand; relative paths use invocation cwd |
| `search.mode` | `"git"` or `"all"` |
| `search.exclude` | Glob array; defaults `.git`, `.cache`, `node_modules`, `.venv`, `target`; basename matching, or root-relative matching for patterns containing `/` |
| `profiles.ID.label` | Display label; ID must contain letters, digits, underscore or hyphen |
| `profiles.ID.enabled` | Boolean, default true; false suppresses new sessions |
| `profiles.ID.command` | Nonempty argv array; overrides replace the preset argv completely |
| `profiles.ID.env` | String map; replaces that profile's configured map, overrides inherited agent environment |
| `terminals.custom.command` | Terminal argv template; exactly one standalone `{command}` expands child argv; `{app_id}` expands window identity |

All root/exclusion/command arrays replace their defaults. Omitted preset fields stay intact. Configuration uses argv, without shell evaluation; explicitly select a shell executable if you need shell semantics. Credentials belong in the agent's own credential store or inherited environment; avoid putting secrets into command arguments. Environment values are not included in doctor output.

See [examples/config.toml](examples/config.toml) for a second Codex profile and custom terminal. The profile ID determines the session prefix even when two profiles run the same CLI.

Discovery includes nested repositories, submodules and worktrees beneath the selected roots; it recognizes `.git` directories/files, excludes bare Git storage in Git mode, and ignores `.gitignore`. It deduplicates canonical paths. Directory symlinks are not recursively followed; explicit symlink roots are resolved. Linked worktrees outside roots are not added automatically. `.git` metadata is never scanned. In `all` mode ordinary directories are eligible. Missing roots are diagnosed while other roots continue. Ctrl-C cancels traversal. Paths containing control characters are unsupported.

CLI overrides: `--profile ID`, `--directory DIR` (requires profile, bypasses discovery), `--root DIR` (repeat), `--discovery git|all`, `--picker`, `--terminal`, `--presentation`, `--config`. `--tmux-socket FILE` selects an explicit server, useful for disposable checks. Attachment retains the selected server even when launched from inside another tmux client. `--help` lists options. Selecting a menu cancellation does nothing. Full paths distinguish equal basenames; picker results are validated by identity.

## Desktop integration

Merge the small [Hyprland 0.55+ Lua example](examples/hyprland.lua) into your configuration, choosing unused bindings, then run `hyprctl reload`. Use `hyprctl clients` to verify the window's class and floating state. Other compositors need equivalent rules. The installer never edits desktop configuration. New attachments open fresh windows; it does not focus or recycle existing windows. WezTerm starts a new process to preserve window-class selection.

## Update, rollback, remove

Use the original installation method, with an exact newer version and `--update`. No automatic updater is installed. Source users rebuild the selected source and explicitly install its archive. The installer keeps the previous binary/notices for one-step rollback:

```sh
bash install.sh --version v0.2.0 --update
bash install.sh --rollback
bash install.sh --uninstall
```

Reuse any custom prefix/data directory. Repeat installation of identical content is a no-op. Changed owned files block updates; uninstall preserves changed files and backups. Configuration is never removed. Installation records are small dotfiles in the data directory. The launcher also creates hashed lock files beneath `$XDG_CACHE_HOME/tmux-agent-launcher/locks` (default `~/.cache`). Short-lived 0700 temporary directories with 0600 launch snapshots carry the invoking environment to tmux, then are removed by the child; they keep values out of tmux's command arguments. An interrupted child startup can leave such a directory; inspect and remove it locally. Close sessions separately when desired; uninstall does not terminate running agents.

`doctor` reports incompatible tmux and missing selected picker/terminal/Git. Missing individual agent executables are informational; live sessions remain attachable. If no new action appears, install/enable a profile or correct its command/PATH. If a window opens with the wrong placement, inspect compositor rules. If an agent exits immediately, check its own CLI/authentication and selected directory. Readable names require recent tmux; older versions are rejected.

MIT licensed. [NOTICE](NOTICE) includes the TOML dependency license. Contributions and forks: [CONTRIBUTING.md](CONTRIBUTING.md).
