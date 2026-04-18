package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestLoad(t *testing.T) {
	t.Run("existing file", func(t *testing.T) {
		dir := t.TempDir()
		configDir := filepath.Join(dir, ".todos")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			t.Fatalf("setup: mkdir failed: %v", err)
		}

		expected := &models.Config{
			FocusedIssueID:    "issue-123",
			ActiveWorkSession: "ws-456",
			PaneHeights:       [3]float64{0.3, 0.4, 0.3},
			SearchQuery:       "test query",
			SortMode:          "priority",
			TypeFilter:        "bug",
			IncludeClosed:     true,
			TitleMinLength:    20,
			TitleMaxLength:    80,
		}

		data, err := json.MarshalIndent(expected, "", "  ")
		if err != nil {
			t.Fatalf("setup: marshal failed: %v", err)
		}

		if err := os.WriteFile(filepath.Join(configDir, "config.json"), data, 0644); err != nil {
			t.Fatalf("setup: write failed: %v", err)
		}

		cfg, err := Load(dir)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if cfg.FocusedIssueID != expected.FocusedIssueID {
			t.Errorf("FocusedIssueID: got %q, want %q", cfg.FocusedIssueID, expected.FocusedIssueID)
		}
		if cfg.ActiveWorkSession != expected.ActiveWorkSession {
			t.Errorf("ActiveWorkSession: got %q, want %q", cfg.ActiveWorkSession, expected.ActiveWorkSession)
		}
		if cfg.PaneHeights != expected.PaneHeights {
			t.Errorf("PaneHeights: got %v, want %v", cfg.PaneHeights, expected.PaneHeights)
		}
		if cfg.SearchQuery != expected.SearchQuery {
			t.Errorf("SearchQuery: got %q, want %q", cfg.SearchQuery, expected.SearchQuery)
		}
		if cfg.SortMode != expected.SortMode {
			t.Errorf("SortMode: got %q, want %q", cfg.SortMode, expected.SortMode)
		}
		if cfg.TypeFilter != expected.TypeFilter {
			t.Errorf("TypeFilter: got %q, want %q", cfg.TypeFilter, expected.TypeFilter)
		}
		if cfg.IncludeClosed != expected.IncludeClosed {
			t.Errorf("IncludeClosed: got %v, want %v", cfg.IncludeClosed, expected.IncludeClosed)
		}
		if cfg.TitleMinLength != expected.TitleMinLength {
			t.Errorf("TitleMinLength: got %d, want %d", cfg.TitleMinLength, expected.TitleMinLength)
		}
		if cfg.TitleMaxLength != expected.TitleMaxLength {
			t.Errorf("TitleMaxLength: got %d, want %d", cfg.TitleMaxLength, expected.TitleMaxLength)
		}
	})

	t.Run("non-existent file returns empty config", func(t *testing.T) {
		dir := t.TempDir()

		cfg, err := Load(dir)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if cfg == nil {
			t.Fatal("Load returned nil config")
		}
		if cfg.FocusedIssueID != "" {
			t.Errorf("FocusedIssueID: got %q, want empty", cfg.FocusedIssueID)
		}
		if cfg.ActiveWorkSession != "" {
			t.Errorf("ActiveWorkSession: got %q, want empty", cfg.ActiveWorkSession)
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		dir := t.TempDir()
		configDir := filepath.Join(dir, ".todos")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			t.Fatalf("setup: mkdir failed: %v", err)
		}

		if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("not valid json{"), 0644); err != nil {
			t.Fatalf("setup: write failed: %v", err)
		}

		_, err := Load(dir)
		if err == nil {
			t.Fatal("Load should fail for invalid JSON")
		}
	})

	t.Run("empty JSON file", func(t *testing.T) {
		dir := t.TempDir()
		configDir := filepath.Join(dir, ".todos")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			t.Fatalf("setup: mkdir failed: %v", err)
		}

		if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("{}"), 0644); err != nil {
			t.Fatalf("setup: write failed: %v", err)
		}

		cfg, err := Load(dir)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if cfg == nil {
			t.Fatal("Load returned nil config")
		}
	})
}

