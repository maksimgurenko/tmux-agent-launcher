package launcher

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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
