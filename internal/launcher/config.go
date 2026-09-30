package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
)

type Profile struct {
	Label   string            `toml:"label"`
	Enabled *bool             `toml:"enabled"`
	Command []string          `toml:"command"`
	Env     map[string]string `toml:"env"`
}

func (p Profile) enabled() bool { return p.Enabled == nil || *p.Enabled }

type Search struct {
	Roots   []string `toml:"roots"`
	Mode    string   `toml:"mode"`
	Exclude []string `toml:"exclude"`
}
type Terminal struct {
	Command []string `toml:"command"`
}
type Config struct {
	Picker       string              `toml:"picker"`
	Terminal     string              `toml:"terminal"`
	Presentation string              `toml:"presentation"`
	Search       Search              `toml:"search"`
	Profiles     map[string]Profile  `toml:"profiles"`
	Terminals    map[string]Terminal `toml:"terminals"`
}

func defaults() Config {
	return Config{Picker: "rofi", Terminal: "kitty", Presentation: "float",
		Search: Search{Roots: []string{"~"}, Mode: "git", Exclude: []string{".git", ".cache", "node_modules", ".venv", "target"}},
		Profiles: map[string]Profile{
			"codex":    {Label: "Codex", Command: []string{"codex", "--yolo"}},
			"claude":   {Label: "Claude", Command: []string{"claude", "--dangerously-skip-permissions"}},
			"pi":       {Label: "Pi", Command: []string{"pi", "--approve"}},
			"opencode": {Label: "OpenCode", Command: []string{"opencode", "--auto"}},
		}, Terminals: map[string]Terminal{},
	}
}
func xdgPath(variable, fallback string) string {
	if v := os.Getenv(variable); filepath.IsAbs(v) {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, fallback)
}
func defaultConfigPath() string {
	return filepath.Join(xdgPath("XDG_CONFIG_HOME", ".config"), "tmux-agent-launcher", "config.toml")
}
func loadConfig(path string, explicit bool) (Config, error) {
	c := defaults()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) && !explicit {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("read configuration: %w", err)
	}
	var raw Config
	m, err := toml.Decode(string(b), &raw)
	if err != nil {
		return c, fmt.Errorf("decode configuration: %w", err)
	}
	if u := m.Undecoded(); len(u) > 0 {
		return c, fmt.Errorf("unknown configuration key: %s", u[0])
	}
	if m.IsDefined("picker") {
		c.Picker = raw.Picker
	}
	if m.IsDefined("terminal") {
		c.Terminal = raw.Terminal
	}
	if m.IsDefined("presentation") {
		c.Presentation = raw.Presentation
	}
	if m.IsDefined("search", "roots") {
		c.Search.Roots = raw.Search.Roots
	}
	if m.IsDefined("search", "mode") {
		c.Search.Mode = raw.Search.Mode
	}
	if m.IsDefined("search", "exclude") {
		c.Search.Exclude = raw.Search.Exclude
	}
	for id, p := range raw.Profiles {
		v := c.Profiles[id]
		if m.IsDefined("profiles", id, "label") {
			v.Label = p.Label
		}
		if m.IsDefined("profiles", id, "enabled") {
			v.Enabled = p.Enabled
		}
		if m.IsDefined("profiles", id, "command") {
			v.Command = p.Command
		}
		if m.IsDefined("profiles", id, "env") {
			v.Env = p.Env
		}
		if v.Label == "" {
			v.Label = id
		}
		c.Profiles[id] = v
	}
	for id, t := range raw.Terminals {
		c.Terminals[id] = t
	}
	return c, nil
}

var profileID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func hasControl(s string) bool { return strings.IndexFunc(s, unicode.IsControl) >= 0 }
func validArgv(a []string) error {
	if len(a) == 0 || a[0] == "" {
		return fmt.Errorf("command must contain an executable")
	}
	for _, v := range a {
		if strings.ContainsRune(v, 0) {
			return fmt.Errorf("command contains NUL")
		}
	}
	return nil
}
func (c Config) validate() error {
	if !contains([]string{"rofi", "fuzzel", "wofi", "fzf"}, c.Picker) {
		return fmt.Errorf("unknown picker %q", c.Picker)
	}
	if !contains([]string{"kitty", "foot", "alacritty", "wezterm", "custom"}, c.Terminal) {
		return fmt.Errorf("unknown terminal %q", c.Terminal)
	}
	if c.Presentation != "float" && c.Presentation != "tile" {
		return fmt.Errorf("presentation must be float or tile")
	}
	if c.Search.Mode != "git" && c.Search.Mode != "all" {
		return fmt.Errorf("search.mode must be git or all")
	}
	for _, pattern := range c.Search.Exclude {
		if _, err := filepath.Match(pattern, "x"); err != nil {
			return fmt.Errorf("invalid exclusion %q", pattern)
		}
	}
	for id, p := range c.Profiles {
		if !profileID.MatchString(id) {
			return fmt.Errorf("invalid profile ID %q (use letters, digits, underscore or hyphen)", id)
		}
		if hasControl(p.Label) {
			return fmt.Errorf("profile %s: label contains a control character", id)
		}
		if err := validArgv(p.Command); err != nil {
			return fmt.Errorf("profile %s: %w", id, err)
		}
		for k, v := range p.Env {
			if k == "" || strings.ContainsAny(k, "=\x00") || strings.ContainsRune(v, 0) {
				return fmt.Errorf("profile %s: invalid environment entry", id)
			}
		}
	}
	if c.Terminal == "custom" {
		a := c.Terminals["custom"].Command
		if err := validArgv(a); err != nil {
			return fmt.Errorf("custom terminal: %w", err)
		}
		count := 0
		for i, v := range a {
			if v == "{command}" {
				count++
				if i == 0 {
					return fmt.Errorf("custom terminal must start with its executable")
				}
				continue
			}
			v = strings.ReplaceAll(v, "{app_id}", "")
			if strings.ContainsAny(v, "{}") {
				return fmt.Errorf("invalid terminal placeholder")
			}
		}
		if count != 1 {
			return fmt.Errorf("custom terminal requires exactly one standalone {command}")
		}
	}
	return nil
}
func contains(a []string, v string) bool {
	for _, s := range a {
		if s == v {
			return true
		}
	}
	return false
}
func sortedProfiles(c Config) []string {
	a := make([]string, 0, len(c.Profiles))
	for k := range c.Profiles {
		a = append(a, k)
	}
	sort.Strings(a)
	return a
}
func expandPath(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if p == "~" {
			p = home
		} else {
			p = filepath.Join(home, strings.TrimPrefix(p, "~/"))
		}
	}
	return filepath.Abs(p)
}
func canonicalDir(path string) (string, error) {
	p, err := expandPath(path)
	if err != nil {
		return "", err
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("directory: %w", err)
	}
	st, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("%s is not a directory", p)
	}
	if hasControl(p) {
		return "", fmt.Errorf("directory contains an unsupported control character")
	}
	return filepath.Clean(p), nil
}
