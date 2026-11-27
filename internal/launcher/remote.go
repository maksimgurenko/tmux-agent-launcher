package launcher

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type FolderService struct {
	Listen   string `toml:"listen"`
	Port     int    `toml:"port"`
	TokenEnv string `toml:"token_env"`
}

type Peer struct {
	Name            string `toml:"name"`
	Host            string `toml:"host"`
	SearchPort      int    `toml:"search_port"`
	SearchTokenEnv  string `toml:"search_token_env"`
	SearchTokenFile string `toml:"search_token_file"`
	Endpoint        string `toml:"codex_endpoint"`
	CodexTokenEnv   string `toml:"codex_token_env"`
	CodexTokenFile  string `toml:"codex_token_file"`
	Profile         string `toml:"profile"`
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var dnsName = regexp.MustCompile(`^[a-zA-Z0-9_-]+([.][a-zA-Z0-9_-]+)*[.]?$`)

func validHost(host string) bool {
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Zone() == ""
	}
	return dnsName.MatchString(host)
}

func validPort(port int) bool { return port > 0 && port <= 65535 }

func (c Config) validateRemote() error {
	if s := c.FolderService; s != nil {
		if !validHost(s.Listen) || !validPort(s.Port) || !envName.MatchString(s.TokenEnv) {
			return fmt.Errorf("folder_service requires a listen host, port (1-65535) and token_env variable name")
		}
	}
	seen := map[[2]string]bool{}
	for _, p := range c.Peers {
		if strings.TrimSpace(p.Name) == "" || hasControl(p.Name) || !validHost(p.Host) || !validPort(p.SearchPort) {
			return fmt.Errorf("peer requires a display name, host and search_port (1-65535)")
		}
		identity := [2]string{p.Host, p.profileID()}
		if seen[identity] {
			return fmt.Errorf("duplicate peer host %q for profile %q", p.Host, p.profileID())
		}
		seen[identity] = true
		if !envName.MatchString(p.SearchTokenEnv) || !envName.MatchString(p.CodexTokenEnv) {
			return fmt.Errorf("peer %s requires search_token_env and codex_token_env variable names", p.Host)
		}
		u, err := url.Parse(p.Endpoint)
		if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || !validHost(u.Hostname()) || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("peer %s requires a ws:// or wss:// Codex endpoint with an explicit port", p.Host)
		}
		port, _ := strconv.Atoi(u.Port())
		if !validPort(port) {
			return fmt.Errorf("peer %s requires an explicit Codex endpoint port (1-65535)", p.Host)
		}
		if p.Profile != "" && !profileID.MatchString(p.Profile) {
			return fmt.Errorf("peer %s: invalid profile ID", p.Host)
		}
	}
	return nil
}

func (p Peer) profileID() string {
	if p.Profile == "" {
		return "codex"
	}
	return p.Profile
}

func (p Peer) menuLabel(peers []Peer) string {
	label := "New Codex [" + p.Host + "]"
	if p.Name != p.Host {
		label += " " + p.Name
	}
	for _, other := range peers {
		if other.Host == p.Host && other.Name == p.Name && other.profileID() != p.profileID() {
			return label + " [" + p.profileID() + "]"
		}
	}
	return label
}

type folderResult struct {
	Rows []string `json:"rows,omitempty"`
	Path string   `json:"path,omitempty"`
}

const folderRoute = "/v1/folders/"

// Both pickers resolve relative input on the machine that owns the directory.
func lookupFolder(home, operation, input string) (folderResult, error) {
	switch operation {
	case "suggest":
		return folderResult{Rows: directorySuggestions(home, input)}, nil
	case "resolve":
		if input != "" && !hasControl(input) {
			path, err := canonicalDir(homePath(home, input))
			return folderResult{Path: path}, err
		}
	}
	return folderResult{}, errors.New("directory unavailable")
}

func folderHandler(home, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		operation, ok := strings.CutPrefix(r.URL.Path, folderRoute)
		if !ok || (operation != "suggest" && operation != "resolve") {
			http.NotFound(w, r)
			return
		}
		result, err := lookupFolder(home, operation, r.URL.Query().Get("input"))
		if err != nil {
			http.Error(w, "directory unavailable", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
}

func serveFolders(ctx context.Context, s *FolderService) error {
	if s == nil {
		return fmt.Errorf("configure [folder_service] before starting the listener")
	}
	token := os.Getenv(s.TokenEnv)
	if token == "" {
		return fmt.Errorf("folder service authentication token %s is unset or empty", s.TokenEnv)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: net.JoinHostPort(s.Listen, strconv.Itoa(s.Port)), Handler: folderHandler(home, token),
		ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second,
	}
	stop := context.AfterFunc(ctx, func() { server.Close() })
	defer stop()
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return ctx.Err()
	}
	return err
}

// Direct connections keep peer bearer credentials on the configured network.
var folderClient = &http.Client{
	Transport: &http.Transport{}, Timeout: 4 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// An explicit file lets desktop launches read a peer credential on demand.
func peerToken(file, name, value string) (string, error) {
	if file != "" {
		if !filepath.IsAbs(file) && !strings.HasPrefix(file, "~/") {
			return "", fmt.Errorf("%s token file needs an absolute or home-relative path", name)
		}
		path, err := expandPath(file)
		if err != nil {
			return "", err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s token file: %w", name, err)
		}
		value = strings.TrimSpace(string(b))
	}
	if value == "" || hasControl(value) {
		return "", fmt.Errorf("authentication token %s is missing or invalid", name)
	}
	return value, nil
}

func (p Peer) folders(ctx context.Context, operation, input string) (folderResult, error) {
	var result folderResult
	token, err := peerToken(p.SearchTokenFile, p.SearchTokenEnv, os.Getenv(p.SearchTokenEnv))
	if err != nil {
		return result, err
	}
	u := url.URL{Scheme: "http", Host: net.JoinHostPort(p.Host, strconv.Itoa(p.SearchPort)), Path: folderRoute + operation,
		RawQuery: url.Values{"input": {input}}.Encode()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return result, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := folderClient.Do(req)
	if err != nil {
		return result, errors.New("folder service unreachable")
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return result, errors.New("folder service authentication failed")
	case http.StatusNotFound:
		return result, errors.New("directory unavailable")
	default:
		return result, fmt.Errorf("folder service returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&result); err != nil {
		return folderResult{}, errors.New("invalid folder service response")
	}
	for _, row := range result.Rows {
		if row == "" || hasControl(row) {
			return folderResult{}, errors.New("invalid directory suggestion")
		}
	}
	if operation == "resolve" && (!filepath.IsAbs(result.Path) || filepath.Clean(result.Path) != result.Path || hasControl(result.Path)) {
		return folderResult{}, errors.New("invalid resolved directory")
	}
	return result, nil
}
