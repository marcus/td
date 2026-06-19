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
	AdminScopeWriteUsers    = "admin:write:users"
)

// ImpersonationScopeRead is the scope carried by admin "view-as" ephemeral
// keys. It is NOT an admin scope — it grants read-only access to the target
// user's /v1/projects/* surface and nothing else.
const ImpersonationScopeRead = "impersonation:read"

// ValidAdminScopes contains all recognized admin scopes.
var ValidAdminScopes = map[string]bool{
	AdminScopeReadServer:    true,
	AdminScopeReadProjects:  true,
	AdminScopeReadEvents:    true,
	AdminScopeReadSnapshots: true,
	AdminScopeExport:        true,
	AdminScopeWriteUsers:    true,
}

// ValidateScopes checks that every comma-separated scope is either "sync",
// a recognized admin scope, or the impersonation:read scope. Returns an
// error listing the first invalid scope.
func ValidateScopes(scopes string) error {
	if scopes == "" {
		return nil
	}
	for _, s := range strings.Split(scopes, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if s == "sync" || s == ImpersonationScopeRead {
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

// HasAnyScope reports whether the comma-separated scopes string contains ANY
// of the required scopes. Returns false if no required scopes are provided.
func HasAnyScope(scopes string, required ...string) bool {
	for _, s := range strings.Split(scopes, ",") {
		s = strings.TrimSpace(s)
		for _, r := range required {
			if s == r {
				return true
			}
		}
	}
	return false
}
