package launcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const ownerKey = "@tmux_agent_launcher"
const ownerValue = "v1"

type Session struct{ ID, Name, Profile, Project, Host string }
type Tmux struct{ Prefix []string }

func (t Tmux) command(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "tmux", append(append([]string{}, t.Prefix...), args...)...)
}
func (t Tmux) output(ctx context.Context, args ...string) (string, error) {
	b, err := t.command(ctx, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux: %s (%w)", strings.TrimSpace(string(b)), err)
	}
	return strings.TrimSuffix(string(b), "\n"), nil
}

var tmuxVersionRE = regexp.MustCompile(`^tmux (\d+)\.(\d+)([a-z]?)`)

func (t Tmux) checkVersion(ctx context.Context) error {
	v, err := t.output(ctx, "-V")
	if err != nil {
		return err
	}
	m := tmuxVersionRE.FindStringSubmatch(v)
	if m == nil {
		return fmt.Errorf("unrecognized tmux version %q; tested minimum is 3.7c", v)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	if major < 3 || (major == 3 && (minor < 7 || (minor == 7 && m[3] < "c"))) {
		return fmt.Errorf("tmux 3.7c or later is required for literal path names (found %s)", v)
	}
	return nil
}
func legacySession(name string) (string, string, bool) {
	for _, p := range []string{"codex", "claude"} {
		prefix := "/" + p
		if strings.HasPrefix(name, prefix+"/") && strings.HasSuffix(name, "/") {
			dir := strings.TrimSuffix(strings.TrimPrefix(name, prefix), "/")
			if dir == "" {
				dir = "/"
			}
			return p, dir, true
		}
	}
	return "", "", false
}
func (t Tmux) sessions(ctx context.Context) ([]Session, error) {
	b, err := t.command(ctx, "list-sessions", "-F", "#{session_id}\t#{session_name}").CombinedOutput()
	if err != nil {
		msg := string(b)
		if strings.Contains(msg, "no server running") || strings.Contains(msg, "No such file or directory") {
			return nil, nil
		}
		return nil, fmt.Errorf("list tmux sessions: %s", strings.TrimSpace(msg))
	}
	var a []Session
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		id, name, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		owner, err := t.output(ctx, "show-options", "-qv", "-t", id, ownerKey)
		if err != nil {
			continue
		}
		s := Session{ID: id, Name: name}
		if owner == ownerValue {
			s.Profile, err = t.output(ctx, "show-options", "-qv", "-t", id, "@tmux_agent_profile")
			if err != nil {
				continue
			}
			s.Project, err = t.output(ctx, "show-options", "-qv", "-t", id, "@tmux_agent_project")
			if err != nil {
				continue
			}
			s.Host, err = t.output(ctx, "show-options", "-qv", "-t", id, "@tmux_agent_host")
			if err != nil || (s.Host != "" && !validHost(s.Host)) {
				continue
			}
			if !profileID.MatchString(s.Profile) || !filepath.IsAbs(s.Project) || hasControl(s.Project) {
				continue
			}
		} else {
			var valid bool
			s.Profile, s.Project, valid = legacySession(name)
			if !valid || hasControl(s.Project) {
				continue
			}
		}
		a = append(a, s)
	}
	return a, nil
}
func sessionName(profile, project string) string { return "/" + profile + project + "/" }
func remoteSessionName(host, profile, project string) string {
	if base, ok := strings.CutSuffix(profile, "-peer"); ok && base != "" {
		profile = base
	}
	return "/" + host + sessionName(profile, project)
}

func duplicateSession(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "tmux: duplicate session: ")
}

func (t Tmux) attachCommand(ctx context.Context, id string) ([]string, error) {
	socket, err := t.output(ctx, "display-message", "-p", "-t", id, "#{socket_path}")
	if err != nil {
		return nil, err
	}
	return []string{"tmux", "-S", socket, "attach-session", "-t", id}, nil
}
func findSession(sessions []Session, profile, project, host string) (Session, bool) {
	for _, s := range sessions {
		if s.Profile == profile && s.Project == project && s.Host == host {
			return s, true
		}
	}
	return Session{}, false
}

func (t Tmux) socketIdentity() (string, error) {
	var socket, label string
	for i := 0; i < len(t.Prefix); i++ {
		switch t.Prefix[i] {
		case "-S", "-L", "-f", "-T", "-c":
			if i+1 == len(t.Prefix) {
				return "", fmt.Errorf("missing tmux option value")
			}
			if t.Prefix[i] == "-S" {
				socket = t.Prefix[i+1]
			} else if t.Prefix[i] == "-L" {
				label = t.Prefix[i+1]
			}
			i++
		}
	}
	// Match tmux's precedence: -S, then -L, then TMUX, then the default
	// socket under TMUX_TMPDIR (falling back to /tmp if it cannot resolve).
	if socket == "" && label == "" {
		socket, _, _ = strings.Cut(os.Getenv("TMUX"), ",")
	}
	if socket == "" {
		base := "/tmp"
		if tmp := os.Getenv("TMUX_TMPDIR"); tmp != "" {
			if resolved, err := filepath.EvalSymlinks(tmp); err == nil {
				base = resolved
			}
		}
		if label == "" {
			label = "default"
		}
		socket = filepath.Join(base, fmt.Sprintf("tmux-%d", os.Getuid()), label)
	}
	abs, err := filepath.Abs(socket)
	if err != nil {
		return "", err
	}
	// The socket and its private parent may not exist until the first launch.
	// Resolve the existing ancestor so aliases have one identity in both cases.
	path, suffix := abs, ""
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			return filepath.Join(resolved, suffix), nil
		}
		if !os.IsNotExist(err) || path == filepath.Dir(path) {
			return "", err
		}
		suffix = filepath.Join(filepath.Base(path), suffix)
		path = filepath.Dir(path)
	}
}

