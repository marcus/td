// Package serve provides port file lifecycle management and web session
// bootstrap for the td serve HTTP server.
package serve

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	portFileName     = "serve-port"
	portLockFileName = "serve-port.lock"
	instancePrefix   = "srv_"
	healthTimeout    = 2 * time.Second
)

// PortInfo contains the metadata written to the port file when the server starts.
type PortInfo struct {
	Port       int       `json:"port"`
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"started_at"`
	InstanceID string    `json:"instance_id"`
}

// GenerateInstanceID creates a new random instance ID with the srv_ prefix
// and 6 random hex characters (e.g. "srv_8f3b2c").
func GenerateInstanceID() (string, error) {
	b := make([]byte, 3) // 3 bytes = 6 hex chars
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate instance id: %w", err)
	}
	return instancePrefix + hex.EncodeToString(b), nil
}

// portFilePath returns the full path to the port file inside baseDir/.todos.
func portFilePath(baseDir string) string {
	return filepath.Join(baseDir, ".todos", portFileName)
}

// portLockFilePath returns the full path to the port file lock.
func portLockFilePath(baseDir string) string {
	return filepath.Join(baseDir, ".todos", portLockFileName)
}

// WritePortFile writes the port file to baseDir/.todos/serve-port after
// acquiring an exclusive lock on the lock file. The lock is released after
// writing. This ensures only one server instance can register itself at a time.
func WritePortFile(baseDir string, info *PortInfo) error {
	lockPath := portLockFilePath(baseDir)
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open port lock file: %w", err)
	}
	defer func() { _ = lockFile.Close() }()

	// Acquire exclusive lock (blocking with a timeout via retry)
	if err := acquireFileLockTimeout(lockFile, 5*time.Second); err != nil {
		return fmt.Errorf("acquire port lock: %w", err)
	}
	defer releaseFileLock(lockFile)

	// Re-check under lock to avoid a startup race between parallel processes.
	if existing, err := ReadPortFile(baseDir); err == nil {
		if !IsPortFileStale(existing) {
			return fmt.Errorf("td serve already running on port %d (pid %d)", existing.Port, existing.PID)
		}
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal port info: %w", err)
	}

	pfPath := portFilePath(baseDir)
	if err := os.WriteFile(pfPath, data, 0644); err != nil {
		return fmt.Errorf("write port file: %w", err)
	}

	return nil
}

// ReadPortFile reads and parses the port file from baseDir/.todos/serve-port.
// Returns an error if the file doesn't exist or is missing required fields.
func ReadPortFile(baseDir string) (*PortInfo, error) {
	pfPath := portFilePath(baseDir)
	data, err := os.ReadFile(pfPath)
	if err != nil {
		return nil, fmt.Errorf("read port file: %w", err)
	}

	var info PortInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("parse port file: %w", err)
	}

	// Validate required fields
	if info.Port == 0 {
		return nil, fmt.Errorf("port file missing required field: port")
	}
	if info.PID == 0 {
		return nil, fmt.Errorf("port file missing required field: pid")
	}
	if info.InstanceID == "" {
		return nil, fmt.Errorf("port file missing required field: instance_id")
	}

	return &info, nil
}

// DeletePortFile removes the port file. Called on server shutdown.
// Errors are returned but callers may choose to ignore them during cleanup.
func DeletePortFile(baseDir string) error {
	pfPath := portFilePath(baseDir)
	if err := os.Remove(pfPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove port file: %w", err)
	}
	return nil
}

// IsServerHealthy checks if a server at the given port is alive by sending
// an HTTP GET to localhost:{port}/health. Returns true only if a 200 response
// is received within the health timeout.
func IsServerHealthy(port int) bool {
	client := &http.Client{Timeout: healthTimeout}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/health", port))
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}

// IsPortFileStale checks whether the port file describes a server that is no
// longer running. A port file is stale if:
//   - The PID is not alive, OR
//   - The PID is alive but the health endpoint doesn't respond
func IsPortFileStale(info *PortInfo) bool {
	if !isProcessAlive(info.PID) {
		return true
	}
	return !IsServerHealthy(info.Port)
}
