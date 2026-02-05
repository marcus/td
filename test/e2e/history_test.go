package e2e_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/marcus/td/test/e2e"
)

func TestHistoryRecord(t *testing.T) {
	h := e2e.NewOperationHistory()

	h.Record(e2e.OperationRecord{
		Action:   "create",
		Actor:    "alice",
		TargetID: "td-abc123",
		Args:     []string{"create", "test issue"},
		Output:   "created td-abc123",
		Result:   "ok",
		Duration: 50 * time.Millisecond,
	})

	h.Record(e2e.OperationRecord{
		Action:   "update",
		Actor:    "bob",
		TargetID: "td-abc123",
		Args:     []string{"update", "td-abc123", "-t", "renamed"},
		Output:   "updated",
		Result:   "ok",
		Duration: 30 * time.Millisecond,
	})

	if h.Len() != 2 {
		t.Fatalf("expected 2 records, got %d", h.Len())
	}

	recs := h.Records()
	if recs[0].Seq != 1 || recs[1].Seq != 2 {
		t.Fatalf("unexpected seq: %d, %d", recs[0].Seq, recs[1].Seq)
	}
	if recs[0].Timestamp.IsZero() {
		t.Fatal("timestamp not auto-set")
	}
}

func TestHistoryFilter(t *testing.T) {
	h := e2e.NewOperationHistory()

	h.Record(e2e.OperationRecord{Action: "create", Actor: "alice", TargetID: "td-001", Result: "ok"})
	h.Record(e2e.OperationRecord{Action: "sync", Actor: "alice", Result: "ok"})
	h.Record(e2e.OperationRecord{Action: "update", Actor: "bob", TargetID: "td-001", Result: "ok"})
	h.Record(e2e.OperationRecord{Action: "create", Actor: "bob", TargetID: "td-002", Result: "expected_fail", Error: "conflict"})
	h.Record(e2e.OperationRecord{Action: "delete", Actor: "carol", TargetID: "td-001", Result: "ok"})

	// ForActor
	alice := h.ForActor("alice")
	if len(alice) != 2 {
		t.Fatalf("alice: expected 2, got %d", len(alice))
	}

	bob := h.ForActor("bob")
	if len(bob) != 2 {
		t.Fatalf("bob: expected 2, got %d", len(bob))
	}

	// ForIssue
	issue1 := h.ForIssue("td-001")
	if len(issue1) != 3 {
		t.Fatalf("td-001: expected 3, got %d", len(issue1))
	}

	issue2 := h.ForIssue("td-002")
	if len(issue2) != 1 {
		t.Fatalf("td-002: expected 1, got %d", len(issue2))
	}

	// Custom filter
	fails := h.Filter(func(r e2e.OperationRecord) bool {
		return r.Result == "expected_fail"
	})
	if len(fails) != 1 {
		t.Fatalf("expected_fail: expected 1, got %d", len(fails))
	}
}

func TestHistorySummary(t *testing.T) {
	h := e2e.NewOperationHistory()

	h.Record(e2e.OperationRecord{Action: "create", Actor: "alice", TargetID: "td-001", Result: "ok", Duration: 10 * time.Millisecond})
	h.Record(e2e.OperationRecord{Action: "sync", Actor: "alice", Result: "ok", Duration: 100 * time.Millisecond})
	h.Record(e2e.OperationRecord{Action: "update", Actor: "bob", TargetID: "td-001", Result: "ok", Duration: 20 * time.Millisecond})
	h.Record(e2e.OperationRecord{Action: "create", Actor: "bob", TargetID: "td-002", Result: "unexpected_fail", Duration: 5 * time.Millisecond, Error: "timeout"})

	s := h.Summary()

	if s.TotalOps != 4 {
		t.Fatalf("total: expected 4, got %d", s.TotalOps)
	}
	if s.UniqueIssues != 2 {
		t.Fatalf("issues: expected 2, got %d", s.UniqueIssues)
	}
	if s.ByResult["ok"] != 3 {
		t.Fatalf("ok count: expected 3, got %d", s.ByResult["ok"])
	}
	if s.ByResult["unexpected_fail"] != 1 {
		t.Fatalf("unexpected_fail count: expected 1, got %d", s.ByResult["unexpected_fail"])
	}
	if s.ByAction["create"] != 2 {
		t.Fatalf("create count: expected 2, got %d", s.ByAction["create"])
	}
	if s.ByActor["alice"] != 2 {
		t.Fatalf("alice count: expected 2, got %d", s.ByActor["alice"])
	}
	if s.MaxDuration != 100*time.Millisecond {
		t.Fatalf("max duration: expected 100ms, got %s", s.MaxDuration)
	}
	expectedTotal := 135 * time.Millisecond
	if s.TotalDuration != expectedTotal {
		t.Fatalf("total duration: expected %s, got %s", expectedTotal, s.TotalDuration)
	}
	expectedAvg := expectedTotal / 4
	if s.AvgDuration != expectedAvg {
		t.Fatalf("avg duration: expected %s, got %s", expectedAvg, s.AvgDuration)
	}
}

