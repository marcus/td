package session

import (
	"os"
	"strings"
	"testing"
)

func TestAgentFingerprintString(t *testing.T) {
	tests := []struct {
		name     string
		fp       AgentFingerprint
		expected string
	}{
		{
			name:     "claude-code with PID",
			fp:       AgentFingerprint{Type: AgentClaudeCode, PID: 12345},
			expected: "claude-code_12345",
		},
		{
			name:     "cursor with PID",
			fp:       AgentFingerprint{Type: AgentCursor, PID: 67890},
			expected: "cursor_67890",
		},
		{
			name:     "terminal without PID",
			fp:       AgentFingerprint{Type: AgentTerminal, PID: 0},
			expected: "terminal",
		},
		{
			name:     "unknown without PID",
			fp:       AgentFingerprint{Type: AgentUnknown, PID: 0},
			expected: "unknown",
		},
		{
			name:     "explicit with ID",
			fp:       AgentFingerprint{Type: AgentType("explicit"), PID: 0, ExplicitID: "my-session"},
			expected: "explicit_my-session",
		},
		{
			name:     "explicit with special chars sanitized",
			fp:       AgentFingerprint{Type: AgentType("explicit"), PID: 0, ExplicitID: "session/with:special*chars"},
			expected: "explicit_session_with_special_chars",
		},
		{
			name:     "explicit with long ID truncated",
			fp:       AgentFingerprint{Type: AgentType("explicit"), PID: 0, ExplicitID: "this-is-a-very-long-session-id-that-exceeds-limit"},
			expected: "explicit_this-is-a-very-long-session-id-t",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.fp.String()
			if result != tt.expected {
				t.Errorf("String() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetAgentFingerprintWithExplicitOverride(t *testing.T) {
	// Set explicit override
	t.Setenv("TD_SESSION_ID", "my-explicit-session")

	fp := GetAgentFingerprint()

	if fp.Type != "explicit" {
		t.Errorf("Type = %q, want %q", fp.Type, "explicit")
	}
	if fp.PID != 0 {
		t.Errorf("PID = %d, want 0 for explicit override", fp.PID)
	}
	if fp.ExplicitID != "my-explicit-session" {
		t.Errorf("ExplicitID = %q, want %q", fp.ExplicitID, "my-explicit-session")
	}
}

func TestGetAgentFingerprintWithCursorEnv(t *testing.T) {
	// Clear explicit override
	os.Unsetenv("TD_SESSION_ID")
	// Set Cursor agent env var
	t.Setenv("CURSOR_AGENT", "1")

	fp := GetAgentFingerprint()

	if fp.Type != AgentCursor {
		t.Errorf("Type = %q, want %q", fp.Type, AgentCursor)
	}
	// PID should be set (parent PID)
	if fp.PID == 0 {
		t.Errorf("PID should be set for Cursor agent")
	}
}

func TestGetAgentFingerprintFallback(t *testing.T) {
	// Clear all agent-related env vars
	os.Unsetenv("TD_SESSION_ID")
	os.Unsetenv("CURSOR_AGENT")

	fp := GetAgentFingerprint()

	// Should detect claude-code or fall back to terminal/unknown
	// In test environment, we're likely running under go test, not an agent
	validTypes := map[AgentType]bool{
		AgentClaudeCode: true,
		AgentCursor:     true,
		AgentCodex:      true,
		AgentWindsurf:   true,
		AgentZed:        true,
		AgentAider:      true,
		AgentCopilot:    true,
		AgentGemini:     true,
		AgentTerminal:   true,
		AgentUnknown:    true,
	}

	if !validTypes[fp.Type] {
		t.Errorf("Type = %q, not a valid agent type", fp.Type)
	}
}

func TestDetectAgentAncestorReturnsUnknownForNoAgent(t *testing.T) {
	// This test verifies the function doesn't panic and returns something
	// In most test environments, we won't find an agent ancestor
	fp := detectAgentAncestor()

	// Should return either a detected agent or unknown
	if fp.Type == "" {
		t.Error("Type should not be empty")
	}
}

func TestGetTerminalSessionID(t *testing.T) {
	// Clear any terminal session vars
	for _, env := range []string{
		"TERM_SESSION_ID",
		"TMUX_PANE",
		"STY",
		"WINDOWID",
		"KONSOLE_DBUS_SESSION",
		"GNOME_TERMINAL_SCREEN",
	} {
		os.Unsetenv(env)
	}

	// Should return empty when no terminal vars set
	result := getTerminalSessionID()
	if result != "" {
		t.Errorf("getTerminalSessionID() = %q, want empty", result)
	}

	// Set a terminal var and verify it's detected
	t.Setenv("TERM_SESSION_ID", "test-terminal-123")
	result = getTerminalSessionID()
	if result != "test-terminal-123" {
		t.Errorf("getTerminalSessionID() = %q, want %q", result, "test-terminal-123")
	}
}

func TestAgentPatternsContainsExpectedAgents(t *testing.T) {
	expectedPatterns := []string{
		"claude",
		"cursor",
		"codex",
		"windsurf",
		"zed",
		"aider",
		"copilot",
		"gemini",
	}

	for _, pattern := range expectedPatterns {
		if _, ok := agentPatterns[pattern]; !ok {
			t.Errorf("agentPatterns missing pattern %q", pattern)
		}
	}
}

// TestExplicitIDOverridesAutoDetection verifies that ExplicitID takes priority
func TestExplicitIDOverridesAutoDetection(t *testing.T) {
	tests := []struct {
		name             string
		sessionID        string
		otherEnvVars     map[string]string
		expectExplicitID string
		expectType       AgentType
	}{
		{
			name:             "explicit ID overrides CURSOR_AGENT",
			sessionID:        "explicit-session-1",
			otherEnvVars:     map[string]string{"CURSOR_AGENT": "1"},
			expectExplicitID: "explicit-session-1",
			expectType:       "explicit",
		},
		{
			name:             "explicit ID with numeric value",
			sessionID:        "12345",
			otherEnvVars:     map[string]string{"CURSOR_AGENT": "1"},
			expectExplicitID: "12345",
			expectType:       "explicit",
		},
		{
			name:             "explicit ID with UUID format",
			sessionID:        "550e8400-e29b-41d4-a716-446655440000",
			otherEnvVars:     map[string]string{"CURSOR_AGENT": "1"},
			expectExplicitID: "550e8400-e29b-41d4-a716-446655440000",
			expectType:       "explicit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all env vars first
			os.Unsetenv("TD_SESSION_ID")
			os.Unsetenv("CURSOR_AGENT")

			// Set explicit ID
			t.Setenv("TD_SESSION_ID", tt.sessionID)

			// Set other env vars if provided
			for k, v := range tt.otherEnvVars {
				t.Setenv(k, v)
			}

			fp := GetAgentFingerprint()

			if fp.ExplicitID != tt.expectExplicitID {
				t.Errorf("ExplicitID = %q, want %q", fp.ExplicitID, tt.expectExplicitID)
			}
			if fp.Type != tt.expectType {
				t.Errorf("Type = %q, want %q", fp.Type, tt.expectType)
			}
			if fp.PID != 0 {
				t.Errorf("PID = %d, want 0 for explicit ID", fp.PID)
			}
		})
	}
}

// TestMultipleExplicitIDValues verifies different fingerprints with different ExplicitIDs
func TestMultipleExplicitIDValues(t *testing.T) {
	tests := []struct {
		name       string
		sessionID1 string
		sessionID2 string
		shouldDiffer bool
	}{
		{
			name:         "different explicit IDs produce different strings",
			sessionID1:   "session-alpha",
			sessionID2:   "session-beta",
			shouldDiffer: true,
		},
		{
			name:         "same explicit ID produces same string",
			sessionID1:   "session-gamma",
			sessionID2:   "session-gamma",
			shouldDiffer: false,
		},
		{
			name:         "case-sensitive explicit IDs",
			sessionID1:   "SESSION-DELTA",
			sessionID2:   "session-delta",
			shouldDiffer: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp1 := AgentFingerprint{Type: "explicit", ExplicitID: tt.sessionID1}
			fp2 := AgentFingerprint{Type: "explicit", ExplicitID: tt.sessionID2}

			str1 := fp1.String()
			str2 := fp2.String()

			if tt.shouldDiffer {
				if str1 == str2 {
					t.Errorf("fingerprints should differ: %q vs %q", str1, str2)
				}
			} else {
				if str1 != str2 {
					t.Errorf("fingerprints should be equal: %q vs %q", str1, str2)
				}
			}
		})
	}
}

// TestEmptyVsPopulatedExplicitID verifies behavior with empty vs populated ExplicitID
func TestEmptyVsPopulatedExplicitID(t *testing.T) {
	tests := []struct {
		name          string
		explicit      string
		explicitType  AgentType
		pid           int
		expectedStr   string
	}{
		{
			name:         "empty ExplicitID falls back to PID format",
			explicit:     "",
			explicitType: AgentClaudeCode,
			pid:          9999,
			expectedStr:  "claude-code_9999",
		},
		{
			name:         "empty ExplicitID without PID returns just type",
			explicit:     "",
			explicitType: AgentTerminal,
			pid:          0,
			expectedStr:  "terminal",
		},
		{
			name:         "populated ExplicitID ignores PID",
			explicit:     "my-session",
			explicitType: "explicit",
			pid:          5555,
			expectedStr:  "explicit_my-session",
		},
		{
			name:         "populated ExplicitID ignores type when explicit",
			explicit:     "test-id",
			explicitType: "explicit",
			pid:          0,
			expectedStr:  "explicit_test-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp := AgentFingerprint{
				Type:       tt.explicitType,
				PID:        tt.pid,
				ExplicitID: tt.explicit,
			}
			result := fp.String()
			if result != tt.expectedStr {
				t.Errorf("String() = %q, want %q", result, tt.expectedStr)
			}
		})
	}
}

