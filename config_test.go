package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigMergeAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	text := `picker="fzf"
[search]
roots=["/projects"]
exclude=[]
[profiles.codex]
label="Custom Codex"
env={EXAMPLE="new"}
[profiles.experimental]
command=["agent", "argument with spaces", ""]
enabled=false
`
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Profiles["codex"].Command, []string{"codex", "--yolo"}) || c.Profiles["codex"].Label != "Custom Codex" {
		t.Fatal("field overrides replaced a preset")
	}
	if len(c.Search.Exclude) != 0 || !reflect.DeepEqual(c.Search.Roots, []string{"/projects"}) || c.Search.Mode != "git" {
		t.Fatal("arrays or omitted defaults merged incorrectly")
	}
	if c.Profiles["experimental"].enabled() {
		t.Fatal("disabled profile enabled")
	}
	for _, bad := range []string{`typo=true`, `[profiles.bad]
command=[]`, `[profiles."bad/name"]
command=["agent"]`, `[terminals.custom]
command=["term","prefix{command}"]
terminal="custom"`} {
		os.WriteFile(path, []byte(bad), 0600)
		c, err := loadConfig(path, true)
		if err == nil {
			err = c.validate()
		}
		if err == nil {
			t.Fatalf("accepted invalid config %q", bad)
		}
	}
}
func TestEnvAndPathIdentity(t *testing.T) {
	e := agentEnv([]string{"PATH=/bin", "EXAMPLE=old", "UNCHANGED=yes", "TMUX=old"}, map[string]string{"EXAMPLE": "a value"})
	if envValue(e, "EXAMPLE") != "a value" || envValue(e, "UNCHANGED") != "yes" || envValue(e, "TMUX") != "" {
		t.Fatal(e)
	}
	dir := t.TempDir()
	alias := dir + "-alias"
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(alias)
	a, err := canonicalDir(alias)
	if err != nil || a != dir {
		t.Fatalf("alias identity %s %v", a, err)
	}
	if _, err := canonicalDir(filepath.Join(dir, "no\nline")); err == nil {
		t.Fatal("accepted invalid directory")
	}
	c := defaults()
	c.Terminal = "custom"
	c.Terminals["custom"] = Terminal{Command: []string{"term", "{app_id}", "{command}"}}
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	c.Terminals["custom"] = Terminal{Command: []string{"term", "bad{command}"}}
	if err := c.validate(); err == nil {
		t.Fatal("accepted embedded argv expansion")
	}
	if strings.Contains(defaultConfigPath(), "/~") {
		t.Fatal("incorrect home expansion")
	}
}
