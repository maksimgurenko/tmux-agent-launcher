package launcher

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProjectPickerLaunchesRootsAndEmptyDirectories(t *testing.T) {
	for _, target := range []string{"root", "empty"} {
		t.Run(target, func(t *testing.T) {
			tm := disposableTmux(t)
			root := t.TempDir()
			empty := filepath.Join(root, "empty")
			if err := os.Mkdir(empty, 0700); err != nil {
				t.Fatal(err)
			}
			project := root
			if target == "empty" {
				project = empty
			}
			bin := t.TempDir()
			proof := filepath.Join(bin, "proof")
			t.Setenv("PICKER_TEST_PROJECT", project)
			t.Setenv("PICKER_TEST_PROOF", proof)
			t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
			picker := "#!/bin/sh\ncase \"$*\" in *'Agent session'*) cat >/dev/null; echo 0;; *) i=0; while IFS= read -r row; do if [ \"$row\" = \"$PICKER_TEST_PROJECT\" ]; then echo \"$i\"; fi; i=$((i + 1)); done;; esac\n"
			if err := os.WriteFile(filepath.Join(bin, "rofi"), []byte(picker), 0700); err != nil {
				t.Fatal(err)
			}
			cfg := defaults()
			cfg.Search.Roots = []string{root}
			cfg.Profiles = map[string]Profile{"test": {Command: []string{"/bin/sh", "-c", "pwd > \"$PICKER_TEST_PROOF\"; exec /bin/sleep 60"}}}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session, err := chooseSession(ctx, cfg, tm, "test", testLauncher)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(waitProof(t, proof)); got != project || session.Project != project {
				t.Fatalf("launched in %q with session project %q; want %q", got, session.Project, project)
			}
		})
	}
}

func TestConfigInitEmbedsExampleAndPreservesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings", "config.toml")
	args := []string{"config", "init", "--config", path}
	if out, err := exec.Command(testLauncher, args...).CombinedOutput(); err != nil {
		t.Fatalf("config init: %s %v", out, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, exampleConfig) {
		t.Fatalf("embedded example missing or changed: %v", err)
	}
	c, err := loadConfig(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.validate(); err != nil {
		t.Fatalf("generated configuration is invalid: %v", err)
	}
	personal := []byte("picker = \"fzf\"\n")
	if err := os.WriteFile(path, personal, 0600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(testLauncher, args...).Run(); err == nil {
		t.Fatal("config init replaced an existing file")
	}
	got, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(got, personal) {
		t.Fatal("existing configuration was changed", err)
	}
}

func TestRefreshDepthOverrideWithoutUIDependencies(t *testing.T) {
	root := t.TempDir()
	deep := root
	for i := 0; i < 8; i++ {
		deep = filepath.Join(deep, "nested")
	}
	if err := os.MkdirAll(deep, 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "config.toml")
	text := "[search]\nroots = [" + fmtTOMLString(root) + "]\nmode = \"all\"\nmax_depth = 2\n"
	if err := os.WriteFile(config, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir()) // No tmux, Git, picker or terminal available.
	cmd := exec.Command(testLauncher, "refresh", "--config", config, "--max-depth", "7")
	if b, err := cmd.CombinedOutput(); err != nil || string(b) != "Indexed 8 project directories\n" {
		t.Fatalf("refresh with explicit depth: %s %v", b, err)
	}
	c, err := loadConfig(config, true)
	if err != nil {
		t.Fatal(err)
	}
	c.Search.MaxDepth = 7
	_, path, err := projectCacheLocation(c.Search)
	if err != nil {
		t.Fatal(err)
	}
	index, err := readProjectIndex(path)
	if err != nil || len(index.Projects) != 8 || contains(index.Projects, deep) {
		t.Fatalf("refresh did not cache the requested boundary: %v %v", index, err)
	}
	for _, value := range []string{"0", "7"} {
		o, err := parseOptions([]string{"--max-depth", value})
		if err != nil {
			t.Fatal(err)
		}
		applyOptions(&c, o)
		if (value == "0" && c.Search.MaxDepth != 0) || (value == "7" && c.Search.MaxDepth != 7) {
			t.Fatal("explicit depth did not override configuration")
		}
	}
	if _, err := parseOptions([]string{"--max-depth", "-1"}); err == nil {
		t.Fatal("negative CLI depth accepted")
	}
}

func TestFzfTerminalHandoffPreservesExplicitUnlimitedDepth(t *testing.T) {
	tm := disposableTmux(t)
	dir := t.TempDir()
	proof := filepath.Join(dir, "arguments")
	capture := filepath.Join(dir, "terminal")
	if err := os.WriteFile(capture, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$DEPTH_TEST_ARGS\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPTH_TEST_ARGS", proof)
	config := filepath.Join(dir, "config.toml")
	text := "picker = \"fzf\"\nterminal = \"custom\"\n[search]\nroots = [" + fmtTOMLString(dir) + "]\nmax_depth = 7\n[terminals.custom]\ncommand = [" + fmtTOMLString(capture) + ", \"{command}\"]\n"
	if err := os.WriteFile(config, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(testLauncher, "--config", config, "--max-depth", "0", "--tmux-socket", tm.Prefix[1])
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fzf handoff: %s %v", b, err)
	}
	b, err := os.ReadFile(proof)
	if err != nil || !strings.Contains(string(b), "--max-depth\n0\n") {
		t.Fatalf("terminal handoff dropped explicit depth: %s %v", b, err)
	}
}
