# `td depscan` — Dependency Risk Scanner

`td depscan` analyzes the current project's Go module dependencies for security
and maintenance risks. It parses `go.mod` (via `go mod edit -json`) and `go.sum`,
applies a set of offline static heuristics, and can optionally layer in
`govulncheck` and `go list -m -u` for known-CVE and outdated-module detection.

The command lives in the **System Commands** group and takes no positional
arguments.

```
td depscan [flags]
```

## Flags

| Flag | Description |
| --- | --- |
| `--json` | Emit the full report as JSON instead of the human-readable view. |
| `--severity <high\|medium\|low>` | Minimum severity to report. Defaults to `low` (everything). |
| `--check-updates` | Run `go list -m -u -json all` to flag outdated modules. Requires network access. |
| `--vuln` | Run `govulncheck -json ./...` for known CVEs. Requires `govulncheck` on `PATH`. |

## Risk heuristics

### Static (always run, fully offline)

| Category | Severity | What it flags |
| --- | --- | --- |
| `pseudo-version` | medium (direct) / low (indirect) | A dependency pinned to an untagged commit (e.g. `v0.0.0-20250623103423-23b8fd6302d7`). Untagged commits are not part of a released, reviewed version. |
| `pre-1.0` | medium (direct) / low (indirect) | A `v0.x` module. Pre-1.0 modules make no API-stability promise, so upgrades can break unexpectedly. |
| `incompatible` | medium | A `+incompatible` version — the module is at `v2+` but has not adopted Go modules' major-version import path, so the toolchain cannot reason about its compatibility. |
| `replace-directive` | high (local path) / medium (module) | A `replace` directive. A directive pointing at a local filesystem path is **high** severity because the build is not reproducible outside the current checkout; a module-to-module replacement is **medium** (supply-chain override risk). |
| `indirect-surface` | medium (≥60, or >5× direct) / low (≥30) | An unusually large transitive dependency graph. More indirect dependencies means more code to audit and a larger supply-chain attack surface. |
| `go-directive` | medium (≥4 behind) / low (≥2 behind) | The `go` directive in `go.mod` lags the building toolchain by several minor versions, so language and standard-library security fixes may not be in use. |

### Dynamic (opt-in, degrade gracefully)

- **`--vuln`** runs `govulncheck` and parses its streamed JSON output. Each
  distinct OSV advisory that affects the build is reported as a **high**
  severity `vulnerability` finding. If `govulncheck` is not installed, or it
  produces no output (e.g. offline with no cached vulnerability database), the
  scan is skipped and a note is added to the report rather than failing.
- **`--check-updates`** runs `go list -m -u -json all` and reports every module
  with a newer version available as a **low** severity `outdated` finding. If
  the module graph cannot be loaded (e.g. offline), the check is skipped with a
  note.

Skipped dynamic checks appear under `notes` in JSON output and as warnings in
the human-readable view; they never cause the command to error.

## Output

The human-readable report prints a summary header (module, `go.mod` path, `go`
directive, dependency counts, `go.sum` verified-module count, and finding totals
by severity), followed by findings grouped under `HIGH` / `MEDIUM` / `LOW`
sections. Findings are severity-ranked.

With `--json`, the command prints a single JSON object:

```json
{
  "module": "github.com/marcus/td",
  "go_mod_path": "/path/to/go.mod",
  "go_directive": "1.25.5",
  "summary": {
    "total": 36, "high": 0, "medium": 11, "low": 25,
    "direct_deps": 16, "indirect_deps": 40, "go_sum_modules": 80,
    "vuln_checked": false, "updates_checked": false
  },
  "findings": [
    {
      "severity": "medium",
      "category": "pseudo-version",
      "module": "github.com/charmbracelet/bubbles",
      "version": "v0.21.1-0.20250623103423-23b8fd6302d7",
      "detail": "direct dependency pinned to an untagged commit (pseudo-version)"
    }
  ],
  "notes": ["govulncheck not found on PATH; skipped known-CVE scan ..."]
}
```

## Exit & usage behavior

`td depscan` exits non-zero only on an operational error — for example, when no
`go.mod` can be found from the working directory, when `go mod edit -json` fails,
or when an invalid `--severity` value is passed. The **presence of findings does
not change the exit code**: a scan that surfaces high-severity risks still exits
`0`. This keeps the command safe to run in informational contexts; gate CI on
the JSON output (`summary.high`, `summary.total`) if you need a hard failure.
