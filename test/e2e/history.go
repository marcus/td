package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"
)

// OperationRecord captures a single action performed during a chaos run.
type OperationRecord struct {
	Seq       int           `json:"seq"`
	Timestamp time.Time     `json:"timestamp"`
	Action    string        `json:"action"`
	Actor     string        `json:"actor"`
	TargetID  string        `json:"target_id,omitempty"`
	Args      []string      `json:"args"`
	Output    string        `json:"output"`
	Result    string        `json:"result"` // "ok", "expected_fail", "unexpected_fail", "skip"
	Duration  time.Duration `json:"duration_ns"`
	Error     string        `json:"error,omitempty"`
}

// HistorySummary holds aggregate stats over recorded operations.
type HistorySummary struct {
	TotalOps      int               `json:"total_ops"`
	ByResult      map[string]int    `json:"by_result"`
	ByAction      map[string]int    `json:"by_action"`
	ByActor       map[string]int    `json:"by_actor"`
	AvgDuration   time.Duration     `json:"avg_duration_ns"`
	MaxDuration   time.Duration     `json:"max_duration_ns"`
	TotalDuration time.Duration     `json:"total_duration_ns"`
	UniqueIssues  int               `json:"unique_issues"`
}

// OperationHistory records all operations performed during a chaos run.
// Thread-safe for concurrent recording from multiple goroutines.
type OperationHistory struct {
	mu      sync.Mutex
	records []OperationRecord
	seq     int
}

// NewOperationHistory creates an empty history.
func NewOperationHistory() *OperationHistory {
	return &OperationHistory{}
}

// Record adds an operation to the history. It assigns a monotonic sequence
// number and timestamps the record if Timestamp is zero.
func (h *OperationHistory) Record(rec OperationRecord) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	rec.Seq = h.seq
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now()
	}
	h.records = append(h.records, rec)
}

// Records returns a snapshot of all recorded operations.
func (h *OperationHistory) Records() []OperationRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]OperationRecord, len(h.records))
	copy(out, h.records)
	return out
}

// Len returns the number of recorded operations.
func (h *OperationHistory) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.records)
}

// Filter returns records matching a predicate.
func (h *OperationHistory) Filter(fn func(OperationRecord) bool) []OperationRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []OperationRecord
	for _, r := range h.records {
		if fn(r) {
			out = append(out, r)
		}
	}
	return out
}

// ForActor returns records for a specific actor.
func (h *OperationHistory) ForActor(actor string) []OperationRecord {
	return h.Filter(func(r OperationRecord) bool {
		return r.Actor == actor
	})
}

// ForIssue returns records targeting a specific issue ID.
func (h *OperationHistory) ForIssue(issueID string) []OperationRecord {
	return h.Filter(func(r OperationRecord) bool {
		return r.TargetID == issueID
	})
}

// Summary returns aggregate stats over all recorded operations.
func (h *OperationHistory) Summary() HistorySummary {
	h.mu.Lock()
	defer h.mu.Unlock()

	s := HistorySummary{
		TotalOps: len(h.records),
		ByResult: make(map[string]int),
		ByAction: make(map[string]int),
		ByActor:  make(map[string]int),
	}

	issues := make(map[string]struct{})
	for _, r := range h.records {
		s.ByResult[r.Result]++
		s.ByAction[r.Action]++
		s.ByActor[r.Actor]++
		s.TotalDuration += r.Duration
		if r.Duration > s.MaxDuration {
			s.MaxDuration = r.Duration
		}
		if r.TargetID != "" {
			issues[r.TargetID] = struct{}{}
		}
	}

	s.UniqueIssues = len(issues)
	if s.TotalOps > 0 {
		s.AvgDuration = s.TotalDuration / time.Duration(s.TotalOps)
	}
	return s
}

// WriteJSON writes the full history as JSON to the given path.
func (h *OperationHistory) WriteJSON(path string) error {
	h.mu.Lock()
	data, err := json.MarshalIndent(h.records, "", "  ")
	h.mu.Unlock()
	if err != nil {
		return fmt.Errorf("marshal history: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// WriteReport writes a human-readable summary to w.
func (h *OperationHistory) WriteReport(w io.Writer) {
	s := h.Summary()
	records := h.Records()

	fmt.Fprintf(w, "=== Operation History Report ===\n")
	fmt.Fprintf(w, "Total operations: %d\n", s.TotalOps)
	fmt.Fprintf(w, "Unique issues:    %d\n", s.UniqueIssues)
	fmt.Fprintf(w, "Total duration:   %s\n", s.TotalDuration)
	fmt.Fprintf(w, "Avg duration:     %s\n", s.AvgDuration)
	fmt.Fprintf(w, "Max duration:     %s\n\n", s.MaxDuration)

	// Results breakdown
	fmt.Fprintf(w, "--- Results ---\n")
	for _, result := range sortedKeys(s.ByResult) {
		fmt.Fprintf(w, "  %-20s %d\n", result, s.ByResult[result])
	}

	// Actions breakdown
	fmt.Fprintf(w, "\n--- Actions ---\n")
	for _, action := range sortedKeys(s.ByAction) {
		fmt.Fprintf(w, "  %-20s %d\n", action, s.ByAction[action])
	}

	// Actors breakdown
	fmt.Fprintf(w, "\n--- Actors ---\n")
	for _, actor := range sortedKeys(s.ByActor) {
		fmt.Fprintf(w, "  %-20s %d\n", actor, s.ByActor[actor])
	}

	// Recent operations (last 20)
	fmt.Fprintf(w, "\n--- Recent Operations (last 20) ---\n")
	start := 0
	if len(records) > 20 {
		start = len(records) - 20
	}
	for _, r := range records[start:] {
		errSuffix := ""
		if r.Error != "" {
			errSuffix = " err=" + r.Error
		}
		fmt.Fprintf(w, "  #%-4d %-8s %-10s %-12s %-8s %s%s\n",
			r.Seq, r.Actor, r.Action, r.TargetID, r.Result, r.Duration, errSuffix)
	}
}

// sortedKeys returns map keys in sorted order.
func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
