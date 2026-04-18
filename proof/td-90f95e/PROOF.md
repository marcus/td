# td-90f95e Proof

Verified with CLI output captured in this round:

- `init.txt`
- `create-epic.txt`
- `create-review-task.txt`
- `create-open-task.txt`
- `start-review-task.txt`
- `review-task.txt`
- `list-epic-dot-in-review.txt`
- `list-epic-dot-open-in-progress.txt`
- `close-guidance.txt`
- `status-json.txt`

What this proves:

- `td list --epic . -s in_review` resolves the active epic context without focus.
- `td list --epic . -s open,in_progress` also resolves the active epic context without focus.
- `td close` on an already-in-review task now points to `td approve` instead of sending the user back to review.
- `td status --json` returns machine-readable state for scripted lookups.

Notes:

- This repository does not include a browser screenshot harness for the CLI task, so the proof artifact here is command output.
