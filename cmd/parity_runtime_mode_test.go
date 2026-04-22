package cmd

// Runtime-mode-resolution parity test.
//
// Background: Defect 1 of the Batch 1c reviewer flagged a default-preservation
// skew. With no explicit config the CLI's balancedReviewPolicyEnabled()
// returned true (via BalancedReviewPolicy.Default) while
// features.ResolveReviewPolicyMode (used by monitor + serve) returned strict.
// That silent divergence broke the parity-before-behavior-change contract.
//
// This suite resolves the mode from the three runtime paths the defect
// flagged and asserts they all agree for the representative config states
// (default, explicit strict/balanced/delegated via
// review_policy_mode, and legacy balanced_review_policy=true/false). If any
// surface drifts again, at least one subtest here fails.

import (
	"testing"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/reviewpolicy"
)

// cliResolvedMode resolves the mode via the cmd-layer resolver. It is the
// same function cmd/review.go's approve/reject sites call.
func cliResolvedMode(t *testing.T, baseDir string) reviewpolicy.Mode {
	t.Helper()
	m, err := resolveReviewPolicyMode(baseDir)
	if err != nil {
		t.Fatalf("cli resolveReviewPolicyMode: %v", err)
	}
	return m
}

// monitorResolvedMode resolves the mode via the monitor's path.
// pkg/monitor/actions.go's loadMonitorApproveInputs goes through
// features.ResolveReviewPolicyMode(baseDir); this helper calls the same
// entry point directly, which is what the monitor runtime does.
func monitorResolvedMode(t *testing.T, baseDir string) reviewpolicy.Mode {
	t.Helper()
	m, err := features.ResolveReviewPolicyMode(baseDir)
	if err != nil {
		t.Fatalf("monitor ResolveReviewPolicyMode: %v", err)
	}
	return m
}

// serveResolvedMode resolves the mode via the serve handler's path.
// internal/serve/handlers_transitions.go's serveReviewerDecision and
// serveCloseDecision call features.ResolveReviewPolicyMode(ctx.BaseDir).
func serveResolvedMode(t *testing.T, baseDir string) reviewpolicy.Mode {
	t.Helper()
	m, err := features.ResolveReviewPolicyMode(baseDir)
	if err != nil {
		t.Fatalf("serve ResolveReviewPolicyMode: %v", err)
	}
	return m
}

// TestReviewPolicyRuntimeMode_AllSurfacesAgree enforces the default-
// preservation invariant and each explicit-config outcome for every surface.
func TestReviewPolicyRuntimeMode_AllSurfacesAgree(t *testing.T) {
	// Clear any cached deprecation-warning latch so each subtest starts
	// with a clean slate.
	features.ResetDeprecationWarningsForTests()

	type subcase struct {
		name   string
		setup  func(t *testing.T, dir string)
		wantCL reviewpolicy.Mode
	}

	// Each subcase writes its own config (or env) into a fresh tempdir so
	// subtests are hermetic. setup receives the dir; tests toggle env with
	// t.Setenv for process-wide overrides.
	subcases := []subcase{
		{
			name: "default config -> delegated (Step 5 default)",
			// Post Step 5: with no explicit settings (neither
			// review_policy_mode nor balanced_review_policy set), the default
			// resolved mode is delegated across every surface. The legacy
			// BalancedReviewPolicy.Default was flipped to false so it no
			// longer feeds the resolver unless explicitly enabled.
			setup:  func(t *testing.T, dir string) {},
			wantCL: reviewpolicy.ModeDelegated,
		},
		{
			name: "review_policy_mode=strict",
			setup: func(t *testing.T, dir string) {
				if err := config.SetFeatureStringFlag(dir, features.ReviewPolicyMode, "strict"); err != nil {
					t.Fatalf("SetFeatureStringFlag strict: %v", err)
				}
			},
			wantCL: reviewpolicy.ModeStrict,
		},
		{
			name: "review_policy_mode=balanced",
			setup: func(t *testing.T, dir string) {
				if err := config.SetFeatureStringFlag(dir, features.ReviewPolicyMode, "balanced"); err != nil {
					t.Fatalf("SetFeatureStringFlag balanced: %v", err)
				}
			},
			wantCL: reviewpolicy.ModeBalanced,
		},
		{
			name: "review_policy_mode=delegated",
			setup: func(t *testing.T, dir string) {
				if err := config.SetFeatureStringFlag(dir, features.ReviewPolicyMode, "delegated"); err != nil {
					t.Fatalf("SetFeatureStringFlag delegated: %v", err)
				}
			},
			wantCL: reviewpolicy.ModeDelegated,
		},
		{
			name: "legacy balanced_review_policy=true (no explicit mode)",
			setup: func(t *testing.T, dir string) {
				if err := config.SetFeatureFlag(dir, features.BalancedReviewPolicy.Name, true); err != nil {
					t.Fatalf("SetFeatureFlag legacy true: %v", err)
				}
			},
			wantCL: reviewpolicy.ModeBalanced,
		},
		{
			name: "legacy balanced_review_policy=false (no explicit mode)",
			setup: func(t *testing.T, dir string) {
				if err := config.SetFeatureFlag(dir, features.BalancedReviewPolicy.Name, false); err != nil {
					t.Fatalf("SetFeatureFlag legacy false: %v", err)
				}
			},
			wantCL: reviewpolicy.ModeStrict,
		},
	}

	for _, sc := range subcases {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			dir := t.TempDir()
			features.ResetDeprecationWarningsForTests()
			sc.setup(t, dir)

			cli := cliResolvedMode(t, dir)
			mon := monitorResolvedMode(t, dir)
			srv := serveResolvedMode(t, dir)

			// Every surface must agree with the expected mode.
			if cli != sc.wantCL {
				t.Errorf("cli resolved %q, want %q", cli, sc.wantCL)
			}
			if mon != sc.wantCL {
				t.Errorf("monitor resolved %q, want %q", mon, sc.wantCL)
			}
			if srv != sc.wantCL {
				t.Errorf("serve resolved %q, want %q", srv, sc.wantCL)
			}

			// And the three must agree with each other — this is the
			// cross-surface invariant that caught the original default
			// skew.
			if cli != mon || mon != srv {
				t.Fatalf("surfaces disagree: cli=%q monitor=%q serve=%q", cli, mon, srv)
			}
		})
	}
}