func TestSave(t *testing.T) {
	t.Run("creates directories and writes valid JSON", func(t *testing.T) {
		dir := t.TempDir()

		cfg := &models.Config{
			FocusedIssueID:    "test-issue",
			ActiveWorkSession: "test-session",
			PaneHeights:       [3]float64{0.33, 0.34, 0.33},
		}

		if err := Save(dir, cfg); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		// Verify file exists
		configPath := filepath.Join(dir, ".todos", "config.json")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Fatal("config file not created")
		}

		// Verify content is valid JSON
		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read config failed: %v", err)
		}

		var loaded models.Config
		if err := json.Unmarshal(data, &loaded); err != nil {
			t.Fatalf("config is not valid JSON: %v", err)
		}

		if loaded.FocusedIssueID != cfg.FocusedIssueID {
			t.Errorf("FocusedIssueID: got %q, want %q", loaded.FocusedIssueID, cfg.FocusedIssueID)
		}
	})

	t.Run("overwrites existing file", func(t *testing.T) {
		dir := t.TempDir()

		cfg1 := &models.Config{FocusedIssueID: "first"}
		if err := Save(dir, cfg1); err != nil {
			t.Fatalf("first Save failed: %v", err)
		}

		cfg2 := &models.Config{FocusedIssueID: "second"}
		if err := Save(dir, cfg2); err != nil {
			t.Fatalf("second Save failed: %v", err)
		}

		loaded, err := Load(dir)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if loaded.FocusedIssueID != "second" {
			t.Errorf("FocusedIssueID: got %q, want %q", loaded.FocusedIssueID, "second")
		}
	})

	t.Run("empty config", func(t *testing.T) {
		dir := t.TempDir()

		cfg := &models.Config{}
		if err := Save(dir, cfg); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		loaded, err := Load(dir)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if loaded == nil {
			t.Fatal("Load returned nil")
		}
	})
}

func TestFocus(t *testing.T) {
	t.Run("SetFocus/GetFocus round trip", func(t *testing.T) {
		dir := t.TempDir()

		if err := SetFocus(dir, "issue-abc"); err != nil {
			t.Fatalf("SetFocus failed: %v", err)
		}

		got, err := GetFocus(dir)
		if err != nil {
			t.Fatalf("GetFocus failed: %v", err)
		}
		if got != "issue-abc" {
			t.Errorf("GetFocus: got %q, want %q", got, "issue-abc")
		}
	})

	t.Run("ClearFocus", func(t *testing.T) {
		dir := t.TempDir()

		if err := SetFocus(dir, "issue-123"); err != nil {
			t.Fatalf("SetFocus failed: %v", err)
		}

		if err := ClearFocus(dir); err != nil {
			t.Fatalf("ClearFocus failed: %v", err)
		}

		got, err := GetFocus(dir)
		if err != nil {
			t.Fatalf("GetFocus failed: %v", err)
		}
		if got != "" {
			t.Errorf("GetFocus after clear: got %q, want empty", got)
		}
	})

	t.Run("GetFocus on empty config returns empty", func(t *testing.T) {
		dir := t.TempDir()

		got, err := GetFocus(dir)
		if err != nil {
			t.Fatalf("GetFocus failed: %v", err)
		}
		if got != "" {
			t.Errorf("GetFocus: got %q, want empty", got)
		}
	})

	t.Run("SetFocus preserves other config fields", func(t *testing.T) {
		dir := t.TempDir()

		// Set up initial config with multiple fields
		cfg := &models.Config{
			ActiveWorkSession: "ws-123",
			SearchQuery:       "existing query",
		}
		if err := Save(dir, cfg); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		// Set focus
		if err := SetFocus(dir, "new-focus"); err != nil {
			t.Fatalf("SetFocus failed: %v", err)
		}

		// Verify other fields preserved
		loaded, err := Load(dir)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if loaded.ActiveWorkSession != "ws-123" {
			t.Errorf("ActiveWorkSession lost: got %q", loaded.ActiveWorkSession)
		}
		if loaded.SearchQuery != "existing query" {
			t.Errorf("SearchQuery lost: got %q", loaded.SearchQuery)
		}
	})
}

