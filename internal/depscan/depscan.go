// Package depscan analyzes a Go module's dependencies for security and
// maintenance risks. It parses go.mod (via `go mod edit -json`) and go.sum,
// applies offline static heuristics, and optionally integrates govulncheck
// and `go list -m -u` for known-CVE and outdated-module detection.
package depscan

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Severity ranks a finding's importance.
type Severity string

const (
	SeverityHigh   Severity = "high"
	SeverityMedium Severity = "medium"
	SeverityLow    Severity = "low"
)

// rank returns a numeric weight for ordering (higher == more severe).
func (s Severity) rank() int {
	switch s {
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	default:
		return 0
	}
}

// AtLeast reports whether s is at least as severe as min.
func (s Severity) AtLeast(min Severity) bool {
	return s.rank() >= min.rank()
}

// ParseSeverity converts a string to a Severity, defaulting to low on
// unrecognized input.
func ParseSeverity(s string) (Severity, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high":
		return SeverityHigh, nil
	case "medium", "med":
		return SeverityMedium, nil
	case "low", "":
		return SeverityLow, nil
	default:
		return SeverityLow, fmt.Errorf("invalid severity %q (want high|medium|low)", s)
	}
}

// Categories for findings.
const (
	CategoryPseudoVersion = "pseudo-version"
	CategoryPreRelease    = "pre-1.0"
	CategoryIncompatible  = "incompatible"
	CategoryReplace       = "replace-directive"
	CategoryIndirect      = "indirect-surface"
	CategoryGoDirective   = "go-directive"
	CategoryVulnerability = "vulnerability"
	CategoryOutdated      = "outdated"
)

// Finding is a single dependency risk observation.
type Finding struct {
	Severity Severity `json:"severity"`
	Category string   `json:"category"`
	Module   string   `json:"module,omitempty"`
	Version  string   `json:"version,omitempty"`
	Detail   string   `json:"detail"`
}

// Summary holds aggregate counts for a report.
type Summary struct {
	Total          int  `json:"total"`
	High           int  `json:"high"`
	Medium         int  `json:"medium"`
	Low            int  `json:"low"`
	DirectDeps     int  `json:"direct_deps"`
	IndirectDeps   int  `json:"indirect_deps"`
	GoSumModules   int  `json:"go_sum_modules"`
	VulnChecked    bool `json:"vuln_checked"`
	UpdatesChecked bool `json:"updates_checked"`
}

// Report is the full result of a dependency scan.
type Report struct {
	Module   string    `json:"module"`
	GoMod    string    `json:"go_mod_path"`
	Go       string    `json:"go_directive"`
	Summary  Summary   `json:"summary"`
	Findings []Finding `json:"findings"`
	Notes    []string  `json:"notes,omitempty"`
}

// modInfo mirrors the JSON emitted by `go mod edit -json`.
type modInfo struct {
	Module struct {
		Path string `json:"Path"`
	} `json:"Module"`
	Go      string `json:"Go"`
	Require []struct {
		Path     string `json:"Path"`
		Version  string `json:"Version"`
		Indirect bool   `json:"Indirect"`
	} `json:"Require"`
	Replace []struct {
		Old struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
		} `json:"Old"`
		New struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
		} `json:"New"`
	} `json:"Replace"`
}

// Options controls which checks run.
type Options struct {
	// Dir is the directory to scan from. Empty means the current directory.
	Dir string
	// CheckUpdates enables `go list -m -u` outdated-module detection.
	CheckUpdates bool
	// Vuln enables govulncheck integration when the tool is on PATH.
	Vuln bool
}

// pseudoVersionRe matches the timestamp + commit-hash suffix of a Go
// pseudo-version. The timestamp is preceded by '-' in the base form
// (v0.0.0-20250623103423-23b8fd6302d7) or '.' in the pre-release form
// (v0.21.1-0.20250623103423-23b8fd6302d7).
var pseudoVersionRe = regexp.MustCompile(`[-.][0-9]{14}-[0-9a-f]{12}$`)

// IsPseudoVersion reports whether version is a Go pseudo-version (a synthetic
// version derived from an untagged commit).
func IsPseudoVersion(version string) bool {
	return pseudoVersionRe.MatchString(version)
}

// IsPreRelease reports whether version is a pre-1.0 (v0.x) release.
func IsPreRelease(version string) bool {
	return strings.HasPrefix(version, "v0.")
}

// IsIncompatible reports whether version carries the +incompatible suffix.
func IsIncompatible(version string) bool {
	return strings.HasSuffix(version, "+incompatible")
}

