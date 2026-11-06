package launcher

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func discover(ctx context.Context, cfg Search, warn func(string)) ([]string, error) {
	seen := map[string]bool{}
	for _, root := range cfg.Roots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		root, err := canonicalDir(root)
		if err != nil {
			warn(fmt.Sprintf("skip root: %v", err))
			continue
		}
		if contains(strings.Split(root, string(filepath.Separator)), ".git") {
			continue
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				warn(fmt.Sprintf("skip %s: %v", path, walkErr))
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if hasControl(path) {
				return filepath.SkipDir
			}
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			depth := 0
			if path != root {
				if cfg.ExcludeHidden && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				rel, _ := filepath.Rel(root, path)
				depth = strings.Count(rel, string(filepath.Separator)) + 1
				for _, pattern := range cfg.Exclude {
					name := d.Name()
					if strings.ContainsRune(pattern, '/') {
						name = rel
					}
					if match, _ := filepath.Match(pattern, name); match {
						return filepath.SkipDir
					}
				}
			}
			// Empty directories inside bare repository storage are Git metadata.
			if cfg.Mode == "git" && isBareGitRepository(ctx, path) {
				return filepath.SkipDir
			}
			eligible := cfg.Mode == "all" || path == root || isEmptyDir(path)
			if !eligible {
				eligible = isGitProject(ctx, path)
			}
			if eligible {
				seen[path] = true
			}
			if cfg.MaxDepth > 0 && depth >= cfg.MaxDepth {
				return filepath.SkipDir
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a := make([]string, 0, len(seen))
	for p := range seen {
		a = append(a, p)
	}
	sort.Strings(a)
	return a, nil
}

func isGitProject(ctx context.Context, path string) bool {
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		return false
	}
	out, err := gitRevParse(ctx, path, "--show-toplevel")
	if err != nil {
		return false
	}
	top, err := canonicalDir(out)
	return err == nil && top == path
}

func isBareGitRepository(ctx context.Context, path string) bool {
	if _, err := os.Stat(filepath.Join(path, "HEAD")); err != nil {
		return false
	}
	out, err := gitRevParse(ctx, path, "--is-bare-repository")
	return err == nil && out == "true"
}

func gitRevParse(ctx context.Context, path, flag string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", flag)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !contains([]string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE"}, key) {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	out, err := cmd.Output()
	return strings.TrimSuffix(string(out), "\n"), err
}

func isEmptyDir(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	_, err = f.Readdirnames(1)
	return err == io.EOF
}

func validateProject(ctx context.Context, cfg Search, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	canonical, err := canonicalDir(path)
	if err != nil {
		return fmt.Errorf("project unavailable; run refresh: %w", err)
	}
	if canonical != path {
		return fmt.Errorf("project directory changed since discovery; run refresh")
	}
	if cfg.Mode == "git" && !isEmptyDir(path) && !isGitProject(ctx, path) {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, root := range cfg.Roots {
			if canonical, err := canonicalDir(root); err == nil && canonical == path {
				return ctx.Err()
			}
		}
		return fmt.Errorf("project is no longer a Git working tree, search root or empty directory; run refresh")
	}
	return ctx.Err()
}
