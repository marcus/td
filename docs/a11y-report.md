# Accessibility Report — td

This is a narrative analysis of TUI/CLI accessibility heuristics. It is
intentionally not a checkbox list — each finding includes context and a
recommendation. Treat severities as guidance, not gates.

## Environment signals

- NO_COLOR is **not** referenced in the scanned tree. Users who set
  `NO_COLOR=1` (per https://no-color.org) expect colored output to be
  suppressed. Add a single check at TUI bootstrap and gate lipgloss styles.

## Summary by category

- color/no-color: 2 finding(s)
- contrast/adaptive: 131 finding(s)
- icon-only label: 2 finding(s)

## Findings

### [HIGH] color/no-color — pkg/monitor/styles.go:360

    bgCode := "\x1b[48;5;237m" // Background color 237

Hardcoded ANSI escape; route color through lipgloss + honor NO_COLOR (https://no-color.org).

### [HIGH] color/no-color — pkg/monitor/styles.go:361

    reset := "\x1b[0m"

Hardcoded ANSI escape; route color through lipgloss + honor NO_COLOR (https://no-color.org).

### [MEDIUM] contrast/adaptive — cmd/stats_analytics.go:18

    analyticsHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — cmd/stats_analytics.go:19

    analyticsLabelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — cmd/stats_analytics.go:20

    analyticsValueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("45"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — cmd/stats_analytics.go:21

    analyticsErrorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — cmd/stats_analytics.go:22

    analyticsWarnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — cmd/sync_tail.go:19

    pushArrow = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("→")  // green

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — cmd/sync_tail.go:20

    pullArrow = lipgloss.NewStyle().Foreground(lipgloss.Color("45")).Render("←")  // cyan

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — cmd/sync_tail.go:21

    dimStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] icon-only label — cmd/ws.go:698

    statusMark = " ✓"

Pair the icon with a short text label so screen readers / NO_COLOR users get the meaning.

### [MEDIUM] contrast/adaptive — pkg/monitor/kanban.go:61

    return lipgloss.Color("183") // light purple (pending review)

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/kanban.go:67

    return lipgloss.Color("255")

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/keymap/help.go:14

    Foreground(lipgloss.Color("212")) // Primary color (purple/magenta)

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal.go:1569

    return style.Background(lipgloss.Color("27")).Foreground(lipgloss.Color("255")).Render("PROGRESS")

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal.go:1571

    return style.Background(lipgloss.Color("135")).Foreground(lipgloss.Color("255")).Render("DECISION")

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal.go:1573

    return style.Background(lipgloss.Color("196")).Foreground(lipgloss.Color("255")).Render("BLOCKER")

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal.go:1575

    return style.Background(lipgloss.Color("208")).Foreground(lipgloss.Color("255")).Render("HYPOTHESIS")

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal.go:1577

    return style.Background(lipgloss.Color("214")).Foreground(lipgloss.Color("255")).Render("TRIED")

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal.go:1579

    return style.Background(lipgloss.Color("40")).Foreground(lipgloss.Color("255")).Render("RESULT")

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal.go:1581

    return style.Background(lipgloss.Color("39")).Foreground(lipgloss.Color("255")).Render("ORCHESTRATION")

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal.go:1583

    return style.Background(lipgloss.Color("160")).Foreground(lipgloss.Color("255")).Render("SECURITY")

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal.go:1585

    return style.Background(lipgloss.Color("240")).Foreground(lipgloss.Color("255")).Render(strings.ToUpper(string(logType)))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:11

    Primary      = lipgloss.Color("212") // primaryColor

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:12

    Error        = lipgloss.Color("196") // errorColor

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:13

    Warning      = lipgloss.Color("214") // warningColor

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:14

    Info         = lipgloss.Color("45")  // cyan

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:15

    Muted        = lipgloss.Color("241") // mutedColor

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:16

    BgSecondary  = lipgloss.Color("235") // modal background

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:17

    TextMuted    = lipgloss.Color("241") // mutedColor

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:18

    BorderNormal = lipgloss.Color("240") // default border

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:24

    Foreground(lipgloss.Color("252")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:25

    Background(lipgloss.Color("238")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:29

    Foreground(lipgloss.Color("255")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:35

    Foreground(lipgloss.Color("255")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:36

    Background(lipgloss.Color("245")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:40

    Foreground(lipgloss.Color("252")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:41

    Background(lipgloss.Color("238")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:45

    Foreground(lipgloss.Color("255")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:51

    Foreground(lipgloss.Color("255")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:52

    Background(lipgloss.Color("203")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:66

    Foreground(lipgloss.Color("252"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:69

    Background(lipgloss.Color("237")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:70

    Foreground(lipgloss.Color("255"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:73

    Background(lipgloss.Color("237")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/modal/styles.go:74

    Foreground(lipgloss.Color("255")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/overlay.go:14

    var DimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:22

    primaryColor   = lipgloss.Color("212")

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:23

    secondaryColor = lipgloss.Color("141")

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:24

    mutedColor     = lipgloss.Color("241")

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:25

    successColor   = lipgloss.Color("42")

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:26

    warningColor   = lipgloss.Color("214")

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:27

    errorColor     = lipgloss.Color("196")

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:28

    cyanColor      = lipgloss.Color("45")

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:33

    BorderForeground(lipgloss.Color("240")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:44

    BorderForeground(lipgloss.Color("245")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:49

    Background(lipgloss.Color("237")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:50

    Foreground(lipgloss.Color("255")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:57

    timestampStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:66

    models.StatusOpen:       lipgloss.NewStyle().Foreground(lipgloss.Color("45")),

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:75

    models.StatusOpen:       lipgloss.NewStyle().Foreground(lipgloss.Color("45")),

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:76

    models.StatusInProgress: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:77

    models.StatusBlocked:    lipgloss.NewStyle().Foreground(lipgloss.Color("196")),

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:78

    models.StatusInReview:   lipgloss.NewStyle().Foreground(lipgloss.Color("141")),

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:86

    models.PriorityP2: lipgloss.NewStyle().Foreground(lipgloss.Color("45")),

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:94

    commentBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("45"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:99

    Foreground(lipgloss.Color("255")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:110

    Foreground(lipgloss.Color("45")). // Cyan when focused

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:114

    Background(lipgloss.Color("237")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:115

    Foreground(lipgloss.Color("255"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:122

    Background(lipgloss.Color("237")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:129

    Foreground(lipgloss.Color("196")). // Red when focused

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:133

    Background(lipgloss.Color("237")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:134

    Foreground(lipgloss.Color("255"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:138

    Foreground(lipgloss.Color("45")). // Cyan when focused

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:142

    Background(lipgloss.Color("237")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:143

    Foreground(lipgloss.Color("255"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:147

    Foreground(lipgloss.Color("244")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:153

    Foreground(lipgloss.Color("0")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:158

    Foreground(lipgloss.Color("255")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:163

    models.TypeEpic:    lipgloss.NewStyle().Foreground(lipgloss.Color("212")), // Purple/magenta

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:164

    models.TypeFeature: lipgloss.NewStyle().Foreground(lipgloss.Color("42")),  // Green

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:165

    models.TypeBug:     lipgloss.NewStyle().Foreground(lipgloss.Color("196")), // Red

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:166

    models.TypeTask:    lipgloss.NewStyle().Foreground(lipgloss.Color("45")),  // Cyan

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:167

    models.TypeChore:   lipgloss.NewStyle().Foreground(lipgloss.Color("241")), // Gray

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] icon-only label — pkg/monitor/styles.go:174

    models.TypeBug:     "✗", // X mark - defect

Pair the icon with a short text label so screen readers / NO_COLOR users get the meaning.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:183

    BorderForeground(lipgloss.Color("45")). // Cyan

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:189

    BorderForeground(lipgloss.Color("214")). // Orange/Yellow

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:194

    Foreground(lipgloss.Color("252")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:195

    Background(lipgloss.Color("238")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:199

    Foreground(lipgloss.Color("255")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:205

    Foreground(lipgloss.Color("255")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:206

    Background(lipgloss.Color("245")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:211

    Foreground(lipgloss.Color("252")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:212

    Background(lipgloss.Color("238")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:216

    Foreground(lipgloss.Color("255")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:222

    Foreground(lipgloss.Color("255")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:223

    Background(lipgloss.Color("203")). // Lighter red

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:229

    Foreground(lipgloss.Color("255"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:232

    Background(lipgloss.Color("237"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:235

    kanbanTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:236

    kanbanHintStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:237

    kanbanSepStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/styles.go:242

    BorderForeground(lipgloss.Color("240")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:1610

    BorderForeground(lipgloss.Color("42")). // Green for handoffs

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:1674

    BorderForeground(lipgloss.Color("212")). // Purple

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:1825

    borderColor := lipgloss.Color("45") // Cyan

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2006

    borderColor = lipgloss.Color("45") // Cyan

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2008

    borderColor = lipgloss.Color("214") // Orange for depth 3+

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2199

    pinkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")) // Pink

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2233

    BorderForeground(lipgloss.Color("240")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2354

    filterStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2358

    filterStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2410

    BorderForeground(lipgloss.Color("141")). // Purple for help

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2601

    readyColor         = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2602

    reviewColor        = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2603

    blockedColor       = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2604

    reworkColor        = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Orange/warning

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2605

    inProgressColor    = lipgloss.NewStyle().Foreground(lipgloss.Color("45"))  // Cyan

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2606

    pendingReviewColor = lipgloss.NewStyle().Foreground(lipgloss.Color("183")) // Light purple

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2611

    Foreground(lipgloss.Color("0")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2612

    Background(lipgloss.Color("141"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2617

    Foreground(lipgloss.Color("0")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2618

    Background(lipgloss.Color("42")) // Green bg

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2622

    Foreground(lipgloss.Color("255")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2623

    Background(lipgloss.Color("196")) // Red bg

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2627

    Foreground(lipgloss.Color("0")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2628

    Background(lipgloss.Color("45")) // Cyan bg

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2632

    Foreground(lipgloss.Color("0")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2633

    Background(lipgloss.Color("183")) // Light purple bg

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2638

    Foreground(lipgloss.Color("0")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2639

    Background(lipgloss.Color("42"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2643

    Foreground(lipgloss.Color("45"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2648

    Foreground(lipgloss.Color("0")).

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

### [MEDIUM] contrast/adaptive — pkg/monitor/view.go:2649

    Background(lipgloss.Color("214"))

Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.