// TestExplicitIDWithSpecialCharacters verifies sanitization of special characters
func TestExplicitIDWithSpecialCharacters(t *testing.T) {
	tests := []struct {
		name         string
		sessionID    string
		expectedStr  string
		description  string
	}{
		{
			name:        "slashes converted to underscores",
			sessionID:   "path/to/session",
			expectedStr: "explicit_path_to_session",
			description: "forward slashes",
		},
		{
			name:        "colons converted to underscores",
			sessionID:   "session:id:value",
			expectedStr: "explicit_session_id_value",
			description: "colons",
		},
		{
			name:        "asterisks converted to underscores",
			sessionID:   "session*with*asterisks",
			expectedStr: "explicit_session_with_asterisks",
			description: "asterisks",
		},
		{
			name:        "mixed special characters",
			sessionID:   "id@host:port/path",
			expectedStr: "explicit_id_host_port_path",
			description: "mixed special chars",
		},
		{
			name:        "spaces converted to underscores",
			sessionID:   "session with spaces",
			expectedStr: "explicit_session_with_spaces",
			description: "spaces",
		},
		{
			name:        "hyphens and underscores preserved",
			sessionID:   "session-id_value",
			expectedStr: "explicit_session-id_value",
			description: "hyphens and underscores",
		},
		{
			name:        "alphanumerics preserved",
			sessionID:   "Session123ABC",
			expectedStr: "explicit_Session123ABC",
			description: "alphanumerics",
		},
		{
			name:        "dots converted to underscores",
			sessionID:   "session.v1.2.3",
			expectedStr: "explicit_session_v1_2_3",
			description: "dots",
		},
		{
			name:        "brackets and braces converted",
			sessionID:   "session[id]{value}",
			expectedStr: "explicit_session_id__value_",
			description: "brackets and braces",
		},
		{
			name:        "unicode characters converted",
			sessionID:   "session_é_à_ñ",
			expectedStr: "explicit_session______",
			description: "unicode characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			fp := AgentFingerprint{
				Type:       "explicit",
				ExplicitID: tt.sessionID,
			}
			result := fp.String()
			if result != tt.expectedStr {
				t.Errorf("String() = %q, want %q", result, tt.expectedStr)
			}
		})
	}
}

