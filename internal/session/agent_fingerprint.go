package session

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// AgentType identifies the type of AI agent
type AgentType string

const (
	AgentClaudeCode AgentType = "claude-code"
	AgentCursor     AgentType = "cursor"
	AgentCodex      AgentType = "codex"
	AgentWindsurf   AgentType = "windsurf"
	AgentZed        AgentType = "zed"
	AgentAider      AgentType = "aider"
	AgentCopilot    AgentType = "copilot"
	AgentGemini     AgentType = "gemini"
	AgentUnknown    AgentType = "unknown"
	AgentTerminal   AgentType = "terminal"
)

// AgentFingerprint identifies a specific agent session
type AgentFingerprint struct {
	Type       AgentType
	PID        int
	ExplicitID string // set when TD_SESSION_ID is used
}

// String returns a filesystem-safe string representation
func (af AgentFingerprint) String() string {
	if af.ExplicitID != "" {
		// Use hash of explicit ID for filesystem-safe name
		return fmt.Sprintf("explicit_%s", sanitizeForFilename(af.ExplicitID))
	}
	if af.PID > 0 {
		return fmt.Sprintf("%s_%d", af.Type, af.PID)
	}
	return string(af.Type)
}

// sanitizeForFilename makes a string safe for use in filenames
func sanitizeForFilename(s string) string {
	// Replace problematic characters
	result := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, s)
	// Limit length
	if len(result) > 32 {
		result = result[:32]
	}
	return result
}

// agentPatterns maps process name substrings to agent types
var agentPatterns = map[string]AgentType{
	"claude":   AgentClaudeCode,
	"cursor":   AgentCursor,
	"codex":    AgentCodex,
	"windsurf": AgentWindsurf,
	"zed":      AgentZed,
	"aider":    AgentAider,
	"copilot":  AgentCopilot,
	"gemini":   AgentGemini,
}

// Cache the expensive process tree walk (won't change during process lifetime)
var (
	cachedAncestor     AgentFingerprint
	cachedAncestorOnce sync.Once
)

// GetAgentFingerprint detects the agent running this process by walking the process tree.
// The expensive process tree walk is cached; env var checks are always re-evaluated.
func GetAgentFingerprint() AgentFingerprint {
	// Priority 1: Explicit override (cheap env check, always re-evaluate)
	if id := os.Getenv("TD_SESSION_ID"); id != "" {
		return AgentFingerprint{Type: AgentType("explicit"), PID: 0, ExplicitID: id}
	}

	// Priority 2: Agent-provided session IDs (cheap env check)
	if os.Getenv("CURSOR_AGENT") != "" {
		return AgentFingerprint{Type: AgentCursor, PID: os.Getppid()}
	}

	// Priority 3: Walk process ancestry (expensive, cached)
	cachedAncestorOnce.Do(func() {
		cachedAncestor = detectAgentAncestor()
	})
	if cachedAncestor.Type != AgentUnknown {
		return cachedAncestor
	}

	// Priority 4: Terminal session detection (cheap env check)
	if getTerminalSessionID() != "" {
		return AgentFingerprint{Type: AgentTerminal, PID: 0}
	}

	return AgentFingerprint{Type: AgentUnknown, PID: 0}
}

// detectAgentAncestor walks up the process tree looking for known agent processes
func detectAgentAncestor() AgentFingerprint {
	pid := os.Getppid()

	for depth := 0; depth < 15; depth++ {
		name, ppid, err := getProcessInfo(pid)
		if err != nil {
			break
		}

		nameLower := strings.ToLower(name)
		for pattern, agentType := range agentPatterns {
			if strings.Contains(nameLower, pattern) {
				return AgentFingerprint{Type: agentType, PID: pid}
			}
		}

		if ppid <= 1 {
			break
		}
		pid = ppid
	}

	return AgentFingerprint{Type: AgentUnknown, PID: 0}
}

// getProcessInfo returns process name and parent PID for a given PID
func getProcessInfo(pid int) (name string, ppid int, err error) {
	// Use ps command for cross-platform compatibility
	out, err := exec.Command("ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", 0, err
	}

	line := strings.TrimSpace(string(out))
	if line == "" {
		return "", 0, fmt.Errorf("process not found: %d", pid)
	}

	parts := strings.Fields(line)
	if len(parts) < 2 {
		return "", 0, fmt.Errorf("unexpected ps output: %s", line)
	}

	ppid, err = strconv.Atoi(parts[0])
	if err != nil {
		return "", 0, err
	}

	// Join remaining parts as command name (may contain spaces)
	name = strings.Join(parts[1:], " ")
	return name, ppid, nil
}

// getTerminalSessionID returns a terminal session identifier if available
func getTerminalSessionID() string {
	for _, env := range []string{
		"TERM_SESSION_ID",
		"TMUX_PANE",
		"STY",
		"WINDOWID",
		"KONSOLE_DBUS_SESSION",
		"GNOME_TERMINAL_SCREEN",
	} {
		if val := os.Getenv(env); val != "" {
			return val
		}
	}
	return ""
}
