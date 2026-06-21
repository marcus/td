package sync

import "encoding/json"

var syncLocalOnlyFields = map[string][]string{
	"work_sessions": {"worktree_id", "worktree_root", "repo_root"},
}

func scrubLocalOnlySyncFields(entityType string, fields map[string]any) {
	for _, field := range syncLocalOnlyFields[entityType] {
		delete(fields, field)
	}
}

func scrubLocalOnlySyncPayload(entityType string, raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	if len(syncLocalOnlyFields[entityType]) == 0 {
		return raw
	}

	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw
	}
	scrubLocalOnlySyncFields(entityType, fields)
	for _, key := range []string{"new_data", "previous_data"} {
		nested, ok := fields[key].(map[string]any)
		if !ok {
			continue
		}
		scrubLocalOnlySyncFields(entityType, nested)
	}

	scrubbed, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
	return scrubbed
}