func TestActiveWorkSession(t *testing.T) {
	t.Run("SetActiveWorkSession/GetActiveWorkSession round trip", func(t *testing.T) {
		dir := t.TempDir()

		if err := SetActiveWorkSession(dir, "ws-xyz"); err != nil {
			t.Fatalf("SetActiveWorkSession failed: %v", err)
		}

		got, err := GetActiveWorkSession(dir)
		if err != nil {
			t.Fatalf("GetActiveWorkSession failed: %v", err)
		}
		if got != "ws-xyz" {
			t.Errorf("GetActiveWorkSession: got %q, want %q", got, "ws-xyz")
		}
	})

	t.Run("ClearActiveWorkSession", func(t *testing.T) {
		dir := t.TempDir()

		if err := SetActiveWorkSession(dir, "ws-123"); err != nil {
			t.Fatalf("SetActiveWorkSession failed: %v", err)
		}

		if err := ClearActiveWorkSession(dir); err != nil {
			t.Fatalf("ClearActiveWorkSession failed: %v", err)
		}

		got, err := GetActiveWorkSession(dir)
		if err != nil {
			t.Fatalf("GetActiveWorkSession failed: %v", err)
		}
		if got != "" {
			t.Errorf("GetActiveWorkSession after clear: got %q, want empty", got)
		}
	})

	t.Run("GetActiveWorkSession on empty config returns empty", func(t *testing.T) {
		dir := t.TempDir()

		got, err := GetActiveWorkSession(dir)
		if err != nil {
			t.Fatalf("GetActiveWorkSession failed: %v", err)
		}
		if got != "" {
			t.Errorf("GetActiveWorkSession: got %q, want empty", got)
		}
	})

	t.Run("SetActiveWorkSession preserves other config fields", func(t *testing.T) {
		dir := t.TempDir()

		cfg := &models.Config{
			FocusedIssueID: "focus-123",
			SortMode:       "updated",
		}
		if err := Save(dir, cfg); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		if err := SetActiveWorkSession(dir, "new-ws"); err != nil {
			t.Fatalf("SetActiveWorkSession failed: %v", err)
		}

		loaded, err := Load(dir)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if loaded.FocusedIssueID != "focus-123" {
			t.Errorf("FocusedIssueID lost: got %q", loaded.FocusedIssueID)
		}
		if loaded.SortMode != "updated" {
			t.Errorf("SortMode lost: got %q", loaded.SortMode)
		}
	})
}

func TestFeatureFlags(t *testing.T) {
	t.Run("SetFeatureFlag/GetFeatureFlag round trip", func(t *testing.T) {
		dir := t.TempDir()

		if err := SetFeatureFlag(dir, "sync_cli", true); err != nil {
			t.Fatalf("SetFeatureFlag failed: %v", err)
		}

		value, ok, err := GetFeatureFlag(dir, "sync_cli")
		if err != nil {
			t.Fatalf("GetFeatureFlag failed: %v", err)
		}
		if !ok {
			t.Fatal("feature flag should exist")
		}
		if !value {
			t.Fatal("sync_cli should be true")
		}
	})

	t.Run("UnsetFeatureFlag removes value", func(t *testing.T) {
		dir := t.TempDir()

		if err := SetFeatureFlag(dir, "sync_cli", true); err != nil {
			t.Fatalf("SetFeatureFlag failed: %v", err)
		}
		if err := UnsetFeatureFlag(dir, "sync_cli"); err != nil {
			t.Fatalf("UnsetFeatureFlag failed: %v", err)
		}

		_, ok, err := GetFeatureFlag(dir, "sync_cli")
		if err != nil {
			t.Fatalf("GetFeatureFlag failed: %v", err)
		}
		if ok {
			t.Fatal("sync_cli should be unset")
		}
	})

	t.Run("feature flag updates preserve existing config fields", func(t *testing.T) {
		dir := t.TempDir()

		cfg := &models.Config{
			FocusedIssueID: "td-123",
			SearchQuery:    "existing",
		}
		if err := Save(dir, cfg); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		if err := SetFeatureFlag(dir, "sync_cli", true); err != nil {
			t.Fatalf("SetFeatureFlag failed: %v", err)
		}

		loaded, err := Load(dir)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if loaded.FocusedIssueID != "td-123" {
			t.Fatalf("FocusedIssueID changed: %q", loaded.FocusedIssueID)
		}
		if loaded.SearchQuery != "existing" {
			t.Fatalf("SearchQuery changed: %q", loaded.SearchQuery)
		}
		if loaded.FeatureFlags == nil || !loaded.FeatureFlags["sync_cli"] {
			t.Fatal("sync_cli should be persisted in feature_flags")
		}
	})
}

