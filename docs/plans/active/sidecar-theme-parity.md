# Plan: Full Sidecar theme parity for the embedded td monitor

**Status:** plan — ready to implement
**Research snapshot:** 2026-08-17
**Canonical owner:** td (`pkg/monitor`)
**Consumer:** Sidecar (`internal/plugins/tdmonitor`)
**Related td issue:** `td-d52201` — declarative modals ignore the injected renderer/theme
**Sidecar companion:** `~/code/sidecar/docs/plans/active/embedded-td-theme-parity.md`

## Outcome

When td monitor is embedded in Sidecar, every td-owned surface uses the active
Sidecar theme: panels, lists, kanban, statuses, selections, forms, declarative
and legacy modals, help, markdown, buttons, toasts, loading/error states, and
scroll or hover feedback. Changing or previewing a Sidecar theme repaints an
already-running monitor immediately, without reopening the tab, rebuilding the
monitor, reconnecting its database, or resetting its interaction state.

Standalone `td monitor` keeps its current visual appearance through td's own
default theme. Sidecar does not pass a Sidecar theme name into td and td does
not import Sidecar packages; the boundary is a small semantic palette owned by
td and populated by any embedder.

This is presentation-layer integration. It does not add a new td CLI command or
API endpoint: Sidecar is selecting the appearance of a view it hosts, not
changing task data or a td-owned workflow. If standalone td later gains
user-selectable themes, that separate capability should have config and CLI
paths over this same theme contract.

## Affected journey

The plan is complete only when this exact journey works:

1. Open a project with td initialized in Sidecar and select the td monitor tab.
2. See the whole monitor, including nested/modal content, rendered from the
   project's resolved Sidecar palette rather than td's default ANSI-256 colors.
3. Open td issue details, markdown/notes, create/edit forms, help, board picker,
   kanban and confirmation flows; each retains readable semantic colors and
   backgrounds from the same palette.
4. Open Sidecar's theme switcher and move through built-in, community, light,
   dark, and overridden themes. The visible td monitor previews each theme on
   the next frame while preserving the selected issue, modal stack, focus,
   scroll offsets, form values, filters, and polling chain.
5. Cancel the preview and see td return to the prior project/global theme.
   Confirm a selection, switch projects, and restart Sidecar; td follows the
   resolved theme in each case.
6. Run `td monitor` directly and see its existing default appearance with no
   Sidecar dependency or required theme configuration.

## Current behavior and root cause

Theme support at the embedding boundary is partial:

| Area | Current path | Current result |
| --- | --- | --- |
| Panel borders | Sidecar supplies `EmbeddedOptions.PanelRenderer`; the closure reads `styles.GetCurrentTheme()` during render | Follows live Sidecar gradients |
| Legacy modal borders | Sidecar supplies `EmbeddedOptions.ModalRenderer`; the closure reads the current theme | Some modal chrome follows Sidecar; several semantic depth/type colors are still hardcoded in Sidecar |
| Markdown | Sidecar snapshots `buildMarkdownTheme()` into `EmbeddedOptions.MarkdownTheme` during monitor construction | Initially themed, but does not follow live previews and covers markdown only |
| Main monitor body | Package-level styles and raw `lipgloss.Color("…")` values in `pkg/monitor` | Remains td purple/cyan/gray regardless of Sidecar theme |
| Declarative modals | Package-level palette and styles in `pkg/monitor/modal/styles.go` | Bypasses the injected legacy `ModalRenderer`; this is `td-d52201` |
| Huh forms | `huh.ThemeDracula` in `pkg/monitor/form.go` | Always Dracula-derived |
| Help and overlays | Package-level/raw colors in `keymap/help.go`, `overlay.go`, and view helpers | Remain td defaults |

The fixed colors are not confined to one file. The research snapshot found
theme-bearing production code in `styles.go`, `view.go`, `modal/styles.go`,
`modal.go`, `kanban.go`, `modal/input.go`, `modal/layout.go`, `board_editor.go`,
`overlay.go`, `keymap/help.go`, and `form_autofill.go`. `styles.go` alone contains
most of the package-level derived styles, while `view.go` also creates styles
inline. A modal-only fix would leave most of the user journey mismatched.

