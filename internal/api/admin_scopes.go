package api

import (
	"fmt"
	"strings"
)

// Admin scope constants for the admin API.
const (
	AdminScopeReadServer    = "admin:read:server"
	AdminScopeReadProjects  = "admin:read:projects"
	AdminScopeReadEvents    = "admin:read:events"
	AdminScopeReadSnapshots = "admin:read:snapshots"
	AdminScopeExport        = "admin:export"
)

// ValidAdminScopes contains all recognized admin scopes.
var ValidAdminScopes = map[string]bool{
	AdminScopeReadServer:    true,
	AdminScopeReadProjects:  true,
	AdminScopeReadEvents:    true,
	AdminScopeReadSnapshots: true,
	AdminScopeExport:        true,
}

// ValidateScopes checks that every comma-separated scope is either "sync" or
// a recognized admin scope. Returns an error listing the first invalid scope.
func ValidateScopes(scopes string) error {
	if scopes == "" {
		return nil
	}
	for _, s := range strings.Split(scopes, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if s == "sync" {
			continue
		}
		if !ValidAdminScopes[s] {
			return fmt.Errorf("invalid scope: %q", s)
		}
	}
	return nil
}

// HasScope reports whether the comma-separated scopes string contains the
// required scope.
func HasScope(scopes string, required string) bool {
	for _, s := range strings.Split(scopes, ",") {
		if strings.TrimSpace(s) == required {
			return true
		}
	}
	return false
}
