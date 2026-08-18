package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestConfigCompat_MissingFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load with missing file should not error, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load with missing file should return non-nil Config")
	}
	// All fields should be zero-value
	if cfg.FocusedIssueID != "" {
		t.Errorf("expected empty FocusedIssueID, got %q", cfg.FocusedIssueID)
	}
	if cfg.SortMode != "" {
		t.Errorf("expected empty SortMode, got %q", cfg.SortMode)
	}
}

func TestConfigCompat_EmptyJSON(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".todos")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load with empty JSON: %v", err)
	}
	if cfg.FocusedIssueID != "" {
		t.Errorf("expected empty FocusedIssueID, got %q", cfg.FocusedIssueID)
	}
}

func TestConfigCompat_PartialFields(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".todos")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write a config with only some fields set
	partial := `{"focused_issue_id": "td-abc123", "sort_mode": "priority"}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(partial), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load with partial fields: %v", err)
	}
	if cfg.FocusedIssueID != "td-abc123" {
		t.Errorf("FocusedIssueID: got %q, want %q", cfg.FocusedIssueID, "td-abc123")
	}
	if cfg.SortMode != "priority" {
		t.Errorf("SortMode: got %q, want %q", cfg.SortMode, "priority")
	}
	// Unset fields should be zero-value
	if cfg.ActiveWorkSession != "" {
		t.Errorf("expected empty ActiveWorkSession, got %q", cfg.ActiveWorkSession)
	}
	if cfg.IncludeClosed {
		t.Error("expected IncludeClosed=false for missing field")
	}
	if cfg.TitleMinLength != 0 {
		t.Errorf("expected TitleMinLength=0 for missing field, got %d", cfg.TitleMinLength)
	}
}

func TestConfigCompat_UnknownExtraFields(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".todos")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// JSON with known + unknown fields (forward compat: newer config, older binary)
	withExtra := `{
		"focused_issue_id": "td-xyz",
		"future_field_v99": true,
		"another_unknown": {"nested": "value"},
		"sort_mode": "updated"
	}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(withExtra), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load with extra fields should not error, got: %v", err)
	}
	if cfg.FocusedIssueID != "td-xyz" {
		t.Errorf("FocusedIssueID: got %q, want %q", cfg.FocusedIssueID, "td-xyz")
	}
	if cfg.SortMode != "updated" {
		t.Errorf("SortMode: got %q, want %q", cfg.SortMode, "updated")
	}
}

func TestConfigCompat_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	original := &models.Config{
		FocusedIssueID:    "td-roundtrip",
		ActiveWorkSession: "ws-001",
		PaneHeights:       [3]float64{0.3, 0.4, 0.3},
		FeatureFlags:      map[string]bool{"dark_mode": true, "beta": false},
		SearchQuery:       "status:open",
		SortMode:          "created",
		TypeFilter:        "bug",
		IncludeClosed:     true,
		TitleMinLength:    10,
		TitleMaxLength:    200,
	}

	if err := Save(dir, original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}

	if loaded.FocusedIssueID != original.FocusedIssueID {
		t.Errorf("FocusedIssueID mismatch: %q vs %q", loaded.FocusedIssueID, original.FocusedIssueID)
	}
	if loaded.ActiveWorkSession != original.ActiveWorkSession {
		t.Errorf("ActiveWorkSession mismatch: %q vs %q", loaded.ActiveWorkSession, original.ActiveWorkSession)
	}
	if loaded.PaneHeights != original.PaneHeights {
		t.Errorf("PaneHeights mismatch: %v vs %v", loaded.PaneHeights, original.PaneHeights)
	}
	if loaded.SearchQuery != original.SearchQuery {
		t.Errorf("SearchQuery mismatch: %q vs %q", loaded.SearchQuery, original.SearchQuery)
	}
	if loaded.SortMode != original.SortMode {
		t.Errorf("SortMode mismatch: %q vs %q", loaded.SortMode, original.SortMode)
	}
	if loaded.TypeFilter != original.TypeFilter {
		t.Errorf("TypeFilter mismatch: %q vs %q", loaded.TypeFilter, original.TypeFilter)
	}
	if loaded.IncludeClosed != original.IncludeClosed {
		t.Errorf("IncludeClosed mismatch: %v vs %v", loaded.IncludeClosed, original.IncludeClosed)
	}
	if loaded.TitleMinLength != original.TitleMinLength {
		t.Errorf("TitleMinLength mismatch: %d vs %d", loaded.TitleMinLength, original.TitleMinLength)
	}
	if loaded.TitleMaxLength != original.TitleMaxLength {
		t.Errorf("TitleMaxLength mismatch: %d vs %d", loaded.TitleMaxLength, original.TitleMaxLength)
	}
	if len(loaded.FeatureFlags) != len(original.FeatureFlags) {
		t.Errorf("FeatureFlags length mismatch: %d vs %d", len(loaded.FeatureFlags), len(original.FeatureFlags))
	}
	for k, v := range original.FeatureFlags {
		if loaded.FeatureFlags[k] != v {
			t.Errorf("FeatureFlags[%q] mismatch: %v vs %v", k, loaded.FeatureFlags[k], v)
		}
	}
}

func TestConfigCompat_DefaultsApplied(t *testing.T) {
	dir := t.TempDir()

	// GetPaneHeights should return defaults for empty config
	heights, err := GetPaneHeights(dir)
	if err != nil {
		t.Fatalf("GetPaneHeights: %v", err)
	}
	expected := DefaultPaneHeights()
	if heights != expected {
		t.Errorf("GetPaneHeights default: got %v, want %v", heights, expected)
	}

	// GetTitleLengthLimits should return defaults for empty config
	min, max, err := GetTitleLengthLimits(dir)
	if err != nil {
		t.Fatalf("GetTitleLengthLimits: %v", err)
	}
	if min != DefaultTitleMinLength {
		t.Errorf("default min: got %d, want %d", min, DefaultTitleMinLength)
	}
	if max != DefaultTitleMaxLength {
		t.Errorf("default max: got %d, want %d", max, DefaultTitleMaxLength)
	}
}

func TestConfigCompat_SaveCreatesDir(t *testing.T) {
	dir := t.TempDir()

	// .todos doesn't exist yet — Save should create it
	cfg := &models.Config{FocusedIssueID: "td-create-dir"}
	if err := Save(dir, cfg); err != nil {
		t.Fatalf("Save to new dir: %v", err)
	}

	// Verify file was created
	data, err := os.ReadFile(filepath.Join(dir, ".todos", "config.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	var loaded models.Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.FocusedIssueID != "td-create-dir" {
		t.Errorf("got %q, want %q", loaded.FocusedIssueID, "td-create-dir")
	}
}
