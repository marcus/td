package cmd

import (
	"reflect"
	"testing"
)

// TestMergeMultiValueFlag tests the mergeMultiValueFlag helper function
func TestMergeMultiValueFlag(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		expect []string
	}{
		{
			name:   "single comma-separated value",
			input:  []string{"dispatch,agent:claude"},
			expect: []string{"dispatch", "agent:claude"},
		},
		{
			name:   "repeated flags",
			input:  []string{"dispatch", "agent:claude"},
			expect: []string{"dispatch", "agent:claude"},
		},
		{
			name:   "mixed repeated and comma-separated",
			input:  []string{"dispatch,agent:claude", "backend"},
			expect: []string{"dispatch", "agent:claude", "backend"},
		},
		{
			name:   "deduplication",
			input:  []string{"dispatch", "dispatch"},
			expect: []string{"dispatch"},
		},
		{
			name:   "deduplication across comma and repeated",
			input:  []string{"a,b", "b,c"},
			expect: []string{"a", "b", "c"},
		},
		{
			name:   "whitespace trimming",
			input:  []string{" dispatch , agent:claude "},
			expect: []string{"dispatch", "agent:claude"},
		},
		{
			name:   "empty strings filtered",
			input:  []string{"", "dispatch", ""},
			expect: []string{"dispatch"},
		},
		{
			name:   "all empty returns nil",
			input:  []string{"", ""},
			expect: nil,
		},
		{
			name:   "nil input returns nil",
			input:  nil,
			expect: nil,
		},
		{
			name:   "single value no comma",
			input:  []string{"dispatch"},
			expect: []string{"dispatch"},
		},
		{
			name:   "trailing comma",
			input:  []string{"dispatch,"},
			expect: []string{"dispatch"},
		},
		{
			name:   "leading comma",
			input:  []string{",dispatch"},
			expect: []string{"dispatch"},
		},
		{
			name:   "three repeated flags",
			input:  []string{"a", "b", "c"},
			expect: []string{"a", "b", "c"},
		},
		{
			name:   "issue IDs for depends-on",
			input:  []string{"td-abc123", "td-def456"},
			expect: []string{"td-abc123", "td-def456"},
		},
		{
			name:   "comma-separated issue IDs",
			input:  []string{"td-abc123,td-def456"},
			expect: []string{"td-abc123", "td-def456"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeMultiValueFlag(tt.input)
			if !reflect.DeepEqual(got, tt.expect) {
				t.Errorf("mergeMultiValueFlag(%v) = %v, want %v", tt.input, got, tt.expect)
			}
		})
	}
}

// TestCreateLabelsRepeatedFlag tests that create command labels flag accepts StringArray
func TestCreateLabelsRepeatedFlag(t *testing.T) {
	flag := createCmd.Flags().Lookup("labels")
	if flag == nil {
		t.Fatal("Expected --labels flag to be defined")
	}
	if flag.Value.Type() != "stringArray" {
		t.Errorf("Expected --labels to be stringArray, got %s", flag.Value.Type())
	}
	if flag.Shorthand != "l" {
		t.Errorf("Expected --labels shorthand to be 'l', got %q", flag.Shorthand)
	}
}

// TestCreateDependsOnRepeatedFlag tests that create command depends-on flag accepts StringArray
func TestCreateDependsOnRepeatedFlag(t *testing.T) {
	flag := createCmd.Flags().Lookup("depends-on")
	if flag == nil {
		t.Fatal("Expected --depends-on flag to be defined")
	}
	if flag.Value.Type() != "stringArray" {
		t.Errorf("Expected --depends-on to be stringArray, got %s", flag.Value.Type())
	}
}

// TestCreateBlocksRepeatedFlag tests that create command blocks flag accepts StringArray
func TestCreateBlocksRepeatedFlag(t *testing.T) {
	flag := createCmd.Flags().Lookup("blocks")
	if flag == nil {
		t.Fatal("Expected --blocks flag to be defined")
	}
	if flag.Value.Type() != "stringArray" {
		t.Errorf("Expected --blocks to be stringArray, got %s", flag.Value.Type())
	}
}

