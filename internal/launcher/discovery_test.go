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

func TestDiscoverySelectsRootsAndFullyEmptyDirectories(t *testing.T) {
	root := t.TempDir()
	emptyRoot := t.TempDir()
	for _, name := range []string{"empty", "collection/empty", "hidden-file", "symlink-only", ".hidden", "node_modules/empty", "beyond/depth/empty"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "hidden-file", ".keep"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(root, "symlink-only", "link")); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "root-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"git", "all"} {
		t.Run(mode, func(t *testing.T) {
			cfg := defaults().Search
			cfg.Mode = mode
			cfg.Roots = []string{alias, root, emptyRoot}
			cfg.ExcludeHidden = true
			cfg.MaxDepth = 2
			got, err := discover(context.Background(), cfg, func(s string) { t.Error(s) })
			want := []string{root, emptyRoot, filepath.Join(root, "empty"), filepath.Join(root, "collection", "empty")}
			if mode == "all" {
				for _, name := range []string{"collection", "hidden-file", "symlink-only", "beyond", "beyond/depth"} {
					want = append(want, filepath.Join(root, name))
				}
			}
			sort.Strings(want)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("got %q %v; want %q", got, err, want)
			}
			for _, path := range got {
				if err := validateProject(context.Background(), cfg, path); err != nil {
					t.Errorf("discovered directory rejected: %s: %v", path, err)
				}
			}
		})
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
	want := []string{root, repo, nested, work, sub, ignored}
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

func TestDiscoveryExcludeHidden(t *testing.T) {
	root := t.TempDir()
	visible := []string{filepath.Join(root, "project"), filepath.Join(root, "project.name")}
	hiddenRoot := filepath.Join(root, ".hidden")
	hiddenChild := filepath.Join(hiddenRoot, "child")
	collection := filepath.Join(root, ".collection")
	hidden := []string{hiddenRoot, hiddenChild, filepath.Join(hiddenRoot, ".nested"), filepath.Join(collection, "project"), filepath.Join(visible[0], ".nested")}
	for _, path := range append(visible, hidden...) {
		gitForTest(t, "init", "-q", path)
	}
	for _, mode := range []string{"git", "all"} {
		t.Run(mode, func(t *testing.T) {
			cfg := defaults().Search
			cfg.Roots = []string{root}
			cfg.Mode = mode
			for _, excludeHidden := range []bool{false, true} {
				cfg.ExcludeHidden = excludeHidden
				got, err := discover(context.Background(), cfg, func(string) {})
				want := append([]string{root}, visible...)
				if !excludeHidden {
					want = append(want, hidden...)
					if mode == "all" {
						want = append(want, collection)
					}
				}
				sort.Strings(want)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("exclude_hidden=%t: %q %v; want %q", excludeHidden, got, err, want)
				}
			}
			// Explicit roots opt into a hidden tree; hidden descendants are still pruned.
			cfg.Roots = []string{hiddenRoot}
			got, err := discover(context.Background(), cfg, func(string) {})
			if err != nil || !reflect.DeepEqual(got, []string{hiddenRoot, hiddenChild}) {
				t.Fatalf("explicit hidden root: %q %v", got, err)
			}
		})
	}
}

func TestDiscoveryPrunesExplicitGitMetadataRoots(t *testing.T) {
	root := t.TempDir()
	metadata := filepath.Join(root, "project", ".git")
	objects := filepath.Join(metadata, "objects")
	if err := os.MkdirAll(filepath.Join(objects, "pack"), 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "metadata-alias")
	if err := os.Symlink(metadata, alias); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"all", "git"} {
		cfg := Search{Mode: mode, Roots: []string{metadata, objects, alias}}
		got, err := discover(context.Background(), cfg, func(string) {})
		if err != nil || len(got) != 0 {
			t.Fatalf("%s metadata roots exposed projects: %q %v", mode, got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discover(ctx, Search{Mode: "all", Roots: []string{metadata}}, func(string) {}); err != context.Canceled {
		t.Fatalf("pruned-root cancellation ignored: %v", err)
	}
}

func TestDiscoveryMaxDepthIncludesBoundaryAndExplicitDeeperRoots(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	boundary := filepath.Join(child, "ordinary", "boundary")
	deeper := filepath.Join(boundary, "deeper")
	for _, path := range []string{root, child, boundary, deeper} {
		gitForTest(t, "init", "-q", path)
	}
	for _, mode := range []string{"git", "all"} {
		t.Run(mode, func(t *testing.T) {
			cfg := defaults().Search
			cfg.Roots = []string{root}
			cfg.Mode = mode
			cfg.MaxDepth = 3
			got, err := discover(context.Background(), cfg, func(string) {})
			want := []string{root, child, boundary}
			if mode == "all" {
				want = append(want, filepath.Dir(boundary))
			}
			sort.Strings(want)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("depth boundary: %q %v; want %q", got, err, want)
			}
			cfg.Roots = append(cfg.Roots, deeper)
			got, err = discover(context.Background(), cfg, func(string) {})
			if err != nil || !contains(got, deeper) {
				t.Fatalf("explicit deeper root missing: %q %v", got, err)
			}
			cfg.Roots = []string{root}
			cfg.MaxDepth = 0
			got, err = discover(context.Background(), cfg, func(string) {})
			if err != nil || !contains(got, deeper) {
				t.Fatalf("unlimited depth lost nested repository: %q %v", got, err)
			}
		})
	}
}
