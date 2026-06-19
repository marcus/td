package email

import (
	"context"
	"log/slog"
)

// LogSender logs a structured record for each login email. It intentionally
// omits Subject, Text, and HTML because those fields carry the magic link token.
type LogSender struct{}

// NewLogSender returns a LogSender.
func NewLogSender() *LogSender {
	return &LogSender{}
}

// SendLoginLink logs To, Purpose, and TraceID via slog. Token material
// (Subject, Text, HTML) is never logged.
func (s *LogSender) SendLoginLink(ctx context.Context, msg LoginEmail) error {
	slog.InfoContext(ctx, "email_log_sender: would send login link",
		slog.String("to", msg.To),
		slog.String("purpose", msg.Purpose),
		slog.String("trace_id", msg.TraceID),
	)
	return nil
}
