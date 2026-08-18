package compat

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var testBinary string

func TestMain(m *testing.M) {
	// Build td binary once for all tests
	binDir, err := os.MkdirTemp("", "td-compat-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(binDir)

	bin := filepath.Join(binDir, "td-test")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	repoRoot := findRepoRootFromWd()
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build td binary: %v\n%s\n", err, out)
		os.Exit(1)
	}

	testBinary = bin
	os.Exit(m.Run())
}

func findRepoRootFromWd() string {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getwd: %v\n", err)
		os.Exit(1)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			fmt.Fprintf(os.Stderr, "could not find repo root (go.mod)\n")
			os.Exit(1)
		}
		dir = parent
	}
}

// expectedCommands is the set of top-level commands that must always exist.
// Removing a command from td is a backward-compatibility break.
var expectedCommands = []string{
	"approve",
	"block",
	"blocked",
	"blocked-by",
	"board",
	"close",
	"comment",
	"comments",
	"completion",
	"create",
	"critical-path",
	"debug-stats",
	"defer",
	"delete",
	"depends-on",
	"due",
	"epic",
	"errors",
	"export",
	"feature",
	"files",
	"handoff",
	"help",
	"import",
	"in-review",
	"info",
	"init",
	"last",
	"link",
	"list",
	"log",
	"monitor",
	"next",
	"note",
	"query",
	"reject",
	"reopen",
	"restore",
	"review",
	"search",
	"security",
	"serve",
	"show",
	"start",
	"stats",
	"task",
	"tree",
	"unblock",
	"undo",
	"unlink",
	"unstart",
	"update",
	"upgrade",
	"version",
	"workflow",
}

// expectedGlobalFlags are flags that must remain on the root command.
var expectedGlobalFlags = []string{
	"--work-dir",
	"--help",
	"--version",
}

// expectedAliases maps commands to aliases that must remain stable.
var expectedAliases = map[string][]string{
	"create":  {"add", "new"},
	"list":    {"ls"},
	"show":    {"context", "view", "get"},
	"close":   {"done", "complete"},
	"start":   {"begin"},
	"unstart": {"stop"},
	"review":  {"submit", "finish"},
}

func getHelpOutput(t *testing.T) string {
	t.Helper()
	out, err := exec.Command(testBinary, "help").CombinedOutput()
	if err != nil {
		t.Fatalf("td help: %v\n%s", err, out)
	}
	return string(out)
}

func TestCLI_CommandsExist(t *testing.T) {
	helpOutput := getHelpOutput(t)

	for _, cmd := range expectedCommands {
		if !strings.Contains(helpOutput, cmd) {
			t.Errorf("command %q missing from 'td help' output", cmd)
		}
	}
}

func TestCLI_GlobalFlags(t *testing.T) {
	helpOutput := getHelpOutput(t)

	for _, flag := range expectedGlobalFlags {
		if !strings.Contains(helpOutput, flag) {
			t.Errorf("global flag %q missing from 'td help' output", flag)
		}
	}
}

func TestCLI_Aliases(t *testing.T) {
	helpOutput := getHelpOutput(t)

	for cmd, aliases := range expectedAliases {
		for _, alias := range aliases {
			if !strings.Contains(helpOutput, alias) {
				t.Errorf("alias %q for command %q missing from 'td help' output", alias, cmd)
			}
		}
	}
}

func TestCLI_ShowHasFormatFlag(t *testing.T) {
	out, _ := exec.Command(testBinary, "show", "--help").CombinedOutput()
	if !strings.Contains(string(out), "--format") {
		t.Error("'td show --help' missing --format flag")
	}
}

func TestCLI_ListHasFormatFlag(t *testing.T) {
	out, _ := exec.Command(testBinary, "list", "--help").CombinedOutput()
	if !strings.Contains(string(out), "--format") {
		t.Error("'td list --help' missing --format flag")
	}
}
