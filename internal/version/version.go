// Package version provides update checking against GitHub releases and
// semantic version comparison.
package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	repoOwner = "marcus"
	repoName  = "td"
	apiURL    = "https://api.github.com/repos/%s/%s/releases/latest"
)

// Release represents a GitHub release response.
type Release struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

// CheckResult holds the result of a version check.
type CheckResult struct {
	CurrentVersion string
	LatestVersion  string
	UpdateURL      string
	HasUpdate      bool
	Error          error
}

// Check fetches the latest release from GitHub and compares versions.
func Check(currentVersion string) CheckResult {
	result := CheckResult{CurrentVersion: currentVersion}

	if IsDevelopmentVersion(currentVersion) {
		return result
	}

	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf(apiURL, repoOwner, repoName)

	resp, err := client.Get(url)
	if err != nil {
		result.Error = err
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Errorf("github api: %s", resp.Status)
		return result
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		result.Error = err
		return result
	}

	result.LatestVersion = release.TagName
	result.UpdateURL = release.HTMLURL
	result.HasUpdate = isNewer(release.TagName, currentVersion)

	return result
}

// IsDevelopmentVersion returns true for non-release versions.
func IsDevelopmentVersion(v string) bool {
	if v == "" || v == "unknown" || v == "dev" || v == "devel" {
		return true
	}
	if strings.HasPrefix(v, "devel+") {
		return true
	}
	return false
}

// validVersionRegex matches valid semver versions (v1.2.3, v1.2.3-beta, etc.)
// Prerelease identifiers must be alphanumeric, separated by dots or hyphens.
// Rejects double hyphens (v1.2.3--), trailing hyphens (v1.2.3-), etc.
var validVersionRegex = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[a-zA-Z0-9]+([.-][a-zA-Z0-9]+)*)?$`)

// UpdateCommand generates the go install command for updating.
// Returns empty string if version is invalid (prevents shell injection).
func UpdateCommand(version string) string {
	if !validVersionRegex.MatchString(version) {
		return ""
	}
	return fmt.Sprintf(
		"go install -ldflags \"-X main.Version=%s\" github.com/marcus/td@%s",
		version, version,
	)
}
