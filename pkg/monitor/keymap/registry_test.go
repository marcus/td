package keymap

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if r.bindings == nil {
		t.Error("bindings map not initialized")
	}
	if r.userOverrides == nil {
		t.Error("userOverrides map not initialized")
	}
}

func TestRegisterBinding(t *testing.T) {
	r := NewRegistry()
	b := Binding{
		Key:     "j",
		Command: CmdCursorDown,
		Context: ContextMain,
	}
	r.RegisterBinding(b)

	bindings := r.BindingsForContext(ContextMain)
	if len(bindings) != 1 {
		t.Errorf("expected 1 binding, got %d", len(bindings))
	}
	if bindings[0].Key != "j" {
		t.Errorf("expected key 'j', got '%s'", bindings[0].Key)
	}
}

func TestRegisterDefaults(t *testing.T) {
	r := NewRegistry()
	RegisterDefaults(r)

	// Check that global bindings exist
	globalBindings := r.BindingsForContext(ContextGlobal)
	if len(globalBindings) == 0 {
		t.Error("no global bindings registered")
	}

	// Check that main bindings exist
	mainBindings := r.BindingsForContext(ContextMain)
	if len(mainBindings) == 0 {
		t.Error("no main bindings registered")
	}

	// Check that modal bindings exist
	modalBindings := r.BindingsForContext(ContextModal)
	if len(modalBindings) == 0 {
		t.Error("no modal bindings registered")
	}
}

