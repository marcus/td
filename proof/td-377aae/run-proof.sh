#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$ROOT/proof/td-377aae"
BIN="$ROOT/td"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/td-377aae-proof.XXXXXX")"

cleanup() {
  rm -rf "$TMP"
}
trap cleanup EXIT

mkdir -p "$OUT"
rm -f "$OUT"/*.txt "$OUT"/*.png "$OUT"/proof.html

capture() {
  local name="$1"
  shift
  "$@" >"$OUT/$name" 2>&1
}

capture init.txt env TD_SESSION_ID=ses_impl "$BIN" -w "$TMP" init
capture create-happy.txt env TD_SESSION_ID=ses_impl "$BIN" -w "$TMP" create "Proof stale transition workflow"
ISSUE_ID="$(sed -n 's/^CREATED //p' "$OUT/create-happy.txt" | head -n1)"
capture start-happy.txt env TD_SESSION_ID=ses_impl "$BIN" -w "$TMP" start "$ISSUE_ID"
capture handoff-happy.txt env TD_SESSION_ID=ses_impl "$BIN" -w "$TMP" handoff "$ISSUE_ID" --done "Captured reviewer handoff context"
capture review-happy.txt env TD_SESSION_ID=ses_impl "$BIN" -w "$TMP" review "$ISSUE_ID"
capture approve-happy.txt env TD_SESSION_ID=ses_reviewer "$BIN" -w "$TMP" approve "$ISSUE_ID"
capture show-happy.txt env TD_SESSION_ID=ses_reviewer "$BIN" -w "$TMP" show "$ISSUE_ID"

capture db-stale-test.txt go test ./internal/db -run TestUpdateIssueLoggedIfStatusDetectsStaleTransition -v
capture cmd-stale-test.txt go test ./cmd -run TestDescribeStaleTransitionUpdate -v
capture guidance-tests.txt go test ./cmd -run 'TestCloseFollowupGuidance|TestReviewFollowupGuidance|TestApproveFollowupGuidance' -v

python3 - "$OUT" "$ISSUE_ID" <<'PY'
from html import escape
from pathlib import Path
import sys

out = Path(sys.argv[1])
issue_id = sys.argv[2]

def read(name: str) -> str:
    return (out / name).read_text()

sections = {
    "init": read("init.txt"),
    "create": read("create-happy.txt"),
    "start": read("start-happy.txt"),
    "handoff": read("handoff-happy.txt"),
    "review": read("review-happy.txt"),
    "approve": read("approve-happy.txt"),
    "show": read("show-happy.txt"),
    "db_stale": read("db-stale-test.txt"),
    "cmd_stale": read("cmd-stale-test.txt"),
    "guidance": read("guidance-tests.txt"),
}

html = f"""<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>td-377aae proof</title>
<style>
:root {{
  color-scheme: light;
  --bg: #f6f3ea;
  --card: #fffdf8;
  --ink: #1e2725;
  --muted: #55605c;
  --accent: #0f766e;
  --accent-soft: rgba(15, 118, 110, 0.10);
  --border: #d9e3de;
}}
* {{ box-sizing: border-box; }}
body {{
  margin: 32px;
  font-family: "Avenir Next", "Segoe UI", sans-serif;
  color: var(--ink);
  background:
    radial-gradient(circle at top left, rgba(15, 118, 110, 0.14), transparent 28%),
    linear-gradient(180deg, #faf8f2 0%, var(--bg) 100%);
}}
h1, h2 {{
  margin: 0;
  font-family: "Iowan Old Style", "Palatino Linotype", serif;
  font-weight: 700;
}}
h1 {{
  font-size: 40px;
  margin-bottom: 10px;
}}
p {{
  max-width: 960px;
  line-height: 1.55;
}}
.lede {{
  color: var(--muted);
  margin-bottom: 24px;
}}
.grid {{
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(360px, 1fr));
  gap: 22px;
  margin-top: 22px;
}}
.card {{
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 20px;
  box-shadow: 0 20px 44px rgba(25, 42, 38, 0.08);
  padding: 22px;
}}
.eyebrow {{
  font-size: 12px;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--accent);
  margin-bottom: 10px;
}}
.summary {{
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 16px;
  margin-top: 24px;
}}
.pill {{
  background: var(--accent-soft);
  border: 1px solid rgba(15, 118, 110, 0.12);
  border-radius: 999px;
  padding: 12px 16px;
  font-weight: 600;
}}
pre {{
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  font: 13px/1.55 "SFMono-Regular", Menlo, monospace;
}}
code {{
  font-family: "SFMono-Regular", Menlo, monospace;
}}
</style>
</head>
<body>
<h1>td-377aae proof</h1>
<p class="lede">This proof combines two things: the happy-path CLI flow still works end to end for <code>{escape(issue_id)}</code>, and the new stale-transition protection is exercised by deterministic tests that capture the exact messages users now see when a concurrent status change makes their local transition stale.</p>

<section class="summary">
  <div class="pill">Happy path: create → start → handoff → review → approve</div>
  <div class="pill">DB guard: refuses stale status write and preserves the newer state</div>
  <div class="pill">CLI wording: explains the concurrent status change and the next command</div>
</section>

<div class="grid">
  <section class="card">
    <div class="eyebrow">Happy Path</div>
    <h2>Review command still works normally</h2>
    <pre>{escape(sections["review"])}</pre>
  </section>

  <section class="card">
    <div class="eyebrow">Happy Path</div>
    <h2>Approve still closes the reviewed issue</h2>
    <pre>{escape(sections["approve"])}</pre>
  </section>
</div>

<div class="grid" id="stale">
  <section class="card">
    <div class="eyebrow">Stale Guard</div>
    <h2>DB rejects a stale concurrent transition</h2>
    <pre>{escape(sections["db_stale"])}</pre>
  </section>

  <section class="card">
    <div class="eyebrow">User Message</div>
    <h2>CLI guidance names the new status and next step</h2>
    <pre>{escape(sections["cmd_stale"])}</pre>
  </section>
</div>

<div class="grid">
  <section class="card">
    <div class="eyebrow">Follow-up Guidance</div>
    <h2>Closed-state guidance stays explicit</h2>
    <pre>{escape(sections["guidance"])}</pre>
  </section>

  <section class="card">
    <div class="eyebrow">Final State</div>
    <h2>Happy-path issue ends closed with history intact</h2>
    <pre>{escape(sections["show"])}</pre>
  </section>
</div>
</body>
</html>
"""

(out / "proof.html").write_text(html)
PY

if ! command -v playwright >/dev/null 2>&1; then
  echo "playwright is required to capture proof screenshots" >&2
  exit 1
fi

PROOF_URL="file://$OUT/proof.html"
playwright screenshot --device "Desktop Chrome HiDPI" --wait-for-timeout 750 --full-page "$PROOF_URL" "$OUT/proof-full.png" >/dev/null
playwright screenshot --device "Desktop Chrome HiDPI" --wait-for-timeout 750 "$PROOF_URL" "$OUT/proof-normal.png" >/dev/null
playwright screenshot --device "Desktop Chrome HiDPI" --wait-for-timeout 750 "$PROOF_URL#stale" "$OUT/proof-stale.png" >/dev/null
