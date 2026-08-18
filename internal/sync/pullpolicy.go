package sync

import "fmt"

// PullOutcome is the decision about what to do with a batch of applied events.
//
// It lives here, not in cmd/, so every pull path reaches the same verdict: the
// batch pull (cmd/sync.go runPull), autosync (cmd/autosync.go), and the sync
// test harness. td-8fe2bc was a wedge in exactly this decision, and a wedge
// that only one of three call sites is protected against is not fixed.
type PullOutcome struct {
	// Abort is non-nil when the batch must be rolled back with the cursor
	// preserved, so a later pull can retry it. Only transient failures abort.
	Abort error
	// Record lists events that will not be applied and must be durably
	// recorded before the cursor advances past them.
	Record []SkippedEvent
}

// ResolvePullOutcome decides whether a batch aborts and what it leaves behind.
//
// A TRANSIENT failure aborts the batch: the event was fine and the environment
// was not, so preserving the cursor and retrying is correct.
//
// A PERMANENT failure does not abort. Retrying one reproduces it exactly, so
// aborting on it wedges the peer forever: every later pull replays the same
// batch, fails on the same event, rolls back, and no event behind it ever
// applies again. Those are quarantined instead — recorded with their error and
// server_seq, and stepped over. See IsPermanentApplyError for the rule that
// separates the two.
//
// Deliberate skips (an orphaned create, see OrphanedParentError) are never
// failures and never abort; they are recorded for the same reason: nothing is
// dropped without a trace.
func ResolvePullOutcome(result ApplyResult) PullOutcome {
	var out PullOutcome
	out.Record = append(out.Record, result.Skipped...)

	var transient []FailedEvent
	for _, f := range result.Failed {
		if !IsPermanentApplyError(f.Error) {
			transient = append(transient, f)
			continue
		}
		out.Record = append(out.Record, SkippedEvent{
			ServerSeq:  f.ServerSeq,
			DeviceID:   f.DeviceID,
			ActionType: f.ActionType,
			EntityType: f.EntityType,
			EntityID:   f.EntityID,
			Reason:     SkipReasonQuarantined,
			Detail:     f.Error.Error(),
			Payload:    f.Payload,
		})
	}

	if len(transient) > 0 {
		first := transient[0]
		out.Abort = fmt.Errorf("%d remote event(s) failed (first: seq=%d: %v); batch rolled back and sync cursor preserved",
			len(transient), first.ServerSeq, first.Error)
	}
	return out
}