// TestExplicitIDConsistency verifies that same ExplicitID always produces same result
func TestExplicitIDConsistency(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		iterations int
	}{
		{
			name:       "simple ID consistency",
			sessionID:  "consistent-session",
			iterations: 10,
		},
		{
			name:       "complex ID consistency",
			sessionID:  "session-with-many-chars-!@#$%^&*()",
			iterations: 5,
		},
		{
			name:       "long ID consistency",
			sessionID:  "this-is-a-very-long-session-identifier-that-should-be-consistent",
			iterations: 3,
		},
		{
			name:       "numeric ID consistency",
			sessionID:  "1234567890",
			iterations: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp := AgentFingerprint{
				Type:       "explicit",
				ExplicitID: tt.sessionID,
			}

			// Get string representation multiple times
			results := make([]string, tt.iterations)
			for i := 0; i < tt.iterations; i++ {
				results[i] = fp.String()
			}

			// Verify all results are identical
			for i := 1; i < len(results); i++ {
				if results[i] != results[0] {
					t.Errorf("inconsistent result at iteration %d: %q != %q", i, results[i], results[0])
				}
			}
		})
	}
}

// TestExplicitIDTruncation verifies long IDs are truncated correctly
func TestExplicitIDTruncation(t *testing.T) {
	tests := []struct {
		name        string
		sessionID   string
		maxLen      int // prefix + max sanitized len should be 32
		description string
	}{
		{
			name:      "very long ID without special chars",
			sessionID: "abcdefghijklmnopqrstuvwxyz0123456789",
			maxLen:    32,
			description: "long alphanumeric",
		},
		{
			name:      "very long ID with special chars",
			sessionID: "session-with-very-long-name-containing-special-chars-!@#$%^&*()",
			maxLen:    32,
			description: "long with special chars",
		},
		{
			name:      "UUID-like long ID",
			sessionID: "550e8400-e29b-41d4-a716-446655440000-extra-long-suffix",
			maxLen:    32,
			description: "long UUID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			fp := AgentFingerprint{
				Type:       "explicit",
				ExplicitID: tt.sessionID,
			}
			result := fp.String()

			// Result should be "explicit_" + sanitized (max 32 chars)
			if len(result) > (len("explicit_") + 32) {
				t.Errorf("String() length = %d, want max %d", len(result), len("explicit_")+32)
			}

			// Should start with "explicit_"
			if !strings.HasPrefix(result, "explicit_") {
				t.Errorf("String() should start with explicit_, got %q", result)
			}
		})
	}
}

