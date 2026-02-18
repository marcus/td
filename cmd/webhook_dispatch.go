package cmd

import (
	"log/slog"
	"os"
	"os/exec"
	"syscall"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/webhook"
)

// webhookPreRunRowid is captured in PersistentPreRun so we can query
// action_log entries created during the command. We use rowid instead of
// timestamps because the sidecar writes RFC3339 ("…T…Z") while the
// modernc.org/sqlite driver serializes time.Time via Go's .String()
// ("… …-0500 EST m=+…"). SQLite text comparison on these mixed formats
// is unreliable — 'T' > ' ' makes sidecar actions appear newer than any
// CLI-written timestamp.
var webhookPreRunRowid int64

// captureWebhookState saves the current max action_log rowid for later
// use by dispatchWebhookAsync.
func captureWebhookState() {
	dir := getBaseDir()
	if dir == "" || !webhook.IsEnabled(dir) {
		return
	}

	database, err := db.Open(dir)
	if err != nil {
		return
	}
	defer database.Close()

	webhookPreRunRowid, _ = database.MaxActionRowid()
}

// dispatchWebhookAsync checks for new action_log entries since the pre-run
// rowid snapshot, writes a temp file, and spawns a detached child process to
// POST the webhook. The parent does not wait for the child.
func dispatchWebhookAsync() {
	dir := getBaseDir()
	if dir == "" {
		return
	}

	if !webhook.IsEnabled(dir) {
		return
	}

	database, err := db.Open(dir)
	if err != nil {
		slog.Debug("webhook: open db", "err", err)
		return
	}
	defer database.Close()

	actions, err := database.GetActionsAfterRowid(webhookPreRunRowid)
	if err != nil {
		slog.Debug("webhook: query actions", "err", err)
		return
	}
	if len(actions) == 0 {
		return
	}

	payload := webhook.BuildPayload(dir, actions)
	tf := &webhook.TempFile{
		URL:     webhook.GetURL(dir),
		Secret:  webhook.GetSecret(dir),
		Payload: payload,
	}

	path, err := webhook.WriteTempFile(tf)
	if err != nil {
		slog.Debug("webhook: write temp file", "err", err)
		return
	}

	// Spawn detached child: td _webhook-send <tempfile>
	child := exec.Command(os.Args[0], "_webhook-send", path)
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	child.Stdout = nil
	child.Stderr = nil
	child.Stdin = nil

	if err := child.Start(); err != nil {
		slog.Debug("webhook: spawn child", "err", err)
		os.Remove(path)
		return
	}

	slog.Debug("webhook: dispatched", "pid", child.Process.Pid, "actions", len(actions))
	// Don't wait — parent exits immediately.
}
