package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/db"
)

func TestTruncateID(t *testing.T) {
	tests := []struct {
		id   string
		max  int
		want string
	}{
		{"short", 16, "short"},
		{"exactly16chars!!", 16, "exactly16chars!!"},
		{"this-is-a-very-long-id-string", 16, "this-is-a-ver..."},
		{"abc", 10, "abc"},
		{"abcdefghij", 10, "abcdefghij"},
		{"abcdefghijk", 10, "abcdefg..."},
		{"", 10, ""},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%d", tt.id, tt.max), func(t *testing.T) {
			got := truncateID(tt.id, tt.max)
			if got != tt.want {
				t.Errorf("truncateID(%q, %d) = %q, want %q", tt.id, tt.max, got, tt.want)
			}
			if len(got) > tt.max {
				t.Errorf("truncateID(%q, %d) length %d exceeds max %d", tt.id, tt.max, len(got), tt.max)
			}
		})
	}
}

func TestPrintSyncEntry(t *testing.T) {
	tests := []struct {
		name     string
		entry    db.SyncHistoryEntry
		contains []string
	}{
		{
			name: "push entry",
			entry: db.SyncHistoryEntry{
				ID:         1,
				Direction:  "push",
				ActionType: "create",
				EntityType: "issues",
				EntityID:   "i_abc123",
				ServerSeq:  42,
				DeviceID:   "dev-1",
				Timestamp:  time.Date(2025, 1, 15, 10, 30, 45, 0, time.UTC),
			},
			contains: []string{"push", "issues", "i_abc123", "create", "seq:42", "10:30:45"},
		},
		{
			name: "pull entry with device",
			entry: db.SyncHistoryEntry{
				ID:         2,
				Direction:  "pull",
				ActionType: "update",
				EntityType: "logs",
				EntityID:   "l_def456",
				ServerSeq:  99,
				DeviceID:   "other-device-id",
				Timestamp:  time.Date(2025, 3, 20, 14, 5, 0, 0, time.UTC),
			},
			contains: []string{"pull", "logs", "l_def456", "update", "seq:99", "14:05:00", "from:other-dev..."},
		},
		{
			name: "pull entry without device",
			entry: db.SyncHistoryEntry{
				ID:         3,
				Direction:  "pull",
				ActionType: "delete",
				EntityType: "comments",
				EntityID:   "c_short",
				ServerSeq:  7,
				DeviceID:   "",
				Timestamp:  time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
			},
			contains:    []string{"pull", "comments", "c_short", "delete", "seq:7"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			old := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			printSyncEntry(tt.entry)

			w.Close()
			os.Stdout = old

			var buf bytes.Buffer
			io.Copy(&buf, r)
			output := buf.String()

			for _, s := range tt.contains {
				if !strings.Contains(output, s) {
					t.Errorf("output missing %q\ngot: %s", s, output)
				}
			}

			// Verify "from:" is absent when DeviceID is empty
			if tt.entry.DeviceID == "" && strings.Contains(output, "from:") {
				t.Errorf("output should not contain \"from:\" when DeviceID is empty\ngot: %s", output)
			}
		})
	}
}
