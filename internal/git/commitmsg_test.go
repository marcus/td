package git

import (
	"strings"
	"testing"
)

func TestParseCommitMessage(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantType    string
		wantScope   string
		wantBreak   bool
		wantDesc    string
		wantBody    string
		wantTrailer int
	}{
		{
			name:     "simple",
			raw:      "feat: add thing",
			wantType: "feat",
			wantDesc: "add thing",
		},
		{
			name:      "with scope",
			raw:       "fix(parser): handle edge case",
			wantType:  "fix",
			wantScope: "parser",
			wantDesc:  "handle edge case",
		},
		{
			name:      "breaking with scope",
			raw:       "refactor(api)!: remove deprecated endpoint",
			wantType:  "refactor",
			wantScope: "api",
			wantBreak: true,
			wantDesc:  "remove deprecated endpoint",
		},
		{
			name:      "breaking without scope",
			raw:       "feat!: new API",
			wantType:  "feat",
			wantBreak: true,
			wantDesc:  "new API",
		},
		{
			name:     "with body",
			raw:      "docs: update readme\n\nAdded install instructions\nand examples.",
			wantType: "docs",
			wantDesc: "update readme",
			wantBody: "Added install instructions\nand examples.",
		},
		{
			name:        "with trailers",
			raw:         "feat: add feature\n\nSome body text.\n\nCo-Authored-By: Alice <a@b.com>\nSigned-Off-By: Bob <b@c.com>",
			wantType:    "feat",
			wantDesc:    "add feature",
			wantBody:    "Some body text.",
			wantTrailer: 2,
		},
		{
			name:        "trailers without body",
			raw:         "fix: quick patch\n\nCo-Authored-By: Alice <a@b.com>",
			wantType:    "fix",
			wantDesc:    "quick patch",
			wantTrailer: 1,
		},
		{
			name:     "non-conventional",
			raw:      "just a plain message",
			wantType: "",
			wantDesc: "just a plain message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm, err := ParseCommitMessage(tt.raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cm.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", cm.Type, tt.wantType)
			}
			if cm.Scope != tt.wantScope {
				t.Errorf("Scope = %q, want %q", cm.Scope, tt.wantScope)
			}
			if cm.Breaking != tt.wantBreak {
				t.Errorf("Breaking = %v, want %v", cm.Breaking, tt.wantBreak)
			}
			if cm.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", cm.Description, tt.wantDesc)
			}
			if cm.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", cm.Body, tt.wantBody)
			}
			if len(cm.Trailers) != tt.wantTrailer {
				t.Errorf("Trailers = %d, want %d", len(cm.Trailers), tt.wantTrailer)
			}
		})
	}
}

func TestParseEmpty(t *testing.T) {
	_, err := ParseCommitMessage("")
	if err == nil {
		t.Fatal("expected error for empty message")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cm      CommitMessage
		wantErr string // substring of first error, or "" for valid
	}{
		{
			name: "valid",
			cm:   CommitMessage{Type: "feat", Description: "add thing"},
		},
		{
			name:    "missing type",
			cm:      CommitMessage{Description: "add thing"},
			wantErr: "type is required",
		},
		{
			name:    "unknown type",
			cm:      CommitMessage{Type: "yolo", Description: "do stuff"},
			wantErr: "unknown type",
		},
		{
			name:    "empty description",
			cm:      CommitMessage{Type: "fix"},
			wantErr: "description is required",
		},
		{
			name:    "subject too long",
			cm:      CommitMessage{Type: "feat", Description: strings.Repeat("x", 70)},
			wantErr: "subject is",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := Validate(&tt.cm)
			if tt.wantErr == "" {
				if len(errs) > 0 {
					t.Errorf("unexpected errors: %v", errs)
				}
			} else {
				if len(errs) == 0 {
					t.Fatal("expected validation error, got none")
				}
				found := false
				for _, e := range errs {
					if strings.Contains(e.Error(), tt.wantErr) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, errs)
				}
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		name     string
		cm       CommitMessage
		wantType string
		wantDesc string
		wantKey  string // first trailer key, if any
	}{
		{
			name:     "lowercase type",
			cm:       CommitMessage{Type: "FEAT", Description: "add thing"},
			wantType: "feat",
			wantDesc: "add thing",
		},
		{
			name:     "lowercase description start",
			cm:       CommitMessage{Type: "fix", Description: "Add thing"},
			wantType: "fix",
			wantDesc: "add thing",
		},
		{
			name:     "strip trailing period",
			cm:       CommitMessage{Type: "fix", Description: "fix the bug."},
			wantType: "fix",
			wantDesc: "fix the bug",
		},
		{
			name:     "strip multiple trailing periods",
			cm:       CommitMessage{Type: "fix", Description: "fix the bug..."},
			wantType: "fix",
			wantDesc: "fix the bug",
		},
		{
			name:     "normalize trailer casing",
			cm:       CommitMessage{Type: "feat", Description: "x", Trailers: []Trailer{{Key: "co-authored-by", Value: "A"}}},
			wantType: "feat",
			wantDesc: "x",
			wantKey:  "Co-Authored-By",
		},
		{
			name:     "normalize mixed case trailer",
			cm:       CommitMessage{Type: "feat", Description: "x", Trailers: []Trailer{{Key: "CO-AUTHORED-BY", Value: "A"}}},
			wantType: "feat",
			wantDesc: "x",
			wantKey:  "Co-Authored-By",
		},
		{
			name:     "combined fixes",
			cm:       CommitMessage{Type: "FIX", Description: "Update thing.", Trailers: []Trailer{{Key: "signed-off-by", Value: "B"}}},
			wantType: "fix",
			wantDesc: "update thing",
			wantKey:  "Signed-Off-By",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Normalize(&tt.cm)
			if tt.cm.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", tt.cm.Type, tt.wantType)
			}
			if tt.cm.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", tt.cm.Description, tt.wantDesc)
			}
			if tt.wantKey != "" && len(tt.cm.Trailers) > 0 {
				if tt.cm.Trailers[0].Key != tt.wantKey {
					t.Errorf("Trailer key = %q, want %q", tt.cm.Trailers[0].Key, tt.wantKey)
				}
			}
		})
	}
}

func TestFormatRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "simple",
			raw:  "feat: add thing\n",
		},
		{
			name: "with scope",
			raw:  "fix(db): handle nil\n",
		},
		{
			name: "with body",
			raw:  "docs: update readme\n\nMore details here.\n",
		},
		{
			name: "with body and trailers",
			raw:  "feat: add feature\n\nBody paragraph.\n\nCo-Authored-By: Alice <a@b.com>\nSigned-Off-By: Bob <b@c.com>\n",
		},
		{
			name: "breaking",
			raw:  "refactor(api)!: drop v1\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm, err := ParseCommitMessage(tt.raw)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			got := FormatCommitMessage(cm)
			if got != tt.raw {
				t.Errorf("round-trip mismatch:\n got: %q\nwant: %q", got, tt.raw)
			}
		})
	}
}

func TestFormatSubject(t *testing.T) {
	cm := &CommitMessage{Type: "feat", Scope: "cli", Description: "add flag"}
	got := FormatSubject(cm)
	want := "feat(cli): add flag"
	if got != want {
		t.Errorf("FormatSubject = %q, want %q", got, want)
	}
}