The architectural cause is that `EmbeddedOptions` exposes three unrelated
presentation seams—two chrome renderers and a markdown-only palette—while td's
actual semantic colors live in package globals or literals. There is no complete
theme value, no model-owned derived style set, and no runtime retheme operation.

## Design decisions

### 1. td owns a semantic, host-neutral theme contract

Add an exported monitor theme value (names illustrative; settle exact names in
the first implementation slice):

```go
type Theme struct {
    Primary, Secondary, Accent       string
    Success, Warning, Error, Info    string
    TextPrimary, TextSecondary       string
    TextMuted, TextSubtle            string
    TextSelection                    string
    OnPrimary, OnWarning, OnError    string
    Background, Surface, Selection   string
    SurfaceRaised                    string
    Border, BorderMuted, BorderActive string
    Link                             string
    SyntaxTheme, MarkdownTheme       string
}
```

Use semantic slots, not Sidecar's full `ColorPalette`, theme names, JSON maps,
or renderer-specific `lipgloss.Style` values. The contract should contain only
colors td actually needs, but it must be complete for td's owned surfaces.
Fields should be plain color strings accepted by Lip Gloss (Sidecar will pass
normalized hex), so the contract remains usable by other hosts.

Provide `DefaultTheme()` (or an equivalent immutable default) that reproduces
the current standalone palette. Normalize an incomplete embedded theme by
overlaying supplied slots on the default, so adding a new slot is backward
compatible and no missing value becomes an unreadable empty color. Validate
all non-empty supplied colors before changing the model: an invalid explicit
value returns a clear error and leaves the prior theme intact, while an omitted
slot inherits td's default. Do not make Sidecar know td's defaults.

Keep `PanelRenderer` and `ModalRenderer` as optional chrome adapters: Sidecar's
gradient borders are genuinely host-owned presentation. They are not the color
transport. Fold markdown colors into the complete theme. Retain
`MarkdownTheme` as a compatibility fallback for one release if another caller
uses it, with documented precedence (`Theme` wins); then deprecate it instead
of maintaining two permanent theming models.

### 2. Theme state and derived styles belong to each monitor model

Store the normalized `Theme` and a derived, unexported style set on
`monitor.Model`. Build styles from that value through one constructor. Render
helpers should read the model's style set or receive the small relevant style
group explicitly.

Do not implement the feature by adding a package-global `SetTheme`. A global
setter would make tests order-dependent, prevent two embedded monitors from
using different palettes in one process, and leave the standalone/embedded
boundary implicit. The same rule applies to `pkg/monitor/modal`: modal style
data must be instance-owned or passed at construction/render time, not mutated
through exported package globals.

Organize the derived styles by semantic role (base text/surfaces, statuses and
priorities, panels, lists/selection, buttons, kanban, modal, markdown/form), not
by copying Sidecar token names. Status/type/priority distinctions remain td
product semantics; map them to the theme's success/warning/error/info/
primary/secondary slots in one documented table.

### 3. Runtime retheming is explicit and state-preserving

Expose a synchronous model method intended for the host's Bubble Tea goroutine,
for example:

```go
func (m *Model) SetTheme(theme Theme) error
```

It should normalize the palette, replace only theme-derived style state, update
themeable child models, and invalidate rendered-color caches. It must not create
a new monitor model, touch the database, restart polling, call `Init`, or reset
navigation and modal state.

Specifically audit and preserve:

- active panel, cursors, selected IDs, filters, and pane sizes;
- modal stack, modal focus/hover/scroll, and open issue/board state;
- create/edit form values, page and field focus, validation state, and
  autocomplete selection;
- notes/markdown position and source mapping;
- existing tick/poll chain and shared database reference.

Markdown is currently pre-rendered in closures/caches. On a theme change,
invalidate or lazily regenerate colored markdown from the retained source text;
never keep ANSI produced by the prior palette. Apply the new Huh theme to the
existing form if the library permits it. If Huh requires reconstruction,
snapshot and restore all user-visible form state explicitly and cover that path
with a regression test.

### 4. One theme must reach every td renderer

Migrate the full renderer graph, including the existing declarative-modal issue,
before claiming parity. Maintain a checked coverage ledger during
implementation with at least these families:

