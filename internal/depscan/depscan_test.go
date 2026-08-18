package depscan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsPseudoVersion(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		{"v0.0.0-20250623103423-23b8fd6302d7", true},
		{"v0.21.1-0.20250623103423-23b8fd6302d7", true},
		{"v1.2.3-0.20191109021931-daa7c04131f5", true},
		{"v1.2.3", false},
		{"v0.10.0", false},
		{"v3.2.1+incompatible", false},
		{"v0.0.0", false},
	}
	for _, c := range cases {
		if got := IsPseudoVersion(c.version); got != c.want {
			t.Errorf("IsPseudoVersion(%q) = %v, want %v", c.version, got, c.want)
		}
	}
}

func TestIsPreRelease(t *testing.T) {
	if !IsPreRelease("v0.8.0") {
		t.Error("v0.8.0 should be pre-release")
	}
	if IsPreRelease("v1.0.0") {
		t.Error("v1.0.0 should not be pre-release")
	}
	if IsPreRelease("v10.0.0") {
		t.Error("v10.0.0 should not be pre-release")
	}
}

func TestIsIncompatible(t *testing.T) {
	if !IsIncompatible("v3.2.1+incompatible") {
		t.Error("expected +incompatible to be detected")
	}
	if IsIncompatible("v1.2.3") {
		t.Error("v1.2.3 is compatible")
	}
}