// findGoMod resolves the path to the active module's go.mod. It prefers
// `go env GOMOD`, then falls back to walking up from dir.
func findGoMod(dir string) (string, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	cmd := exec.Command("go", "env", "GOMOD")
	cmd.Dir = dir
	if out, err := cmd.Output(); err == nil {
		p := strings.TrimSpace(string(out))
		if p != "" && p != os.DevNull {
			return p, nil
		}
	}
	// Fallback: walk up looking for go.mod.
	cur := dir
	for {
		candidate := filepath.Join(cur, "go.mod")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return "", fmt.Errorf("no go.mod found from %s", dir)
}

// parseGoMod runs `go mod edit -json` against goModPath and decodes the result.
func parseGoMod(goModPath string) (*modInfo, error) {
	cmd := exec.Command("go", "mod", "edit", "-json")
	cmd.Dir = filepath.Dir(goModPath)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go mod edit -json failed: %w", err)
	}
	return decodeModInfo(out)
}

// decodeModInfo decodes `go mod edit -json` output. Split out for testing.
func decodeModInfo(data []byte) (*modInfo, error) {
	var mi modInfo
	if err := json.Unmarshal(data, &mi); err != nil {
		return nil, fmt.Errorf("parsing go.mod JSON: %w", err)
	}
	return &mi, nil
}

// countGoSumModules counts the distinct module@version entries verified in
// go.sum (lines ending in a plain hash, not the /go.mod hash variant). The
// bool return reports whether go.sum was found and readable, so callers can
// distinguish "no hashed modules" from "no go.sum".
func countGoSumModules(goModPath string) (int, bool) {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(goModPath), "go.sum"))
	if err != nil {
		return 0, false
	}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// fields[1] is either "vX.Y.Z" or "vX.Y.Z/go.mod".
		if strings.HasSuffix(fields[1], "/go.mod") {
			continue
		}
		seen[fields[0]+"@"+fields[1]] = struct{}{}
	}
	return len(seen), true
}

// goMinor extracts the minor version from a Go version string such as
// "1.25.5" or "go1.26.3". Returns -1 when it cannot be parsed.
func goMinor(v string) int {
	v = strings.TrimPrefix(v, "go")
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return -1
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil {
		return -1
	}
	return n
}

// Scan runs the dependency analysis described by opts and returns a Report.
// Static heuristics always run offline; dynamic checks degrade gracefully and
// append entries to Report.Notes when skipped.
func Scan(opts Options) (*Report, error) {
	goModPath, err := findGoMod(opts.Dir)
	if err != nil {
		return nil, err
	}
	mi, err := parseGoMod(goModPath)
	if err != nil {
		return nil, err
	}

	report := &Report{
		Module: mi.Module.Path,
		GoMod:  goModPath,
		Go:     mi.Go,
	}

	direct, indirect := 0, 0
	for _, r := range mi.Require {
		if r.Indirect {
			indirect++
		} else {
			direct++
		}
		report.Findings = append(report.Findings, classifyVersion(r.Path, r.Version, r.Indirect)...)
	}

	// Replace directives: supply-chain / local-override risk.
	for _, rep := range mi.Replace {
		sev := SeverityMedium
		detail := fmt.Sprintf("replaced with %s", rep.New.Path)
		if rep.New.Version != "" {
			detail += " " + rep.New.Version
		}
		if !strings.Contains(rep.New.Path, ".") || strings.HasPrefix(rep.New.Path, ".") || strings.HasPrefix(rep.New.Path, "/") {
			// Local filesystem replacement: not reproducible for other builds.
			sev = SeverityHigh
			detail = fmt.Sprintf("replaced with local path %s (not reproducible outside this checkout)", rep.New.Path)
		}
		report.Findings = append(report.Findings, Finding{
			Severity: sev,
			Category: CategoryReplace,
			Module:   rep.Old.Path,
			Version:  rep.Old.Version,
			Detail:   detail,
		})
	}

	// Indirect dependency surface area.
	if f, ok := classifyIndirectSurface(direct, indirect); ok {
		report.Findings = append(report.Findings, f)
	}

	// Stale go directive relative to the building toolchain.
	if f, ok := classifyGoDirective(mi.Go, runtime.Version()); ok {
		report.Findings = append(report.Findings, f)
	}

	// Dynamic checks.
	if opts.Vuln {
		findings, note := runGovulncheck(filepath.Dir(goModPath))
		report.Findings = append(report.Findings, findings...)
		if note != "" {
			report.Notes = append(report.Notes, note)
		} else {
			report.Summary.VulnChecked = true
		}
	}
	if opts.CheckUpdates {
		findings, note := runUpdateCheck(filepath.Dir(goModPath))
		report.Findings = append(report.Findings, findings...)
		if note != "" {
			report.Notes = append(report.Notes, note)
		} else {
			report.Summary.UpdatesChecked = true
		}
	}

	report.Summary.DirectDeps = direct
	report.Summary.IndirectDeps = indirect
	if n, ok := countGoSumModules(goModPath); ok {
		report.Summary.GoSumModules = n
	} else if len(mi.Require) > 0 {
		report.Notes = append(report.Notes, "go.sum not found or unreadable; hash-verified module count unavailable")
	}
	finalize(report)
	return report, nil
}

