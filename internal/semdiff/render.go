package semdiff

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// RenderText writes a human-readable, file-grouped summary to w.
func RenderText(w io.Writer, s Summary) error {
	if s.Headline == "" {
		s.Headline = "No changes detected."
	}
	if _, err := fmt.Fprintln(w, s.Headline); err != nil {
		return err
	}
	if len(s.Files) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	for _, f := range s.Files {
		marker := "M"
		switch {
		case f.IsNew:
			marker = "A"
		case f.IsDel:
			marker = "D"
		}
		header := fmt.Sprintf("[%s] %s", marker, f.Path)
		if f.Language != "" {
			header += "  (" + f.Language + ")"
		}
		if _, err := fmt.Fprintln(w, header); err != nil {
			return err
		}
		if len(f.Changes) == 0 {
			if _, err := fmt.Fprintln(w, "  - no semantic changes detected"); err != nil {
				return err
			}
			continue
		}
		seen := map[string]bool{}
		for _, c := range f.Changes {
			key := string(c.Category) + "|" + c.Detail
			if seen[key] {
				continue
			}
			seen[key] = true
			line := fmt.Sprintf("  - %s", c.Category)
			if c.Detail != "" {
				line += ": " + c.Detail
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
	}
	return nil
}

// RenderJSON writes a machine-readable summary.
func RenderJSON(w io.Writer, s Summary) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

// Diagnostics produces a one-line headline plus per-category counts. Useful for
// callers that want to embed a summary somewhere small.
func (s Summary) Diagnostics() string {
	counts := map[Category]int{}
	for _, f := range s.Files {
		for _, c := range f.Changes {
			counts[c.Category]++
		}
	}
	if len(counts) == 0 {
		return s.Headline
	}
	parts := []string{s.Headline}
	for cat, n := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", cat, n))
	}
	return strings.Join(parts, "; ")
}