func (t Tmux) ensure(ctx context.Context, c Config, profile, project, self string) (Session, error) {
	return t.ensureProject(ctx, c, profile, project, self, nil)
}

func (t Tmux) ensureProject(ctx context.Context, c Config, profile, project, self string, peer *Peer) (Session, error) {
	if err := t.checkVersion(ctx); err != nil {
		return Session{}, err
	}
	socket, err := t.socketIdentity()
	if err != nil {
		return Session{}, fmt.Errorf("identify tmux server: %w", err)
	}
	identity := socket + "\x00" + profile + "\x00" + project
	host := ""
	name := sessionName(profile, project)
	if peer != nil {
		host = peer.Host
		identity += "\x00" + host
		name = "/@remote/" + host + name
	}
	key := sha256.Sum256([]byte(identity))
	lockdir := filepath.Join(xdgPath("XDG_CACHE_HOME", ".cache"), "tmux-agent-launcher", "locks")
	if err := os.MkdirAll(lockdir, 0700); err != nil {
		return Session{}, err
	}
	fd, err := syscall.Open(filepath.Join(lockdir, hex.EncodeToString(key[:])+".lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return Session{}, err
	}
	f := os.NewFile(uintptr(fd), "session lock")
	defer f.Close()
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			return Session{}, err
		}
		select {
		case <-ctx.Done():
			return Session{}, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	sessions, err := t.sessions(ctx)
	if err != nil {
		return Session{}, err
	}
	if s, ok := findSession(sessions, profile, project, host); ok {
		return s, nil
	}
	p, ok := c.Profiles[profile]
	if !ok {
		return Session{}, fmt.Errorf("profile %q is not configured", profile)
	}
	if !p.enabled() {
		return Session{}, fmt.Errorf("profile %q is disabled", profile)
	}
	env := agentEnv(os.Environ(), p.Env)
	dir := project
	command := p.Command
	if peer != nil {
		dir, err = os.UserHomeDir()
		if err != nil {
			return Session{}, err
		}
		token, err := peerToken(peer.CodexTokenFile, peer.CodexTokenEnv, envValue(env, peer.CodexTokenEnv))
		if err != nil {
			return Session{}, fmt.Errorf("%s: %w", host, err)
		}
		if peer.CodexTokenFile != "" {
			env = agentEnv(env, map[string]string{peer.CodexTokenEnv: token})
		}
		command = append(append([]string{}, command...), "--remote", peer.Endpoint, "--remote-auth-token-env", peer.CodexTokenEnv, "-C", project)
	}
	if _, err := resolveExecutable(p.executable(), env, dir); err != nil {
		return Session{}, fmt.Errorf("profile %s: %w", profile, err)
	}
	launch := launchPayload{Command: command, Env: env, Directory: dir, Shell: p.Shell}
	if p.Shell != "" {
		tmuxExe, err := exec.LookPath("tmux")
		if err != nil {
			return Session{}, err
		}
		tmuxExe, err = filepath.Abs(tmuxExe)
		if err != nil {
			return Session{}, err
		}
		launch.ShellTmux = []string{tmuxExe, "-S", socket}
	}
	payload, err := writePayload(launch)
	if err != nil {
		return Session{}, err
	}
	// -s is a tmux format. Escape every # so path contents stay literal.
	start := func(name string) (string, error) {
		return t.output(ctx, "new-session", "-d", "-P", "-F", "#{session_id}", "-s", strings.ReplaceAll(name, "#", "##"), "-c", dir, self, "__run-agent", payload)
	}
	id, err := start(name)
	if peer == nil && duplicateSession(err) {
		// A remote host may share its name with a local profile.
		current, listErr := t.sessions(ctx)
		if listErr == nil {
			for _, other := range current {
				if other.Host != "" && other.Name == name {
					name = "/@local" + name
					id, err = start(name)
					break
				}
			}
		}
	}
	if err != nil {
		os.RemoveAll(filepath.Dir(payload))
		return Session{}, err
	}
	if !strings.HasPrefix(id, "$") {
		return Session{}, fmt.Errorf("unexpected tmux session ID")
	}
	s := Session{ID: id, Name: name, Profile: profile, Project: project, Host: host}
	metadata := [][2]string{{"@tmux_agent_profile", profile}, {"@tmux_agent_project", project}}
	if host != "" {
		metadata = append(metadata, [2]string{"@tmux_agent_host", host})
	}
	for _, entry := range append(metadata, [2]string{ownerKey, ownerValue}) {
		if _, err := t.output(ctx, "set-option", "-t", id, entry[0], entry[1]); err != nil {
			t.output(ctx, "kill-session", "-t", id)
			return Session{}, fmt.Errorf("record session ownership (agent may have exited): %w", err)
		}
	}
	// Publish the short remote name only after the full identity is recorded.
	// If the name is occupied, the session already has a unique qualified name.
	if peer != nil {
		short := remoteSessionName(host, profile, project)
		_, err := t.output(ctx, "rename-session", "-t", id, strings.ReplaceAll(short, "#", "##"))
		if err == nil {
			s.Name = short
		} else if !duplicateSession(err) {
			return s, fmt.Errorf("rename remote session: %w", err)
		}
	}
	return s, nil
}
