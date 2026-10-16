package launcher

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPickerCaseInsensitiveMatching(t *testing.T) {
	labels := []string{"New [test] Example", "new [test] example", "NEW [test] EXAMPLE", "Attach [test] Elsewhere"}
	for _, backend := range []string{"rofi", "fzf"} {
		t.Run(backend, func(t *testing.T) {
			if _, err := exec.LookPath(backend); err != nil {
				t.Skip(backend + " unavailable")
			}
			// Launcher arguments must override user defaults without losing records.
			args := pickerCommand(backend, "Test")
			if backend == "rofi" {
				if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
					t.Skip("rofi needs a graphical connection, even for -dump")
				}
				config := filepath.Join(t.TempDir(), "test.rasi")
				if err := os.WriteFile(config, []byte("configuration { case-sensitive: true; case-smart: true; }\n"), 0600); err != nil {
					t.Fatal(err)
				}
				args = append([]string{args[0], "-config", config}, args[1:]...)
			} else {
				t.Setenv("FZF_DEFAULT_OPTS", "--no-ignore-case")
				t.Setenv("FZF_DEFAULT_OPTS_FILE", "")
			}
			for _, query := range []string{"new", "New", "NEW"} {
				t.Run(query, func(t *testing.T) {
					filtered := append([]string(nil), args...)
					if backend == "rofi" {
						filtered = append(filtered, "-dump", "-filter", query)
					} else {
						filtered = append(filtered, "--filter="+query)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, filtered[0], filtered[1:]...)
					cmd.Stdin = strings.NewReader(pickerInput(backend, labels))
					out, err := cmd.Output()
					if err != nil {
						t.Fatal(err)
					}
					seen := make(map[int]bool)
					for _, record := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
						i, err := parseSelection(backend, record, labels)
						if err != nil {
							t.Fatal(err)
						}
						seen[i] = true
					}
					if !reflect.DeepEqual(seen, map[int]bool{0: true, 1: true, 2: true}) {
						t.Fatalf("query %q matched %v; want all three case variants", query, seen)
					}
				})
			}
		})
	}
}

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
