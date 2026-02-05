package syncclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Sentinel errors for common HTTP error classes.
var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrNotFound     = errors.New("not found")
)

// Client is an HTTP client for the td-sync server.
type Client struct {
	BaseURL  string
	APIKey   string
	DeviceID string
	HTTP     *http.Client
}

// New creates a new sync client.
func New(baseURL, apiKey, deviceID string) *Client {
	return &Client{
		BaseURL:  baseURL,
		APIKey:   apiKey,
		DeviceID: deviceID,
		HTTP:     &http.Client{Timeout: 30 * time.Second},
	}
}

// --- Auth types (mirrors internal/api/auth.go, independently defined) ---

// LoginStartResponse is the response from POST /v1/auth/login/start.
type LoginStartResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// LoginPollResponse is the response from POST /v1/auth/login/poll.
type LoginPollResponse struct {
	Status    string  `json:"status"`
	APIKey    *string `json:"api_key,omitempty"`
	UserID    *string `json:"user_id,omitempty"`
	Email     *string `json:"email,omitempty"`
	ExpiresAt *string `json:"expires_at,omitempty"`
}

// --- Project types ---

// ProjectResponse represents a project from the server.
type ProjectResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	DeletedAt   *string `json:"deleted_at,omitempty"`
}

// --- Sync types (mirrors internal/api/sync.go, independently defined) ---

// PushRequest is the body for POST /v1/projects/{id}/sync/push.
type PushRequest struct {
	DeviceID  string       `json:"device_id"`
	SessionID string       `json:"session_id"`
	Events    []EventInput `json:"events"`
}

// EventInput is a single event in a push request.
type EventInput struct {
	ClientActionID  int64           `json:"client_action_id"`
	ActionType      string          `json:"action_type"`
	EntityType      string          `json:"entity_type"`
	EntityID        string          `json:"entity_id"`
	Payload         json.RawMessage `json:"payload"`
	ClientTimestamp string          `json:"client_timestamp"`
}

// PushResponse is the response from a push request.
type PushResponse struct {
	Accepted int              `json:"accepted"`
	Acks     []AckResponse    `json:"acks"`
	Rejected []RejectResponse `json:"rejected,omitempty"`
}

// AckResponse is a single acknowledged event.
type AckResponse struct {
	ClientActionID int64 `json:"client_action_id"`
	ServerSeq      int64 `json:"server_seq"`
}

// RejectResponse is a single rejected event.
type RejectResponse struct {
	ClientActionID int64  `json:"client_action_id"`
	Reason         string `json:"reason"`
	ServerSeq      int64  `json:"server_seq,omitempty"`
}

// PullResponse is the response from a pull request.
type PullResponse struct {
	Events        []PullEvent `json:"events"`
	LastServerSeq int64       `json:"last_server_seq"`
	HasMore       bool        `json:"has_more"`
}

// PullEvent is a single event in a pull response.
type PullEvent struct {
	ServerSeq       int64           `json:"server_seq"`
	DeviceID        string          `json:"device_id"`
	SessionID       string          `json:"session_id"`
	ClientActionID  int64           `json:"client_action_id"`
	ActionType      string          `json:"action_type"`
	EntityType      string          `json:"entity_type"`
	EntityID        string          `json:"entity_id"`
	Payload         json.RawMessage `json:"payload"`
	ClientTimestamp string          `json:"client_timestamp"`
}

// SyncStatusResponse is the response from GET /v1/projects/{id}/sync/status.
type SyncStatusResponse struct {
	EventCount    int64  `json:"event_count"`
	LastServerSeq int64  `json:"last_server_seq"`
	LastEventTime string `json:"last_event_time,omitempty"`
}

// HealthResponse is the response from GET /healthz.
type HealthResponse struct {
	Status string `json:"status"`
}

