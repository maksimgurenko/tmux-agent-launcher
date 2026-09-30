package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

type launchPayload struct {
	Command, Env []string
	Directory    string
}

func agentEnv(inherited []string, overrides map[string]string) []string {
	m := map[string]string{}
	for _, entry := range inherited {
		k, v, ok := strings.Cut(entry, "=")
		if ok && k != "TMUX" && k != "TMUX_PANE" {
			m[k] = v
		}
	}
	for k, v := range overrides {
		m[k] = v
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	a := make([]string, 0, len(m))
	for _, k := range keys {
		a = append(a, k+"="+m[k])
	}
	return a
}
func envValue(env []string, key string) string {
	for _, entry := range env {
		if k, v, ok := strings.Cut(entry, "="); ok && k == key {
			return v
		}
	}
	return ""
}
func resolveExecutable(name string, env []string, dir string) (string, error) {
	var candidates []string
	if strings.ContainsRune(name, '/') {
		candidates = []string{name}
	} else {
		for _, p := range filepath.SplitList(envValue(env, "PATH")) {
			candidates = append(candidates, filepath.Join(p, name))
		}
	}
	for _, p := range candidates {
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		st, err := os.Stat(p)
		if err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("executable %q is unavailable; install it or update the profile", name)
}
func writePayload(p launchPayload) (string, error) {
	dir, err := os.MkdirTemp("", "tmux-agent-launcher-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "launch.json")
	b, err := json.Marshal(p)
	if err == nil {
		err = os.WriteFile(path, b, 0600)
	}
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return path, nil
}
func runAgent(path string) error {
	// The short-lived 0600 file preserves argv/environment without putting
	// credentials into tmux command arguments. The containing directory is 0700.
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var p launchPayload
	err = json.Unmarshal(b, &p)
	if err != nil {
		return err
	}
	if filepath.Base(path) != "launch.json" || !strings.HasPrefix(filepath.Base(filepath.Dir(path)), "tmux-agent-launcher-") {
		return fmt.Errorf("invalid launch payload path")
	}
	os.Remove(path)
	os.Remove(filepath.Dir(path))
	if err := validArgv(p.Command); err != nil {
		return err
	}
	if err := os.Chdir(p.Directory); err != nil {
		return err
	}
	// Retain tmux's own environment entries; the launch snapshot overlays them.
	overrides := map[string]string{}
	for _, entry := range p.Env {
		if k, v, ok := strings.Cut(entry, "="); ok {
			overrides[k] = v
		}
	}
	env := agentEnv(os.Environ(), overrides)
	for _, k := range []string{"TMUX", "TMUX_PANE"} {
		if _, ok := overrides[k]; !ok {
			if v := os.Getenv(k); v != "" {
				env = append(env, k+"="+v)
			}
		}
	}
	exe, err := resolveExecutable(p.Command[0], env, p.Directory)
	if err != nil {
		return err
	}
	return syscall.Exec(exe, p.Command, env)
}
