// Package sync implements the event-sourced synchronization engine for td.
// It manages the server-side event log, client push/pull operations,
// conflict detection and resolution (last-writer-wins), pending event
// tracking, backfill of historical data into the event stream, and
// seed parity verification between local and synced databases.
package sync
