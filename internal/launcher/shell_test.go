package launcher

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestShellLaunchLoadsStartupBeforeAliasAndKeepsShell(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		if os.Getenv("REQUIRE_ZSH") == "1" {
			t.Fatal(err)
		}
		t.Skip("Zsh is unavailable")
	}
	tm := shellFixtureTmux(t)
	home := t.TempDir()
	project := filepath.Join(t.TempDir(), "project space.#'literal")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("ZDOTDIR", "")
	os.Unsetenv("ZDOTDIR")
	t.Setenv("EDITOR", "")
	os.Unsetenv("EDITOR")
	proof := filepath.Join(home, "proof")
	t.Setenv("SHELL_FIXTURE_PROOF", proof)
	files := map[string]string{
		".zshenv":   "export SHELL_FIXTURE_ENV=env\n",
		".zprofile": "export SHELL_FIXTURE_PROFILE=profile\n",
		".zlogin":   "export SHELL_FIXTURE_LOGIN=login\n",
		".zshrc": `export EDITOR=nvim
export SHELL_FIXTURE_RC=rc
PROMPT='fixture> '
precmd() { export SHELL_FIXTURE_PROMPT=prompt; }
fixture_question() {
    if [[ -z $_fixture_prompted ]]; then
        _fixture_prompted=1
        local fixture_answer=''
        print waiting > "$HOME/question"
        vared -p 'Fixture question: ' fixture_answer
        print -r -- "$fixture_answer" > "$HOME/answer"
    fi
}
autoload -Uz add-zsh-hook
add-zsh-hook precmd fixture_question
fixture_line_init() { export SHELL_FIXTURE_WIDGET=widget; }
zle -N fixture_line_init
autoload -Uz add-zle-hook-widget
add-zle-hook-widget line-init fixture_line_init
fixture_agent() {
    printf '%s\0' "$@" "$EDITOR" "$SHELL_FIXTURE_ENV" "$SHELL_FIXTURE_PROFILE" \
        "$SHELL_FIXTURE_RC" "$SHELL_FIXTURE_LOGIN" "$SHELL_FIXTURE_PROMPT" \
        "$SHELL_FIXTURE_WIDGET" "$PWD" "${ZDOTDIR-unset}" > "$SHELL_FIXTURE_PROOF.tmp"
    /bin/mv "$SHELL_FIXTURE_PROOF.tmp" "$SHELL_FIXTURE_PROOF"
}

alias fixture-alias=fixture_agent
`,
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(home, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tm.output(context.Background(), "new-session", "-d", "-s", "hold", "/bin/sleep", "60"); err != nil {
		t.Fatal(err)
	}
	terminal := filepath.Join(home, "terminal")
	if err := os.WriteFile(terminal, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	arguments := []string{"", "space value", "quotes'\";$(touch injected)\nnext", "--option=value"}
	var command strings.Builder
	command.WriteString(`["fixture-alias"`)
	for _, arg := range arguments {
		command.WriteString(", " + fmtTOMLString(arg))
	}
	command.WriteString("]")
	config := filepath.Join(home, "config.toml")
	text := fmt.Sprintf("terminal=\"custom\"\n[profiles.fixture]\nshell=\"zsh\"\ncommand=%s\n[terminals.custom]\ncommand=[%s, \"{command}\"]\n", command.String(), fmtTOMLString(terminal))
	if err := os.WriteFile(config, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	launch := func() {
		t.Helper()
		cmd := exec.Command(testLauncher, "--config", config, "--profile", "fixture", "--directory", project, "--tmux-socket", tm.Prefix[1])
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("launch: %s %v", out, err)
		}
	}
	launch()
	waitProof(t, filepath.Join(home, "question"))
	questionSessions, err := tm.sessions(context.Background())
	if err != nil || len(questionSessions) != 1 {
		t.Fatalf("missing shell at its startup question: %v %v", questionSessions, err)
	}
	for _, key := range []string{"fixture-answer", "Enter"} {
		args := []string{"send-keys", "-t", questionSessions[0].ID}
		if key != "Enter" {
			args = append(args, "-l")
		}
		if _, err := tm.output(context.Background(), append(args, key)...); err != nil {
			t.Fatal(err)
		}
	}
	if got := waitProof(t, filepath.Join(home, "answer")); got != "fixture-answer\n" {
		t.Fatalf("agent command was entered into a startup question: %q", got)
	}
	got := strings.Split(strings.TrimSuffix(waitProof(t, proof), "\x00"), "\x00")
	want := append(append([]string{}, arguments...), "nvim", "env", "profile", "rc", "login", "prompt", "widget", project, "unset")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("startup/argv mismatch:\n got: %q\nwant: %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(project, "injected")); !os.IsNotExist(err) {
		t.Fatal("argument was executed as shell syntax")
	}
	sessions, err := tm.sessions(context.Background())
	if err != nil || len(sessions) != 1 {
		t.Fatalf("shell session missing after agent exit: %v %v", sessions, err)
	}
	session := sessions[0]
	if err := os.Remove(proof); err != nil {
		t.Fatal(err)
	}
	launch() // Reattachment must not enter the command a second time.
	_, err = tm.output(context.Background(), "send-keys", "-t", session.ID, "-l", "print returned > returned")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tm.output(context.Background(), "send-keys", "-t", session.ID, "Enter"); err != nil {
		t.Fatal(err)
	}
	if got := waitProof(t, filepath.Join(project, "returned")); got != "returned\n" {
		t.Fatal("not returned to an interactive shell", got)
	}
	if _, err := os.Stat(proof); !os.IsNotExist(err) {
		t.Fatal("reattachment relaunched the agent")
	}
	for name, text := range files {
		got, err := os.ReadFile(filepath.Join(home, name))
		if err != nil || !bytes.Equal(got, []byte(text)) {
			t.Fatalf("user startup file changed: %s %v", name, err)
		}
	}
	leftovers, err := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), "tmux-agent-launcher*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary startup files remain: %v %v", leftovers, err)
	}
}