func TestPaneHeights(t *testing.T) {
	t.Run("DefaultPaneHeights returns equal thirds", func(t *testing.T) {
		heights := DefaultPaneHeights()
		expected := 1.0 / 3.0

		for i, h := range heights {
			if h != expected {
				t.Errorf("height[%d]: got %f, want %f", i, h, expected)
			}
		}
	})

	t.Run("GetPaneHeights returns defaults when not configured", func(t *testing.T) {
		dir := t.TempDir()

		heights, err := GetPaneHeights(dir)
		if err != nil {
			t.Fatalf("GetPaneHeights failed: %v", err)
		}

		defaults := DefaultPaneHeights()
		if heights != defaults {
			t.Errorf("GetPaneHeights: got %v, want %v", heights, defaults)
		}
	})

	t.Run("SetPaneHeights/GetPaneHeights round trip", func(t *testing.T) {
		dir := t.TempDir()

		expected := [3]float64{0.2, 0.5, 0.3}
		if err := SetPaneHeights(dir, expected); err != nil {
			t.Fatalf("SetPaneHeights failed: %v", err)
		}

		got, err := GetPaneHeights(dir)
		if err != nil {
			t.Fatalf("GetPaneHeights failed: %v", err)
		}
		if got != expected {
			t.Errorf("GetPaneHeights: got %v, want %v", got, expected)
		}
	})

	t.Run("GetPaneHeights returns defaults for invalid sum", func(t *testing.T) {
		dir := t.TempDir()

		// Sum > 1.01
		cfg := &models.Config{PaneHeights: [3]float64{0.5, 0.5, 0.5}}
		if err := Save(dir, cfg); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		heights, err := GetPaneHeights(dir)
		if err != nil {
			t.Fatalf("GetPaneHeights failed: %v", err)
		}

		defaults := DefaultPaneHeights()
		if heights != defaults {
			t.Errorf("should return defaults for invalid sum: got %v", heights)
		}
	})

	t.Run("GetPaneHeights returns defaults for pane < 10%", func(t *testing.T) {
		dir := t.TempDir()

		// One pane is less than 10%
		cfg := &models.Config{PaneHeights: [3]float64{0.05, 0.5, 0.45}}
		if err := Save(dir, cfg); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		heights, err := GetPaneHeights(dir)
		if err != nil {
			t.Fatalf("GetPaneHeights failed: %v", err)
		}

		defaults := DefaultPaneHeights()
		if heights != defaults {
			t.Errorf("should return defaults for pane < 10%%: got %v", heights)
		}
	})

	t.Run("GetPaneHeights returns defaults for all zeros", func(t *testing.T) {
		dir := t.TempDir()

		cfg := &models.Config{PaneHeights: [3]float64{0, 0, 0}}
		if err := Save(dir, cfg); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		heights, err := GetPaneHeights(dir)
		if err != nil {
			t.Fatalf("GetPaneHeights failed: %v", err)
		}

		defaults := DefaultPaneHeights()
		if heights != defaults {
			t.Errorf("should return defaults for zeros: got %v", heights)
		}
	})
}

func TestFilterState(t *testing.T) {
	t.Run("GetFilterState on empty config", func(t *testing.T) {
		dir := t.TempDir()

		state, err := GetFilterState(dir)
		if err != nil {
			t.Fatalf("GetFilterState failed: %v", err)
		}
		if state == nil {
			t.Fatal("GetFilterState returned nil")
		}
		if state.SearchQuery != "" || state.SortMode != "" || state.TypeFilter != "" || state.IncludeClosed {
			t.Errorf("expected empty filter state, got %+v", state)
		}
	})

	t.Run("SetFilterState/GetFilterState round trip", func(t *testing.T) {
		dir := t.TempDir()

		expected := &FilterState{
			SearchQuery:   "search term",
			SortMode:      "created",
			TypeFilter:    "epic",
			IncludeClosed: true,
		}

		if err := SetFilterState(dir, expected); err != nil {
			t.Fatalf("SetFilterState failed: %v", err)
		}

		got, err := GetFilterState(dir)
		if err != nil {
			t.Fatalf("GetFilterState failed: %v", err)
		}

		if got.SearchQuery != expected.SearchQuery {
			t.Errorf("SearchQuery: got %q, want %q", got.SearchQuery, expected.SearchQuery)
		}
		if got.SortMode != expected.SortMode {
			t.Errorf("SortMode: got %q, want %q", got.SortMode, expected.SortMode)
		}
		if got.TypeFilter != expected.TypeFilter {
			t.Errorf("TypeFilter: got %q, want %q", got.TypeFilter, expected.TypeFilter)
		}
		if got.IncludeClosed != expected.IncludeClosed {
			t.Errorf("IncludeClosed: got %v, want %v", got.IncludeClosed, expected.IncludeClosed)
		}
	})

	t.Run("SetFilterState preserves other config fields", func(t *testing.T) {
		dir := t.TempDir()

		cfg := &models.Config{
			FocusedIssueID:    "focus-abc",
			ActiveWorkSession: "ws-abc",
		}
		if err := Save(dir, cfg); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		state := &FilterState{SearchQuery: "new search"}
		if err := SetFilterState(dir, state); err != nil {
			t.Fatalf("SetFilterState failed: %v", err)
		}

		loaded, err := Load(dir)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if loaded.FocusedIssueID != "focus-abc" {
			t.Errorf("FocusedIssueID lost: got %q", loaded.FocusedIssueID)
		}
		if loaded.ActiveWorkSession != "ws-abc" {
			t.Errorf("ActiveWorkSession lost: got %q", loaded.ActiveWorkSession)
		}
	})
}