// TestUpdateLabelsRepeatedFlag tests that update command labels flag accepts StringArray
func TestUpdateLabelsRepeatedFlag(t *testing.T) {
	flag := updateCmd.Flags().Lookup("labels")
	if flag == nil {
		t.Fatal("Expected --labels flag to be defined on update command")
	}
	if flag.Value.Type() != "stringArray" {
		t.Errorf("Expected --labels to be stringArray, got %s", flag.Value.Type())
	}
}

// TestUpdateDependsOnRepeatedFlag tests that update command depends-on flag accepts StringArray
func TestUpdateDependsOnRepeatedFlag(t *testing.T) {
	flag := updateCmd.Flags().Lookup("depends-on")
	if flag == nil {
		t.Fatal("Expected --depends-on flag to be defined on update command")
	}
	if flag.Value.Type() != "stringArray" {
		t.Errorf("Expected --depends-on to be stringArray, got %s", flag.Value.Type())
	}
}

// TestUpdateBlocksRepeatedFlag tests that update command blocks flag accepts StringArray
func TestUpdateBlocksRepeatedFlag(t *testing.T) {
	flag := updateCmd.Flags().Lookup("blocks")
	if flag == nil {
		t.Fatal("Expected --blocks flag to be defined on update command")
	}
	if flag.Value.Type() != "stringArray" {
		t.Errorf("Expected --blocks to be stringArray, got %s", flag.Value.Type())
	}
}

// TestTaskCreateLabelsRepeatedFlag tests that task create command labels flag accepts StringArray
func TestTaskCreateLabelsRepeatedFlag(t *testing.T) {
	flag := taskCreateCmd.Flags().Lookup("labels")
	if flag == nil {
		t.Fatal("Expected --labels flag to be defined on task create command")
	}
	if flag.Value.Type() != "stringArray" {
		t.Errorf("Expected --labels to be stringArray, got %s", flag.Value.Type())
	}
}

// TestEpicCreateLabelsRepeatedFlag tests that epic create command labels flag accepts StringArray
func TestEpicCreateLabelsRepeatedFlag(t *testing.T) {
	flag := epicCreateCmd.Flags().Lookup("labels")
	if flag == nil {
		t.Fatal("Expected --labels flag to be defined on epic create command")
	}
	if flag.Value.Type() != "stringArray" {
		t.Errorf("Expected --labels to be stringArray, got %s", flag.Value.Type())
	}
}

// TestLabelAliasesAreStringArray tests that label alias flags are also StringArray
func TestLabelAliasesAreStringArray(t *testing.T) {
	aliases := []string{"label", "tags", "tag"}
	for _, alias := range aliases {
		flag := createCmd.Flags().Lookup(alias)
		if flag == nil {
			t.Errorf("Expected --%s flag to be defined", alias)
			continue
		}
		if flag.Value.Type() != "stringArray" {
			t.Errorf("Expected --%s to be stringArray, got %s", alias, flag.Value.Type())
		}
	}
}

// TestListLabelsUnchanged tests that list command labels flag is still StringArrayP (was already correct)
func TestListLabelsUnchanged(t *testing.T) {
	flag := listCmd.Flags().Lookup("labels")
	if flag == nil {
		t.Fatal("Expected --labels flag to be defined on list command")
	}
	if flag.Value.Type() != "stringArray" {
		t.Errorf("Expected list --labels to be stringArray, got %s", flag.Value.Type())
	}
}

// TestSearchLabelsUnchanged tests that search command labels flag is still StringArrayP (was already correct)
func TestSearchLabelsUnchanged(t *testing.T) {
	flag := searchCmd.Flags().Lookup("labels")
	if flag == nil {
		t.Fatal("Expected --labels flag to be defined on search command")
	}
	if flag.Value.Type() != "stringArray" {
		t.Errorf("Expected search --labels to be stringArray, got %s", flag.Value.Type())
	}
}

// TestMergeMultiValueFlagPreservesOrder tests that order is preserved (first seen wins)
func TestMergeMultiValueFlagPreservesOrder(t *testing.T) {
	input := []string{"c", "a", "b"}
	got := mergeMultiValueFlag(input)
	expect := []string{"c", "a", "b"}
	if !reflect.DeepEqual(got, expect) {
		t.Errorf("Expected order preserved: got %v, want %v", got, expect)
	}
}
