package launcher

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func chooseDirectory(ctx context.Context, self string, peer *Peer) (string, error) {
	dir, err := os.MkdirTemp("", "tmux-agent-launcher-rofi-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	// Rofi parses mode definitions as text. Keep user paths out of the definition.
	if err := os.Symlink(self, filepath.Join(dir, "launcher")); err != nil {
		return "", err
	}
	callback := "__rofi-path"
	if peer != nil {
		b, err := json.Marshal(peer)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, "peer.json"), b, 0600); err != nil {
			return "", err
		}
		callback = "__rofi-remote-path"
	}
	args := rofiCommand("-modes", "Path:./launcher "+callback, "-show", "Path",
		"-theme-str", `configuration { inputchange { action: "kb-custom-1"; } }`,
		"-kb-row-select", "Tab", "-kb-row-tab", "", "-kb-element-next", "", "-no-sort", "-i", "-no-case-smart")
	if peer != nil {
		args = append(args, "-display-Path", peer.Host)
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && stderr.Len() == 0 {
			return "", errCancelled
		}
		return "", fmt.Errorf("Rofi path picker: %w %s", err, strings.TrimSpace(stderr.String()))
	}
	selected, err := os.Readlink(filepath.Join(dir, "selection"))
	if errors.Is(err, os.ErrNotExist) {
		return "", errCancelled
	}
	if err != nil {
		return "", err
	}
	if peer != nil {
		return selected, nil
	}
	return canonicalDir(selected)
}

func homePath(home, input string) string {
	if filepath.IsAbs(input) {
		return input
	}
	if input == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(input, "~/"))
}

// Rofi runs the same executable on each input change and on acceptance.
// Returning rows keeps the menu open. An accepted selection ends the mode.
func rofiPathCallback(ctx context.Context, args []string, remote bool) error {
	var host string
	var lookup func(string, string) (folderResult, error)
	if remote {
		b, err := os.ReadFile("peer.json")
		if err != nil {
			return err
		}
		var peer Peer
		if err := json.Unmarshal(b, &peer); err != nil {
			return err
		}
		host = peer.Host
		lookup = func(operation, input string) (folderResult, error) {
			return peer.folders(ctx, operation, input)
		}
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		lookup = func(operation, input string) (folderResult, error) {
			return lookupFolder(home, operation, input)
		}
	}
	input := os.Getenv("ROFI_INPUT")
	switch os.Getenv("ROFI_RETV") {
	case "0":
		input = ""
	case "10": // inputchange triggers kb-custom-1
	case "1", "2":
		if len(args) == 1 && args[0] != "" && os.Getenv("ROFI_INFO") != "retry" {
			result, err := lookup("resolve", args[0])
			if err == nil {
				return os.Symlink(result.Path, "selection")
			}
			if remote {
				return writePathError(os.Stdout, host, err)
			}
		}
	default:
		return nil
	}
	result, err := lookup("suggest", input)
	if err != nil {
		return writePathError(os.Stdout, host, err)
	}
	return writePathRows(os.Stdout, result.Rows)
}

func writePathError(w io.Writer, host string, cause error) error {
	_, err := fmt.Fprintf(w, "\x00keep-filter\x1ftrue\n\x00use-hot-keys\x1ftrue\n\x00message\x1f%s · Enter to retry · Esc to cancel\nRetry\x00info\x1fretry\x1fpermanent\x1ftrue\n", html.EscapeString(host+": "+cause.Error()))
	return err
}

func writePathRows(w io.Writer, rows []string) error {
	out := bufio.NewWriter(w)
	fmt.Fprint(out, "\x00keep-filter\x1ftrue\n\x00use-hot-keys\x1ftrue\n\x00message\x1fTab completes · / lists folders · Enter selects\n")
	for _, row := range rows {
		fmt.Fprintln(out, row)
	}
	// Rofi closes a script mode that returns no rows.
	if len(rows) == 0 {
		fmt.Fprint(out, "No matching folders\x00nonselectable\x1ftrue\x1fpermanent\x1ftrue\n")
	}
	return out.Flush()
}

func directorySuggestions(home, input string) []string {
	if hasControl(input) {
		return nil
	}
	var rows []string
	if st, err := os.Stat(homePath(home, input)); err == nil && st.IsDir() {
		current := input
		if current == "" || current == "~" {
			current = "~/"
		}
		rows = append(rows, current)
	}
	parent, prefix := filepath.Split(input)
	foldedPrefix := strings.ToLower(prefix)
	entries, _ := os.ReadDir(homePath(home, parent))
	for _, entry := range entries {
		name := entry.Name()
		if hasControl(name) || !utf8.ValidString(name) || !strings.HasPrefix(strings.ToLower(name), foldedPrefix) {
			continue
		}
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		row := parent + name
		if row == input {
			continue // The exact directory is already first.
		}
		if !entry.IsDir() {
			if entry.Type()&os.ModeSymlink == 0 {
				continue
			}
			if st, err := os.Stat(homePath(home, row)); err != nil || !st.IsDir() {
				continue
			}
		}
		rows = append(rows, row)
	}
	return rows
}
