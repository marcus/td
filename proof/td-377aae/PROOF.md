# td-377aae Proof

Verified on March 29, 2026 with CLI output plus focused stale-transition tests.

CLI flow:

```text
$ ./td init -w /var/folders/9z/_hxsyhcx59d_cbrbhxfk9j000000gn/T/tmp.o8eIgE81lX
INITIALIZED /var/folders/9z/_hxsyhcx59d_cbrbhxfk9j000000gn/T/tmp.o8eIgE81lX/.todos
Added .todos/ to .gitignore
Session: ses_a2c8a3

$ ./td create "Review transition proof" --type task --priority P1 -w /var/folders/9z/_hxsyhcx59d_cbrbhxfk9j000000gn/T/tmp.o8eIgE81lX
CREATED td-58ae0e

$ ./td review td-58ae0e -w /var/folders/9z/_hxsyhcx59d_cbrbhxfk9j000000gn/T/tmp.o8eIgE81lX
Warning: auto-created minimal handoff for td-58ae0e - consider using 'td handoff' for better documentation
REVIEW REQUESTED td-58ae0e (session: ses_a2c8a3)

$ ./td show td-58ae0e -w /var/folders/9z/_hxsyhcx59d_cbrbhxfk9j000000gn/T/tmp.o8eIgE81lX
td-58ae0e: Review transition proof
Status: [in_review]
Type: task | Priority: P1

CURRENT HANDOFF (ses_a2c8a3, just now):
  Done:
    - Auto-generated for review submission

SESSION LOG:
  [17:42] Submitted for review

AWAITING REVIEW - requires different session to approve/reject

SESSIONS INVOLVED:
  ses_a2c8a3 (implementer)
```

Focused stale-transition coverage:

```text
$ go test ./cmd ./internal/db -run 'Test(SubmitIssueForReviewReportsStaleConcurrentTransition|StaleTransitionMessage|TransitionIssueLogged)' -v
=== RUN   TestSubmitIssueForReviewReportsStaleConcurrentTransition
--- PASS: TestSubmitIssueForReviewReportsStaleConcurrentTransition (0.14s)
=== RUN   TestStaleTransitionMessageIncludesGuidance
=== RUN   TestStaleTransitionMessageIncludesGuidance/review_points_blocked_issues_to_unblock
=== RUN   TestStaleTransitionMessageIncludesGuidance/approve_points_closed_issues_to_reopen
=== RUN   TestStaleTransitionMessageIncludesGuidance/nil_error_falls_back_to_generic_message
--- PASS: TestStaleTransitionMessageIncludesGuidance (0.00s)
    --- PASS: TestStaleTransitionMessageIncludesGuidance/review_points_blocked_issues_to_unblock (0.00s)
    --- PASS: TestStaleTransitionMessageIncludesGuidance/approve_points_closed_issues_to_reopen (0.00s)
    --- PASS: TestStaleTransitionMessageIncludesGuidance/nil_error_falls_back_to_generic_message (0.00s)
PASS
ok  	github.com/marcus/td/cmd	0.533s
=== RUN   TestTransitionIssueLogged
--- PASS: TestTransitionIssueLogged (0.10s)
=== RUN   TestTransitionIssueLogged_RejectsStaleStatusWithoutOverwritingCurrentRow
--- PASS: TestTransitionIssueLogged_RejectsStaleStatusWithoutOverwritingCurrentRow (0.17s)
PASS
ok  	github.com/marcus/td/internal/db	0.817s
```

What this proves:

- `td review` still performs the normal open/in-progress to `in_review` workflow.
- Review transitions now preserve the current row instead of overwriting a newer status from another session.
- Stale concurrent transitions produce explicit guidance, including unblock/reopen next steps where appropriate.

Notes:

- This repository does not include a browser screenshot harness for CLI-only tasks, so the proof artifact here is captured CLI and test output instead of browser screenshots.
