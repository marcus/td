package api

import (
	"testing"
)

func TestValidateScopes(t *testing.T) {
	tests := []struct {
		name    string
		scopes  string
		wantErr bool
	}{
		{"empty", "", false},
		{"sync only", "sync", false},
		{"single admin scope", AdminScopeReadServer, false},
		{"multiple admin scopes", "admin:read:server,admin:read:projects,admin:export", false},
		{"admin and sync mixed", "sync,admin:read:events", false},
		{"with whitespace", " admin:read:server , sync ", false},
		{"invalid scope", "admin:write:server", true},
		{"mixed valid and invalid", "sync,admin:read:server,bogus", true},
		{"completely unknown", "foo", true},
		{"trailing comma (empty part)", "sync,", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateScopes(tt.scopes)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateScopes(%q) error = %v, wantErr %v", tt.scopes, err, tt.wantErr)
			}
		})
	}
}

func TestHasScope(t *testing.T) {
	tests := []struct {
		name     string
		scopes   string
		required string
		want     bool
	}{
		{"present single", "admin:read:server", "admin:read:server", true},
		{"present in list", "sync,admin:read:server,admin:export", "admin:read:server", true},
		{"absent", "sync,admin:export", "admin:read:server", false},
		{"empty scopes", "", "admin:read:server", false},
		{"with whitespace", " admin:read:server , sync ", "admin:read:server", true},
		{"sync scope", "sync", "sync", true},
		{"partial match not accepted", "admin:read:serverx", "admin:read:server", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasScope(tt.scopes, tt.required)
			if got != tt.want {
				t.Errorf("HasScope(%q, %q) = %v, want %v", tt.scopes, tt.required, got, tt.want)
			}
		})
	}
}

func TestAllScopeConstantsInValidMap(t *testing.T) {
	scopes := []string{
		AdminScopeReadServer,
		AdminScopeReadProjects,
		AdminScopeReadEvents,
		AdminScopeReadSnapshots,
		AdminScopeExport,
	}
	for _, s := range scopes {
		if !ValidAdminScopes[s] {
			t.Errorf("scope constant %q not in ValidAdminScopes map", s)
		}
	}
	// Also verify the map has exactly the expected number of entries.
	if len(ValidAdminScopes) != len(scopes) {
		t.Errorf("ValidAdminScopes has %d entries, expected %d", len(ValidAdminScopes), len(scopes))
	}
}
