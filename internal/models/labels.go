package models

import (
	"fmt"
	"strings"
)

// LabelChange describes either a replacement or an incremental label edit.
// Matching is case-sensitive, as it is for existing label storage and filters.
type LabelChange struct {
	Replace []string
	Add     []string
	Remove  []string
}

func normalizeLabelDelta(labels []string) ([]string, error) {
	var result []string
	seen := map[string]bool{}
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" || strings.ContainsAny(label, ",\r\n") {
			return nil, fmt.Errorf("label must be non-empty and contain no commas or newlines")
		}
		if !seen[label] {
			result = append(result, label)
			seen[label] = true
		}
	}
	return result, nil
}

// Apply validates the whole change before applying it. Existing order and case
// are preserved; new labels are appended in request order.
func (change LabelChange) Apply(current []string) ([]string, error) {
	if change.Replace != nil && (change.Add != nil || change.Remove != nil) {
		return nil, fmt.Errorf("cannot combine label replacement with label additions or removals")
	}
	add, err := normalizeLabelDelta(change.Add)
	if err != nil {
		return nil, err
	}
	remove, err := normalizeLabelDelta(change.Remove)
	if err != nil {
		return nil, err
	}
	removed := map[string]bool{}
	for _, label := range remove {
		removed[label] = true
	}
	for _, label := range add {
		if removed[label] {
			return nil, fmt.Errorf("label %q cannot be both added and removed", label)
		}
	}
	if change.Replace != nil {
		return append([]string{}, change.Replace...), nil
	}
	var result []string
	seen := map[string]bool{}
	for _, label := range current {
		if !removed[label] {
			result = append(result, label)
			seen[label] = true
		}
	}
	for _, label := range add {
		if !seen[label] {
			result = append(result, label)
			seen[label] = true
		}
	}
	return result, nil
}