func TestTitleLengthLimits(t *testing.T) {
	t.Run("returns defaults for empty config", func(t *testing.T) {
		dir := t.TempDir()

		min, max, err := GetTitleLengthLimits(dir)
		if err != nil {
			t.Fatalf("GetTitleLengthLimits failed: %v", err)
		}
		if min != DefaultTitleMinLength {
			t.Errorf("min: got %d, want %d", min, DefaultTitleMinLength)
		}
		if max != DefaultTitleMaxLength {
			t.Errorf("max: got %d, want %d", max, DefaultTitleMaxLength)
		}
	})

	t.Run("returns configured values", func(t *testing.T) {
		dir := t.TempDir()

		cfg := &models.Config{
			TitleMinLength: 25,
			TitleMaxLength: 75,
		}
		if err := Save(dir, cfg); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		min, max, err := GetTitleLengthLimits(dir)
		if err != nil {
			t.Fatalf("GetTitleLengthLimits failed: %v", err)
		}
		if min != 25 {
			t.Errorf("min: got %d, want 25", min)
		}
		if max != 75 {
			t.Errorf("max: got %d, want 75", max)
		}
	})

	t.Run("returns defaults for zero values", func(t *testing.T) {
		dir := t.TempDir()

		cfg := &models.Config{
			TitleMinLength: 0,
			TitleMaxLength: 0,
		}
		if err := Save(dir, cfg); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		min, max, err := GetTitleLengthLimits(dir)
		if err != nil {
			t.Fatalf("GetTitleLengthLimits failed: %v", err)
		}
		if min != DefaultTitleMinLength {
			t.Errorf("min: got %d, want %d", min, DefaultTitleMinLength)
		}
		if max != DefaultTitleMaxLength {
			t.Errorf("max: got %d, want %d", max, DefaultTitleMaxLength)
		}
	})

	t.Run("returns defaults for negative values", func(t *testing.T) {
		dir := t.TempDir()

		cfg := &models.Config{
			TitleMinLength: -10,
			TitleMaxLength: -5,
		}
		if err := Save(dir, cfg); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		min, max, err := GetTitleLengthLimits(dir)
		if err != nil {
			t.Fatalf("GetTitleLengthLimits failed: %v", err)
		}
		if min != DefaultTitleMinLength {
			t.Errorf("min: got %d, want %d", min, DefaultTitleMinLength)
		}
		if max != DefaultTitleMaxLength {
			t.Errorf("max: got %d, want %d", max, DefaultTitleMaxLength)
		}
	})

	t.Run("partial config - only min set", func(t *testing.T) {
		dir := t.TempDir()

		cfg := &models.Config{
			TitleMinLength: 30,
		}
		if err := Save(dir, cfg); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		min, max, err := GetTitleLengthLimits(dir)
		if err != nil {
			t.Fatalf("GetTitleLengthLimits failed: %v", err)
		}
		if min != 30 {
			t.Errorf("min: got %d, want 30", min)
		}
		if max != DefaultTitleMaxLength {
			t.Errorf("max: got %d, want %d", max, DefaultTitleMaxLength)
		}
	})
}

func TestConstants(t *testing.T) {
	t.Run("default title length constants", func(t *testing.T) {
		if DefaultTitleMinLength != 15 {
			t.Errorf("DefaultTitleMinLength: got %d, want 15", DefaultTitleMinLength)
		}
		if DefaultTitleMaxLength != 200 {
			t.Errorf("DefaultTitleMaxLength: got %d, want 200", DefaultTitleMaxLength)
		}
	})
}

