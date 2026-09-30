package launcher

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

var version = "development"
var publicBase = "unpublished"
var sourceDigest = "unknown"
var sourceDirty = "true"

//go:embed default-config.toml
var exampleConfig []byte

type rootsFlag []string

func (r *rootsFlag) String() string     { return strings.Join(*r, ",") }
func (r *rootsFlag) Set(v string) error { *r = append(*r, v); return nil }

type options struct {
	config, profile, directory, picker, terminal, presentation, discovery, tmuxSocket string
	roots                                                                             rootsFlag
	showVersion                                                                       bool
}

func parseOptions(args []string) (options, error) {
	var o options
	f := flag.NewFlagSet("tmux-agent-launcher", flag.ContinueOnError)
	f.StringVar(&o.config, "config", "", "TOML configuration path")
	f.StringVar(&o.tmuxSocket, "tmux-socket", "", "explicit tmux server socket (optional)")
	f.StringVar(&o.profile, "profile", "", "profile ID")
	f.StringVar(&o.directory, "directory", "", "project directory (bypass discovery; requires --profile)")
	f.StringVar(&o.picker, "picker", "", "rofi, fuzzel, wofi or fzf")
	f.StringVar(&o.terminal, "terminal", "", "kitty, foot, alacritty, wezterm or custom")
	f.StringVar(&o.presentation, "presentation", "", "float or tile")
	f.StringVar(&o.discovery, "discovery", "", "git or all")
	f.Var(&o.roots, "root", "search root; repeat to replace configured roots")
	f.BoolVar(&o.showVersion, "version", false, "show build identity")
	if err := f.Parse(args); err != nil {
		return o, err
	}
	if f.NArg() != 0 {
		return o, fmt.Errorf("unexpected positional argument %q", f.Arg(0))
	}
	if o.directory != "" && o.profile == "" {
		return o, fmt.Errorf("--directory requires --profile")
	}
	if o.profile != "" && !profileID.MatchString(o.profile) {
		return o, fmt.Errorf("invalid profile ID")
	}
	return o, nil
}
func applyOptions(c *Config, o options) {
	if o.picker != "" {
		c.Picker = o.picker
	}
	if o.terminal != "" {
		c.Terminal = o.terminal
	}
	if o.presentation != "" {
		c.Presentation = o.presentation
	}
	if o.discovery != "" {
		c.Search.Mode = o.discovery
	}
	if len(o.roots) > 0 {
		c.Search.Roots = o.roots
	}
}

// Main runs the launcher command.
func Main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil && !errors.Is(err, errCancelled) && !errors.Is(err, context.Canceled) && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "tmux-agent-launcher:", err)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string) error {
	insideTerminal := false
	sub := ""
	if len(args) > 0 {
		switch args[0] {
		case "__run-agent":
			if len(args) != 2 {
				return fmt.Errorf("invalid runner invocation")
			}
			return runAgent(args[1])
		case "__ui":
			insideTerminal = true
			args = args[1:]
		case "doctor":
			sub = "doctor"
			args = args[1:]
		case "config":
			if len(args) < 2 || args[1] != "init" {
				return fmt.Errorf("usage: tmux-agent-launcher config init [--config FILE]")
			}
			sub = "init"
			args = args[2:]
		}
	}
	o, err := parseOptions(args)
	if err != nil {
		return err
	}
	if o.showVersion {
		fmt.Printf("tmux-agent-launcher %s public-base=%s source=%s local-changes=%s\n", version, publicBase, sourceDigest, sourceDirty)
		return nil
	}
	path := o.config
	if path == "" {
		path = defaultConfigPath()
	}
	if sub == "init" {
		return initConfig(path)
	}
	c, err := loadConfig(path, o.config != "")
	if err != nil {
		return err
	}
	applyOptions(&c, o)
	if err := c.validate(); err != nil {
		return err
	}
	t := Tmux{}
	if o.tmuxSocket != "" {
		socket, err := expandPath(o.tmuxSocket)
		if err != nil {
			return err
		}
		t.Prefix = []string{"-S", socket}
	}
	if sub == "doctor" {
		return doctor(ctx, c, t)
	}
	if err := t.checkVersion(ctx); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return err
	}
	if c.Picker == "fzf" && o.directory == "" && !insideTerminal && !haveTTY() {
		// Forward original flags with absolute paths; terminal cwd may differ.
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		child := []string{self, "__ui", "--config", abs}
		if _, err := os.Stat(path); os.IsNotExist(err) && o.config == "" {
			child = []string{self, "__ui"}
		}
		for _, pair := range [][2]string{{"--profile", o.profile}, {"--picker", c.Picker}, {"--terminal", c.Terminal}, {"--presentation", c.Presentation}, {"--discovery", c.Search.Mode}} {
			if pair[1] != "" {
				child = append(child, pair[0], pair[1])
			}
		}
		if o.tmuxSocket != "" {
			child = append(child, "--tmux-socket", t.Prefix[1])
		} else if socket, err := t.output(ctx, "display-message", "-p", "#{socket_path}"); err == nil {
			child = append(child, "--tmux-socket", socket)
		}
		for _, root := range c.Search.Roots {
			p, err := expandPath(root)
			if err != nil {
				return err
			}
			child = append(child, "--root", p)
		}
		return openTerminal(c, child)
	}
	var chosen Session
	if o.directory != "" {
		p, err := canonicalDir(o.directory)
		if err != nil {
			return err
		}
		chosen, err = t.ensure(ctx, c, o.profile, p, self)
		if err != nil {
			return err
		}
	} else {
		chosen, err = chooseSession(ctx, c, t, o.profile, self)
		if err != nil {
			return err
		}
	}
	child, err := t.attachCommand(ctx, chosen.ID)
	if err != nil {
		return err
	}
	if insideTerminal {
		return execArgv(child, os.Environ())
	}
	return openTerminal(c, child)
}

