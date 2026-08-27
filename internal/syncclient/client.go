package syncclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors for common HTTP error classes.
var (
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrNotFound      = errors.New("not found")
	ErrStreamStalled = errors.New("event stream stalled")
)

// HTTPError preserves the response status for callers implementing transport
// degradation policy without making that policy part of this HTTP adapter.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body) }

// IsHTTPStatus reports whether err came from one of the supplied HTTP status
// codes. It also recognizes the package's historical sentinel errors.
func IsHTTPStatus(err error, codes ...int) bool {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		for _, code := range codes {
			if httpErr.StatusCode == code {
				return true
			}
		}
	}
	for _, code := range codes {
		if code == http.StatusUnauthorized && errors.Is(err, ErrUnauthorized) {
			return true
		}
		if code == http.StatusForbidden && errors.Is(err, ErrForbidden) {
			return true
		}
		if code == http.StatusNotFound && errors.Is(err, ErrNotFound) {
			return true
		}
	}
	return false
}

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

// DeviceStartResponse is the response from POST /v1/auth/device/start.
// The flow is non-enumerating: a syntactically-valid email always yields a
// device_code and email_sent=true, even for unknown accounts (in which case no
// email is sent and poll never transitions to complete).
type DeviceStartResponse struct {
	DeviceCode string `json:"device_code"`
	ExpiresIn  int    `json:"expires_in"`
	Interval   int    `json:"interval"`
	EmailSent  bool   `json:"email_sent"`
}

// DevicePollResponse is the response from POST /v1/auth/device/poll.
// When status=="pending" only Status is set; when status=="complete" all fields
// are populated.
type DevicePollResponse struct {
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

// ProjectEvent is one notification from the project's SSE endpoint. The
// payload is intentionally opaque: events are refresh hints, never the data
// path. State still arrives through Pull.
type ProjectEvent struct {
	ID   string
	Type string
	Data []byte
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

// DeviceStart initiates the PKCE device-login flow. The caller generates a
// local code_verifier and passes only its S256 code_challenge here; the verifier
// is never sent until DevicePoll. No API key required.
//
// method must be "S256". The server emails a magic approval link to the address
// and returns a device_code used to poll for completion.
func (c *Client) DeviceStart(email, codeChallenge, method, deviceName string) (*DeviceStartResponse, error) {
	body := map[string]string{
		"email":                 email,
		"code_challenge":        codeChallenge,
		"code_challenge_method": method,
		"device_name":           deviceName,
	}
	var resp DeviceStartResponse
	if err := c.doNoAuth("POST", "/v1/auth/device/start", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DevicePoll checks the status of a PKCE device login. It sends the device_code
// from DeviceStart together with the local code_verifier; the server only issues
// a key once the emailed approval link has been clicked AND the verifier matches
// the code_challenge sent in DeviceStart. No API key required.
func (c *Client) DevicePoll(deviceCode, codeVerifier string) (*DevicePollResponse, error) {
	body := map[string]string{
		"device_code":   deviceCode,
		"code_verifier": codeVerifier,
	}
	var resp DevicePollResponse
	if err := c.doNoAuth("POST", "/v1/auth/device/poll", body, &resp); err != nil {
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
	Data        []byte
	SnapshotSeq int64
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
	defer func() { _ = resp.Body.Close() }()

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
	return c.SyncStatusContext(context.Background(), projectID)
}

// SyncStatusContext gets sync status with caller cancellation.
func (c *Client) SyncStatusContext(ctx context.Context, projectID string) (*SyncStatusResponse, error) {
	var resp SyncStatusResponse
	if err := c.doRequestContext(ctx, "GET", fmt.Sprintf("/v1/projects/%s/sync/status", projectID), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Events consumes the project event stream until ctx is canceled or the
// connection fails. The request deliberately has no whole-request timeout;
// idle body liveness is enforced separately so a connected SSE request may
// live indefinitely while a buffering or dead proxy is detected.
func (c *Client) Events(ctx context.Context, projectID, lastEventID string, idleTimeout time.Duration, onOpen func(), onEvent func(ProjectEvent)) error {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, c.BaseURL+fmt.Sprintf("/v1/projects/%s/events", projectID), nil)
	if err != nil {
		return fmt.Errorf("create event request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	streamHTTP := *c.HTTP
	streamHTTP.Timeout = 0
	resp, err := streamHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("event request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("%w: %s", ErrUnauthorized, strings.TrimSpace(string(body)))
		}
		if resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("%w: %s", ErrForbidden, strings.TrimSpace(string(body)))
		}
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %s", ErrNotFound, strings.TrimSpace(string(body)))
		}
		return &HTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	if onOpen != nil {
		onOpen()
	}

	activity := make(chan struct{}, 1)
	watchDone := make(chan struct{})
	stalled := make(chan struct{}, 1)
	if idleTimeout > 0 {
		go func() {
			timer := time.NewTimer(idleTimeout)
			defer timer.Stop()
			for {
				select {
				case <-activity:
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(idleTimeout)
				case <-timer.C:
					select {
					case stalled <- struct{}{}:
					default:
					}
					cancel()
					return
				case <-watchDone:
					return
				}
			}
		}()
	}
	defer close(watchDone)

	scanner := bufio.NewScanner(resp.Body)
	// Refresh payloads are tiny, but permit useful future metadata.
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var event ProjectEvent
	for scanner.Scan() {
		if idleTimeout > 0 {
			select {
			case activity <- struct{}{}:
			default:
			}
		}
		line := scanner.Text()
		if line == "" {
			if event.ID != "" || event.Type != "" || len(event.Data) > 0 {
				if onEvent != nil {
					onEvent(event)
				}
				event = ProjectEvent{}
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			event.ID = value
		case "event":
			event.Type = value
		case "data":
			if len(event.Data) > 0 {
				event.Data = append(event.Data, '\n')
			}
			event.Data = append(event.Data, value...)
		}
	}
	select {
	case <-stalled:
		return ErrStreamStalled
	default:
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("read event stream: %w", err)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return io.ErrUnexpectedEOF
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
	return c.doRequestContext(context.Background(), method, path, body, result, auth)
}

func (c *Client) doRequestContext(ctx context.Context, method, path string, body, result any, auth bool) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bodyReader)
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
	defer func() { _ = resp.Body.Close() }()

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
				return &HTTPError{StatusCode: resp.StatusCode, Body: apiErr.Error()}
			}
		}
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return fmt.Errorf("%w: %s", ErrUnauthorized, strings.TrimSpace(string(respBody)))
		case http.StatusForbidden:
			return fmt.Errorf("%w: %s", ErrForbidden, strings.TrimSpace(string(respBody)))
		case http.StatusNotFound:
			return fmt.Errorf("%w: %s", ErrNotFound, strings.TrimSpace(string(respBody)))
		}
		return &HTTPError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}

	return nil
}
