package launcher

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func gitForTest(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %q: %s %v", args, b, err)
	}
}
func TestDiscoveryGitRootsAndAllDirectories(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git required")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	nested := filepath.Join(repo, "nested")
	work := filepath.Join(root, "worktree")
	outside := t.TempDir()
	bare := filepath.Join(root, "bare.git")
	gitForTest(t, "init", "-q", repo)
	gitForTest(t, "-C", repo, "-c", "user.name=Example", "-c", "user.email=example@example.invalid", "commit", "--allow-empty", "-qm", "fixture")
	gitForTest(t, "init", "-q", nested)
	gitForTest(t, "-C", repo, "worktree", "add", "-q", "--detach", work)
	gitForTest(t, "-C", repo, "worktree", "add", "-q", "--detach", outside)
	gitForTest(t, "init", "--bare", "-q", bare)
	// A submodule has a .git file, just like a linked worktree.
	sub := filepath.Join(repo, "submodule")
	gitForTest(t, "-c", "protocol.file.allow=always", "-C", repo, "submodule", "add", "-q", outside, sub)
	ignored := filepath.Join(repo, "ignored")
	gitForTest(t, "init", "-q", ignored)
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("ignored\n"), 0600)
	excluded := filepath.Join(root, "node_modules", "excluded")
	gitForTest(t, "init", "-q", excluded)
	alias := filepath.Join(root, "alias")
	os.Symlink(repo, alias)
	cfg := defaults().Search
	cfg.Roots = []string{root, alias, filepath.Join(root, "missing")}
	warnings := 0
	got, err := discover(context.Background(), cfg, func(string) { warnings++ })
	if err != nil {
		t.Fatal(err)
	}
	want := []string{repo, nested, work, sub, ignored}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	if warnings != 1 {
		t.Fatal("missing root not diagnosed", warnings)
	}
	cfg.Mode = "all"
	cfg.Exclude = nil
	got, err = discover(context.Background(), cfg, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(got, root) || !contains(got, excluded) || contains(got, filepath.Join(repo, ".git")) {
		t.Fatal("all mode or Git metadata pruning", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discover(ctx, cfg, func(string) {}); err == nil {
		t.Fatal("cancellation ignored")
	}
}