| Family | Representative code | Contract |
| --- | --- | --- |
| Main panels and headers | `styles.go`, `view.go`, custom `PanelRenderer` calls | Text, title fill, inactive/active/hover/divider states use semantic slots; optional host border renderer remains authoritative for chrome |
| Task/status/priority/type presentation | `styles.go`, `kanban.go`, task-list helpers | One documented mapping; no local raw palette variants |
| Selection and interaction | row highlighting, activity table, board editor, search, autocomplete, hover, drag dividers | Foreground remains readable on selection/focus surfaces in light and dark themes |
| Declarative modals | `pkg/monitor/modal/*` | Modal instance receives td modal styles; all sections, inputs, buttons, scrollbars, hints, backdrop/body and variants use them; resolves `td-d52201` |
| Legacy modals and stacked issue views | `modal.go`, `view.go`, `kanban.go` | Inner content is themed even when Sidecar supplies outer chrome; depth/type semantics are supplied through the same theme |
| Forms | `form.go`, `form_autofill.go`, form modal/button paths | Huh and td-owned wrappers share the current model theme and retheme without data loss |
| Markdown and notes | `markdown.go`, `notes_modal.go`, issue details | Text, code, syntax, links, quotes and backgrounds update from current theme and caches invalidate |
| Help, overlays, toasts and empty/error states | `keymap/help.go`, `overlay.go`, getting-started/setup-like td views | No default td colors leak through secondary paths |

Raw colors are acceptable only inside `DefaultTheme()` and for content whose
color is the data itself. If a semantic state needs a distinct shade not in the
contract, derive it deterministically from an existing slot with contrast
checks, or justify and add a semantic slot; do not reintroduce a literal beside
the renderer.

### 5. Sidecar is a palette adapter and lifecycle host

Sidecar maps its already-resolved and normalized `styles.Theme.Colors` to
`monitor.Theme`. td must not understand global versus project scope,
community-theme conversion, overrides, or Sidecar's theme registry.

At initial construction Sidecar passes the translated theme in
`EmbeddedOptions`. On every theme preview, cancel/restore, confirmation, project
switch, configuration apply, and startup resolution, Sidecar delivers the
latest translated palette to a live td model exactly once. The companion plan
defines the Sidecar plumbing and tests.

## Implementation phases

Each phase is separately reviewable. The repository may be temporarily mixed
between phases, but no release should advertise full theme support until Phase
5 and the Sidecar consumer proof pass.

### Phase 1 — Contract, defaults, and a visible steel thread

- Add `monitor.Theme`, default/overlay normalization, and the model-owned
  derived style container.
- Add `Theme` to `EmbeddedOptions`; preserve documented compatibility for
  `MarkdownTheme`, `PanelRenderer`, and `ModalRenderer`.
- Route one complete, highly visible slice through it: active/inactive panel
  content plus task-list selection and status text.
- Add tests proving two models can render different palettes in the same
  process and that standalone construction renders the current default.
- Build Sidecar against a local td replacement and pass a deliberately obvious
  palette to prove the public seam before migrating every style.

This is the steel thread: real host input → td model state → derived styles →
visible embedded output, while the rest of td may still use defaults.

### Phase 2 — Core monitor, kanban, and interaction states

- Move main-view package globals and inline styles into the model style set.
- Centralize status, priority, type, review-bucket, badge, toast, chart, kanban,
  panel title, selection, search, hover, and divider mappings.
- Replace hardcoded row background ANSI in `highlightRow` with theme-derived
  rendering that preserves nested foreground styles.
- Cover list mode, kanban mode, board editor, activity/stat views, empty states,
  and mouse hover/drag feedback.
- Add focused render tests using contrasting light and dark palettes; assert
  semantic color presence and absence of known td default escape sequences,
  not entire-frame snapshots.

### Phase 3 — Declarative and legacy modals

- Give `pkg/monitor/modal` an instance-owned theme/style value and thread it
  through layout and every section primitive. Do not expose mutable package
  style variables.
- Update every modal construction path to use the model's modal theme. This
  closes `td-d52201` as part of the broader work.
- Theme legacy inner content and retain the optional host `ModalRenderer` only
  for outer chrome. Remove duplicated hardcoded depth/type colors or derive them
  from semantic slots.
- Prove default, warning, danger, info, nested issue, board picker, notes,
  confirmation, sync, and help modals under light and dark palettes.

### Phase 4 — Forms, markdown, help, and secondary paths

