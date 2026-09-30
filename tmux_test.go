package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var testLauncher string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "launcher-test-build-")
	if err != nil {
		panic(err)
	}
	testLauncher = filepath.Join(dir, "launcher")
	b, err := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-o", testLauncher, ".").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build test launcher: %s %v\n", b, err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
func disposableTmux(t *testing.T) Tmux {
	t.Helper()
	tm := Tmux{Prefix: []string{"-S", filepath.Join(t.TempDir(), "socket"), "-f", "/dev/null"}}
	if err := tm.checkVersion(context.Background()); err != nil {
		if os.Getenv("REQUIRE_TMUX") == "1" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Cleanup(func() { tm.output(context.Background(), "kill-server") })
	return tm
}
func TestTmuxLiteralIdentityConcurrentCreationAndDetachedProfiles(t *testing.T) {
	tm := disposableTmux(t)
	ctx := context.Background()
	project := filepath.Join(t.TempDir(), "repo space.:#(literal)#{session_id}")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	c := defaults()
	c.Profiles["test"] = Profile{Label: "Test", Command: []string{"/bin/sh", "-c", "printf '%s\\n' \"$EXAMPLE\" \"$1\" \"$TMUX_PANE\" > proof; exec sleep 60", "test", "arg with spaces;$()"}, Env: map[string]string{"EXAMPLE": "literal env;$()"}}
	var wg sync.WaitGroup
	results := make(chan Session, 6)
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := tm.ensure(ctx, c, "test", project, testLauncher)
			results <- s
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	id := ""
	for s := range results {
		if id == "" {
			id = s.ID
		}
		if s.ID != id {
			t.Fatal("duplicate session", s)
		}
	}
	ss, err := tm.sessions(ctx)
	if err != nil || len(ss) != 1 {
		t.Fatalf("sessions %v %v", ss, err)
	}
	name, err := tm.output(ctx, "display-message", "-p", "-t", id, "#{session_name}")
	if err != nil || name != sessionName("test", project) {
		t.Fatalf("literal name changed: %q %v", name, err)
	}
	attach, err := tm.attachCommand(ctx, id)
	if err != nil || len(attach) != 6 || attach[2] != tm.Prefix[1] || attach[5] != id {
		t.Fatalf("wrong server attachment: %q %v", attach, err)
	}
	var proof []byte
	for i := 0; i < 100; i++ {
		proof, _ = os.ReadFile(filepath.Join(project, "proof"))
		if len(proof) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.HasPrefix(string(proof), "literal env;$()\narg with spaces;$()\n%") {
		t.Fatalf("argv/environment lost: %q", proof)
	}
	delete(c.Profiles, "test")
	s, err := tm.ensure(ctx, c, "test", project, testLauncher)
	if err != nil || s.ID != id {
		t.Fatalf("removed profile did not reattach: %v %v", s, err)
	}
	disabled := false
	c.Profiles["test"] = Profile{Enabled: &disabled, Command: []string{"absent-agent"}}
	s, err = tm.ensure(ctx, c, "test", project, testLauncher)
	if err != nil || s.ID != id {
		t.Fatal("disabled/missing executable blocks live session", err)
	}
	if _, err = tm.ensure(ctx, c, "test", t.TempDir(), testLauncher); err == nil {
		t.Fatal("disabled profile created")
	}
}
func TestLegacySessionAndGlobalMetadataIsolation(t *testing.T) {
	tm := disposableTmux(t)
	ctx := context.Background()
	project := t.TempDir()
	id, err := tm.output(ctx, "new-session", "-d", "-P", "-F", "#{session_id}", "-s", sessionName("codex", project), "sleep", "60")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tm.output(ctx, "set-option", "-g", ownerKey, ownerValue); err != nil {
		t.Fatal(err)
	}
	tm.output(ctx, "new-session", "-d", "-s", "unrelated", "sleep", "60")
	ss, err := tm.sessions(ctx)
	if err != nil || len(ss) != 1 || ss[0].ID != id {
		t.Fatalf("legacy or inherited metadata incorrectly handled: %v %v", ss, err)
	}
	c := defaults()
	delete(c.Profiles, "codex")
	s, err := tm.ensure(ctx, c, "codex", project, testLauncher)
	if err != nil || s.ID != id {
		t.Fatal("legacy reuse failed", err)
	}
}

func TestInteractiveProfileSurvivesProjectSelection(t *testing.T) {
	tm := disposableTmux(t)
	dir := t.TempDir()
	counter := filepath.Join(dir, "counter")
	picker := filepath.Join(dir, "rofi")
	script := "#!/bin/sh\ncat >/dev/null\nif test -e \"$PICK_COUNTER\"; then echo 0; else touch \"$PICK_COUNTER\"; echo 1; fi\n"
	os.WriteFile(picker, []byte(script), 0700)
	t.Setenv("PICK_COUNTER", counter)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	c := defaults()
	c.Profiles = map[string]Profile{"first": {Command: []string{"sleep", "60"}}, "second": {Command: []string{"sleep", "60"}}}
	c.Search = Search{Roots: []string{dir}, Mode: "all"}
	s, err := chooseSession(context.Background(), c, tm, "", testLauncher)
	if err != nil || s.Profile != "second" || s.Project != dir {
		t.Fatalf("selected profile changed: %v %v", s, err)
	}
}
