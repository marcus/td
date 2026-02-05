// Package e2e provides a Go test harness for end-to-end sync testing.
// It ports the bash harness (scripts/e2e/harness.sh) to Go, building real
// td and td-sync binaries, running a server, and authenticating multiple actors.
package e2e

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Config controls harness setup options.
type Config struct {
	NumActors int  // 2 or 3 (alice, bob, optionally carol)
	AutoSync  bool // enable auto-sync on clients
	Debounce  string
	Interval  string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		NumActors: 2,
		AutoSync:  false,
		Debounce:  "2s",
		Interval:  "10s",
	}
}

// Harness manages a td-sync server and multiple td client environments.
type Harness struct {
	ServerURL string
	ProjectID string
	WorkDir   string

	TdBin   string
	SyncBin string

	clientDirs map[string]string // actor -> working dir
	homeDirs   map[string]string // actor -> HOME dir
	sessionIDs map[string]string // actor -> TD_SESSION_ID

	serverCmd          *exec.Cmd
	serverLog          string   // path to server log file
	serverPort         int      // port the server listens on
	serverData         string   // path to server-data directory
	serverEnvOverrides []string // extra env vars for server (set before Setup)
	config             Config
	t                  *testing.T // nil when used standalone
}

// SetServerEnv adds environment variables for the server process.
// Can be called before Setup() or between StopServer()/StartServer() calls.
// Later values override earlier ones (appended after defaults).
func (h *Harness) SetServerEnv(envs ...string) {
	h.serverEnvOverrides = append(h.serverEnvOverrides, envs...)
}

// actorNames returns the actor names for the configured number of actors.
func actorNames(n int) []string {
	names := []string{"alice", "bob"}
	if n >= 3 {
		names = append(names, "carol")
	}
	return names
}