func TestEdgeCases(t *testing.T) {
	t.Run("empty base dir", func(t *testing.T) {
		// Empty string should work but path will be invalid
		_, err := Load("")
		// The behavior depends on OS - might return error or empty config
		// Just ensure it doesn't panic
		_ = err
	})

	t.Run("concurrent operations", func(t *testing.T) {
		dir := t.TempDir()

		// Write initial config
		if err := Save(dir, &models.Config{}); err != nil {
			t.Fatalf("initial Save failed: %v", err)
		}

		// Simulate concurrent access (not truly concurrent, but tests read-modify-write)
		done := make(chan bool, 10)

		for i := 0; i < 10; i++ {
			go func(n int) {
				defer func() { done <- true }()

				if n%2 == 0 {
					_ = SetFocus(dir, "focus-"+string(rune('0'+n)))
				} else {
					_ = SetActiveWorkSession(dir, "ws-"+string(rune('0'+n)))
				}
			}(i)
		}

		for i := 0; i < 10; i++ {
			<-done
		}

		// Just verify we can still load - no corruption check needed
		_, err := Load(dir)
		if err != nil {
			t.Errorf("Load after concurrent writes: %v", err)
		}
	})

	t.Run("special characters in values", func(t *testing.T) {
		dir := t.TempDir()

		special := "test-\"quoted\"-'single'-\n-newline-\t-tab"
		if err := SetFocus(dir, special); err != nil {
			t.Fatalf("SetFocus with special chars failed: %v", err)
		}

		got, err := GetFocus(dir)
		if err != nil {
			t.Fatalf("GetFocus failed: %v", err)
		}
		if got != special {
			t.Errorf("special chars not preserved: got %q, want %q", got, special)
		}
	})

	t.Run("unicode in values", func(t *testing.T) {
		dir := t.TempDir()

		unicode := "测试-🎉-émoji-日本語"
		if err := SetFocus(dir, unicode); err != nil {
			t.Fatalf("SetFocus with unicode failed: %v", err)
		}

		got, err := GetFocus(dir)
		if err != nil {
			t.Fatalf("GetFocus failed: %v", err)
		}
		if got != unicode {
			t.Errorf("unicode not preserved: got %q, want %q", got, unicode)
		}
	})

	t.Run("very long values", func(t *testing.T) {
		dir := t.TempDir()

		// 10KB string
		longStr := make([]byte, 10*1024)
		for i := range longStr {
			longStr[i] = 'a'
		}
		long := string(longStr)

		if err := SetFocus(dir, long); err != nil {
			t.Fatalf("SetFocus with long value failed: %v", err)
		}

		got, err := GetFocus(dir)
		if err != nil {
			t.Fatalf("GetFocus failed: %v", err)
		}
		if got != long {
			t.Errorf("long value not preserved: len got %d, want %d", len(got), len(long))
		}
	})
}

func TestPermissionErrors(t *testing.T) {
	// Skip on CI or if running as root
	if os.Getuid() == 0 {
		t.Skip("skipping permission tests when running as root")
	}

	t.Run("unreadable config file", func(t *testing.T) {
		dir := t.TempDir()
		configDir := filepath.Join(dir, ".todos")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}

		configPath := filepath.Join(configDir, "config.json")
		if err := os.WriteFile(configPath, []byte("{}"), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		// Remove read permission
		if err := os.Chmod(configPath, 0000); err != nil {
			t.Fatalf("chmod failed: %v", err)
		}
		defer func() { _ = os.Chmod(configPath, 0644) }() // Restore for cleanup

		_, err := Load(dir)
		if err == nil {
			t.Error("Load should fail for unreadable file")
		}
	})

	t.Run("unwritable directory", func(t *testing.T) {
		dir := t.TempDir()
		todosDir := filepath.Join(dir, ".todos")
		if err := os.MkdirAll(todosDir, 0755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}

		// Remove write permission from directory
		if err := os.Chmod(todosDir, 0555); err != nil {
			t.Fatalf("chmod failed: %v", err)
		}
		defer func() { _ = os.Chmod(todosDir, 0755) }() // Restore for cleanup

		err := Save(dir, &models.Config{FocusedIssueID: "test"})
		if err == nil {
			t.Error("Save should fail for unwritable directory")
		}
	})
}
