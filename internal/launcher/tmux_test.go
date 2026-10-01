package launcher

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
	b, err := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-o", testLauncher, "../../cmd/tmux-agent-launcher").CombinedOutput()
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

func waitProof(t *testing.T, path string) string {
	t.Helper()
	for i := 0; i < 200; i++ {
		if b, err := os.ReadFile(path); err == nil {
			return string(b)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("agent did not write proof")
	return ""
}

func TestTmuxLaunchSnapshotDoesNotRestoreServerEnvironment(t *testing.T) {
	tm := disposableTmux(t)
	ctx := context.Background()
	if _, err := tm.output(ctx, "new-session", "-d", "-s", "fixture", "/bin/sleep", "60"); err != nil {
		t.Fatal(err)
	}
	for _, entry := range [][2]string{{"LAUNCHER_REMOVED_FIXTURE", "stale"}, {"LAUNCHER_CURRENT_FIXTURE", "server"}} {
		if _, err := tm.output(ctx, "set-environment", "-g", entry[0], entry[1]); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("LAUNCHER_REMOVED_FIXTURE", "")
	if err := os.Unsetenv("LAUNCHER_REMOVED_FIXTURE"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LAUNCHER_CURRENT_FIXTURE", "caller")
	t.Setenv("TERM", "")
	if err := os.Unsetenv("TERM"); err != nil {
		t.Fatal(err)
	}
	terminal, err := tm.output(ctx, "show-options", "-gv", "default-terminal")
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	c := defaults()
	c.Profiles["test"] = Profile{Command: []string{"/bin/sh", "-c", "printf '%s\\n' \"${LAUNCHER_REMOVED_FIXTURE+present}\" \"$LAUNCHER_CURRENT_FIXTURE\" \"$LAUNCHER_OVERRIDE_FIXTURE\" \"$TMUX\" \"$TMUX_PANE\" \"$TERM\" > proof.tmp; /bin/mv proof.tmp proof; exec /bin/sleep 60"}, Env: map[string]string{"LAUNCHER_OVERRIDE_FIXTURE": "profile"}}
	if _, err := tm.ensure(ctx, c, "test", project, testLauncher); err != nil {
		t.Fatal(err)
	}
	proof := strings.Split(strings.TrimSuffix(waitProof(t, filepath.Join(project, "proof")), "\n"), "\n")
	if len(proof) != 6 || proof[0] != "" || proof[1] != "caller" || proof[2] != "profile" || !strings.HasPrefix(proof[3], tm.Prefix[1]+",") || !strings.HasPrefix(proof[4], "%") || proof[5] != terminal {
		t.Fatalf("snapshot or tmux environment lost: %q", proof)
	}
}

func TestTmuxSocketIdentityBeforeServerCreation(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", alias)
	t.Setenv("TMUX", "")
	socket := filepath.Join(root, fmt.Sprintf("tmux-%d", os.Getuid()), "default")
	for _, connection := range []Tmux{
		{},
		{Prefix: []string{"-S", socket}},
		{Prefix: []string{"-L", "default", "-f", "fixture.conf"}},
		{Prefix: []string{"-S", filepath.Join(alias, fmt.Sprintf("tmux-%d", os.Getuid()), "default")}},
	} {
		if got, err := connection.socketIdentity(); err != nil || got != socket {
			t.Fatalf("equivalent socket identity %q: %q %v", connection.Prefix, got, err)
		}
	}
	t.Setenv("TMUX", filepath.Join(root, "inherited")+",0,0")
	if got, err := (Tmux{}).socketIdentity(); err != nil || got != filepath.Join(root, "inherited") {
		t.Fatalf("inherited socket identity: %q %v", got, err)
	}
	if got, err := (Tmux{Prefix: []string{"-L", "default"}}).socketIdentity(); err != nil || got != socket {
		t.Fatalf("socket label did not override TMUX: %q %v", got, err)
	}
	if got, err := (Tmux{Prefix: []string{"-S", socket, "-L", "other"}}).socketIdentity(); err != nil || got != socket {
		t.Fatalf("socket path did not override label/TMUX: %q %v", got, err)
	}
}

func TestTmuxEquivalentServerConnectionsConcurrentCreation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMUX_TMPDIR", root)
	t.Setenv("TMUX", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	socketDir := filepath.Join(root, fmt.Sprintf("tmux-%d", os.Getuid()))
	if err := os.Mkdir(socketDir, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(socketDir, alias); err != nil {
		t.Fatal(err)
	}
	tm := Tmux{Prefix: []string{"-f", "/dev/null"}}
	if err := tm.checkVersion(context.Background()); err != nil {
		if os.Getenv("REQUIRE_TMUX") == "1" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	t.Cleanup(func() { tm.output(context.Background(), "kill-server") })
	connections := []Tmux{tm,
		{Prefix: []string{"-S", filepath.Join(socketDir, "default"), "-f", "/dev/null"}},
		{Prefix: []string{"-L", "default", "-f", "/dev/null"}},
		{Prefix: []string{"-S", filepath.Join(alias, "default"), "-f", "/dev/null"}},
	}
	c := defaults()
	c.Profiles["test"] = Profile{Command: []string{"/bin/sleep", "60"}}
	for attempt := 0; attempt < 6; attempt++ {
		project := t.TempDir()
		start := make(chan struct{})
		type result struct {
			session Session
			err     error
		}
		results := make(chan result, len(connections)*2)
		for i := 0; i < cap(results); i++ {
			connection := connections[i%len(connections)]
			go func() {
				<-start
				s, err := connection.ensure(context.Background(), c, "test", project, testLauncher)
				results <- result{s, err}
			}()
		}
		close(start)
		id := ""
		failed := false
		for i := 0; i < cap(results); i++ {
			r := <-results
			if r.err != nil {
				t.Errorf("equivalent connection failed on attempt %d: %v", attempt, r.err)
				failed = true
				continue
			}
			if id != "" && r.session.ID != id {
				t.Errorf("equivalent connections created distinct sessions: %s %s", id, r.session.ID)
				failed = true
			}
			id = r.session.ID
		}
		if failed {
			t.FailNow()
		}
		// Exercise default selection through the inherited TMUX connection too.
		t.Setenv("TMUX", filepath.Join(alias, "default")+",0,0")
	}
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

func TestInteractiveProjectDependentExecutables(t *testing.T) {
	for _, fixture := range []struct {
		name, command, path, executable string
	}{
		{"relative command", "./agent", "/bin", "agent"},
		{"relative PATH", "agent", "bin", "bin/agent"},
		{"empty PATH entry", "agent", ":/nonexistent", "agent"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			tm := disposableTmux(t)
			tools, project := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(tools, "rofi"), []byte("#!/bin/sh\ncat >/dev/null\necho 0\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", tools+":"+os.Getenv("PATH"))
			path := filepath.Join(project, fixture.executable)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf fixture > proof.tmp\n/bin/mv proof.tmp proof\nexec /bin/sleep 60\n"), 0700); err != nil {
				t.Fatal(err)
			}
			c := defaults()
			c.Profiles = map[string]Profile{"local": {Command: []string{fixture.command}, Env: map[string]string{"PATH": fixture.path}}}
			c.Search = Search{Roots: []string{project}, Mode: "all"}
			s, err := chooseSession(context.Background(), c, tm, "", testLauncher)
			if err != nil || s.Profile != "local" || s.Project != project {
				t.Fatalf("project-dependent profile disappeared: %v %v", s, err)
			}
			if proof := waitProof(t, filepath.Join(project, "proof")); proof != "fixture" {
				t.Fatalf("project executable did not run: %q", proof)
			}
		})
	}
}