func TestLookup(t *testing.T) {
	r := NewRegistry()
	RegisterDefaults(r)

	tests := []struct {
		name    string
		key     tea.KeyMsg
		context Context
		want    Command
		found   bool
	}{
		{
			name:    "quit with q in main",
			key:     tea.KeyPressMsg{Code: 'q', Text: ""},
			context: ContextMain,
			want:    CmdQuit,
			found:   true,
		},
		{
			name:    "cursor down with j in main",
			key:     tea.KeyPressMsg{Code: 'j', Text: ""},
			context: ContextMain,
			want:    CmdCursorDown,
			found:   true,
		},
		{
			name:    "close with esc in modal",
			key:     tea.KeyPressMsg{Code: tea.KeyEsc},
			context: ContextModal,
			want:    CmdClose,
			found:   true,
		},
		{
			name:    "scroll down with j in modal",
			key:     tea.KeyPressMsg{Code: 'j', Text: ""},
			context: ContextModal,
			want:    CmdScrollDown,
			found:   true,
		},
		{
			name:    "confirm with y in confirm",
			key:     tea.KeyPressMsg{Code: 'y', Text: ""},
			context: ContextConfirm,
			want:    CmdConfirm,
			found:   true,
		},
		{
			// Regression: shift+y must resolve the "Y" binding (td-272d9b).
			// Real terminal input is Code 'y' + ModShift + Text "Y".
			name:    "copy id with shift+y in main",
			key:     tea.KeyPressMsg{Code: 'y', Text: "Y", Mod: tea.ModShift},
			context: ContextMain,
			want:    CmdCopyIDToClipboard,
			found:   true,
		},
		{
			// Lowercase y in main still copies the issue as markdown.
			name:    "copy markdown with y in main",
			key:     tea.KeyPressMsg{Code: 'y', Text: "y"},
			context: ContextMain,
			want:    CmdCopyToClipboard,
			found:   true,
		},
		{
			name:    "unknown key returns not found",
			key:     tea.KeyPressMsg{Code: 'z', Text: ""},
			context: ContextMain,
			want:    "",
			found:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := r.Lookup(tt.key, tt.context)
			if found != tt.found {
				t.Errorf("Lookup() found = %v, want %v", found, tt.found)
			}
			if got != tt.want {
				t.Errorf("Lookup() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMultiKeySequence(t *testing.T) {
	r := NewRegistry()
	// Register a multi-key binding
	r.RegisterBinding(Binding{
		Key:     "g g",
		Command: CmdCursorTop,
		Context: ContextMain,
	})

	// First 'g' should not find a command but set pending
	key1 := tea.KeyPressMsg{Code: 'g', Text: ""}
	cmd, found := r.Lookup(key1, ContextMain)
	if found {
		t.Errorf("first 'g' should not find a command, got %s", cmd)
	}
	if !r.HasPending() {
		t.Error("should have pending key after first 'g'")
	}

	// Second 'g' should complete the sequence
	cmd, found = r.Lookup(key1, ContextMain)
	if !found {
		t.Error("second 'g' should find the command")
	}
	if cmd != CmdCursorTop {
		t.Errorf("expected CmdCursorTop, got %s", cmd)
	}
	if r.HasPending() {
		t.Error("should not have pending key after sequence completes")
	}
}

func TestMultiKeySequenceTimeout(t *testing.T) {
	r := NewRegistry()
	r.RegisterBinding(Binding{
		Key:     "g g",
		Command: CmdCursorTop,
		Context: ContextMain,
	})

	// First 'g'
	key := tea.KeyPressMsg{Code: 'g', Text: ""}
	r.Lookup(key, ContextMain)

	// Simulate timeout by manually setting pendingTime
	r.mu.Lock()
	r.pendingTime = time.Now().Add(-time.Second)
	r.mu.Unlock()

	// Second 'g' after timeout should reset
	cmd, found := r.Lookup(key, ContextMain)
	if found {
		t.Errorf("should not find command after timeout, got %s", cmd)
	}
}

func TestUserOverride(t *testing.T) {
	r := NewRegistry()
	RegisterDefaults(r)

	// Default: 'j' is cursor down in main
	key := tea.KeyPressMsg{Code: 'j', Text: ""}
	cmd, _ := r.Lookup(key, ContextMain)
	if cmd != CmdCursorDown {
		t.Errorf("default 'j' should be CmdCursorDown, got %s", cmd)
	}

	// Override: 'j' is now quit in main
	r.SetUserOverride(ContextMain, "j", CmdQuit)

	cmd, _ = r.Lookup(key, ContextMain)
	if cmd != CmdQuit {
		t.Errorf("overridden 'j' should be CmdQuit, got %s", cmd)
	}
}

func TestGlobalBindingsFallback(t *testing.T) {
	r := NewRegistry()
	// Register a global binding
	r.RegisterBinding(Binding{
		Key:     "?",
		Command: CmdToggleHelp,
		Context: ContextGlobal,
	})

	// Should be found even in modal context
	key := tea.KeyPressMsg{Code: '?', Text: ""}
	cmd, found := r.Lookup(key, ContextModal)
	if !found {
		t.Error("global binding should be found in modal context")
	}
	if cmd != CmdToggleHelp {
		t.Errorf("expected CmdToggleHelp, got %s", cmd)
	}
}

func TestContextOverridesGlobal(t *testing.T) {
	r := NewRegistry()
	// Register global binding
	r.RegisterBinding(Binding{
		Key:     "r",
		Command: CmdRefresh,
		Context: ContextGlobal,
	})
	// Register context-specific binding for same key
	r.RegisterBinding(Binding{
		Key:     "r",
		Command: CmdMarkForReview,
		Context: ContextMain,
	})

	key := tea.KeyPressMsg{Code: 'r', Text: ""}

	// In main context, should use context-specific binding
	cmd, _ := r.Lookup(key, ContextMain)
	if cmd != CmdMarkForReview {
		t.Errorf("main context should override global, got %s", cmd)
	}
}

func TestKeyToString(t *testing.T) {
	tests := []struct {
		key  tea.KeyMsg
		want string
	}{
		{tea.KeyPressMsg{Code: tea.KeyTab}, "tab"},
		{tea.KeyPressMsg{Code: tea.KeyEsc}, "esc"},
		{tea.KeyPressMsg{Code: tea.KeyEnter}, "enter"},
		{tea.KeyPressMsg{Code: tea.KeyUp}, "up"},
		{tea.KeyPressMsg{Code: tea.KeyDown}, "down"},
		{tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, "ctrl+c"},
		{tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}, "ctrl+d"},
		{tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, "shift+tab"},
		{tea.KeyPressMsg{Code: 'j', Text: "j"}, "j"},
		// Shifted printable keys arrive as the unshifted Code plus ModShift and
		// carry the uppercase Text. KeyToString must return "Y"/"G", not
		// "shift+y"/"shift+g", so capital-letter bindings match (td-272d9b).
		{tea.KeyPressMsg{Code: 'y', Text: "Y", Mod: tea.ModShift}, "Y"},
		{tea.KeyPressMsg{Code: 'g', Text: "G", Mod: tea.ModShift}, "G"},
		{tea.KeyPressMsg{Code: '/', Text: "?", Mod: tea.ModShift}, "?"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := KeyToString(tt.key)
			if got != tt.want {
				t.Errorf("KeyToString() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestIsPrintable(t *testing.T) {
	tests := []struct {
		key  tea.KeyMsg
		want bool
	}{
		{tea.KeyPressMsg{Code: 'a', Text: "a"}, true},
		{tea.KeyPressMsg{Code: 'Z', Text: "Z"}, true},
		{tea.KeyPressMsg{Code: '1', Text: "1"}, true},
		{tea.KeyPressMsg{Code: '!', Text: "!"}, true},
		{tea.KeyPressMsg{Code: tea.KeyTab}, false},
		{tea.KeyPressMsg{Code: tea.KeyEnter}, false},
		{tea.KeyPressMsg{Code: tea.KeyEsc}, false},
		{tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, false},
	}

	for _, tt := range tests {
		name := KeyToString(tt.key)
		t.Run(name, func(t *testing.T) {
			got := IsPrintable(tt.key)
			if got != tt.want {
				t.Errorf("IsPrintable(%s) = %v, want %v", name, got, tt.want)
			}
		})
	}
}

func TestResetPending(t *testing.T) {
	r := NewRegistry()
	r.RegisterBinding(Binding{
		Key:     "g g",
		Command: CmdCursorTop,
		Context: ContextMain,
	})

	// Start a sequence
	key := tea.KeyPressMsg{Code: 'g', Text: ""}
	r.Lookup(key, ContextMain)

	if !r.HasPending() {
		t.Error("should have pending after first key")
	}

	r.ResetPending()

	if r.HasPending() {
		t.Error("should not have pending after reset")
	}
}

func TestPendingKey(t *testing.T) {
	r := NewRegistry()
	r.RegisterBinding(Binding{
		Key:     "g g",
		Command: CmdCursorTop,
		Context: ContextMain,
	})

	// No pending initially
	if pk := r.PendingKey(); pk != "" {
		t.Errorf("expected no pending key, got %s", pk)
	}

	// Start a sequence
	key := tea.KeyPressMsg{Code: 'g', Text: ""}
	r.Lookup(key, ContextMain)

	// Should have pending 'g'
	if pk := r.PendingKey(); pk != "g" {
		t.Errorf("expected pending key 'g', got '%s'", pk)
	}
}
