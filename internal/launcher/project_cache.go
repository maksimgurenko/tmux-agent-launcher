package launcher

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const projectCacheVersion = 2
const projectRefreshInterval = 5 * time.Minute

type projectIndex struct {
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Projects  []string  `json:"projects"`
}

func (index projectIndex) fresh() bool {
	age := time.Since(index.UpdatedAt)
	return age >= 0 && age < projectRefreshInterval
}

// Expand relative roots without touching the filesystem: a cache hit must not
// wait for a slow or unavailable search root. Exclusions remain root-relative.
func projectCacheLocation(cfg Search) (Search, string, error) {
	roots := make([]string, len(cfg.Roots))
	for i, root := range cfg.Roots {
		path, err := expandPath(root)
		if err != nil {
			return cfg, "", err
		}
		roots[i] = path
	}
	cfg.Roots = roots
	data, err := json.Marshal(struct {
		Version int
		Search  Search
	}{projectCacheVersion, cfg})
	if err != nil {
		return cfg, "", err
	}
	key := sha256.Sum256(data)
	path := filepath.Join(xdgPath("XDG_CACHE_HOME", ".cache"), "tmux-agent-launcher", "projects", fmt.Sprintf("%x.json", key))
	return cfg, path, nil
}

func readProjectIndex(path string) (projectIndex, error) {
	var index projectIndex
	data, err := os.ReadFile(path)
	if err != nil {
		return index, err
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return index, err
	}
	if index.Version != projectCacheVersion || index.UpdatedAt.IsZero() {
		return index, fmt.Errorf("invalid project cache version or timestamp")
	}
	for i, path := range index.Projects {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || hasControl(path) || (i > 0 && path <= index.Projects[i-1]) {
			return index, fmt.Errorf("invalid project cache entry")
		}
	}
	return index, nil
}

func writeProjectIndex(path string, index projectIndex) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".projects-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := json.NewEncoder(f).Encode(index); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Refreshes share a lock. Cold concurrent launches reuse the first completed
// scan; explicit refresh always requests a new scan. Readers never take a lock.
func refreshProjects(ctx context.Context, cfg Search, force bool, warn func(string)) ([]string, error) {
	cfg, path, err := projectCacheLocation(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(path+".lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	lock := os.NewFile(uintptr(fd), "project cache lock")
	defer lock.Close()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	if !force {
		if index, err := readProjectIndex(path); err == nil && index.fresh() {
			return index.Projects, nil
		}
	}
	dirs, err := discover(ctx, cfg, warn)
	if err != nil {
		return nil, err
	}
	index := projectIndex{Version: projectCacheVersion, UpdatedAt: time.Now(), Projects: dirs}
	return dirs, writeProjectIndex(path, index)
}

func cachedProjects(ctx context.Context, cfg Search, self string, warn func(string)) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !cfg.Cache {
		return discover(ctx, cfg, warn)
	}
	normalized, path, err := projectCacheLocation(cfg)
	if err == nil {
		if index, readErr := readProjectIndex(path); readErr == nil {
			if !index.fresh() {
				if err := startProjectRefresh(self, normalized); err != nil {
					warn(fmt.Sprintf("cannot refresh project cache: %v", err))
				}
			}
			return index.Projects, nil
		}
	}
	dirs, err := refreshProjects(ctx, cfg, false, warn)
	if err == nil || ctx.Err() != nil {
		return dirs, err
	}
	warn(fmt.Sprintf("project cache unavailable: %v", err))
	if dirs != nil { // A successful scan must remain usable if saving failed.
		return dirs, nil
	}
	return discover(ctx, cfg, warn)
}

func startProjectRefresh(self string, cfg Search) error {
	// An unlinked 0600 file delivers the effective settings before the parent
	// can exit. No profile commands, environment snapshots or shell are involved.
	f, err := os.CreateTemp("", "tmux-agent-launcher-search-*")
	if err != nil {
		return err
	}
	defer f.Close()
	if err := os.Remove(f.Name()); err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(cfg); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	cmd := exec.Command(self, "__refresh-projects")
	cmd.Stdin = f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func runProjectRefresh(ctx context.Context) error {
	var search Search
	if err := json.NewDecoder(os.Stdin).Decode(&search); err != nil {
		return err
	}
	c := defaults()
	c.Search = search
	if err := c.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	_, err := refreshProjects(ctx, search, false, func(string) {})
	return err
}