// Setup creates a new Harness: builds binaries, starts the server,
// authenticates actors, creates a project, and links all actors.
// When t is non-nil, t.Cleanup is used for teardown.
func Setup(t *testing.T, cfg Config) *Harness {
	t.Helper()

	if cfg.NumActors < 2 {
		cfg.NumActors = 2
	}
	if cfg.Debounce == "" {
		cfg.Debounce = "2s"
	}
	if cfg.Interval == "" {
		cfg.Interval = "10s"
	}

	h := &Harness{
		clientDirs: make(map[string]string),
		homeDirs:   make(map[string]string),
		sessionIDs: make(map[string]string),
		config:     cfg,
		t:          t,
	}

	// Create temp dir
	workDir, err := os.MkdirTemp("", "td-e2e-")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	h.WorkDir = workDir

	if t != nil {
		t.Cleanup(func() { h.Teardown() })
	}

	// Create directory structure
	serverData := filepath.Join(workDir, "server-data")
	if err := os.MkdirAll(serverData, 0755); err != nil {
		t.Fatalf("mkdir server-data: %v", err)
	}

	actors := actorNames(cfg.NumActors)
	for _, actor := range actors {
		clientDir := filepath.Join(workDir, "client-"+actor)
		homeDir := filepath.Join(workDir, "home-"+actor)
		if err := os.MkdirAll(clientDir, 0755); err != nil {
			t.Fatalf("mkdir client-%s: %v", actor, err)
		}
		if err := os.MkdirAll(filepath.Join(homeDir, ".config", "td"), 0755); err != nil {
			t.Fatalf("mkdir home-%s: %v", actor, err)
		}
		h.clientDirs[actor] = clientDir
		h.homeDirs[actor] = homeDir
		h.sessionIDs[actor] = fmt.Sprintf("e2e-%s-%d", actor, os.Getpid())
	}

	// Locate repo root
	repoDir := findRepoRoot()

	// Build binaries
	h.TdBin = filepath.Join(workDir, "td")
	h.SyncBin = filepath.Join(workDir, "td-sync")

	t.Log("building td binary")
	if out, err := runCmd(repoDir, "go", "build", "-o", h.TdBin, "."); err != nil {
		t.Fatalf("build td: %v\n%s", err, out)
	}
	t.Log("building td-sync binary")
	if out, err := runCmd(repoDir, "go", "build", "-o", h.SyncBin, "./cmd/td-sync"); err != nil {
		t.Fatalf("build td-sync: %v\n%s", err, out)
	}

	// Pick random port
	port, err := randomPort()
	if err != nil {
		t.Fatalf("random port: %v", err)
	}
	h.ServerURL = fmt.Sprintf("http://localhost:%d", port)
	h.serverPort = port
	h.serverData = serverData

	// Start server
	h.serverLog = filepath.Join(workDir, "server.log")
	logFile, err := os.Create(h.serverLog)
	if err != nil {
		t.Fatalf("create server log: %v", err)
	}

	h.serverCmd = exec.Command(h.SyncBin)
	h.serverCmd.Env = append(os.Environ(),
		fmt.Sprintf("SYNC_LISTEN_ADDR=:%d", port),
		fmt.Sprintf("SYNC_SERVER_DB_PATH=%s/server.db", serverData),
		fmt.Sprintf("SYNC_PROJECT_DATA_DIR=%s/projects", serverData),
		"SYNC_ALLOW_SIGNUP=true",
		fmt.Sprintf("SYNC_BASE_URL=%s", h.ServerURL),
		"SYNC_LOG_FORMAT=text",
		"SYNC_LOG_LEVEL=info",
		"SYNC_RATE_LIMIT_AUTH=1000",
		"SYNC_RATE_LIMIT_PUSH=10000",
		"SYNC_RATE_LIMIT_PULL=10000",
		"SYNC_RATE_LIMIT_OTHER=10000",
	)
	h.serverCmd.Env = append(h.serverCmd.Env, h.serverEnvOverrides...)
	h.serverCmd.Stdout = logFile
	h.serverCmd.Stderr = logFile

	if err := h.serverCmd.Start(); err != nil {
		logFile.Close()
		t.Fatalf("start server: %v", err)
	}
	logFile.Close()

	// Wait for server health
	if err := h.waitForHealth(30 * time.Second); err != nil {
		serverLog, _ := os.ReadFile(h.serverLog)
		t.Fatalf("server not healthy: %v\nServer log:\n%s", err, serverLog)
	}
	t.Logf("server ready on port %d", port)

	// Init + auth + link
	for _, actor := range actors {
		// td init (echo "n" to skip sync prompt)
		if out, err := h.Td(actor, "init"); err != nil {
			t.Fatalf("init %s: %v\n%s", actor, err, out)
		}
	}

	for _, actor := range actors {
		email := actor + "@test.local"
		if err := h.authenticate(actor, email); err != nil {
			t.Fatalf("auth %s: %v", actor, err)
		}
	}

	// Alice creates project
	out, err := h.Td("alice", "sync-project", "create", "e2e-test")
	if err != nil {
		t.Fatalf("create project: %v\n%s", err, out)
	}
	h.ProjectID = extractProjectID(out)
	if h.ProjectID == "" {
		t.Fatalf("no project ID from: %s", out)
	}

	// Alice links and syncs
	if out, err := h.Td("alice", "sync-project", "link", h.ProjectID); err != nil {
		t.Fatalf("link alice: %v\n%s", err, out)
	}
	if out, err := h.Td("alice", "sync"); err != nil {
		t.Fatalf("sync alice: %v\n%s", err, out)
	}

	// Invite and link others
	for _, actor := range actors[1:] {
		email := actor + "@test.local"
		if out, err := h.Td("alice", "sync-project", "invite", email, "writer"); err != nil {
			t.Fatalf("invite %s: %v\n%s", actor, err, out)
		}
		if out, err := h.Td(actor, "sync-project", "link", h.ProjectID); err != nil {
			t.Fatalf("link %s: %v\n%s", actor, err, out)
		}
	}

	t.Logf("ready: project=%s actors=%v", h.ProjectID, actors)
	return h
}

// Teardown kills the server and cleans up temp dirs.
func (h *Harness) Teardown() {
	if h.serverCmd != nil && h.serverCmd.Process != nil {
		h.serverCmd.Process.Kill()
		h.serverCmd.Wait()
	}
	if h.WorkDir != "" {
		os.RemoveAll(h.WorkDir)
	}
}