type entry struct {
	label, profile string
	session        *Session
}

func chooseSession(ctx context.Context, c Config, t Tmux, filter, self string) (Session, error) {
	sessions, err := t.sessions(ctx)
	if err != nil {
		return Session{}, err
	}
	var entries []entry
	for _, s := range sessions {
		if filter != "" && s.Profile != filter {
			continue
		}
		entries = append(entries, entry{label: "Attach [" + s.Profile + "] " + s.Project, session: &s})
	}
	for _, id := range sortedProfiles(c) {
		if filter != "" && id != filter {
			continue
		}
		p := c.Profiles[id]
		if !p.enabled() {
			continue
		}
		cwd, _ := os.Getwd()
		_, err := resolveExecutable(p.Command[0], agentEnv(os.Environ(), p.Env), cwd)
		projectRelative := strings.ContainsRune(p.Command[0], '/') && !filepath.IsAbs(p.Command[0])
		if err != nil && !projectRelative {
			continue
		}
		entries = append(entries, entry{label: "New [" + id + "] " + p.Label, profile: id})
	}
	labels := make([]string, len(entries))
	if len(entries) == 0 {
		return Session{}, fmt.Errorf("no live sessions or available enabled profiles; run doctor")
	}
	for i, e := range entries {
		labels[i] = e.label
	}
	i, err := pick(ctx, c.Picker, "Agent session", labels)
	if err != nil {
		return Session{}, err
	}
	if s := entries[i].session; s != nil {
		return *s, nil
	}
	selectedProfile := entries[i].profile
	if c.Search.Mode == "git" {
		if _, err := exec.LookPath("git"); err != nil {
			return Session{}, fmt.Errorf("Git discovery requires git")
		}
	}
	dirs, err := discover(ctx, c.Search, func(s string) { fmt.Fprintln(os.Stderr, s) })
	if err != nil {
		return Session{}, err
	}
	i, err = pick(ctx, c.Picker, "Project", dirs)
	if err != nil {
		return Session{}, err
	}
	return t.ensure(ctx, c, selectedProfile, dirs[i], self)
}
func initConfig(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create config (existing files are preserved): %w", err)
	}
	_, err = f.Write(exampleConfig)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	fmt.Println("Created", path)
	return nil
}
func doctor(ctx context.Context, c Config, t Tmux) error {
	fail := false
	check := func(name string, err error) {
		if err != nil {
			fmt.Printf("FAIL %s: %v\n", name, err)
			fail = true
		} else {
			fmt.Printf("OK   %s\n", name)
		}
	}
	check("tmux", t.checkVersion(ctx))
	if c.Search.Mode == "git" {
		_, err := exec.LookPath("git")
		check("git", err)
	}
	_, err := exec.LookPath(c.Picker)
	check("picker "+c.Picker, err)
	a, err := terminalCommand(c, []string{"tmux"})
	if err == nil {
		_, err = exec.LookPath(a[0])
	}
	check("terminal "+c.Terminal, err)
	available := 0
	cwd, _ := os.Getwd()
	for _, id := range sortedProfiles(c) {
		p := c.Profiles[id]
		if !p.enabled() {
			continue
		}
		_, err := resolveExecutable(p.Command[0], agentEnv(os.Environ(), p.Env), cwd)
		if err != nil {
			fmt.Printf("INFO profile %s: executable unavailable\n", id)
		} else {
			available++
			fmt.Printf("OK   profile %s\n", id)
		}
	}
	if available == 0 {
		fmt.Println("INFO no new-session profiles available; live sessions can still attach")
	}
	fmt.Println("INFO desktop hotkeys/window rules and terminal placement require a real session check")
	if fail {
		return fmt.Errorf("doctor found missing or incompatible requirements")
	}
	return nil
}