func TestHistoryWriteJSON(t *testing.T) {
	h := e2e.NewOperationHistory()

	h.Record(e2e.OperationRecord{
		Action:   "create",
		Actor:    "alice",
		TargetID: "td-001",
		Args:     []string{"create", "hello"},
		Output:   "created td-001",
		Result:   "ok",
		Duration: 42 * time.Millisecond,
	})
	h.Record(e2e.OperationRecord{
		Action:   "sync",
		Actor:    "bob",
		Result:   "ok",
		Duration: 200 * time.Millisecond,
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "history.json")

	if err := h.WriteJSON(path); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var records []e2e.OperationRecord
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records in JSON, got %d", len(records))
	}
	if records[0].Action != "create" || records[0].Actor != "alice" {
		t.Fatalf("unexpected first record: %+v", records[0])
	}
	if records[1].Action != "sync" || records[1].Actor != "bob" {
		t.Fatalf("unexpected second record: %+v", records[1])
	}
}

func TestHistoryWriteReport(t *testing.T) {
	h := e2e.NewOperationHistory()

	h.Record(e2e.OperationRecord{Action: "create", Actor: "alice", TargetID: "td-001", Result: "ok", Duration: 10 * time.Millisecond})
	h.Record(e2e.OperationRecord{Action: "sync", Actor: "bob", Result: "ok", Duration: 50 * time.Millisecond})
	h.Record(e2e.OperationRecord{Action: "update", Actor: "carol", TargetID: "td-001", Result: "unexpected_fail", Duration: 5 * time.Millisecond, Error: "timeout"})

	var buf bytes.Buffer
	h.WriteReport(&buf)
	report := buf.String()

	// Verify key sections present
	for _, substr := range []string{
		"Operation History Report",
		"Total operations: 3",
		"Unique issues:    1",
		"Results",
		"Actions",
		"Actors",
		"Recent Operations",
		"alice",
		"bob",
		"carol",
		"ok",
		"unexpected_fail",
		"err=timeout",
	} {
		if !bytes.Contains([]byte(report), []byte(substr)) {
			t.Errorf("report missing %q", substr)
		}
	}
}

func TestHistoryConcurrency(t *testing.T) {
	h := e2e.NewOperationHistory()
	const goroutines = 10
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := range goroutines {
		go func(id int) {
			defer wg.Done()
			actor := []string{"alice", "bob", "carol"}[id%3]
			for i := range opsPerGoroutine {
				h.Record(e2e.OperationRecord{
					Action:   "create",
					Actor:    actor,
					TargetID: "td-conc",
					Result:   "ok",
					Duration: time.Duration(i) * time.Microsecond,
				})
			}
		}(g)
	}
	wg.Wait()

	total := goroutines * opsPerGoroutine
	if h.Len() != total {
		t.Fatalf("expected %d records, got %d", total, h.Len())
	}

	// Verify seq numbers are unique and sequential
	recs := h.Records()
	seen := make(map[int]bool)
	for _, r := range recs {
		if seen[r.Seq] {
			t.Fatalf("duplicate seq %d", r.Seq)
		}
		seen[r.Seq] = true
	}
	if len(seen) != total {
		t.Fatalf("expected %d unique seqs, got %d", total, len(seen))
	}

	// Summary should work on concurrent data
	s := h.Summary()
	if s.TotalOps != total {
		t.Fatalf("summary total: expected %d, got %d", total, s.TotalOps)
	}
}

func TestHistoryEmptySummary(t *testing.T) {
	h := e2e.NewOperationHistory()
	s := h.Summary()

	if s.TotalOps != 0 {
		t.Fatalf("expected 0 ops, got %d", s.TotalOps)
	}
	if s.AvgDuration != 0 {
		t.Fatalf("expected 0 avg duration, got %s", s.AvgDuration)
	}
	if s.UniqueIssues != 0 {
		t.Fatalf("expected 0 unique issues, got %d", s.UniqueIssues)
	}
}
