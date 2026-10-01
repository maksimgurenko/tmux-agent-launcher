package launcher

import (
	"context"
	"fmt"
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
			if path != root {
				rel, _ := filepath.Rel(root, path)
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
			eligible := cfg.Mode == "all"
			if !eligible {
				if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
					cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--show-toplevel")
					for _, entry := range os.Environ() {
						key, _, _ := strings.Cut(entry, "=")
						if !contains([]string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE"}, key) {
							cmd.Env = append(cmd.Env, entry)
						}
					}
					out, err := cmd.Output()
					if err == nil {
						top, err := canonicalDir(strings.TrimSuffix(string(out), "\n"))
						eligible = err == nil && top == path
					}
				}
			}
			if eligible {
				seen[path] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	a := make([]string, 0, len(seen))
	for p := range seen {
		a = append(a, p)
	}
	sort.Strings(a)
	return a, nil
}
