package launcher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

var errCancelled = errors.New("cancelled")

func pickerCommand(backend, prompt string) []string {
	switch backend {
	case "rofi":
		return []string{"rofi", "-dmenu", "-format", "i", "-no-custom", "-p", prompt}
	case "fuzzel":
		return []string{"fuzzel", "--dmenu", "--index", "--prompt", prompt + ": "}
	case "wofi":
		return []string{"wofi", "--dmenu", "--no-custom-entry", "--prompt", prompt}
	case "fzf":
		return []string{"fzf", "--delimiter=\t", "--with-nth=2..", "--no-multi", "--prompt", prompt + ": "}
	}
	return nil
}
func pickerInput(backend string, labels []string) string {
	var b strings.Builder
	for i, l := range labels {
		if backend == "wofi" || backend == "fzf" {
			fmt.Fprintf(&b, "%d\t%s\n", i, l)
		} else {
			b.WriteString(l)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
func parseSelection(backend, out string, labels []string) (int, error) {
	out = strings.TrimSuffix(out, "\n")
	if out == "" {
		return 0, errCancelled
	}
	number := out
	if backend == "wofi" || backend == "fzf" {
		number, _, _ = strings.Cut(out, "\t")
	}
	i, err := strconv.Atoi(number)
	if err != nil || i < 0 || i >= len(labels) {
		return 0, fmt.Errorf("picker returned an invalid selection")
	}
	if (backend == "wofi" || backend == "fzf") && out != fmt.Sprintf("%d\t%s", i, labels[i]) {
		return 0, fmt.Errorf("picker returned an unknown record")
	}
	return i, nil
}
func pick(ctx context.Context, backend, prompt string, labels []string) (int, error) {
	if len(labels) == 0 {
		return 0, fmt.Errorf("no selectable entries")
	}
	args := pickerCommand(backend, prompt)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = strings.NewReader(pickerInput(backend, labels))
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if backend == "fzf" {
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		var e *exec.ExitError
		if errors.As(err, &e) && (e.ExitCode() == 1 || e.ExitCode() == 130) && stderr.Len() == 0 {
			return 0, errCancelled
		}
		return 0, fmt.Errorf("%s picker failed: %w %s", backend, err, strings.TrimSpace(stderr.String()))
	}
	return parseSelection(backend, out.String(), labels)
}
