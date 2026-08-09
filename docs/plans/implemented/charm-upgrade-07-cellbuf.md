# x/cellbuf + Transitive Dependency Cleanup (td)

> Status: **PLAN / not started** · **Phase 2** — after the v2 stack lands.
> Low risk. The dependency-graph tidy-up that follows the big change.

## x/cellbuf (1 file)

| | Current | Target |
|---|---------|--------|
| Module path | `github.com/charmbracelet/x/cellbuf` (**unchanged**) | same |
| Version | `v0.0.14` | **`v0.0.15`** (re-verify) |

Used in `pkg/monitor/view.go` (efficient width-aware text rendering).

### Why Phase 2

lipgloss v2 **dropped cellbuf** (it uses `github.com/charmbracelet/ultraviolet` internally now). After the stack lands, check whether cellbuf survives in the graph:

```bash
cd ~/code/td
go mod why github.com/charmbracelet/x/cellbuf
```

- It **stays** because td imports it directly (`pkg/monitor/view.go`). Keep as a direct dep.
- Bump to `v0.0.15` (path unchanged):

```bash
go get github.com/charmbracelet/x/cellbuf@v0.0.15
```

- Verify the cellbuf functions td calls keep their signatures (they've been stable). No source change anticipated.

> Optional cleanup (out of scope): if td's cellbuf use is just `cellbuf.Wrap`, it could move to `lipgloss.Wrap` (added in v2) to drop the direct dep. Only if you want one fewer dependency.

## Transitive dependencies — let `go mod tidy` resolve

Not imported in td source (confirmed) — only `// indirect`. After the Phase 1 v2 bump, `go mod tidy` resolves them:

| Library | Resolves via | Expected |
|---------|-------------|----------|
| `colorprofile` | bubbletea v2 / lipgloss v2 | `v0.4.3` |
| `x/term` (charmbracelet) | bubbletea v2 / lipgloss v2 | `v0.2.2` |
| `x/exp/slice`, `x/exp/strings` | transitive | float |
| `x/exp/golden` | test dep of v2 modules | pseudo-version |
| `ultraviolet`, `clipperhouse/displaywidth` | **new** — lipgloss v2 engine + width | pulled by lipgloss v2 |

**Action:** `go mod tidy` after Phase 1, then eyeball the `go.mod`/`go.sum` diff:
- New: `ultraviolet`, `clipperhouse/displaywidth` (and friends).
- Pruned: v1-only deps; cellbuf may drop from *indirect* (stays *direct*).
- Don't hand-pin transitive deps; only bump the directly-imported ones.

## Ordered checklist (after the Phase 1 stack)

1. [ ] `go mod tidy`
2. [ ] `go mod why github.com/charmbracelet/x/cellbuf` → confirm direct
3. [ ] `go get github.com/charmbracelet/x/cellbuf@v0.0.15`
4. [ ] Review `go.mod`/`go.sum` diff for new/pruned entries
5. [ ] `go build ./... && go test ./...`

## After cleanup: the cross-repo handoff

This is the gate to sidecar's Phase 1:

1. [ ] **Cut a new td release** built on the charm.land v2 stack (e.g. `v0.45.0`). Tag + publish so `go install github.com/marcus/td@v0.45.0` works (the version string sidecar uses in its self-update flow — `internal/version/checker.go`).
2. [ ] In sidecar, **bump `github.com/marcus/td`** to that release as **step 0** of sidecar's Phase 1, then proceed with sidecar's own lipgloss/bubbletea/bubbles v2 migration so both repos share the identical `charm.land` v2 modules.
3. [ ] Before tagging, smoke-test the integration with a local `replace github.com/marcus/td => ../td` in sidecar's go.mod to confirm the embedded monitor compiles and renders under the v2 stack.

## Gotchas

- `x/cellbuf`, `x/ansi`, `x/term`, `colorprofile` keep `github.com/charmbracelet/...` paths. Only UI libs moved to `charm.land`.
- Let `go mod tidy` do transitive resolution; bump by hand only the direct `x/cellbuf`.