// HealthCheck hits the /healthz endpoint to verify server reachability.
func (c *Client) HealthCheck() (*HealthResponse, error) {
	var resp HealthResponse
	if err := c.doNoAuth("GET", "/healthz", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// --- Auth methods ---

// LoginStart initiates device auth flow. No API key required.
func (c *Client) LoginStart(email string) (*LoginStartResponse, error) {
	body := map[string]string{"email": email}
	var resp LoginStartResponse
	if err := c.doNoAuth("POST", "/v1/auth/login/start", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// LoginPoll checks the status of a device auth request. No API key required.
func (c *Client) LoginPoll(deviceCode string) (*LoginPollResponse, error) {
	body := map[string]string{"device_code": deviceCode}
	var resp LoginPollResponse
	if err := c.doNoAuth("POST", "/v1/auth/login/poll", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// --- Project methods ---

// CreateProject creates a new project on the server.
func (c *Client) CreateProject(name, description string) (*ProjectResponse, error) {
	body := map[string]string{"name": name, "description": description}
	var resp ProjectResponse
	if err := c.do("POST", "/v1/projects", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListProjects lists all projects for the authenticated user.
func (c *Client) ListProjects() ([]ProjectResponse, error) {
	var resp []ProjectResponse
	if err := c.do("GET", "/v1/projects", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// --- Member types ---

// MemberResponse represents a project member from the server.
type MemberResponse struct {
	ProjectID string `json:"project_id"`
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	InvitedBy string `json:"invited_by"`
	CreatedAt string `json:"created_at"`
}

// --- Member methods ---

// AddMember invites a user to a project by email.
func (c *Client) AddMember(projectID, email, role string) (*MemberResponse, error) {
	body := map[string]string{"email": email, "role": role}
	var resp MemberResponse
	if err := c.do("POST", fmt.Sprintf("/v1/projects/%s/members", projectID), body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListMembers lists all members of a project.
func (c *Client) ListMembers(projectID string) ([]MemberResponse, error) {
	var resp []MemberResponse
	if err := c.do("GET", fmt.Sprintf("/v1/projects/%s/members", projectID), nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// UpdateMemberRole changes a member's role in a project.
func (c *Client) UpdateMemberRole(projectID, userID, role string) error {
	body := map[string]string{"role": role}
	return c.do("PATCH", fmt.Sprintf("/v1/projects/%s/members/%s", projectID, userID), body, nil)
}

// RemoveMember removes a user from a project.
func (c *Client) RemoveMember(projectID, userID string) error {
	return c.do("DELETE", fmt.Sprintf("/v1/projects/%s/members/%s", projectID, userID), nil, nil)
}

// --- Sync methods ---

// Push sends local events to the server.
func (c *Client) Push(projectID string, req *PushRequest) (*PushResponse, error) {
	var resp PushResponse
	if err := c.do("POST", fmt.Sprintf("/v1/projects/%s/sync/push", projectID), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Pull fetches remote events from the server.
func (c *Client) Pull(projectID string, afterSeq int64, limit int, excludeDeviceID string) (*PullResponse, error) {
	params := url.Values{}
	params.Set("after_server_seq", strconv.FormatInt(afterSeq, 10))
	params.Set("limit", strconv.Itoa(limit))
	if excludeDeviceID != "" {
		params.Set("exclude_client", excludeDeviceID)
	}

	var resp PullResponse
	if err := c.do("GET", fmt.Sprintf("/v1/projects/%s/sync/pull?%s", projectID, params.Encode()), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SnapshotResponse holds the result of a snapshot download.
type SnapshotResponse struct {
	Data           []byte
	SnapshotSeq    int64
}

// GetSnapshot downloads a snapshot database for bootstrap.
func (c *Client) GetSnapshot(projectID string) (*SnapshotResponse, error) {
	path := fmt.Sprintf("/v1/projects/%s/sync/snapshot", projectID)
	req, err := http.NewRequest("GET", c.BaseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // no events to snapshot
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("snapshot: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}

	seqStr := resp.Header.Get("X-Snapshot-Seq")
	if seqStr == "" {
		return nil, fmt.Errorf("snapshot response missing X-Snapshot-Seq header")
	}
	seq, err := strconv.ParseInt(seqStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse X-Snapshot-Seq %q: %w", seqStr, err)
	}
	if seq <= 0 {
		return nil, fmt.Errorf("snapshot seq must be positive")
	}

	return &SnapshotResponse{Data: data, SnapshotSeq: seq}, nil
}

// SyncStatus gets the sync status for a project.
func (c *Client) SyncStatus(projectID string) (*SyncStatusResponse, error) {
	var resp SyncStatusResponse
	if err := c.do("GET", fmt.Sprintf("/v1/projects/%s/sync/status", projectID), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// --- HTTP helpers ---

// apiError is the standard error body from the server.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Code
}

// do executes an authenticated HTTP request.
func (c *Client) do(method, path string, body, result any) error {
	return c.doRequest(method, path, body, result, true)
}

// doNoAuth executes an unauthenticated HTTP request.
func (c *Client) doNoAuth(method, path string, body, result any) error {
	return c.doRequest(method, path, body, result, false)
}

func (c *Client) doRequest(method, path string, body, result any, auth bool) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth && c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiErr apiError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Code != "" {
			switch resp.StatusCode {
			case http.StatusUnauthorized:
				return fmt.Errorf("%w: %s", ErrUnauthorized, apiErr.Message)
			case http.StatusForbidden:
				return fmt.Errorf("%w: %s", ErrForbidden, apiErr.Message)
			case http.StatusNotFound:
				return fmt.Errorf("%w: %s", ErrNotFound, apiErr.Message)
			default:
				return &apiErr
			}
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}

	return nil
}
