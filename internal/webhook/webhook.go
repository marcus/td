package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/marcus/td/internal/models"
)

// Payload is the top-level webhook POST body.
type Payload struct {
	ProjectDir string          `json:"project_dir"`
	Timestamp  string          `json:"timestamp"`
	Actions    []ActionPayload `json:"actions"`
}

// ActionPayload is one action_log entry within a webhook payload.
type ActionPayload struct {
	ID           string `json:"id"`
	SessionID    string `json:"session_id"`
	ActionType   string `json:"action_type"`
	EntityType   string `json:"entity_type"`
	EntityID     string `json:"entity_id"`
	PreviousData string `json:"previous_data"`
	NewData      string `json:"new_data"`
	Timestamp    string `json:"timestamp"`
}

// TempFile is the self-contained JSON blob written to disk for the child process.
type TempFile struct {
	URL     string  `json:"url"`
	Secret  string  `json:"secret,omitempty"`
	Payload Payload `json:"payload"`
}

// BuildPayload converts action_log rows into a webhook payload.
func BuildPayload(projectDir string, actions []models.ActionLog) Payload {
	p := Payload{
		ProjectDir: projectDir,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Actions:    make([]ActionPayload, len(actions)),
	}
	for i, a := range actions {
		p.Actions[i] = ActionPayload{
			ID:           a.ID,
			SessionID:    a.SessionID,
			ActionType:   string(a.ActionType),
			EntityType:   a.EntityType,
			EntityID:     a.EntityID,
			PreviousData: a.PreviousData,
			NewData:      a.NewData,
			Timestamp:    a.Timestamp.UTC().Format(time.RFC3339),
		}
	}
	return p
}

// WriteTempFile writes a TempFile to os.TempDir and returns the path.
func WriteTempFile(tf *TempFile) (string, error) {
	data, err := json.Marshal(tf)
	if err != nil {
		return "", fmt.Errorf("marshal temp file: %w", err)
	}
	f, err := os.CreateTemp("", "td-webhook-*.json")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	path := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return "", fmt.Errorf("write temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("close temp file: %w", err)
	}
	return path, nil
}

// ReadTempFile reads and parses a TempFile from disk.
func ReadTempFile(path string) (*TempFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read temp file: %w", err)
	}
	var tf TempFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("parse temp file: %w", err)
	}
	return &tf, nil
}

// Dispatch performs a synchronous HTTP POST to the webhook URL.
// Returns nil on success (2xx status).
func Dispatch(url, secret string, payload Payload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "td-webhook/1")

	unixTS := fmt.Sprintf("%d", time.Now().Unix())
	req.Header.Set("X-TD-Timestamp", unixTS)

	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(unixTS))
		mac.Write([]byte("."))
		mac.Write(body)
		sig := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-TD-Signature", "sha256="+sig)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s: status %d", url, resp.StatusCode)
	}
	return nil
}
