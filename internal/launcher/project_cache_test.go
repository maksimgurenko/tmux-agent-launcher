package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func cacheFixture(t *testing.T) (Search, string, string) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	project := filepath.Join(root, "project")
	gitForTest(t, "init", "-q", project)
	cfg := defaults().Search
	cfg.Roots = []string{root}
	_, path, err := projectCacheLocation(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, project, path
}

func TestProjectCacheHitDoesNotScanAndCanBeDisabled(t *testing.T) {
	cfg, project, path := cacheFixture(t)
	want := []string{project}
	for i := 0; i < 2; i++ {
		got, err := cachedProjects(context.Background(), cfg, testLauncher, func(string) {})
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("cache lookup %d: %q %v", i, got, err)
		}
		if i == 0 {
			if err := os.RemoveAll(project); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := validateProject(context.Background(), cfg, project); err == nil {
		t.Fatal("deleted cached project accepted")
	}
	for _, entry := range []struct {
		path string
		mode os.FileMode
	}{{path, 0600}, {filepath.Dir(path), 0700}} {
		st, err := os.Stat(entry.path)
		if err != nil || st.Mode().Perm() != entry.mode {
			t.Fatalf("cache permissions: %v %v", st, err)
		}
	}
	cfg.Cache = false
	got, err := cachedProjects(context.Background(), cfg, testLauncher, func(string) {})
	if err != nil || len(got) != 0 {
		t.Fatalf("cache=false returned deleted project: %q %v", got, err)
	}
}

func TestProjectCacheSettingsAndRelativeRootsChangeIdentity(t *testing.T) {
	cfg, _, path := cacheFixture(t)
	for _, change := range []func(*Search){
		func(c *Search) { c.MaxDepth = 7 },
		func(c *Search) { c.Mode = "all" },
		func(c *Search) { c.ExcludeHidden = true },
		func(c *Search) { c.Exclude = append([]string{"another"}, c.Exclude...) },
		func(c *Search) { c.Roots = []string{filepath.Join(c.Roots[0], "another")} },
	} {
		other := cfg
		change(&other)
		_, otherPath, err := projectCacheLocation(other)
		if err != nil || otherPath == path {
			t.Fatalf("changed search reused cache: %s %v", otherPath, err)
		}
	}
	cfg.Roots = []string{"relative"}
	t.Chdir(t.TempDir())
	_, first, err := projectCacheLocation(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	_, second, err := projectCacheLocation(cfg)
	if err != nil || first == second {
		t.Fatal("relative root reused cache across working directories", err)
	}
}

func TestProjectCacheCorruptionAndUnavailableStorageFallBackToDiscovery(t *testing.T) {
	cfg, project, path := cacheFixture(t)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`broken`, `{"version":999}`, `{"version":1,"updated_at":"2026-01-01T00:00:00Z","projects":["relative"]}`} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := cachedProjects(context.Background(), cfg, testLauncher, func(string) {})
		if err != nil || !reflect.DeepEqual(got, []string{project}) {
			t.Fatalf("corrupt-cache fallback: %q %v", got, err)
		}
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", blocker)
	warnings := 0
	got, err := cachedProjects(context.Background(), cfg, testLauncher, func(string) { warnings++ })
	if err != nil || !reflect.DeepEqual(got, []string{project}) || warnings != 1 {
		t.Fatalf("unavailable-cache fallback: %q %v; warnings=%d", got, err, warnings)
	}
}

func TestProjectCacheConcurrentColdScansAndCancelledLock(t *testing.T) {
	cfg, project, path := cacheFixture(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	proof := filepath.Join(bin, "calls")
	t.Setenv("CACHE_TEST_GIT", realGit)
	t.Setenv("CACHE_TEST_PROOF", proof)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nprintf x >> \"$CACHE_TEST_PROOF\"\nexec \"$CACHE_TEST_GIT\" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := cachedProjects(context.Background(), cfg, testLauncher, func(string) {})
			if err != nil || !reflect.DeepEqual(got, []string{project}) {
				t.Errorf("concurrent cache: %q %v", got, err)
			}
		}()
	}
	wg.Wait()
	if b, err := os.ReadFile(proof); err != nil || string(b) != "x" {
		t.Fatalf("concurrent launches repeated Git validation: %q %v", b, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := refreshProjects(ctx, cfg, true, func(string) {}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled refresh did not cancel", err)
	}
	if _, err := readProjectIndex(path); err != nil {
		t.Fatal("cancelled refresh damaged existing cache", err)
	}
	lock, err := os.OpenFile(path+".lock", os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	waiting, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stop()
	if _, err := refreshProjects(waiting, cfg, true, func(string) {}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("refresh waiting on another scan did not cancel", err)
	}
}

func TestCachedProjectValidationRejectsChangedRepositoryAndAlias(t *testing.T) {
	cfg, project, _ := cacheFixture(t)
	if err := validateProject(context.Background(), cfg, project); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(project, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := validateProject(context.Background(), cfg, project); err == nil {
		t.Fatal("cached directory that lost Git metadata was accepted")
	}
	if err := os.Remove(project); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), project); err != nil {
		t.Fatal(err)
	}
	cfg.Mode = "all"
	if err := validateProject(context.Background(), cfg, project); err == nil {
		t.Fatal("cached path redirected to a different canonical project")
	}
}

func TestStaleProjectCacheDoesNotBlockPickerAndRefreshSurvivesParentExit(t *testing.T) {
	tm := disposableTmux(t)
	cfg, project, path := cacheFixture(t)
	if _, err := refreshProjects(context.Background(), cfg, true, func(string) {}); err != nil {
		t.Fatal(err)
	}
	index, err := readProjectIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	index.UpdatedAt = time.Now().Add(-2 * projectRefreshInterval)
	if err := writeProjectIndex(path, index); err != nil {
		t.Fatal(err)
	}
	newProject := filepath.Join(cfg.Roots[0], "new-project")
	gitForTest(t, "init", "-q", newProject)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	gate, proof, rows := filepath.Join(bin, "gate"), filepath.Join(bin, "proof"), filepath.Join(bin, "rows")
	t.Setenv("CACHE_TEST_GIT", realGit)
	t.Setenv("CACHE_TEST_GATE", gate)
	t.Setenv("CACHE_TEST_PROOF", proof)
	t.Setenv("CACHE_TEST_ROWS", rows)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	git := "#!/bin/sh\nprintf x > \"$CACHE_TEST_PROOF\"\nwhile [ ! -e \"$CACHE_TEST_GATE\" ]; do /bin/sleep 0.01; done\nexec \"$CACHE_TEST_GIT\" \"$@\"\n"
	rofi := "#!/bin/sh\ncase \"$*\" in *'Agent session'*) cat >/dev/null; echo 0;; *) cat > \"$CACHE_TEST_ROWS\"; exit 1;; esac\n"
	for name, body := range map[string]string{"git": git, "rofi": rofi} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Always release the worker, including when a picker assertion fails.
	t.Cleanup(func() {
		os.WriteFile(gate, nil, 0600)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			updated, err := readProjectIndex(path)
			if err == nil && updated.fresh() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Error("background refresh did not finish")
	})
	config := filepath.Join(bin, "config.toml")
	text := "[search]\nroots = [" + fmtTOMLString(cfg.Roots[0]) + "]\n[profiles.codex]\ncommand = [\"/bin/true\"]\n"
	if err := os.WriteFile(config, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, testLauncher, "--config", config, "--profile", "codex", "--tmux-socket", tm.Prefix[1])
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("picker waited for blocked refresh: %s %v", b, err)
	}
	if b, err := os.ReadFile(rows); err != nil || strings.TrimSpace(string(b)) != project {
		t.Fatalf("picker did not receive cached project: %q %v", b, err)
	}
	waitProof(t, proof)
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		updated, err := readProjectIndex(path)
		if err == nil && contains(updated.Projects, newProject) && updated.fresh() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("detached refresh failed to discover new repository after picker exited")
}

func fmtTOMLString(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
