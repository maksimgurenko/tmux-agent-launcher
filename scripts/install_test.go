package scripts

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testArchive(t *testing.T, dir, content string) (string, string) {
	t.Helper()
	return testArchiveFiles(t, dir, map[string]string{"tmux-agent-launcher": content, "LICENSE": "fixture license", "NOTICE": "fixture notice"})
}

func testArchiveFiles(t *testing.T, dir string, files map[string]string) (string, string) {
	t.Helper()
	stage := filepath.Join(t.TempDir(), "tmux-agent-launcher")
	os.MkdirAll(stage, 0700)
	for name, c := range files {
		if err := os.WriteFile(filepath.Join(stage, name), []byte(c), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a := filepath.Join(dir, "fixture.tar.gz")
	if b, err := exec.Command("tar", "-C", filepath.Dir(stage), "-czf", a, "tmux-agent-launcher").CombinedOutput(); err != nil {
		t.Fatalf("tar: %s %v", b, err)
	}
	b, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	return a, fmt.Sprintf("%x", h)
}

func TestInstallerPreservesUnrelatedMetadataPermissions(t *testing.T) {
	dir := t.TempDir()
	prefix, data := filepath.Join(dir, "prefix"), filepath.Join(dir, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "unrelated executable")
	regular := filepath.Join(data, ".unrelated.sha256")
	link := filepath.Join(data, ".unrelated-link.sha256")
	for _, path := range []string{outside, regular} {
		if err := os.WriteFile(path, []byte("unrelated"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	invoke := func(args ...string) {
		t.Helper()
		base := []string{"./install.sh", "--prefix", prefix, "--data-dir", data}
		if b, err := exec.Command("bash", append(base, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("installer: %s %v", b, err)
		}
		for _, path := range []string{outside, regular} {
			st, err := os.Stat(path)
			if err != nil || st.Mode().Perm() != 0755 {
				t.Fatalf("unrelated permissions changed for %s: %v %v", path, st, err)
			}
		}
		if got, err := os.Readlink(link); err != nil || got != outside {
			t.Fatalf("unrelated symlink changed: %q %v", got, err)
		}
	}
	a, sum := testArchive(t, dir, "first")
	invoke("--archive", a, "--checksum", sum)
	a, sum = testArchive(t, dir, "second")
	invoke("--archive", a, "--checksum", sum, "--update")
	invoke("--rollback")
	invoke("--uninstall")
}

func TestInstallerRequiresUpdateForLicenseAndNoticeChanges(t *testing.T) {
	for _, changed := range []string{"LICENSE", "NOTICE"} {
		t.Run(changed, func(t *testing.T) {
			dir := t.TempDir()
			prefix, data := filepath.Join(dir, "prefix"), filepath.Join(dir, "data")
			invoke := func(ok bool, args ...string) {
				t.Helper()
				base := []string{"./install.sh", "--prefix", prefix, "--data-dir", data}
				if b, err := exec.Command("bash", append(base, args...)...).CombinedOutput(); (err == nil) != ok {
					t.Fatalf("installer: %s %v", b, err)
				}
			}
			a, sum := testArchive(t, dir, "first")
			invoke(true, "--archive", a, "--checksum", sum)
			a, sum = testArchive(t, dir, "second")
			invoke(true, "--archive", a, "--checksum", sum, "--update")
			// Preserve both the installed set and an existing rollback set on rejection.
			before := map[string]string{}
			paths := []string{filepath.Join(prefix, "bin", "tmux-agent-launcher")}
			entries, err := os.ReadDir(data)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				paths = append(paths, filepath.Join(data, e.Name()))
			}
			for _, path := range paths {
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = string(b)
			}
			files := map[string]string{"tmux-agent-launcher": "second", "LICENSE": "fixture license", "NOTICE": "fixture notice"}
			files[changed] = "changed attribution"
			a, sum = testArchiveFiles(t, dir, files)
			invoke(false, "--archive", a, "--checksum", sum)
			for path, want := range before {
				if b, err := os.ReadFile(path); err != nil || string(b) != want {
					t.Fatalf("rejected update changed %s: %q %v", path, b, err)
				}
			}
			invoke(true, "--archive", a, "--checksum", sum, "--update")
			if b, err := os.ReadFile(filepath.Join(data, changed)); err != nil || string(b) != files[changed] {
				t.Fatalf("attribution update missing: %q %v", b, err)
			}
			invoke(true, "--rollback")
			if b, err := os.ReadFile(filepath.Join(data, changed)); err != nil || string(b) != before[filepath.Join(data, changed)] {
				t.Fatalf("attribution rollback failed: %q %v", b, err)
			}
		})
	}
}
func TestInstallerOwnershipUpdateRollbackAndUninstall(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "prefix space")
	data := filepath.Join(dir, "data")
	config := filepath.Join(dir, "config.toml")
	os.WriteFile(config, []byte("personal"), 0600)
	invoke := func(ok bool, args ...string) string {
		t.Helper()
		base := []string{"./install.sh", "--prefix", prefix, "--data-dir", data}
		b, err := exec.Command("bash", append(base, args...)...).CombinedOutput()
		if (err == nil) != ok {
			t.Fatalf("installer %q: %s %v", args, b, err)
		}
		return string(b)
	}
	a, sum := testArchive(t, dir, "first")
	invoke(true, "--archive", a, "--checksum", sum)
	invoke(true, "--archive", a, "--checksum", sum)
	binary := filepath.Join(prefix, "bin", "tmux-agent-launcher")
	assert := func(want string) {
		t.Helper()
		got, err := os.ReadFile(binary)
		if err != nil || string(got) != want {
			t.Fatalf("binary %q %v want %q", got, err, want)
		}
	}
	assert("first")
	a, sum = testArchive(t, dir, "second")
	invoke(false, "--archive", a, "--checksum", sum)
	assert("first")
	invoke(true, "--archive", a, "--checksum", sum, "--update")
	assert("second")
	invoke(true, "--rollback")
	assert("first")
	invoke(true, "--uninstall")
	if _, err := os.Stat(binary); !os.IsNotExist(err) {
		t.Fatal("binary remains", err)
	}
	if got, _ := os.ReadFile(config); string(got) != "personal" {
		t.Fatal("personal configuration changed")
	}
	os.Symlink(config, binary)
	invoke(false, "--archive", a, "--checksum", sum)
	os.Remove(binary)
	os.WriteFile(binary, []byte("other manager"), 0700)
	invoke(false, "--archive", a, "--checksum", sum)
	os.Remove(binary)
	invoke(true, "--archive", a, "--checksum", sum)
	os.WriteFile(binary, []byte("local change"), 0700)
	invoke(false, "--archive", a, "--checksum", sum, "--update")
	if out := invoke(true, "--uninstall"); !strings.Contains(out, "Preserved changed file") {
		t.Fatal(out)
	}
	assert("local change")
}
func TestInstallerRejectsWrongChecksumAndSymlinkArchive(t *testing.T) {
	dir := t.TempDir()
	a, sum := testArchive(t, dir, "first")
	cmd := func(checksum string) error {
		return exec.Command("bash", "./install.sh", "--prefix", filepath.Join(dir, "prefix"), "--data-dir", filepath.Join(dir, "data"), "--archive", a, "--checksum", checksum).Run()
	}
	if err := cmd(strings.Repeat("0", 64)); err == nil {
		t.Fatal("wrong checksum accepted")
	}
	stage := filepath.Join(t.TempDir(), "tmux-agent-launcher")
	os.Mkdir(stage, 0700)
	os.Symlink("/etc/passwd", filepath.Join(stage, "tmux-agent-launcher"))
	for _, n := range []string{"LICENSE", "NOTICE"} {
		os.WriteFile(filepath.Join(stage, n), []byte("fixture"), 0600)
	}
	if err := exec.Command("tar", "-C", filepath.Dir(stage), "-czf", a, "tmux-agent-launcher").Run(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(a)
	sum = fmt.Sprintf("%x", sha256.Sum256(b))
	if err := cmd(sum); err == nil {
		t.Fatal("symlink archive accepted")
	}
}
