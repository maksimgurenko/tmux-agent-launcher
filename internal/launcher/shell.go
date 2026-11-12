package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

// Shell startup is explicit per profile. Command arguments remain literal; only
// a simple, non-reserved command name stays unquoted so user aliases can expand.
var shellWord = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

func shellQuote(word string) string {
	return "'" + strings.ReplaceAll(word, "'", "'\"'\"'") + "'"
}

func shellInput(command []string) string {
	words := make([]string, len(command))
	for i, word := range command {
		words[i] = shellQuote(word)
	}
	reserved := []string{"if", "then", "else", "elif", "fi", "for", "while", "until", "do", "done", "case", "esac", "function", "select", "time", "coproc", "repeat", "foreach", "end", "nocorrect", "noglob"}
	if shellWord.MatchString(command[0]) && !contains(reserved, command[0]) {
		words[0] = command[0]
	}
	return strings.Join(words, " ")
}

func runInteractiveShell(p launchPayload, env []string) error {
	exe, err := resolveExecutable(p.Shell, env, p.Directory)
	if err != nil {
		return err
	}
	// Capture the actual runner pane, independently of profile overrides and
	// later shell configuration. The socket/binary were fixed by the caller.
	pane := os.Getenv("TMUX_PANE")
	if len(p.ShellTmux) == 0 || pane == "" {
		return fmt.Errorf("interactive shell mode requires its tmux pane")
	}
	wake := append(append([]string{}, p.ShellTmux...), "send-keys", "-t", pane, "-l", "~")
	for i := range wake {
		wake[i] = shellQuote(wake[i])
	}
	tempRoot := envValue(env, "TMPDIR")
	if tempRoot == "" {
		tempRoot = "/tmp"
	}
	dir, err := os.MkdirTemp(tempRoot, "tmux-agent-launcher-shell-")
	if err != nil {
		return err
	}
	// syscall.Exec never returns on success. The startup file removes itself;
	// this defer covers preparation/exec failures.
	defer os.RemoveAll(dir)
	startup := filepath.Join(dir, ".zshenv")
	restore := "unset ZDOTDIR"
	shellEnv := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "ZDOTDIR="); ok {
			restore = "export ZDOTDIR=" + shellQuote(value)
		} else {
			shellEnv = append(shellEnv, entry)
		}
	}
	shellEnv = append(shellEnv, "ZDOTDIR="+dir)
	// Restore the user's ZDOTDIR before their .zshenv. Any directory change made
	// there naturally controls the remaining login/interactive startup files.
	// A readable descriptor runs our widget when ZLE starts reading, after the
	// user's prompt and line-init widgets. Unlike an early precmd/line-init hook,
	// it survives normal startup assignments to those hooks.
	text := fmt.Sprintf(`%s
command /bin/rm -f -- %s
command /bin/rmdir -- %s
if [[ -o rcs && -r ${ZDOTDIR-$HOME}/.zshenv ]]; then
    builtin source -- "${ZDOTDIR-$HOME}/.zshenv"
fi
_tmux_agent_launcher_resume() {
    add-zle-hook-widget -d line-finish _tmux_agent_launcher_resume
    add-zle-hook-widget -d line-init _tmux_agent_launcher_resume
    zle -F -w $_tmux_agent_launcher_fd _tmux_agent_launcher_enter_command
}
_tmux_agent_launcher_restore_binding() {
    local binding
    local -a args
    for binding in "${_tmux_agent_launcher_binding[@]}"; do
        args=("${(@Q)${(z)binding}}")
        builtin bindkey "${args[@]:1}"
    done
    unset _tmux_agent_launcher_binding
}
_tmux_agent_launcher_accept() {
    _tmux_agent_launcher_restore_binding
    BUFFER=%s
    zle .accept-line
}
_tmux_agent_launcher_enter_command() {
    zle -F $_tmux_agent_launcher_fd
    if [[ $CONTEXT != start ]]; then
        # Startup vared must receive only the user's answer. Suspend the ready
        # descriptor until that editor finishes, rather than polling its EOF.
        autoload -Uz add-zle-hook-widget
        add-zle-hook-widget line-finish _tmux_agent_launcher_resume
        # Canceled editors skip line-finish; resume on the next line-init too.
        add-zle-hook-widget line-init _tmux_agent_launcher_resume
        return 0
    fi
    exec {_tmux_agent_launcher_fd}<&-
    unset _tmux_agent_launcher_fd
    # The descriptor callback runs inside a pending tty read. Wake it through
    # our own tmux pane, temporarily binding one literal ASCII wakeup byte.
    # Command bytes are assigned only as BUFFER data in the acceptance widget.
    _tmux_agent_launcher_binding=("${(@f)$(builtin bindkey -M "$KEYMAP" -L '~'; builtin bindkey -M "$KEYMAP" -Lp '~')}")
    local wakeup_sent=0
    {
        # Remove longer bindings so KEYTIMEOUT cannot delay the wakeup.
        builtin bindkey -M "$KEYMAP" -rp '~'
        builtin bindkey -M "$KEYMAP" '~' _tmux_agent_launcher_accept
        if command %s; then
            wakeup_sent=1
        else
            print -u2 -- 'tmux-agent-launcher: could not enter the command in its tmux pane'
        fi
    } always {
        # Ctrl-C can abort the callback while waiting for tmux. Zsh's always
        # block restores abandoned bindings without replacing user traps.
        # After success, the acceptance widget owns restoration instead.
        if (( !wakeup_sent || TRY_BLOCK_INTERRUPT )); then
            _tmux_agent_launcher_restore_binding
        fi
    }
}
exec {_tmux_agent_launcher_fd}</dev/null
zle -N _tmux_agent_launcher_resume
zle -N _tmux_agent_launcher_accept
zle -N _tmux_agent_launcher_enter_command
zle -F -w $_tmux_agent_launcher_fd _tmux_agent_launcher_enter_command
`, restore, shellQuote(startup), shellQuote(dir), shellQuote(shellInput(p.Command)), strings.Join(wake, " "))
	if err := os.WriteFile(startup, []byte(text), 0600); err != nil {
		return err
	}
	return syscall.Exec(exe, []string{exe, "-il"}, shellEnv)
}
