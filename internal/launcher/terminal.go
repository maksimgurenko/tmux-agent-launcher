package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func terminalCommand(c Config, child []string) ([]string, error) {
	id := "tmux-floating"
	if c.Presentation == "tile" {
		id = "tmux-tiling"
	}
	var a []string
	switch c.Terminal {
	case "kitty":
		a = []string{"kitty", "--class", id, "-e"}
	case "foot":
		a = []string{"foot", "--app-id", id}
	case "alacritty":
		a = []string{"alacritty", "--class", id, "-e"}
	case "wezterm":
		a = []string{"wezterm", "start", "--always-new-process", "--class", id, "--"}
	case "custom":
		for _, v := range c.Terminals["custom"].Command {
			if v == "{command}" {
				a = append(a, child...)
			} else {
				a = append(a, strings.ReplaceAll(v, "{app_id}", id))
			}
		}
		return a, nil
	default:
		return nil, fmt.Errorf("unknown terminal")
	}
	return append(a, child...), nil
}
func execArgv(a []string, env []string) error {
	p, err := exec.LookPath(a[0])
	if err != nil {
		return err
	}
	return syscall.Exec(p, a, env)
}
func openTerminal(c Config, child []string) error {
	a, err := terminalCommand(c, child)
	if err != nil {
		return err
	}
	// A fresh terminal must not look like a nested tmux client.
	return execArgv(a, agentEnv(os.Environ(), nil))
}
func haveTTY() bool {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}