- Build a Huh theme from `monitor.Theme` and apply it to new and already-open
  forms without losing values or focus.
- Move autocomplete, buttons, validation, markdown, code, notes, help,
  overlays, breadcrumbs, scrollbars, and remaining inline styles to the theme.
- Consolidate `MarkdownThemeConfig` under the complete model theme, maintaining
  the temporary compatibility precedence agreed in Phase 1.
- Audit not only `lipgloss.Color` calls but raw ANSI color sequences, glamour/
  Chroma style construction, and third-party theme constructors.

### Phase 5 — Runtime updates and regression guard

- Implement `SetTheme` over the fully migrated style graph.
- Retheme open child views and invalidate markdown/render caches without
  resetting interaction state or starting work.
- Add a regression test that opens nested/modal/form state, applies a second
  palette, and asserts both new colors and unchanged state.
- Add a repository guard modeled on Sidecar's theme-freeze check. It should fail
  when production monitor code captures a theme-derived style at package init
  or introduces a raw color outside the default/derivation allowlist. Keep the
  allowlist small and documented.
- Update exported API docs and an embedding example showing initial and live
  theme application.

### Phase 6 — Producer release, Sidecar integration, and real proof

- Run td's focused tests, full release-safe test gate, build, and vet.
- Independently review the td change, including the public contract and raw
  color audit.
- In Sidecar, use a local `replace` only for pre-release integration proof.
  Exercise the full affected journey in an isolated Sidecar state tree and tmux
  server; never touch the default tmux server.
- Cut and publish the td release first. Verify the tag/remote artifact, then
  remove the local replacement and pin Sidecar to the released version.
- Complete the Sidecar companion plan, its independent review, and its own
  isolated end-to-end proof.

## Verification matrix

### td focused contract tests

- `DefaultTheme` reproduces standalone semantic colors.
- Partial themes overlay defaults deterministically; invalid/missing slots have
  documented behavior.
- Two simultaneous models with different themes do not contaminate each other.
- Panel and modal renderer adapters remain optional and receive unchanged
  geometry/state arguments.
- Every status/type/priority/modal variant maps to a semantic slot.
- Runtime retheme changes ANSI output while preserving full interaction state.
- Open markdown and forms repaint without stale colors or lost input.
- The theme-freeze/raw-color guard covers every production file below
  `pkg/monitor`, including subpackages.

### td gates

```sh
go test ./pkg/monitor/...
make test
go build ./...
go vet ./...
```

### Cross-repo consumer proof

Use at least:

- Sidecar Modern or another curated dark theme;
- a materially different light theme;
- a community theme;
- a project theme with explicit color overrides;
- live preview, cancel restore, confirmation, project switch, and restart.

For each, inspect the main list, kanban, issue details, nested modal, create/edit
form, markdown/code, help, confirmation, hover/selection, and loading/error
paths. Check narrow and ordinary terminal sizes. Prefer ANSI/color assertions
for automated tests plus PNG/text captures for visual review; text-only
`capture-pane` cannot prove colors.

The Sidecar proof must use `scripts/tmux-drive.sh` or an equivalent run that
isolates both the tmux server and Sidecar state/config. Stop only that isolated
server when finished. Do not restart, kill, or replace the machine's default
tmux server.

## Definition of done

- All td-owned monitor render paths consume the model theme; the coverage
  ledger has no unexplained raw/frozen colors.
- Standalone `td monitor` retains its intended default appearance and behavior.
- A live embedded monitor follows initial, previewed, restored, confirmed, and
  project-switched Sidecar themes without state loss or duplicate polling.
- Light/dark/community/override verification covers nested content, forms and
  markdown, not only outer borders.
- The declarative modal gap in `td-d52201` is resolved by the shared contract,
  not a Sidecar-specific setter.
- td is released before Sidecar pins the new API; no local `replace` remains.
- Both repositories pass their gates and receive independent review.

## Deliberate non-goals

- Sharing Sidecar's theme registry, names, config schema, or community-theme
  conversion with td.
- Making Sidecar's gradient implementation part of td.
- Redesigning td layout, interaction, or semantic status meanings while moving
  colors.
- Adding standalone theme selection in this feature.
- Generalizing a cross-project theme framework before a second non-Sidecar host
  demonstrates a need beyond the semantic `monitor.Theme` value.
