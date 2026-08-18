package depscan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
)

// runGovulncheck runs `govulncheck -json ./...` in dir and parses the result
// for known vulnerabilities. It degrades gracefully: when govulncheck is not
// on PATH (or fails, e.g. offline with no cached DB) it returns a human note
// instead of findings.
func runGovulncheck(dir string) ([]Finding, string) {
	if _, err := exec.LookPath("govulncheck"); err != nil {
		return nil, "govulncheck not found on PATH; skipped known-CVE scan (install with: go install golang.org/x/vuln/cmd/govulncheck@latest)"
	}
	cmd := exec.Command("govulncheck", "-json", "./...")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// govulncheck exits non-zero when vulnerabilities are found; that is not an
	// error for us, so we parse stdout regardless of exit code and only treat a
	// total lack of output as a failure.
	runErr := cmd.Run()
	if stdout.Len() == 0 {
		msg := "govulncheck produced no output; skipped known-CVE scan (network or vuln DB may be unavailable)"
		if runErr != nil {
			msg = fmt.Sprintf("govulncheck failed: %v; skipped known-CVE scan", runErr)
		}
		return nil, msg
	}
	return parseGovulncheck(stdout.Bytes()), ""
}

// govulnMessage is one streamed message from `govulncheck -json`.
type govulnMessage struct {
	OSV *struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
	} `json:"osv"`
	Finding *struct {
		OSV   string `json:"osv"`
		Trace []struct {
			Module  string `json:"module"`
			Version string `json:"version"`
		} `json:"trace"`
	} `json:"finding"`
}

// parseGovulncheck decodes the streamed JSON output of govulncheck into
// findings, deduplicating by OSV id. Split out from runGovulncheck for testing.
func parseGovulncheck(data []byte) []Finding {
	dec := json.NewDecoder(bytes.NewReader(data))
	summaries := map[string]string{}
	type vuln struct {
		module, version string
	}
	vulns := map[string]vuln{}
	for dec.More() {
		var msg govulnMessage
		if err := dec.Decode(&msg); err != nil {
			break
		}
		if msg.OSV != nil {
			summaries[msg.OSV.ID] = msg.OSV.Summary
		}
		if msg.Finding != nil && msg.Finding.OSV != "" {
			v := vulns[msg.Finding.OSV]
			for _, t := range msg.Finding.Trace {
				if t.Module != "" {
					v.module = t.Module
					v.version = t.Version
					break
				}
			}
			vulns[msg.Finding.OSV] = v
		}
	}
	var findings []Finding
	for id, v := range vulns {
		detail := id
		if s := summaries[id]; s != "" {
			detail = fmt.Sprintf("%s: %s", id, s)
		}
		findings = append(findings, Finding{
			Severity: SeverityHigh,
			Category: CategoryVulnerability,
			Module:   v.module,
			Version:  v.version,
			Detail:   detail,
		})
	}
	return findings
}

// runUpdateCheck runs `go list -m -u -json all` in dir to find modules with a
// newer version available. It degrades gracefully when the module graph cannot
// be loaded (e.g. offline), returning a note instead of findings.
func runUpdateCheck(dir string) ([]Finding, string) {
	cmd := exec.Command("go", "list", "-m", "-u", "-json", "all")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Sprintf("could not check for updates (`go list -m -u` failed: %v); network may be unavailable", err)
	}
	return parseUpdateList(stdout.Bytes()), ""
}

// goListModule is one module object from `go list -m -u -json all`.
type goListModule struct {
	Path     string `json:"Path"`
	Version  string `json:"Version"`
	Indirect bool   `json:"Indirect"`
	Main     bool   `json:"Main"`
	Update   *struct {
		Version string `json:"Version"`
	} `json:"Update"`
}

// parseUpdateList decodes the streamed JSON output of `go list -m -u -json all`
// into outdated-module findings. Split out from runUpdateCheck for testing.
func parseUpdateList(data []byte) []Finding {
	dec := json.NewDecoder(bytes.NewReader(data))
	var findings []Finding
	for dec.More() {
		var m goListModule
		if err := dec.Decode(&m); err != nil {
			break
		}
		if m.Main || m.Update == nil || m.Update.Version == "" {
			continue
		}
		scope := "direct"
		sev := SeverityLow
		if m.Indirect {
			scope = "indirect"
		}
		findings = append(findings, Finding{
			Severity: sev,
			Category: CategoryOutdated,
			Module:   m.Path,
			Version:  m.Version,
			Detail:   fmt.Sprintf("%s dependency is outdated; %s is available", scope, m.Update.Version),
		})
	}
	return findings
}
