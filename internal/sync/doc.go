// Package sync implements a bidirectional synchronization engine for
// multi-device data consistency in td.
//
// Clients push local changes as events (with ClientActionID, DeviceID,
// SessionID) and pull remote changes by server sequence number. The
// engine handles conflict resolution, duplicate detection, and automatic
// backfilling of orphaned entities that lack action_log entries. It
// supports complex data validation including cycle detection for issue
// dependencies and soft-delete awareness across multiple entity types
// (issues, logs, comments, handoffs, boards, work sessions, etc.).
package sync
