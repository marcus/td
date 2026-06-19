package email

import (
	"context"
	"fmt"
)

// CloudflareSender is a stub. The real implementation is added in B3.
type CloudflareSender struct {
	cfg EmailConfig
}

// NewCloudflareSender returns a stub sender. B3 replaces this with the live
// Cloudflare Email Workers REST implementation.
func NewCloudflareSender(cfg EmailConfig) (*CloudflareSender, error) {
	return &CloudflareSender{cfg: cfg}, nil
}

// SendLoginLink is not yet implemented; B3 provides the real body.
func (s *CloudflareSender) SendLoginLink(_ context.Context, _ LoginEmail) error {
	return fmt.Errorf("cloudflare email sender: not yet implemented")
}
