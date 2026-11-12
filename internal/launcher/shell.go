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
	return strings.Join(words, " ") + "\n"
}

func runInteractiveShell(p launchPayload, env []string) error {
	exe, err := resolveExecutable(p.Shell, env, p.Directory)
	if err != nil {
		return err
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
	// The first precmd runs after .zlogin, and installs a one-time line-init
	// widget alongside the user's widgets. No timing guesses or rc edits.
	text := fmt.Sprintf(`%s
command /bin/rm -f -- %s
command /bin/rmdir -- %s
if [[ -o rcs && -r ${ZDOTDIR-$HOME}/.zshenv ]]; then
    builtin source -- "${ZDOTDIR-$HOME}/.zshenv"
fi
autoload -Uz add-zsh-hook
_tmux_agent_launcher_first_prompt() {
    add-zsh-hook -d precmd _tmux_agent_launcher_first_prompt
    if [[ ! -o zle ]]; then
        print -u2 -- 'tmux-agent-launcher: interactive shell mode requires ZLE'
        return
    fi
    autoload -Uz add-zle-hook-widget
    _tmux_agent_launcher_enter_command() {
        [[ $CONTEXT == start ]] || return 0
        add-zle-hook-widget -d line-init _tmux_agent_launcher_enter_command
        zle -U %s
    }
    zle -N _tmux_agent_launcher_enter_command
    add-zle-hook-widget line-init _tmux_agent_launcher_enter_command
}
add-zsh-hook precmd _tmux_agent_launcher_first_prompt
`, restore, shellQuote(startup), shellQuote(dir), shellQuote(shellInput(p.Command)))
	if err := os.WriteFile(startup, []byte(text), 0600); err != nil {
		return err
	}
	return syscall.Exec(exe, []string{exe, "-il"}, shellEnv)
}
