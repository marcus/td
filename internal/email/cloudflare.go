package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const cloudflareDefaultBaseURL = "https://api.cloudflare.com/client/v4"

// CloudflareSender sends login emails via the Cloudflare Email Sending REST API.
type CloudflareSender struct {
	accountID   string
	apiToken    string
	fromAddress string
	fromName    string
	replyTo     string
	baseURL     string
	httpClient  *http.Client
}

// NewCloudflareSender constructs a CloudflareSender from cfg.
// Returns an error if accountID, apiToken, or fromAddress are missing.
func NewCloudflareSender(cfg EmailConfig) (*CloudflareSender, error) {
	if cfg.AccountID == "" {
		return nil, fmt.Errorf("cloudflare email sender: accountID is required")
	}
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("cloudflare email sender: apiToken is required")
	}
	if cfg.From == "" {
		return nil, fmt.Errorf("cloudflare email sender: fromAddress is required")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = cloudflareDefaultBaseURL
	}
	return &CloudflareSender{
		accountID:   cfg.AccountID,
		apiToken:    cfg.APIToken,
		fromAddress: cfg.From,
		fromName:    cfg.FromName,
		replyTo:     cfg.ReplyTo,
		baseURL:     baseURL,
		httpClient:  &http.Client{},
	}, nil
}

// cfEnvelope is the standard Cloudflare API response wrapper.
type cfEnvelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

// SendLoginLink sends a magic-link login email via the Cloudflare Email Sending API.
//
// Security constraints:
//   - The API token is never included in any returned error.
//   - msg.HTML, msg.Text, and msg.Subject (which may contain the magic link token)
//     are never logged.
func (s *CloudflareSender) SendLoginLink(ctx context.Context, msg LoginEmail) error {
	url := fmt.Sprintf("%s/accounts/%s/email/sending/send", s.baseURL, s.accountID)

	// Build the from object; omit name if empty.
	fromObj := map[string]string{"address": s.fromAddress}
	if s.fromName != "" {
		fromObj["name"] = s.fromName
	}

	// Build the body. reply_to is omitted when replyTo is empty.
	body := map[string]any{
		"from":    fromObj,
		"to":      []string{msg.To},
		"subject": msg.Subject,
		"html":    msg.HTML,
		"text":    msg.Text,
	}
	if s.replyTo != "" {
		body["reply_to"] = map[string]string{"address": s.replyTo}
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("cloudflare email sender: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("cloudflare email sender: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare email sender: send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("cloudflare email sender: read response: %w", err)
	}

	var envelope cfEnvelope
	if jsonErr := json.Unmarshal(respBody, &envelope); jsonErr != nil {
		// Non-2xx with unparseable body — surface status code only.
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("cloudflare email sender: unexpected status %d", resp.StatusCode)
		}
		return fmt.Errorf("cloudflare email sender: parse response: %w", jsonErr)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if len(envelope.Errors) > 0 {
			return fmt.Errorf("cloudflare email sender: %s", envelope.Errors[0].Message)
		}
		return fmt.Errorf("cloudflare email sender: unexpected status %d", resp.StatusCode)
	}

	if !envelope.Success {
		if len(envelope.Errors) > 0 {
			return fmt.Errorf("cloudflare email sender: %s", envelope.Errors[0].Message)
		}
		return fmt.Errorf("cloudflare email sender: send failed (success=false, no error detail)")
	}

	return nil
}
