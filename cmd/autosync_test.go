package cmd

import "testing"

func TestIsMutatingCommand(t *testing.T) {
	// Commands that should trigger auto-sync
	mutating := []string{"create", "update", "delete", "start", "close", "log", "handoff", "board", "dep", "ws"}
	for _, name := range mutating {
		if !isMutatingCommand(name) {
			t.Errorf("expected %q to be mutating", name)
		}
	}

	// Commands that should NOT trigger auto-sync
	readOnly := []string{"list", "show", "search", "query", "monitor", "sync", "auth", "status", "info", "version", "help", "doctor"}
	for _, name := range readOnly {
		if isMutatingCommand(name) {
			t.Errorf("expected %q to NOT be mutating", name)
		}
	}
}

func TestAutoSyncEnabled_Default(t *testing.T) {
	// With no env var set, auto-sync should be enabled by default
	t.Setenv("TD_AUTO_SYNC", "")
	if !AutoSyncEnabled() {
		t.Error("expected auto-sync enabled by default")
	}
}

func TestAutoSyncEnabled_Disabled(t *testing.T) {
	t.Setenv("TD_AUTO_SYNC", "0")
	if AutoSyncEnabled() {
		t.Error("expected auto-sync disabled when TD_AUTO_SYNC=0")
	}
}

func TestAutoSyncEnabled_Explicit(t *testing.T) {
	t.Setenv("TD_AUTO_SYNC", "true")
	if !AutoSyncEnabled() {
		t.Error("expected auto-sync enabled when TD_AUTO_SYNC=true")
	}

	t.Setenv("TD_AUTO_SYNC", "1")
	if !AutoSyncEnabled() {
		t.Error("expected auto-sync enabled when TD_AUTO_SYNC=1")
	}
}