// Td runs the td binary as the given actor and returns combined output.
// For "init" commands, it pipes "n" to stdin to skip the sync prompt.
func (h *Harness) Td(actor string, args ...string) (string, error) {
	clientDir, ok := h.clientDirs[actor]
	if !ok {
		return "", fmt.Errorf("unknown actor: %s", actor)
	}
	homeDir := h.homeDirs[actor]
	sessionID := h.sessionIDs[actor]

	cmd := exec.Command(h.TdBin, args...)
	cmd.Dir = clientDir
	cmd.Env = append(os.Environ(),
		"HOME="+homeDir,
		"TD_SESSION_ID="+sessionID,
		"TD_ENABLE_FEATURE=sync_cli,sync_autosync,sync_monitor_prompt",
	)

	// For init, pipe "n" to skip sync prompt
	if len(args) > 0 && args[0] == "init" {
		cmd.Stdin = strings.NewReader("n\n")
	}

	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TdA runs td as alice.
func (h *Harness) TdA(args ...string) (string, error) {
	return h.Td("alice", args...)
}

// TdB runs td as bob.
func (h *Harness) TdB(args ...string) (string, error) {
	return h.Td("bob", args...)
}

// TdC runs td as carol.
func (h *Harness) TdC(args ...string) (string, error) {
	return h.Td("carol", args...)
}

// syncWithRetry runs td sync for an actor, retrying on rate-limit (429) errors.
func (h *Harness) syncWithRetry(actor string) (string, error) {
	backoff := 500 * time.Millisecond
	for attempt := range 5 {
		out, err := h.Td(actor, "sync")
		if err == nil {
			return out, nil
		}
		if !strings.Contains(out, "429") && !strings.Contains(strings.ToLower(out), "rate") {
			return out, err
		}
		if attempt == 4 {
			return out, err
		}
		time.Sleep(backoff)
		backoff *= 2
	}
	return "", fmt.Errorf("unreachable")
}

// SyncAll syncs all actors in round-robin for convergence.
// Performs 3 rounds of push+pull for each actor, with rate-limit retry.
func (h *Harness) SyncAll() error {
	actors := actorNames(h.config.NumActors)
	for round := range 3 {
		for _, actor := range actors {
			out, err := h.syncWithRetry(actor)
			if err != nil {
				return fmt.Errorf("sync %s round %d: %v\n%s", actor, round, err, out)
			}
		}
	}
	return nil
}

// DBPath returns the path to an actor's issues.db.
func (h *Harness) DBPath(actor string) string {
	clientDir, ok := h.clientDirs[actor]
	if !ok {
		return ""
	}
	return filepath.Join(clientDir, ".todos", "issues.db")
}

// ClientDir returns the working directory for an actor.
func (h *Harness) ClientDir(actor string) string {
	return h.clientDirs[actor]
}

// HomeDir returns the HOME directory for an actor.
func (h *Harness) HomeDir(actor string) string {
	return h.homeDirs[actor]
}

// ServerLogContents returns the server log file contents.
func (h *Harness) ServerLogContents() string {
	data, _ := os.ReadFile(h.serverLog)
	return string(data)
}

// StopServer kills the running server process and waits for it to exit.
func (h *Harness) StopServer() error {
	if h.serverCmd == nil || h.serverCmd.Process == nil {
		return fmt.Errorf("server not running")
	}
	if err := h.serverCmd.Process.Kill(); err != nil {
		return fmt.Errorf("kill server: %w", err)
	}
	// Wait for process to fully exit (ignore error since Kill causes non-zero exit)
	h.serverCmd.Wait()
	h.serverCmd = nil
	return nil
}

// StartServer starts a new server process using the same data directory and port.
// Blocks until the server passes a health check.
func (h *Harness) StartServer() error {
	logFile, err := os.OpenFile(h.serverLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open server log: %w", err)
	}

	h.serverCmd = exec.Command(h.SyncBin)
	h.serverCmd.Env = append(os.Environ(),
		fmt.Sprintf("SYNC_LISTEN_ADDR=:%d", h.serverPort),
		fmt.Sprintf("SYNC_SERVER_DB_PATH=%s/server.db", h.serverData),
		fmt.Sprintf("SYNC_PROJECT_DATA_DIR=%s/projects", h.serverData),
		"SYNC_ALLOW_SIGNUP=true",
		fmt.Sprintf("SYNC_BASE_URL=%s", h.ServerURL),
		"SYNC_LOG_FORMAT=text",
		"SYNC_LOG_LEVEL=info",
		"SYNC_RATE_LIMIT_AUTH=1000",
		"SYNC_RATE_LIMIT_PUSH=10000",
		"SYNC_RATE_LIMIT_PULL=10000",
		"SYNC_RATE_LIMIT_OTHER=10000",
	)
	h.serverCmd.Env = append(h.serverCmd.Env, h.serverEnvOverrides...)
	h.serverCmd.Stdout = logFile
	h.serverCmd.Stderr = logFile

	if err := h.serverCmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start server: %w", err)
	}
	logFile.Close()

	if err := h.waitForHealth(30 * time.Second); err != nil {
		return fmt.Errorf("server not healthy after restart: %w", err)
	}
	return nil
}

