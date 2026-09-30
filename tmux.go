package main

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

type Session struct{ ID, Name, Profile, Project string }
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
func (t Tmux) attachCommand(ctx context.Context, id string) ([]string, error) {
	socket, err := t.output(ctx, "display-message", "-p", "-t", id, "#{socket_path}")
	if err != nil {
		return nil, err
	}
	return []string{"tmux", "-S", socket, "attach-session", "-t", id}, nil
}
func findSession(sessions []Session, profile, project string) (Session, bool) {
	for _, s := range sessions {
		if s.Profile == profile && s.Project == project {
			return s, true
		}
	}
	return Session{}, false
}
func (t Tmux) ensure(ctx context.Context, c Config, profile, project, self string) (Session, error) {
	if err := t.checkVersion(ctx); err != nil {
		return Session{}, err
	}
	key := sha256.Sum256([]byte(strings.Join(t.Prefix, "\x00") + "\x00" + profile + "\x00" + project))
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
	if s, ok := findSession(sessions, profile, project); ok {
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
	if _, err := resolveExecutable(p.Command[0], env, project); err != nil {
		return Session{}, fmt.Errorf("profile %s: %w", profile, err)
	}
	payload, err := writePayload(launchPayload{Command: p.Command, Env: env, Directory: project})
	if err != nil {
		return Session{}, err
	}
	name := sessionName(profile, project)
	// -s is a tmux format. Escape every # so path contents stay literal.
	id, err := t.output(ctx, "new-session", "-d", "-P", "-F", "#{session_id}", "-s", strings.ReplaceAll(name, "#", "##"), "-c", project, self, "__run-agent", payload)
	if err != nil {
		os.RemoveAll(filepath.Dir(payload))
		return Session{}, err
	}
	if !strings.HasPrefix(id, "$") {
		return Session{}, fmt.Errorf("unexpected tmux session ID")
	}
	s := Session{ID: id, Name: name, Profile: profile, Project: project}
	for _, entry := range [][2]string{{"@tmux_agent_profile", profile}, {"@tmux_agent_project", project}, {ownerKey, ownerValue}} {
		if _, err := t.output(ctx, "set-option", "-t", id, entry[0], entry[1]); err != nil {
			t.output(ctx, "kill-session", "-t", id)
			return Session{}, fmt.Errorf("record session ownership (agent may have exited): %w", err)
		}
	}
	return s, nil
}
