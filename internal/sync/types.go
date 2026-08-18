package sync

import (
	"encoding/json"
	"time"
)

// Event represents a single sync action from a device.
type Event struct {
	ClientActionID  int64
	DeviceID        string
	SessionID       string
	ActionType      string
	EntityType      string
	EntityID        string
	Payload         []byte // JSON
	ClientTimestamp time.Time
	ServerSeq       int64
}

// PushResult is the server response to a push request.
type PushResult struct {
	Accepted int
	Acks     []Ack
	Rejected []Rejection
}

// Ack confirms a client action was accepted with a server sequence number.
type Ack struct {
	ClientActionID int64
	ServerSeq      int64
}

// Rejection explains why a client action was refused.
type Rejection struct {
	ClientActionID int64
	Reason         string
	ServerSeq      int64 // populated for "duplicate" rejections
}

// PullResult is the server response to a pull request.
type PullResult struct {
	Events        []Event
	LastServerSeq int64
	HasMore       bool
}

// ApplyResult summarises the outcome of applying a batch of events.
type ApplyResult struct {
	LastAppliedSeq int64
	Applied        int
	Overwrites     int
	Conflicts      []ConflictRecord
	Failed         []FailedEvent
	// Skipped holds events deliberately not applied because applying them
	// would be incorrect, not because something went wrong — today, a create
	// whose FOREIGN KEY parent has been deleted (see OrphanedParentError).
	// These are recorded in sync_skipped_events and must not block the stream.
	Skipped []SkippedEvent
}

// SkippedEvent records a single event that was deliberately not applied.
type SkippedEvent struct {
	ServerSeq  int64
	DeviceID   string
	ActionType string
	EntityType string
	EntityID   string
	Reason     string // SkipReason* constant
	Detail     string
	Payload    []byte
}

// Reasons an event lands in sync_skipped_events.
const (
	// SkipReasonOrphanedParent — a create whose ON DELETE CASCADE parent no
	// longer exists. Dropping it is the outcome that agrees with every other
	// peer; see OrphanedParentError.
	SkipReasonOrphanedParent = "orphaned_parent"
	// SkipReasonQuarantined — the event failed to apply with an error that
	// cannot succeed on retry. Advancing past it keeps the stream moving; see
	// IsPermanentApplyError.
	SkipReasonQuarantined = "quarantined"
)

// ConflictRecord captures the details of a local row overwritten by a remote event.
type ConflictRecord struct {
	EntityType    string
	EntityID      string
	ServerSeq     int64
	LocalData     json.RawMessage
	RemoteData    json.RawMessage
	OverwrittenAt time.Time
}

// FailedEvent records a single event that could not be applied.
type FailedEvent struct {
	ServerSeq  int64
	DeviceID   string
	ActionType string
	EntityType string
	EntityID   string
	Payload    []byte
	Error      error
}

// EntityValidator returns true if the given entity type is allowed.
type EntityValidator func(entityType string) bool