// TestExplicitIDEnvironmentVarPriority verifies TD_SESSION_ID env var handling
func TestExplicitIDEnvironmentVarPriority(t *testing.T) {
	tests := []struct {
		name              string
		sessionID         string
		shouldHaveExplicit bool
	}{
		{
			name:              "non-empty TD_SESSION_ID is used",
			sessionID:         "env-session-id",
			shouldHaveExplicit: true,
		},
		{
			name:              "whitespace-only TD_SESSION_ID treated as empty",
			sessionID:         "   ",
			shouldHaveExplicit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Unsetenv("TD_SESSION_ID")
			os.Unsetenv("CURSOR_AGENT")

			if tt.sessionID != "" {
				t.Setenv("TD_SESSION_ID", tt.sessionID)
			}

			fp := GetAgentFingerprint()

			if tt.shouldHaveExplicit {
				if fp.ExplicitID == "" {
					t.Errorf("ExplicitID should be set for %q", tt.sessionID)
				}
				if fp.Type != "explicit" {
					t.Errorf("Type should be explicit, got %q", fp.Type)
				}
			}
		})
	}
}

// TestExplicitIDEdgeCases tests various edge cases for ExplicitID
func TestExplicitIDEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		pidValue   int
		typeValue  AgentType
		expectedLen int
		description string
	}{
		{
			name:        "single character ID",
			sessionID:   "a",
			typeValue:   "explicit",
			expectedLen: len("explicit_a"),
			description: "single character",
		},
		{
			name:        "ID with only hyphens",
			sessionID:   "---",
			typeValue:   "explicit",
			expectedLen: len("explicit_---"),
			description: "only hyphens",
		},
		{
			name:        "ID with only underscores",
			sessionID:   "___",
			typeValue:   "explicit",
			expectedLen: len("explicit____"),
			description: "only underscores",
		},
		{
			name:        "ID with leading special char",
			sessionID:   "!important",
			typeValue:   "explicit",
			expectedLen: len("explicit__important"),
			description: "leading special char",
		},
		{
			name:        "ID with trailing special char",
			sessionID:   "important!",
			typeValue:   "explicit",
			expectedLen: len("explicit_important_"),
			description: "trailing special char",
		},
		{
			name:        "all special characters",
			sessionID:   "!@#$%^&*()",
			typeValue:   "explicit",
			expectedLen: len("explicit__________"),
			description: "all special characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			fp := AgentFingerprint{
				Type:       tt.typeValue,
				PID:        tt.pidValue,
				ExplicitID: tt.sessionID,
			}
			result := fp.String()

			// Verify result is not empty
			if result == "" {
				t.Errorf("String() returned empty result")
			}

			// Verify result starts with explicit_
			if !strings.HasPrefix(result, "explicit_") {
				t.Errorf("String() should start with explicit_, got %q", result)
			}

			// Verify length doesn't exceed limits
			if len(result) > (len("explicit_") + 32) {
				t.Errorf("String() length = %d, want max %d", len(result), len("explicit_")+32)
			}
		})
	}
}
