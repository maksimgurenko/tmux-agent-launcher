package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func testPeer(t *testing.T, h http.Handler) (Peer, *httptest.Server) {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	addr := s.Listener.Addr().(*net.TCPAddr)
	p := Peer{Name: "Build machine", Host: addr.IP.String(), SearchPort: addr.Port,
		SearchTokenEnv: "FIXTURE_FOLDER_TOKEN", Endpoint: "wss://build.example:9443", CodexTokenEnv: "FIXTURE_CODEX_TOKEN"}
	t.Setenv(p.SearchTokenEnv, "fixture-search")
	return p, s
}

func TestRemoteFolderAuthenticationAndParity(t *testing.T) {
	home := t.TempDir()
	for _, dir := range []string{"Project one/nested", "Project Two", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("Project one", filepath.Join(home, "Project alias")); err != nil {
		t.Fatal(err)
	}
	p, server := testPeer(t, folderHandler(home, "fixture-search"))
	for _, token := range []string{"", "Bearer incorrect"} {
		for _, operation := range []string{"suggest", "resolve"} {
			req, err := http.NewRequest(http.MethodGet, server.URL+folderRoute+operation+"?input=Project+one", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", token)
			resp, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != http.StatusUnauthorized || string(body) != "authentication required\n" {
				t.Fatalf("unauthorized %s: %d %q %v", operation, resp.StatusCode, body, err)
			}
		}
	}
	for _, input := range []string{"", "~", "~/", "pROJ", "~/.h", "~/Project one/", home + "/Pro", "no-match"} {
		got, err := p.folders(t.Context(), "suggest", input)
		want := directorySuggestions(home, input)
		if err != nil || !slices.Equal(got.Rows, want) {
			t.Errorf("%q: got %q, want %q (%v)", input, got.Rows, want, err)
		}
	}
	got, err := p.folders(t.Context(), "resolve", "~/Project alias")
	if err != nil || got.Path != filepath.Join(home, "Project one") {
		t.Fatalf("canonical path: %q %v", got.Path, err)
	}
	if _, err := p.folders(t.Context(), "resolve", "~/removed"); err == nil {
		t.Fatal("accepted missing directory")
	}
	t.Setenv(p.SearchTokenEnv, "")
	if _, err := p.folders(t.Context(), "suggest", ""); err == nil {
		t.Fatal("missing token accepted")
	}
	if err := serveFolders(t.Context(), &FolderService{Listen: p.Host, Port: p.SearchPort, TokenEnv: p.SearchTokenEnv}); err == nil || !strings.Contains(err.Error(), "unset or empty") {
		t.Fatal("service needs a token before listening", err)
	}
}

func TestRemoteConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := `[folder_service]
listen="::1"
port=9330
token_env="INBOUND"
[[peers]]
name="Build machine"
host="build.example"
search_port=9330
search_token_env="SEARCH"
codex_endpoint="wss://build.example:9443"
codex_token_env="CODEX_TOKEN"
profile="custom"
[profiles.custom]
command=["codex"]
hide_local_action=true
`
	if err := os.WriteFile(path, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	if c.FolderService.Listen != "::1" || len(c.Peers) != 1 || c.Peers[0].profileID() != "custom" || !c.Profiles["custom"].HideLocalAction {
		t.Fatal(c)
	}
	orig := c.Peers[0]
	for _, mutate := range []func(*Peer){
		func(p *Peer) { p.Host = "host/elsewhere" }, func(p *Peer) { p.SearchPort = -1 },
		func(p *Peer) { p.CodexTokenEnv = "a=b" }, func(p *Peer) { p.SearchTokenEnv = "" },
		func(p *Peer) { p.Endpoint = "wss://host" }, func(p *Peer) { p.Endpoint = "http://host:80" }, func(p *Peer) { p.Endpoint = "ws://user@host:80" },
	} {
		p := orig
		mutate(&p)
		c.Peers = []Peer{p}
		if c.validate() == nil {
			t.Fatalf("accepted %#v", p)
		}
	}
}

func TestRemotePickerLaunchReuseOfflineAttachAndCancellation(t *testing.T) {
	tm := disposableTmux(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	// The server's canonical path deliberately does not exist on the client.
	remotePath := filepath.Join(dir, "remote only #:/project")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Header.Get("Authorization") != "Bearer fixture-search":
			http.Error(w, "authentication required", 401)
		case r.URL.Path == folderRoute+"suggest" && r.URL.Query().Get("input") == "~/PRO":
			json.NewEncoder(w).Encode(folderResult{Rows: []string{"~/Project"}})
		case r.URL.Path == folderRoute+"resolve" && r.URL.Query().Get("input") == "~/Project":
			json.NewEncoder(w).Encode(folderResult{Path: remotePath})
		default:
			http.NotFound(w, r)
		}
	})
	peer, server := testPeer(t, handler)
	second := peer
	second.Host = "localhost"
	c := defaults()
	c.Peers = []Peer{peer, second}
	c.Search = Search{Mode: "all", Roots: []string{home}}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	capture := filepath.Join(dir, "codex-fixture")
	proof := filepath.Join(dir, "argv")
	t.Setenv("FIXTURE_ARGV", proof)
	t.Setenv(peer.CodexTokenEnv, "fixture-codex")
	if err := os.WriteFile(capture, []byte("#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$FIXTURE_CODEX_TOKEN\" \"$@\" > \"$FIXTURE_ARGV.tmp\"\nmv \"$FIXTURE_ARGV.tmp\" \"$FIXTURE_ARGV\"\nexec sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	c.Profiles = map[string]Profile{"codex": {Label: "Codex", Command: []string{capture, "--yolo", "-m", "model with spaces"}}}
	picker := `#!/bin/sh
set -eu
case "$*" in
  *'Agent session'*)
    i=0
    while IFS= read -r label; do
      if [ "$label" = "$FIXTURE_LABEL" ]; then echo "$i"; exit; fi
      i=$((i+1))
    done
    exit 2;;
  *'__rofi-remote-path'*)
    if [ "$FIXTURE_PICK" = cancel ]; then exit 1; fi
    ROFI_RETV=10 ROFI_INPUT='~/PRO' ./launcher __rofi-remote-path > rows
    if [ "$FIXTURE_PICK" = offline ]; then
      tr '\000\037' '  ' <rows | grep -F '127.0.0.1: folder service unreachable' >/dev/null
      grep -aF Retry rows >/dev/null
      exit 1
    fi
    grep -Fx '~/Project' rows >/dev/null
    ROFI_RETV=1 ./launcher __rofi-remote-path '~/Project';;
  *) cat >/dev/null; echo 0;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "rofi"), []byte(picker), 0700); err != nil {
		t.Fatal(err)
	}
	choose := func(label string) (Session, error) {
		t.Setenv("FIXTURE_LABEL", label)
		return chooseSession(t.Context(), c, tm, "codex", testLauncher)
	}
	newLabel := func(p Peer) string { return fmt.Sprintf("New Codex [%s] %s", p.Host, p.Name) }
	t.Setenv("FIXTURE_PICK", "cancel")
	if _, err := choose(newLabel(peer)); !errors.Is(err, errCancelled) {
		t.Fatalf("cancel: %v", err)
	}
	if sessions, err := tm.sessions(t.Context()); err != nil || len(sessions) != 0 {
		t.Fatalf("cancel created session: %v %v", sessions, err)
	}
	t.Setenv("FIXTURE_PICK", "select")
	// A desktop hotkey need not inherit either token from the shell.
	for i := range c.Peers {
		p := &c.Peers[i]
		p.SearchTokenFile = filepath.Join(dir, "folder-token")
		p.CodexTokenFile = filepath.Join(dir, "codex-token")
		for file, value := range map[string]string{p.SearchTokenFile: "fixture-search", p.CodexTokenFile: "fixture-codex"} {
			if err := os.WriteFile(file, []byte(value+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv(peer.SearchTokenEnv, "")
	t.Setenv(peer.CodexTokenEnv, "")
	first, err := choose(newLabel(peer))
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{home, "fixture-codex", "--yolo", "-m", "model with spaces", "--remote", peer.Endpoint, "--remote-auth-token-env", peer.CodexTokenEnv, "-C", remotePath}
	if got := strings.Split(strings.TrimSuffix(waitProof(t, proof), "\n"), "\n"); !slices.Equal(got, wantArgs) {
		t.Fatalf("runner got %q, want %q", got, wantArgs)
	}
	again, err := choose(newLabel(peer))
	if err != nil || again.ID != first.ID {
		t.Fatalf("reuse: %v %v", again, err)
	}
	other, err := choose(newLabel(second))
	if err != nil || other.ID == first.ID || other.Host != second.Host || other.Project != first.Project {
		t.Fatalf("host identity: %v %v", other, err)
	}
	server.Close()
	t.Setenv("FIXTURE_PICK", "offline")
	if _, err := choose(newLabel(peer)); !errors.Is(err, errCancelled) {
		t.Fatalf("offline picker: %v", err)
	}
	local, err := choose("New Codex [local]")
	if err != nil || local.Host != "" || local.Project != home {
		t.Fatalf("local launch: %v %v", local, err)
	}
	c.Peers = nil
	delete(c.Profiles, "codex")
	t.Setenv(peer.CodexTokenEnv, "")
	attached, err := choose("Attach [codex] [" + peer.Host + "] " + remotePath)
	if err != nil || attached.ID != first.ID {
		t.Fatalf("attach: %v %v", attached, err)
	}
}

func TestShortRemoteSessionNameAndCollisions(t *testing.T) {
	tm := disposableTmux(t)
	ctx := t.Context()
	dir := t.TempDir()
	project := filepath.Join(dir, "literal #S")
	// A local profile with the hostname and a project under /codex would
	// produce exactly the same readable name as this remote session.
	host := "build"
	peer := Peer{Host: host, CodexTokenEnv: "FIXTURE_CODEX_TOKEN"}
	t.Setenv(peer.CodexTokenEnv, "fixture")
	profile := Profile{Command: []string{"/bin/sh", "-c", "exec sleep 60", "fixture"}}
	c := defaults()
	for _, id := range []string{"codex-peer", "codex", host} {
		c.Profiles[id] = profile
	}
	first, err := tm.ensureProject(ctx, c, "codex-peer", project, testLauncher, &peer)
	if err != nil {
		t.Fatal(err)
	}
	want := "/build/codex" + project + "/"
	ss, err := tm.sessions(ctx)
	if err != nil || len(ss) != 1 || ss[0].Name != want || ss[0].Profile != "codex-peer" {
		t.Fatalf("short name or identity: %v %v", ss, err)
	}
	other, err := tm.ensureProject(ctx, c, "codex", project, testLauncher, &peer)
	if err != nil || other.ID == first.ID || other.Name == first.Name {
		t.Fatalf("profile collision: %v %v", other, err)
	}
	again, err := tm.ensureProject(ctx, c, "codex-peer", project, testLauncher, &peer)
	if err != nil || again.ID != first.ID {
		t.Fatalf("reattach: %v %v", again, err)
	}
	// Put the remote session at a name that corresponds to a real local directory.
	if _, err := tm.output(ctx, "rename-session", "-t", first.ID, sessionName(host, dir)); err != nil {
		t.Fatal(err)
	}
	local, err := tm.ensure(ctx, c, host, dir, testLauncher)
	if err != nil || local.ID == first.ID || local.Name == sessionName(host, dir) {
		t.Fatalf("local collision: %v %v", local, err)
	}
}

func TestDedicatedPeerProfileMenu(t *testing.T) {
	tm := disposableTmux(t)
	dir := t.TempDir()
	labels := filepath.Join(dir, "labels")
	t.Setenv("CAPTURE_MENU", labels)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "rofi"), []byte("#!/bin/sh\ncat > \"$CAPTURE_MENU\"\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	c := defaults()
	c.Profiles = map[string]Profile{
		"codex":  {Label: "Codex", Command: []string{"/bin/true"}},
		"native": {Label: "Native", Command: []string{"/bin/true"}, HideLocalAction: true},
	}
	peer := Peer{Name: "build", Host: "build", Profile: "native"}
	c.Peers = []Peer{peer}
	if _, err := chooseSession(t.Context(), c, tm, "", testLauncher); !errors.Is(err, errCancelled) {
		t.Fatal(err)
	}
	got, err := os.ReadFile(labels)
	if err != nil || string(got) != "New Codex [local]\nNew Codex [build]\n" {
		t.Fatalf("menu: %q, %v", got, err)
	}
	c.Profiles["alternate"] = c.Profiles["codex"]
	if _, err := chooseSession(t.Context(), c, tm, "", testLauncher); !errors.Is(err, errCancelled) {
		t.Fatal(err)
	}
	got, err = os.ReadFile(labels)
	if err != nil || string(got) != "New Codex [local] [alternate]\nNew Codex [local] [codex]\nNew Codex [build]\n" {
		t.Fatalf("menu with identically named profiles: %q, %v", got, err)
	}
	c.Peers = append(c.Peers, Peer{Name: "build", Host: "build", Profile: "codex"})
	for _, p := range c.Peers {
		if got := p.menuLabel(c.Peers); got != "New Codex [build] ["+p.Profile+"]" {
			t.Fatalf("ambiguous profile label: %q", got)
		}
	}
}
