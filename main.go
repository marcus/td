package main

import (
	"runtime/debug"
	"strings"

	"github.com/marcus/td/cmd"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/query"
)

// Version may be set at build time via -ldflags "-X main.Version=...".
// If left as "dev", we will attempt to derive a version from Go build info.
var Version = "dev"

func init() {
	// Wire up TDQ validator to break import cycle between db and query packages
	db.QueryValidator = func(queryStr string) error {
		_, err := query.Parse(queryStr)
		return err
	}
}

func effectiveVersion(v string) string {
	// If the build injected a real version, prefer it.
	if v != "" && v != "dev" {
		return v
	}

	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return v
	}

	// When installed via `go install module@vX.Y.Z`, this will typically be `vX.Y.Z`.
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}

	// Otherwise, try to provide a slightly more useful dev version.
	var rev, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if rev != "" {
		short := rev
		if len(short) > 12 {
			short = short[:12]
		}
		parts := []string{"devel", short}
		if modified == "true" {
			parts = append(parts, "dirty")
		}
		return strings.Join(parts, "+")
	}

	return v
}

func main() {
	cmd.SetVersion(effectiveVersion(Version))
	cmd.Execute()
}