func TestParseSeverity(t *testing.T) {
	cases := []struct {
		in      string
		want    Severity
		wantErr bool
	}{
		{"high", SeverityHigh, false},
		{"MEDIUM", SeverityMedium, false},
		{"med", SeverityMedium, false},
		{"low", SeverityLow, false},
		{"", SeverityLow, false},
		{"bogus", SeverityLow, true},
	}
	for _, c := range cases {
		got, err := ParseSeverity(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("ParseSeverity(%q) err = %v, wantErr %v", c.in, err, c.wantErr)
		}
		if got != c.want {
			t.Errorf("ParseSeverity(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSeverityAtLeast(t *testing.T) {
	if !SeverityHigh.AtLeast(SeverityLow) {
		t.Error("high should be at least low")
	}
	if SeverityLow.AtLeast(SeverityMedium) {
		t.Error("low should not be at least medium")
	}
	if !SeverityMedium.AtLeast(SeverityMedium) {
		t.Error("medium should be at least medium")
	}
}

func TestDecodeModInfo(t *testing.T) {
	data := []byte(`{
		"Module": {"Path": "example.com/proj"},
		"Go": "1.22",
		"Require": [
			{"Path": "example.com/a", "Version": "v1.0.0"},
			{"Path": "example.com/b", "Version": "v0.0.0-20250101000000-abcdefabcdef", "Indirect": true}
		],
		"Replace": [
			{"Old": {"Path": "example.com/a"}, "New": {"Path": "../local/a"}}
		]
	}`)
	mi, err := decodeModInfo(data)
	if err != nil {
		t.Fatalf("decodeModInfo: %v", err)
	}
	if mi.Module.Path != "example.com/proj" {
		t.Errorf("module path = %q", mi.Module.Path)
	}
	if mi.Go != "1.22" {
		t.Errorf("go directive = %q", mi.Go)
	}
	if len(mi.Require) != 2 {
		t.Fatalf("require count = %d, want 2", len(mi.Require))
	}
	if !mi.Require[1].Indirect {
		t.Error("second require should be indirect")
	}
	if len(mi.Replace) != 1 || mi.Replace[0].New.Path != "../local/a" {
		t.Errorf("replace not parsed: %+v", mi.Replace)
	}
}

func TestDecodeModInfoInvalid(t *testing.T) {
	if _, err := decodeModInfo([]byte("not json")); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestClassifyVersion(t *testing.T) {
	// Pseudo-version, direct -> medium.
	f := classifyVersion("example.com/a", "v0.0.0-20250101000000-abcdefabcdef", false)
	if len(f) != 1 || f[0].Category != CategoryPseudoVersion || f[0].Severity != SeverityMedium {
		t.Errorf("direct pseudo-version: %+v", f)
	}
	// Pseudo-version, indirect -> low.
	f = classifyVersion("example.com/a", "v0.0.0-20250101000000-abcdefabcdef", true)
	if len(f) != 1 || f[0].Severity != SeverityLow {
		t.Errorf("indirect pseudo-version: %+v", f)
	}
	// +incompatible -> medium.
	f = classifyVersion("example.com/a", "v3.0.0+incompatible", false)
	if len(f) != 1 || f[0].Category != CategoryIncompatible {
		t.Errorf("incompatible: %+v", f)
	}
	// Pre-1.0 direct -> medium.
	f = classifyVersion("example.com/a", "v0.8.0", false)
	if len(f) != 1 || f[0].Category != CategoryPreRelease || f[0].Severity != SeverityMedium {
		t.Errorf("pre-release direct: %+v", f)
	}
	// Pre-1.0 indirect -> low.
	f = classifyVersion("example.com/a", "v0.8.0", true)
	if len(f) != 1 || f[0].Severity != SeverityLow {
		t.Errorf("pre-release indirect: %+v", f)
	}
	// Stable release -> no findings.
	f = classifyVersion("example.com/a", "v1.4.2", false)
	if len(f) != 0 {
		t.Errorf("stable release should produce no findings: %+v", f)
	}
}

func TestClassifyIndirectSurface(t *testing.T) {
	if _, ok := classifyIndirectSurface(10, 5); ok {
		t.Error("small surface should not be flagged")
	}
	f, ok := classifyIndirectSurface(10, 35)
	if !ok || f.Severity != SeverityLow {
		t.Errorf("moderate surface: %+v ok=%v", f, ok)
	}
	f, ok = classifyIndirectSurface(10, 80)
	if !ok || f.Severity != SeverityMedium {
		t.Errorf("large surface: %+v ok=%v", f, ok)
	}
	// Disproportionate ratio.
	f, ok = classifyIndirectSurface(5, 31)
	if !ok || f.Severity != SeverityMedium {
		t.Errorf("disproportionate surface: %+v ok=%v", f, ok)
	}
}

func TestClassifyGoDirective(t *testing.T) {
	if _, ok := classifyGoDirective("1.25", "go1.26.3"); ok {
		t.Error("1-version lag should not be flagged")
	}
	f, ok := classifyGoDirective("1.24", "go1.26.3")
	if !ok || f.Severity != SeverityLow {
		t.Errorf("2-version lag: %+v ok=%v", f, ok)
	}
	f, ok = classifyGoDirective("1.20", "go1.26.3")
	if !ok || f.Severity != SeverityMedium {
		t.Errorf("6-version lag: %+v ok=%v", f, ok)
	}
	if _, ok := classifyGoDirective("garbage", "go1.26.3"); ok {
		t.Error("unparseable directive should not be flagged")
	}
}

func TestGoMinor(t *testing.T) {
	cases := map[string]int{
		"1.25.5":   25,
		"go1.26.3": 26,
		"1.22":     22,
		"garbage":  -1,
		"1":        -1,
	}
	for in, want := range cases {
		if got := goMinor(in); got != want {
			t.Errorf("goMinor(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestFinalizeSortsAndCounts(t *testing.T) {
	r := &Report{Findings: []Finding{
		{Severity: SeverityLow, Category: CategoryPreRelease, Module: "z"},
		{Severity: SeverityHigh, Category: CategoryReplace, Module: "a"},
		{Severity: SeverityMedium, Category: CategoryIncompatible, Module: "m"},
		{Severity: SeverityHigh, Category: CategoryReplace, Module: "b"},
	}}
	finalize(r)
	if r.Summary.Total != 4 || r.Summary.High != 2 || r.Summary.Medium != 1 || r.Summary.Low != 1 {
		t.Errorf("summary counts wrong: %+v", r.Summary)
	}
	// High first, and within equal severity+category, sorted by module.
	if r.Findings[0].Severity != SeverityHigh || r.Findings[0].Module != "a" {
		t.Errorf("first finding = %+v", r.Findings[0])
	}
	if r.Findings[1].Module != "b" {
		t.Errorf("second finding should be module b, got %+v", r.Findings[1])
	}
	if r.Findings[3].Severity != SeverityLow {
		t.Errorf("last finding should be low: %+v", r.Findings[3])
	}
}

func TestFilterBySeverity(t *testing.T) {
	r := &Report{
		Findings: []Finding{
			{Severity: SeverityLow, Category: CategoryPreRelease},
			{Severity: SeverityHigh, Category: CategoryReplace},
			{Severity: SeverityMedium, Category: CategoryIncompatible},
		},
		Summary: Summary{DirectDeps: 12, IndirectDeps: 40, GoSumModules: 99},
	}
	finalize(r)

	// low == identity.
	if got := FilterBySeverity(r, SeverityLow); got != r {
		t.Error("filtering at low should return the same report")
	}

	med := FilterBySeverity(r, SeverityMedium)
	if med.Summary.Total != 2 || med.Summary.High != 1 || med.Summary.Medium != 1 || med.Summary.Low != 0 {
		t.Errorf("medium filter summary: %+v", med.Summary)
	}
	// Surface counts must survive filtering.
	if med.Summary.DirectDeps != 12 || med.Summary.IndirectDeps != 40 || med.Summary.GoSumModules != 99 {
		t.Errorf("surface counts lost after filter: %+v", med.Summary)
	}

	high := FilterBySeverity(r, SeverityHigh)
	if high.Summary.Total != 1 || high.Findings[0].Category != CategoryReplace {
		t.Errorf("high filter: %+v", high.Findings)
	}
}

func TestCountGoSumModules(t *testing.T) {
	dir := t.TempDir()
	goMod := filepath.Join(dir, "go.mod")

	// Missing go.sum: ok == false.
	if n, ok := countGoSumModules(goMod); ok || n != 0 {
		t.Errorf("missing go.sum: got (%d, %v), want (0, false)", n, ok)
	}

	// Present go.sum: distinct module@version, ignoring /go.mod hash lines.
	sum := "example.com/a v1.0.0 h1:aaa=\n" +
		"example.com/a v1.0.0/go.mod h1:bbb=\n" +
		"example.com/b v2.1.0 h1:ccc=\n" +
		"example.com/b v2.1.0/go.mod h1:ddd=\n"
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), []byte(sum), 0o644); err != nil {
		t.Fatalf("write go.sum: %v", err)
	}
	if n, ok := countGoSumModules(goMod); !ok || n != 2 {
		t.Errorf("present go.sum: got (%d, %v), want (2, true)", n, ok)
	}
}

func TestParseGovulncheck(t *testing.T) {
	// Two streamed messages: one OSV definition, one finding referencing it.
	data := []byte(`{"osv":{"id":"GO-2024-0001","summary":"Example vuln in pkg"}}
{"finding":{"osv":"GO-2024-0001","trace":[{"module":"example.com/pkg","version":"v1.2.0"}]}}
{"finding":{"osv":"GO-2024-0001","trace":[{"module":"example.com/pkg","version":"v1.2.0"}]}}`)
	findings := parseGovulncheck(data)
	if len(findings) != 1 {
		t.Fatalf("expected 1 deduplicated finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Severity != SeverityHigh || f.Category != CategoryVulnerability {
		t.Errorf("vuln finding severity/category: %+v", f)
	}
	if f.Module != "example.com/pkg" || f.Version != "v1.2.0" {
		t.Errorf("vuln finding module/version: %+v", f)
	}
	if f.Detail != "GO-2024-0001: Example vuln in pkg" {
		t.Errorf("vuln finding detail: %q", f.Detail)
	}
}

func TestParseGovulncheckEmpty(t *testing.T) {
	if f := parseGovulncheck([]byte("")); len(f) != 0 {
		t.Errorf("empty input should yield no findings, got %+v", f)
	}
}

func TestParseUpdateList(t *testing.T) {
	data := []byte(`{"Path":"example.com/proj","Version":"v1.0.0","Main":true}
{"Path":"example.com/a","Version":"v1.0.0","Update":{"Version":"v1.1.0"}}
{"Path":"example.com/b","Version":"v2.0.0","Indirect":true,"Update":{"Version":"v2.3.1"}}
{"Path":"example.com/c","Version":"v3.0.0"}`)
	findings := parseUpdateList(data)
	if len(findings) != 2 {
		t.Fatalf("expected 2 outdated findings, got %d: %+v", len(findings), findings)
	}
	for _, f := range findings {
		if f.Category != CategoryOutdated || f.Severity != SeverityLow {
			t.Errorf("outdated finding wrong shape: %+v", f)
		}
	}
	if findings[0].Module != "example.com/a" || findings[1].Module != "example.com/b" {
		t.Errorf("unexpected modules: %+v", findings)
	}
}
