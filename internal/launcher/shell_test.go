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
	// Ubuntu global compinit can prompt on hosted runners. Completion is not
	// part of this fixture; keep startup deterministic via its documented switch.
	files := map[string]string{
		".zshenv":   "skip_global_compinit=1\nexport SHELL_FIXTURE_ENV=env\ntypeset fixture_env_scope=env-scope\n",
		".zprofile": "export SHELL_FIXTURE_PROFILE=profile\ntypeset fixture_profile_scope=profile-scope\n",
		".zlogin":   "export SHELL_FIXTURE_LOGIN=login\ntypeset fixture_login_scope=login-scope\n",
		".zshrc": `export EDITOR=nvim
export SHELL_FIXTURE_RC=rc
typeset fixture_rc_scope=rc-scope
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
        "$SHELL_FIXTURE_WIDGET" "$PWD" "${ZDOTDIR-unset}" \
        "$fixture_env_scope" "$fixture_profile_scope" "$fixture_rc_scope" "$fixture_login_scope" > "$SHELL_FIXTURE_PROOF.tmp"
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
	want := append(append([]string{}, arguments...), "nvim", "env", "profile", "rc", "login", "prompt", "widget", project, "unset", "env-scope", "profile-scope", "rc-scope", "login-scope")
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
				filepath.Join(home, ".zshenv"):   "skip_global_compinit=1\nexport SHELL_FIXTURE_ORIGIN=home\nexport ZDOTDIR=" + shellQuote(dotdir) + "\n",
				filepath.Join(dotdir, ".zshenv"): "skip_global_compinit=1\nexport SHELL_FIXTURE_ORIGIN=dotdir\n",
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

func TestShellLaunchPreservesArgumentBytesAndIgnoresKeyBindings(t *testing.T) {
	for _, test := range []struct {
		name, bindings string
		arguments      []string
	}{
		{"emacs", "bindkey -e", []string{"one\ttwo", "one\rtwo", "one\x01two", "one\x15two", "one\x1btwo"}},
		{"vi", "bindkey -v", []string{"one\ttwo", "one\rtwo", "one\x01two", "one\x15two", "one\x1btwo"}},
		{"printable", "bindkey -e\nbindkey a backward-delete-char", []string{"literal-argument"}},
		{"accept-line-widget", "bindkey -e\nfixture_accept() { print overridden > \"$HOME/accept-line\"; }\nzle -N accept-line fixture_accept", []string{"literal-argument"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			home, _ := launchShellRegression(t, test.bindings, test.arguments)
			got := waitProof(t, filepath.Join(home, "proof"))
			if want := strings.Join(test.arguments, "\x00") + "\x00"; got != want {
				t.Fatalf("literal argv changed:\n got: %q\nwant: %q", got, want)
			}
			if _, err := os.Stat(filepath.Join(home, "accept-line")); !os.IsNotExist(err) {
				t.Fatal("configured acceptance widget intercepted automatic launch")
			}
		})
	}
}

func TestShellLaunchSurvivesPromptHookReplacement(t *testing.T) {
	rc := `fixture_prompt() {
    print -r -- "${precmd_functions[@]}" > "$HOME/hooks"
    export SHELL_FIXTURE_PROMPT=prompt
}
precmd_functions=(fixture_prompt)
fixture_line_init() {
    export SHELL_FIXTURE_WIDGET=widget
    BUFFER='user initial buffer'
}
zle -N zle-line-init fixture_line_init
fixture_agent() {
    printf '%s\0' "$SHELL_FIXTURE_PROMPT" "$SHELL_FIXTURE_WIDGET" > "$HOME/proof.tmp"
    /bin/mv "$HOME/proof.tmp" "$HOME/proof"
}
`
	home, tm := launchShellRegression(t, rc, nil)
	if got := waitProof(t, filepath.Join(home, "proof")); got != "prompt\x00widget\x00" {
		t.Fatalf("command ran before user prompt/line initialization: %q", got)
	}
	if got := waitProof(t, filepath.Join(home, "hooks")); got != "fixture_prompt\n" {
		t.Fatalf("user prompt-hook array changed: %q", got)
	}
	sessions, err := tm.sessions(context.Background())
	if err != nil || len(sessions) != 1 {
		t.Fatalf("shell missing after automatic command: %v %v", sessions, err)
	}
}

func TestShellLaunchResumesAfterCanceledStartupEditor(t *testing.T) {
	rc := `fixture_prompt() {
    if [[ -z $fixture_asked ]]; then
        fixture_asked=yes
        local answer=''
        print waiting > "$HOME/question"
        vared -p 'Fixture question: ' answer
    fi
}
precmd_functions=(fixture_prompt)
`
	home, tm := launchShellRegression(t, rc, []string{"literal-argument"})
	waitProof(t, filepath.Join(home, "question"))
	sessions, err := tm.sessions(context.Background())
	if err != nil || len(sessions) != 1 {
		t.Fatalf("shell missing at startup editor: %v %v", sessions, err)
	}
	if _, err := tm.output(context.Background(), "send-keys", "-t", sessions[0].ID, "C-c"); err != nil {
		t.Fatal(err)
	}
	if got := waitProof(t, filepath.Join(home, "proof")); got != "literal-argument\x00" {
		t.Fatalf("command changed after startup cancellation: %q", got)
	}
}

func TestShellLaunchRestoresBindingsAndClosesWatcher(t *testing.T) {
	for _, test := range []struct{ name, binding string }{
		{"widget", "bindkey '~' fixture_tilde"},
		{"macro", "bindkey -s '~' 'literal macro'"},
		{"prefix", "KEYTIMEOUT=6000\nbindkey -r '~'\nbindkey '~x' fixture_tilde\nbindkey -s '~y' 'literal macro'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rc := `zmodload zsh/parameter zsh/zleparameter
bindkey -e
fixture_tilde() { BUFFER+='user tilde'; }
zle -N fixture_tilde
`
			rc += test.binding + "\n"
			rc += `bindkey -M main -L > "$HOME/binding-before"
fixture_startup_fd=${${(z)$(zle -F -L)}[4]}
fixture_preexec() { print preexec > "$HOME/preexec"; }
preexec_functions=(fixture_preexec)
# User startup may change these values; the wakeup must still target this pane.
export TMUX=/fixture/missing-socket,0,0 TMUX_PANE=%999999 PATH=/fixture/empty-path
fixture_agent() {
    local descriptor=closed
    [[ -e /proc/$$/fd/$fixture_startup_fd ]] && descriptor=open
    /bin/sh -c 'test ! -e "/proc/$$/fd/$1"' fixture "$fixture_startup_fd" || descriptor=inherited
    local handlers=$(zle -F -L)
    local launcher_parameters="${(M)${(k)parameters}:#_tmux_agent_launcher*}"
    printf '%s\0' "$descriptor" "$handlers" "$launcher_parameters" \
        "${preexec_functions[@]}" > "$HOME/proof.tmp"
    bindkey -M main -L > "$HOME/binding-after"
    /bin/mv "$HOME/proof.tmp" "$HOME/proof"
}
`
			home, _ := launchShellRegression(t, rc, nil)
			if got := waitProof(t, filepath.Join(home, "proof")); got != "closed\x00\x00\x00fixture_preexec\x00" {
				t.Fatalf("launcher state remained in the command environment: %q", got)
			}
			before := waitProof(t, filepath.Join(home, "binding-before"))
			if after := waitProof(t, filepath.Join(home, "binding-after")); after != before {
				t.Fatalf("user binding changed: before %q, after %q", before, after)
			}
			if got := waitProof(t, filepath.Join(home, "preexec")); got != "preexec\n" {
				t.Fatalf("user preexec did not run: %q", got)
			}
		})
	}
}

func TestShellLaunchCleansUpWakeup(t *testing.T) {
	for _, test := range []struct {
		name, binding, input, trap, outcome string
	}{
		{"widget", "bindkey '~' fixture_tilde", "~", "", "interrupt"},
		{"macro", "bindkey -s '~' 'original macro'", "~", "", "interrupt"},
		{"prefix", "KEYTIMEOUT=6000\nbindkey -r '~'\nbindkey '~x' fixture_tilde\nbindkey -s '~y' 'original macro'", "~x", "", "interrupt"},
		{"user-interrupt-trap", "bindkey -s '~' 'original macro'", "~", "TRAPINT() { print handled > \"$HOME/trap-fired\"; return 130; }", "interrupt"},
		{"delayed-success", "bindkey -s '~' 'original macro'", "~", "", "success"},
		{"failure", "KEYTIMEOUT=6000\nbindkey -r '~'\nbindkey '~x' fixture_tilde\nbindkey -s '~y' 'original macro'", "~x", "", "failure"},
	} {
		t.Run(test.name, func(t *testing.T) {
			realTmux, err := exec.LookPath("tmux")
			if err != nil {
				if os.Getenv("REQUIRE_TMUX") == "1" {
					t.Fatal(err)
				}
				t.Skip(err)
			}
			bin := t.TempDir()
			// Widen the child-process wait deterministically, without modifying
			// the launcher or delaying the test's real-tmux control commands.
			wait := "/bin/sleep 30"
			switch test.outcome {
			case "success":
				wait = "while [ ! -e \"$HOME/wake-release\" ]; do /bin/sleep .01; done"
			case "failure":
				wait = "exit 71"
			}
			wrapper := "#!/bin/sh\nfor arg; do\n if [ \"$arg\" = send-keys ]; then\n  printf started > \"$HOME/wake-start\"\n  " + wait + "\n fi\ndone\nexec " + shellQuote(realTmux) + " \"$@\"\n"
			if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte(wrapper), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
			rc := `bindkey -e
fixture_tilde() { BUFFER+='original widget'; }
zle -N fixture_tilde
` + test.binding + "\n" + test.trap + `
fixture_traps() { trap; functions TRAPINT; }
fixture_traps > "$HOME/traps-before"
bindkey -L > "$HOME/binding-before"
fixture_prompt() {
    [[ -e "$HOME/wake-start" ]] && print resumed > "$HOME/resumed"
}
precmd_functions=(fixture_prompt)
fixture_inspect() {
    bindkey -L > "$HOME/binding-after"
    zle -F -L > "$HOME/handlers"
    builtin typeset -pm '_tmux_agent_launcher*' > "$HOME/parameters"
    fixture_traps > "$HOME/traps-after"
    print -r -- "${precmd_functions[@]}" > "$HOME/hooks"
    print inspected > "$HOME/inspected"
}
fixture_agent() {
    fixture_inspect
    printf '%s\0' "$@" > "$HOME/proof.tmp"
    /bin/mv "$HOME/proof.tmp" "$HOME/proof"
}
`
			home, tm := launchShellRegression(t, rc, []string{"literal-argument"})
			waitProof(t, filepath.Join(home, "wake-start"))
			control := func(args ...string) string {
				t.Helper()
				cmd := exec.Command(realTmux, append(append([]string{}, tm.Prefix...), args...)...)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("tmux control: %s %v", out, err)
				}
				return strings.TrimSpace(string(out))
			}
			// The hold session is the only unmanaged session in this fixture.
			panes := control("list-panes", "-a", "-F", "#{pane_id} #{session_name}")
			var pane string
			for _, line := range strings.Split(panes, "\n") {
				if fields := strings.Fields(line); len(fields) > 1 && fields[1] != "hold" {
					pane = fields[0]
				}
			}
			if pane == "" {
				t.Fatal("missing shell pane", panes)
			}
			if test.outcome == "success" {
				if _, err := os.Stat(filepath.Join(home, "proof")); !os.IsNotExist(err) {
					t.Fatal("command ran before the wakeup was released", err)
				}
				if err := os.WriteFile(filepath.Join(home, "wake-release"), nil, 0600); err != nil {
					t.Fatal(err)
				}
				if got := waitProof(t, filepath.Join(home, "proof")); got != "literal-argument\x00" {
					t.Fatalf("delayed wakeup changed the command: %q", got)
				}
			} else {
				if test.outcome == "interrupt" {
					control("send-keys", "-t", pane, "C-c")
				}
				control("send-keys", "-t", pane, "Enter")
				waitProof(t, filepath.Join(home, "resumed"))
				control("send-keys", "-t", pane, "-l", "fixture_inspect")
				control("send-keys", "-t", pane, "Enter")
				waitProof(t, filepath.Join(home, "inspected"))
			}
			before := waitProof(t, filepath.Join(home, "binding-before"))
			if after := waitProof(t, filepath.Join(home, "binding-after")); after != before {
				t.Errorf("wakeup changed user bindings:\n before: %q\n after: %q", before, after)
			}
			for _, name := range []string{"handlers", "parameters"} {
				if got := waitProof(t, filepath.Join(home, name)); got != "" {
					t.Errorf("wakeup left active %s: %q", name, got)
				}
			}
			if before, after := waitProof(t, filepath.Join(home, "traps-before")), waitProof(t, filepath.Join(home, "traps-after")); after != before {
				t.Errorf("user interrupt trap changed: before %q, after %q", before, after)
			}
			if got := waitProof(t, filepath.Join(home, "hooks")); got != "fixture_prompt\n" {
				t.Errorf("user prompt hook changed: %q", got)
			}
			if test.trap != "" {
				waitProof(t, filepath.Join(home, "trap-fired"))
			}
			if test.outcome == "success" {
				if err := os.Remove(filepath.Join(home, "proof")); err != nil {
					t.Fatal(err)
				}
			} else if _, err := os.Stat(filepath.Join(home, "proof")); !os.IsNotExist(err) {
				t.Fatal("abandoned wakeup launched the agent", err)
			}
			if test.outcome == "failure" && !strings.Contains(control("capture-pane", "-p", "-t", pane), "could not enter the command in its tmux pane") {
				t.Error("failed wakeup did not print its diagnostic")
			}
			// A later ordinary byte/prefix must retain its original editor
			// meaning, without accepting the abandoned agent command.
			control("send-keys", "-t", pane, "-l", test.input)
			control("send-keys", "-t", pane, "C-u")
			control("send-keys", "-t", pane, "-l", "print returned > \"$HOME/returned\"")
			control("send-keys", "-t", pane, "Enter")
			waitProof(t, filepath.Join(home, "returned"))
			if _, err := os.Stat(filepath.Join(home, "proof")); !os.IsNotExist(err) {
				t.Fatal("later ordinary input launched the abandoned agent command", err)
			}
		})
	}
}

func launchShellRegression(t *testing.T, rc string, arguments []string) (string, Tmux) {
	t.Helper()
	if _, err := exec.LookPath("zsh"); err != nil {
		if os.Getenv("REQUIRE_ZSH") == "1" {
			t.Fatal(err)
		}
		t.Skip("Zsh is unavailable")
	}
	tm := shellFixtureTmux(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("ZDOTDIR", "")
	os.Unsetenv("ZDOTDIR")
	files := map[string]string{
		".zshenv": "skip_global_compinit=1\n",
		".zshrc": `PROMPT='fixture> '
fixture_agent() {
    printf '%s\0' "$@" > "$HOME/proof.tmp"
    /bin/mv "$HOME/proof.tmp" "$HOME/proof"
}
` + rc + "\n",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(home, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	command := []string{fmtTOMLString("fixture_agent")}
	for _, argument := range arguments {
		command = append(command, fmtTOMLString(argument))
	}
	config := filepath.Join(home, "config.toml")
	text := "terminal=\"custom\"\n[profiles.fixture]\nshell=\"zsh\"\ncommand=[" + strings.Join(command, ", ") + "]\n[terminals.custom]\ncommand=[\"/bin/true\", \"{command}\"]\n"
	if err := os.WriteFile(config, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.output(context.Background(), "new-session", "-d", "-s", "hold", "/bin/sleep", "60"); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(testLauncher, "--config", config, "--profile", "fixture", "--directory", home, "--tmux-socket", tm.Prefix[1])
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("launch: %s %v", out, err)
	}
	return home, tm
}

func shellFixtureTmux(t *testing.T) Tmux {
	t.Helper()
	tm := disposableTmux(t)
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		raw, _ := tm.output(context.Background(), "list-sessions", "-F", "#{session_id}\t#{session_name}")
		t.Logf("raw shell sessions: %q", raw)
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
			if output = strings.TrimSpace(output); output != "" || err != nil {
				t.Logf("shell fixture pane %s: %s (%v)", pane, output, err)
			}
		}
	})
	return tm
}