func TestShellLaunchPreservesZdotdirAndNestedShells(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		if os.Getenv("REQUIRE_ZSH") == "1" {
			t.Fatal(err)
		}
		t.Skip("Zsh is unavailable")
	}
	for _, inherited := range []bool{false, true} {
		t.Run(fmt.Sprintf("inherited=%v", inherited), func(t *testing.T) {
			tm := shellFixtureTmux(t)
			home := t.TempDir()
			dotdir := filepath.Join(t.TempDir(), "startup space'quoted")
			if err := os.Mkdir(dotdir, 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("ZDOTDIR", dotdir)
			wantOrigin := "dotdir"
			if !inherited {
				os.Unsetenv("ZDOTDIR")
				wantOrigin = "home"
			}
			files := map[string]string{
				filepath.Join(home, ".zshenv"):   "export SHELL_FIXTURE_ORIGIN=home\nexport ZDOTDIR=" + shellQuote(dotdir) + "\n",
				filepath.Join(dotdir, ".zshenv"): "export SHELL_FIXTURE_ORIGIN=dotdir\n",
				filepath.Join(dotdir, ".zshrc"): `PROMPT='fixture> '
export EDITOR=fixture-editor
fixture_agent() {
    printf '%s\n' "$SHELL_FIXTURE_ORIGIN" "$ZDOTDIR" > "$HOME/proof.tmp"
    /bin/mv "$HOME/proof.tmp" "$HOME/proof"
}
`,
			}
			for path, text := range files {
				if err := os.WriteFile(path, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			config := filepath.Join(home, "config.toml")
			text := "terminal=\"custom\"\n[profiles.fixture]\nshell=\"zsh\"\ncommand=[\"fixture_agent\"]\n[terminals.custom]\ncommand=[\"/bin/true\", \"{command}\"]\n"
			if err := os.WriteFile(config, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := tm.output(context.Background(), "new-session", "-d", "-s", "hold", "/bin/sleep", "60"); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(testLauncher, "--config", config, "--profile", "fixture", "--directory", home, "--tmux-socket", tm.Prefix[1])
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("launch with custom ZDOTDIR: %s %v", out, err)
			}
			proof := filepath.Join(home, "proof")
			if got := waitProof(t, proof); got != wantOrigin+"\n"+dotdir+"\n" {
				t.Fatalf("user ZDOTDIR/startup order changed: %q", got)
			}
			sessions, err := tm.sessions(context.Background())
			if err != nil || len(sessions) != 1 {
				t.Fatalf("missing interactive shell: %v %v", sessions, err)
			}
			id := sessions[0].ID
			if err := os.Remove(proof); err != nil {
				t.Fatal(err)
			}
			// A nested interactive shell must use the user's startup files, and
			// must not inherit the private injection mechanism.
			for _, line := range []string{"zsh -il", `printf '%s\n' "$EDITOR" > nested`, "exit", "print outer > outer"} {
				if _, err := tm.output(context.Background(), "send-keys", "-t", id, "-l", line); err != nil {
					t.Fatal(err)
				}
				if _, err := tm.output(context.Background(), "send-keys", "-t", id, "Enter"); err != nil {
					t.Fatal(err)
				}
			}
			if got := waitProof(t, filepath.Join(home, "nested")); got != "fixture-editor\n" {
				t.Fatal("nested shell startup was not preserved", got)
			}
			waitProof(t, filepath.Join(home, "outer"))
			if _, err := os.Stat(proof); !os.IsNotExist(err) {
				t.Fatal("nested shell relaunched the agent")
			}
		})
	}
}

func shellFixtureTmux(t *testing.T) Tmux {
	t.Helper()
	tm := disposableTmux(t)
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		sessions, _ := tm.output(context.Background(), "list-sessions", "-F", "#{session_id}")
		for _, id := range strings.Fields(sessions) {
			for _, option := range []string{"@tmux_agent_launcher", "@tmux_agent_profile", "@tmux_agent_project"} {
				value, err := tm.output(context.Background(), "show-options", "-qv", "-t", id, option)
				t.Logf("shell fixture %s %s=%q (%v)", id, option, value, err)
			}
		}
		panes, _ := tm.output(context.Background(), "list-panes", "-a", "-F", "#{pane_id}")
		for _, pane := range strings.Fields(panes) {
			output, err := tm.output(context.Background(), "capture-pane", "-p", "-S", "-100", "-t", pane)
			t.Logf("shell fixture pane %s: %s (%v)", pane, output, err)
		}
	})
	return tm
}