// classifyVersion produces static findings for a single required module.
func classifyVersion(path, version string, indirect bool) []Finding {
	var findings []Finding
	scope := "direct"
	if indirect {
		scope = "indirect"
	}
	switch {
	case IsPseudoVersion(version):
		sev := SeverityMedium
		if indirect {
			sev = SeverityLow
		}
		findings = append(findings, Finding{
			Severity: sev,
			Category: CategoryPseudoVersion,
			Module:   path,
			Version:  version,
			Detail:   fmt.Sprintf("%s dependency pinned to an untagged commit (pseudo-version)", scope),
		})
	case IsIncompatible(version):
		findings = append(findings, Finding{
			Severity: SeverityMedium,
			Category: CategoryIncompatible,
			Module:   path,
			Version:  version,
			Detail:   fmt.Sprintf("%s dependency uses +incompatible (module is not yet migrated to Go modules)", scope),
		})
	case IsPreRelease(version):
		sev := SeverityLow
		if !indirect {
			sev = SeverityMedium
		}
		findings = append(findings, Finding{
			Severity: sev,
			Category: CategoryPreRelease,
			Module:   path,
			Version:  version,
			Detail:   fmt.Sprintf("%s dependency is pre-1.0 (v0.x); API stability is not guaranteed", scope),
		})
	}
	return findings
}

// classifyIndirectSurface flags an unusually large indirect-dependency surface.
func classifyIndirectSurface(direct, indirect int) (Finding, bool) {
	if indirect < 30 {
		return Finding{}, false
	}
	sev := SeverityLow
	detail := fmt.Sprintf("%d indirect dependencies expand the audit and supply-chain surface", indirect)
	if indirect >= 60 || (direct > 0 && indirect > direct*5) {
		sev = SeverityMedium
		detail = fmt.Sprintf("%d indirect dependencies (vs %d direct) is a large transitive surface to audit", indirect, direct)
	}
	return Finding{
		Severity: sev,
		Category: CategoryIndirect,
		Detail:   detail,
	}, true
}

// classifyGoDirective flags a go.mod `go` directive that lags the toolchain.
func classifyGoDirective(goDirective, toolchain string) (Finding, bool) {
	dm := goMinor(goDirective)
	tm := goMinor(toolchain)
	if dm < 0 || tm < 0 {
		return Finding{}, false
	}
	lag := tm - dm
	if lag < 2 {
		return Finding{}, false
	}
	sev := SeverityLow
	if lag >= 4 {
		sev = SeverityMedium
	}
	return Finding{
		Severity: sev,
		Category: CategoryGoDirective,
		Detail: fmt.Sprintf("go directive (1.%d) lags the building toolchain (%s) by %d minor versions; language and security fixes may be unavailable",
			dm, strings.TrimPrefix(toolchain, "go"), lag),
	}, true
}

// finalize sorts findings by severity and fills in summary counts.
func finalize(r *Report) {
	sort.SliceStable(r.Findings, func(i, j int) bool {
		if r.Findings[i].Severity.rank() != r.Findings[j].Severity.rank() {
			return r.Findings[i].Severity.rank() > r.Findings[j].Severity.rank()
		}
		if r.Findings[i].Category != r.Findings[j].Category {
			return r.Findings[i].Category < r.Findings[j].Category
		}
		return r.Findings[i].Module < r.Findings[j].Module
	})
	r.Summary.Total = len(r.Findings)
	r.Summary.High, r.Summary.Medium, r.Summary.Low = 0, 0, 0
	for _, f := range r.Findings {
		switch f.Severity {
		case SeverityHigh:
			r.Summary.High++
		case SeverityMedium:
			r.Summary.Medium++
		case SeverityLow:
			r.Summary.Low++
		}
	}
}

// FilterBySeverity returns a copy of report containing only findings at or
// above min severity, with summary counts recomputed.
func FilterBySeverity(report *Report, min Severity) *Report {
	if min == SeverityLow {
		return report
	}
	filtered := *report
	filtered.Findings = nil
	for _, f := range report.Findings {
		if f.Severity.AtLeast(min) {
			filtered.Findings = append(filtered.Findings, f)
		}
	}
	finalize(&filtered)
	// finalize overwrites dep counts indirectly? No—it only touches Total/High/
	// Medium/Low. Restore the surface counts that came from the original scan.
	filtered.Summary.DirectDeps = report.Summary.DirectDeps
	filtered.Summary.IndirectDeps = report.Summary.IndirectDeps
	filtered.Summary.GoSumModules = report.Summary.GoSumModules
	filtered.Summary.VulnChecked = report.Summary.VulnChecked
	filtered.Summary.UpdatesChecked = report.Summary.UpdatesChecked
	return &filtered
}
