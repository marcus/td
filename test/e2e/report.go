package e2e

import "time"

// ChaosReport is the JSON-serializable report for CI integration.
type ChaosReport struct {
	Seed     int64           `json:"seed"`
	Actions  int             `json:"actions"`
	Duration int64           `json:"duration_ms"`
	Actors   int             `json:"actors"`
	Results  ReportResults   `json:"results"`
	PerAction map[string]ReportActionStats `json:"per_action"`
	Verifications []ReportVerification `json:"verifications"`
	SyncStats ReportSyncStats `json:"sync_stats"`
	Pass      bool           `json:"pass"`
}

// ReportResults aggregates action outcomes.
type ReportResults struct {
	Total      int `json:"total"`
	OK         int `json:"ok"`
	ExpFail    int `json:"expected_fail"`
	UnexpFail  int `json:"unexpected_fail"`
	Skipped    int `json:"skipped"`
}

// ReportActionStats tracks per-action-type outcomes.
type ReportActionStats struct {
	OK        int `json:"ok"`
	ExpFail   int `json:"expected_fail"`
	UnexpFail int `json:"unexpected_fail"`
}

// ReportVerification records the outcome of a single verification.
type ReportVerification struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Details string `json:"details,omitempty"`
}

// ReportSyncStats tracks sync operations.
type ReportSyncStats struct {
	Count int `json:"count"`
}

// BuildReport constructs a ChaosReport from engine and verifier state.
func BuildReport(seed int64, eng *ChaosEngine, v *Verifier, elapsed time.Duration) ChaosReport {
	// Aggregate per-action stats
	totalOK := 0
	perAction := make(map[string]ReportActionStats)
	for name, as := range eng.Stats.PerAction {
		perAction[name] = ReportActionStats{
			OK:        as.OK,
			ExpFail:   as.ExpFail,
			UnexpFail: as.UnexpFail,
		}
		totalOK += as.OK
	}

	// Map verifications
	var verifications []ReportVerification
	for _, r := range v.Results() {
		verifications = append(verifications, ReportVerification{
			Name:    r.Name,
			Passed:  r.Passed,
			Details: r.Details,
		})
	}

	return ChaosReport{
		Seed:     seed,
		Actions:  eng.Stats.ActionCount,
		Duration: elapsed.Milliseconds(),
		Actors:   eng.NumActors,
		Results: ReportResults{
			Total:     eng.Stats.ActionCount,
			OK:        totalOK,
			ExpFail:   eng.Stats.ExpectedFailures,
			UnexpFail: eng.Stats.UnexpectedFailures,
			Skipped:   eng.Stats.Skipped,
		},
		PerAction:     perAction,
		Verifications: verifications,
		SyncStats: ReportSyncStats{
			Count: eng.Stats.SyncCount,
		},
		Pass: v.AllPassed() && eng.Stats.UnexpectedFailures == 0,
	}
}
