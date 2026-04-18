package cmd

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
)

type commandFlagSnapshot struct {
	value   string
	changed bool
}

func saveAndRestoreCommandFlags(t *testing.T, cmd *cobra.Command, names ...string) {
	t.Helper()

	flags := cmd.Flags()
	snapshots := make(map[string]commandFlagSnapshot, len(names))
	for _, name := range names {
		flag := flags.Lookup(name)
		if flag == nil {
			t.Fatalf("flag %q not found", name)
		}
		snapshots[name] = commandFlagSnapshot{
			value:   flag.Value.String(),
			changed: flag.Changed,
		}
		if err := flag.Value.Set(flag.DefValue); err != nil {
			t.Fatalf("reset flag %q: %v", name, err)
		}
		flag.Changed = false
	}

	t.Cleanup(func() {
		for _, name := range names {
			flag := flags.Lookup(name)
			snapshot := snapshots[name]
			if err := flag.Value.Set(snapshot.value); err != nil {
				t.Fatalf("restore flag %q: %v", name, err)
			}
			flag.Changed = snapshot.changed
		}
	})
}

func replaceStdinWithFile(t *testing.T, content string) {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "stdin-*")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	if _, err := file.WriteString(content); err != nil {
		t.Fatalf("WriteString failed: %v", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatalf("Seek failed: %v", err)
	}

	oldStdin := os.Stdin
	os.Stdin = file
	t.Cleanup(func() {
		os.Stdin = oldStdin
		file.Close()
	})
}
