# td-db1f2a Proof

Artifacts captured from a fresh temp project:

- `init.txt`
- `create-warning.txt`
- `start-warning.txt`
- `review-warning.txt`
- `show-warning.txt`
- `create-suppressed.txt`
- `start-suppressed.txt`
- `log-suppressed.txt`
- `review-suppressed.txt`
- `show-suppressed.txt`
- `proof.html`
- `proof-full.png`
- `proof-warning.png`
- `proof-suppressed.png`

What this proves:

- `td review` still auto-creates a minimal handoff when no handoff exists.
- The warning is retained when the issue only has routine workflow context.
- The warning is suppressed when substantive session context already exists.
- Both paths still leave the issue in `in_review` with the auto-generated handoff visible in `td show`.
