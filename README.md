# tmux-agent-launcher

Pick a project, start a coding agent in tmux, and reattach its live session from a desktop hotkey or terminal.

```sh
tmux-agent-launcher --profile codex --directory ~/projects/example
tmux-agent-launcher --picker fzf --terminal foot --presentation tile
```

Built-in profiles run **Codex `--yolo`, Claude `--dangerously-skip-permissions`, Pi `--approve`, and OpenCode `--auto`**. These defaults enable the supported unattended modes. Pi normally has no per-tool approval prompts; its flag trusts project configuration for this process. OpenCode's mode honors explicit denies. Change the command arrays to choose different policies. Authentication and native agent settings stay with each CLI. [Upstream flag references](docs/upstream.md).

One live tmux session belongs to each profile and canonical project directory. Its name stays readable: `/codex/absolute/path/to/project/`. Repeated launches attach the same session in a fresh terminal. Profile edits affect future sessions; removed/disabled profiles and missing agent executables do not hide their live sessions. Existing `/codex/.../` and `/claude/.../` sessions are recognized. Sessions end when their agent exits. Saved conversations and agent orchestration are outside this tool's scope.

## Requirements

Linux; **tmux 3.7c or later** for literal punctuation in names; one selected picker (Rofi, Fuzzel, Wofi or fzf); one selected terminal (Kitty, Foot, Alacritty, WezTerm or custom argv). Git is required for Git discovery, the default. Install the agent CLIs you want independently.

Hyprland is optional. Floating/tiling flags set `tmux-floating` / `tmux-tiling` window identities; placement requires compositor rules. Graphical pickers need their normal graphical session. fzf needs a TTY: a desktop launch opens one fresh terminal for selection and attachment; a launch from an existing TTY selects there and attaches in a fresh terminal.

Tested on Arch Linux x86_64 with tmux 3.7c, Go 1.26.0/1.27.1, Kitty 0.49.1, Rofi 2.0.0 and fzf 0.74.4, including fresh Kitty attachments, float/tile placement under Hyprland 0.56.2, and desktop fzf selection/cancellation. Both Linux amd64 and arm64 binaries are built. Fuzzel, Wofi, Foot, Alacritty and WezTerm have adapter contract tests; their full graphical workflows and native ARM64 runtime are untested. See [development checks](CONTRIBUTING.md). Agent providers can change their flags; overrides remain available.

## Install

Releases contain a compiled executable; Go is unnecessary for binary installation. The installer requires Bash, curl for downloads, GNU tar/coreutils and awk. It installs only the launcher plus its license/notices, keeps configuration, and requires an explicit version.

From an extracted source checkout of the selected tag:

```sh
bash scripts/install.sh --version v0.1.1 --repo maksimgurenko/tmux-agent-launcher
export PATH="$HOME/.local/bin:$PATH"
tmux-agent-launcher --version
tmux-agent-launcher doctor
```

For manual downloads, download the matching architecture archive, `checksums.txt` and this revision's `scripts/install.sh` from the chosen repository/release. Verify the archive against the published checksum, then supply that digest:

```sh
bash scripts/install.sh --archive ./tmux-agent-launcher-v0.1.1-linux-amd64.tar.gz --checksum SHA256_FROM_CHECKSUMS
```

Checksums detect corruption; use a repository and release you trust. The installer refuses existing unmanaged files and symlinks. `--prefix /absolute/path` changes the executable directory to `PREFIX/bin`; `--data-dir /absolute/path` changes notices and ownership state from `$XDG_DATA_HOME/tmux-agent-launcher` (default `~/.local/share/tmux-agent-launcher`). These options must be reused on update/removal.

To build from source, install Go 1.26 or later plus the installer utilities, and use a checked-out revision:

```sh
./scripts/build.sh
./build/tmux-agent-launcher doctor
./scripts/release.sh v0.1.1
# Supply the digest printed in dist/checksums.txt to scripts/install.sh --archive.
```

The first build downloads the declared TOML module through Go's module mechanism. `CGO_ENABLED=0`, `-trimpath` and `-buildvcs=false` are set. `--version` reports version, explicit public base, runtime-source digest, and local drift against `EXPECTED_SOURCE_DIGEST`. An ordinary development build reports `public-base=unpublished`; it does not invent a published revision.

For AI-assisted installation: **“Install this project using [INSTALL.agent.md](INSTALL.agent.md) from this release.”**

## Configure

Defaults work without a file: Rofi, Kitty, floating windows, home search root, Git working trees and four enabled profiles. Create an example, preserving any existing configuration:

```sh
tmux-agent-launcher config init
```

Edit `$XDG_CONFIG_HOME/tmux-agent-launcher/config.toml` (default `~/.config/tmux-agent-launcher/config.toml`). Configuration is read on each launch. Defaults are overridden by TOML, then explicit CLI flags:

```toml
picker = "rofi"
terminal = "kitty"
presentation = "float"

[search]
roots = ["~/projects"]
mode = "git"

[profiles.codex-work]
label = "Codex (work)"
command = ["codex", "--yolo"]
```

See the [configuration guide](docs/configuration.md) for all fields, profile overrides, exclusions, custom terminals including Ghostty, menu providers and CLI examples. Arrays contain separate arguments and replace defaults; no implicit shell evaluation is added. Existing live sessions keep their running agent after profile edits.

## Desktop integration

Merge the small [Hyprland 0.55+ Lua example](examples/hyprland.lua) into your configuration, choosing unused bindings, then run `hyprctl reload`. Use `hyprctl clients` to verify the window's class and floating state. Other compositors need equivalent rules. The installer never edits desktop configuration. New attachments open fresh windows; it does not focus or recycle existing windows. WezTerm starts a new process to preserve window-class selection.

## Update, rollback, remove

Use the original installation method, with an exact newer version and `--update`. No automatic updater is installed. Source users rebuild the selected source and explicitly install its archive. The installer keeps the previous binary/notices for one-step rollback:

```sh
bash scripts/install.sh --version v0.2.0 --update
bash scripts/install.sh --rollback
bash scripts/install.sh --uninstall
```

Reuse any custom prefix/data directory. Repeat installation of identical content is a no-op. Changed owned files block updates; uninstall preserves changed files and backups. Configuration is never removed. Hidden ownership records are stored in the data directory. The launcher also creates hashed lock files beneath `$XDG_CACHE_HOME/tmux-agent-launcher/locks` (default `~/.cache`). Short-lived 0700 temporary directories with 0600 launch snapshots carry the invoking environment to tmux, then are removed by the child; they keep values out of tmux's command arguments. An interrupted child startup can leave such a directory; inspect and remove it locally. Close sessions separately when desired; uninstall does not terminate running agents.

`doctor` reports incompatible tmux and missing selected picker/terminal/Git. Missing individual agent executables are informational; live sessions remain attachable. If no new action appears, install/enable a profile or correct its command/PATH. If a window opens with the wrong placement, inspect compositor rules. If an agent exits immediately, check its own CLI/authentication and selected directory. Readable names require recent tmux; older versions are rejected.

Architecture and repository layout: [docs/architecture.md](docs/architecture.md). Terms: [CONTEXT.md](CONTEXT.md). Architectural decisions: [docs/adr/](docs/adr/).

MIT licensed. [NOTICE](NOTICE) includes the TOML dependency license. Contributions and forks: [CONTRIBUTING.md](CONTRIBUTING.md).