// --- internal helpers ---

func findRepoRoot() string {
	// Walk up from current dir looking for go.mod
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Fallback: try relative to this source file's expected location
			// test/e2e/ -> repo root is ../..
			return dir
		}
		dir = parent
	}
}

func randomPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

func runCmd(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (h *Harness) waitForHealth(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	healthURL := h.ServerURL + "/healthz"
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		// Check if server process died
		if h.serverCmd.ProcessState != nil {
			return fmt.Errorf("server process exited")
		}

		resp, err := client.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("health check timed out after %v", timeout)
}

// authenticate performs the device auth flow for an actor.
func (h *Harness) authenticate(actor, email string) error {
	homeDir := h.homeDirs[actor]
	client := &http.Client{Timeout: 10 * time.Second}

	// Step 1: POST /v1/auth/login/start
	startBody, _ := json.Marshal(map[string]string{"email": email})
	resp, err := client.Post(h.ServerURL+"/v1/auth/login/start", "application/json", bytes.NewReader(startBody))
	if err != nil {
		return fmt.Errorf("login/start: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("login/start status %d: %s", resp.StatusCode, body)
	}

	var startResp struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
	}
	if err := json.Unmarshal(body, &startResp); err != nil {
		return fmt.Errorf("parse login/start: %w", err)
	}

	// Step 2: POST /auth/verify
	verifyData := url.Values{"user_code": {startResp.UserCode}}
	resp2, err := client.PostForm(h.ServerURL+"/auth/verify", verifyData)
	if err != nil {
		return fmt.Errorf("auth/verify: %w", err)
	}
	resp2.Body.Close()

	// Step 3: POST /v1/auth/login/poll
	pollBody, _ := json.Marshal(map[string]string{"device_code": startResp.DeviceCode})
	resp3, err := client.Post(h.ServerURL+"/v1/auth/login/poll", "application/json", bytes.NewReader(pollBody))
	if err != nil {
		return fmt.Errorf("login/poll: %w", err)
	}
	defer resp3.Body.Close()
	body3, _ := io.ReadAll(resp3.Body)
	if resp3.StatusCode != 200 {
		return fmt.Errorf("login/poll status %d: %s", resp3.StatusCode, body3)
	}

	var pollResp struct {
		ApiKey string `json:"api_key"`
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(body3, &pollResp); err != nil {
		return fmt.Errorf("parse login/poll: %w", err)
	}

	// Generate device ID
	deviceIDBytes := make([]byte, 16)
	rand.Read(deviceIDBytes)
	deviceID := hex.EncodeToString(deviceIDBytes)

	// Write auth.json
	authJSON, _ := json.Marshal(map[string]string{
		"api_key":    pollResp.ApiKey,
		"user_id":    pollResp.UserID,
		"email":      email,
		"server_url": h.ServerURL,
		"device_id":  deviceID,
	})
	authPath := filepath.Join(homeDir, ".config", "td", "auth.json")
	if err := os.WriteFile(authPath, authJSON, 0600); err != nil {
		return fmt.Errorf("write auth.json: %w", err)
	}

	// Write config.json
	configData := map[string]any{
		"sync": map[string]any{
			"url":                h.ServerURL,
			"enabled":            true,
			"snapshot_threshold": 0,
			"auto": map[string]any{
				"enabled":  h.config.AutoSync,
				"on_start": false,
				"debounce": h.config.Debounce,
				"interval": h.config.Interval,
				"pull":     true,
			},
		},
	}
	configJSON, _ := json.Marshal(configData)
	configPath := filepath.Join(homeDir, ".config", "td", "config.json")
	if err := os.WriteFile(configPath, configJSON, 0644); err != nil {
		return fmt.Errorf("write config.json: %w", err)
	}

	return nil
}

// extractProjectID finds p_<hex> in the output string.
func extractProjectID(output string) string {
	// Split on any whitespace or parentheses
	for word := range strings.FieldsFuncSeq(output, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '(' || r == ')' || r == ','
	}) {
		if strings.HasPrefix(word, "p_") {
			return strings.TrimRight(word, ".,;:!?)")
		}
	}
	return ""
}
