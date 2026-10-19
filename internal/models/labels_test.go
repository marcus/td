package models

import (
	"reflect"
	"testing"
)

func TestLabelChange(t *testing.T) {
	tests := []struct {
		name    string
		change  LabelChange
		want    []string
		invalid bool
	}{
		{"idempotent deltas", LabelChange{Add: []string{" new ", "new", "Keep"}, Remove: []string{"gone", "gone", "absent"}}, []string{"Keep", "other", "new"}, false},
		{"case sensitive", LabelChange{Add: []string{"keep"}, Remove: []string{"keep"}}, nil, true},
		{"normalized overlap", LabelChange{Add: []string{" x "}, Remove: []string{"x"}}, nil, true},
		{"empty replacement clears", LabelChange{Replace: []string{}}, []string{}, false},
		{"replacement plus delta", LabelChange{Replace: []string{}, Add: []string{"x"}}, nil, true},
		{"blank", LabelChange{Add: []string{" "}}, nil, true},
		{"comma", LabelChange{Remove: []string{"a,b"}}, nil, true},
		{"newline", LabelChange{Add: []string{"a\nb"}}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.change.Apply([]string{"Keep", "gone", "other"})
			if (err != nil) != tt.invalid {
				t.Fatalf("err=%v", err)
			}
			if !tt.invalid && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("labels=%v want=%v", got, tt.want)
			}
		})
	}
}
