package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestApproveCmdHasCommentFlag(t *testing.T) {
	f := approveCmd.Flags().Lookup("comment")
	if f == nil {
		t.Fatalf("expected approveCmd to have --comment flag")
	}
}

func TestApprovalReasonPrecedence(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("reason", "", "")
	cmd.Flags().String("message", "", "")
	cmd.Flags().String("comment", "", "")
	cmd.Flags().String("note", "", "")
	cmd.Flags().String("notes", "", "")

	// Lowest precedence: --comment
	if err := cmd.Flags().Set("comment", "c"); err != nil {
		t.Fatalf("set comment: %v", err)
	}
	if got := approvalReason(cmd); got != "c" {
		t.Fatalf("comment only: got %q, want %q", got, "c")
	}

	// Middle precedence: --message overrides comment
	if err := cmd.Flags().Set("message", "m"); err != nil {
		t.Fatalf("set message: %v", err)
	}
	if got := approvalReason(cmd); got != "m" {
		t.Fatalf("message+comment: got %q, want %q", got, "m")
	}

	// Highest precedence: --reason overrides message
	if err := cmd.Flags().Set("reason", "r"); err != nil {
		t.Fatalf("set reason: %v", err)
	}
	if got := approvalReason(cmd); got != "r" {
		t.Fatalf("reason+message+comment: got %q, want %q", got, "r")
	}
}

func TestApprovalReasonEmpty(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("reason", "", "")
	cmd.Flags().String("message", "", "")
	cmd.Flags().String("comment", "", "")
	cmd.Flags().String("note", "", "")
	cmd.Flags().String("notes", "", "")

	if got := approvalReason(cmd); got != "" {
		t.Fatalf("empty: got %q, want empty", got)
	}
}

func TestApproveCmdHasNoteFlags(t *testing.T) {
	// Test --note flag exists
	if f := approveCmd.Flags().Lookup("note"); f == nil {
		t.Error("expected approveCmd to have --note flag")
	}

	// Test --notes flag exists
	if f := approveCmd.Flags().Lookup("notes"); f == nil {
		t.Error("expected approveCmd to have --notes flag")
	}
}

func TestApprovalReasonWithNoteFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("reason", "", "")
	cmd.Flags().String("message", "", "")
	cmd.Flags().String("comment", "", "")
	cmd.Flags().String("note", "", "")
	cmd.Flags().String("notes", "", "")

	// Test --note works
	if err := cmd.Flags().Set("note", "my note"); err != nil {
		t.Fatalf("set note: %v", err)
	}
	if got := approvalReason(cmd); got != "my note" {
		t.Fatalf("note only: got %q, want %q", got, "my note")
	}

	// Reset and test --notes
	cmd.Flags().Set("note", "")
	if err := cmd.Flags().Set("notes", "my notes"); err != nil {
		t.Fatalf("set notes: %v", err)
	}
	if got := approvalReason(cmd); got != "my notes" {
		t.Fatalf("notes only: got %q, want %q", got, "my notes")
	}

	// --comment has lower priority than --note
	cmd.Flags().Set("notes", "")
	cmd.Flags().Set("comment", "c")
	cmd.Flags().Set("note", "n")
	if got := approvalReason(cmd); got != "n" {
		t.Fatalf("note vs comment: got %q, want %q", got, "n")
	}
}
