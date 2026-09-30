-- Hyprland 0.55+ Lua configuration. Merge only the entries you want.
-- Use the installed absolute path when ~/.local/bin is absent from compositor PATH.
local launcher = os.getenv("HOME") .. "/.local/bin/tmux-agent-launcher"
hl.bind("SUPER + C", hl.dsp.exec_cmd(launcher .. " --presentation float"))
hl.bind("SUPER + ALT + C", hl.dsp.exec_cmd(launcher .. " --presentation tile"))

hl.window_rule({
    match = { class = "^tmux-floating$" },
    float = true,
    center = true,
    size = { 900, 600 },
})
hl.window_rule({ match = { class = "^tmux-tiling$" }, tile = true })
