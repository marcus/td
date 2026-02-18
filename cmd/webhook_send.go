package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/marcus/td/internal/webhook"
	"github.com/spf13/cobra"
)

var webhookSendCmd = &cobra.Command{
	Use:    "_webhook-send",
	Short:  "Internal: deliver a webhook payload from a temp file",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]

		tf, err := webhook.ReadTempFile(path)
		if err != nil {
			return fmt.Errorf("read temp file: %w", err)
		}

		// Always clean up the temp file, even on failure.
		defer os.Remove(path)

		if err := webhook.Dispatch(tf.URL, tf.Secret, tf.Payload); err != nil {
			slog.Debug("webhook: dispatch failed", "err", err)
			return nil
		}

		return nil
	},
	// Disable all hooks for the internal send command.
	PersistentPreRun:  func(cmd *cobra.Command, args []string) {},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {},
}

func init() {
	rootCmd.AddCommand(webhookSendCmd)
}
