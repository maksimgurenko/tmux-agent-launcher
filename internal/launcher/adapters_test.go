package launcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPickerIdentityAndFailures(t *testing.T) {
	labels := []string{"same label", "same label", "/a/path with spaces:#"}
	for _, backend := range []string{"rofi", "fuzzel", "wofi", "fzf"} {
		out := "1\n"
		if backend == "wofi" || backend == "fzf" {
			out = "1\tsame label\n"
		}
		i, err := parseSelection(backend, out, labels)
		if err != nil || i != 1 {
			t.Fatalf("%s: %d %v", backend, i, err)
		}
		for _, bad := range []string{"99\n", "-1\n", "a\n", "1\twrong\n", "0\nextra\n"} {
			if _, err := parseSelection(backend, bad, labels); err == nil {
				t.Fatalf("%s accepted %q", backend, bad)
			}
		}
		if _, err := parseSelection(backend, "", labels); !errors.Is(err, errCancelled) {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	script := filepath.Join(dir, "rofi")
	for _, test := range []struct {
		body         string
		cancel, fail bool
	}{{"exit 1", true, false}, {"echo broken >&2; exit 1", false, true}, {"echo 999", false, true}, {"echo 2", false, false}} {
		os.WriteFile(script, []byte("#!/bin/sh\n"+test.body+"\n"), 0700)
		_, err := pick(context.Background(), "rofi", "Test", labels)
		if test.cancel && !errors.Is(err, errCancelled) {
			t.Fatal(err)
		}
		if test.fail && (err == nil || errors.Is(err, errCancelled)) {
			t.Fatal("picker error hidden", err)
		}
		if !test.fail && !test.cancel && err != nil {
			t.Fatal(err)
		}
	}
}
func TestTerminalArgumentBoundaries(t *testing.T) {
	child := []string{"tmux", "attach-session", "-t", "$12"}
	expected := map[string][]string{
		"kitty":     {"kitty", "--class", "tmux-floating", "-e"},
		"foot":      {"foot", "--app-id", "tmux-floating"},
		"alacritty": {"alacritty", "--class", "tmux-floating", "-e"},
		"wezterm":   {"wezterm", "start", "--always-new-process", "--class", "tmux-floating", "--"},
	}
	for terminal, prefix := range expected {
		c := defaults()
		c.Terminal = terminal
		a, err := terminalCommand(c, child)
		if err != nil || !reflect.DeepEqual(a, append(prefix, child...)) {
			t.Fatalf("%s: %q %v", terminal, a, err)
		}
	}
	c := defaults()
	c.Terminal = "custom"
	c.Presentation = "tile"
	c.Terminals["custom"] = Terminal{Command: []string{"terminal", "--label={app_id}", "{command}", "trailing argument"}}
	a, err := terminalCommand(c, child)
	want := append([]string{"terminal", "--label=tmux-tiling"}, child...)
	want = append(want, "trailing argument")
	if err != nil || !reflect.DeepEqual(a, want) {
		t.Fatalf("%q %v", a, err)
	}
}
