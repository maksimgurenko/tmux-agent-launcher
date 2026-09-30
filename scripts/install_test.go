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
	stage := filepath.Join(t.TempDir(), "tmux-agent-launcher")
	os.MkdirAll(stage, 0700)
	for name, c := range map[string]string{"tmux-agent-launcher": content, "LICENSE": "fixture license", "NOTICE": "fixture notice"} {
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
