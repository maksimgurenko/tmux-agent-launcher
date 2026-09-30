# Upstream command contracts

Reviewed against primary documentation during development (2026-09-29). The launcher passes native argv and does not maintain agent authentication/configuration.

- [Codex approval/sandbox combinations](https://learn.chatgpt.com/docs/agent-approvals-security): `--yolo` bypasses sandbox and approvals.
- [Claude CLI](https://code.claude.com/docs/en/cli-reference): `--dangerously-skip-permissions`.
- [Pi CLI](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/cli.md) and [security](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/security.md): `--approve` trusts project config for this process. Pi's default tool execution does not prompt per call.
- [OpenCode permissions](https://opencode.ai/docs/permissions/): `--auto` autoapproves asks while honoring explicit denies.
- [Rofi dmenu](https://davatorium.github.io/rofi/current/rofi-dmenu.5/): zero-based original row index using `-format i`.
- [Fuzzel manual](https://codeberg.org/dnkl/fuzzel/src/branch/master/doc/fuzzel.1.scd): `--dmenu --index`.
- [Wofi upstream](https://hg.sr.ht/~scoopta/wofi): original prefixed record with `--no-custom-entry`; index-base assumptions are avoided.
- [fzf manual](https://github.com/junegunn/fzf/blob/master/man/man1/fzf.1): display fields using `--with-nth`; return original record.
- [Kitty invocation](https://sw.kovidgoyal.net/kitty/invocation/), [Foot manual](https://codeberg.org/dnkl/foot/src/branch/master/doc/foot.1.scd), [Alacritty CLI](https://alacritty.org/cmd-alacritty.html), [WezTerm start](https://wezterm.org/cli/start.html): window identity and child argv; WezTerm uses a new process.
- [tmux source](https://github.com/tmux/tmux/tree/3.7c): literal punctuation, `-s` format expansion, session IDs and local user options. Older versions normalize punctuation; tested minimum is 3.7c.
- [Hyprland Lua window rules](https://wiki.hypr.land/Configuring/Basics/Window-Rules/): float/center/size and tile effects. Example requires 0.55+ Lua configuration.
