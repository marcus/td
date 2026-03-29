// Package serverdb provides a SQLite-based database layer for managing
// server-side state in the td-sync project system.
//
// It implements CRUD operations and business logic for users, projects,
// API keys, memberships with role-based access control (owner, writer,
// reader), authentication events, rate-limit events, and sync cursors
// that track client synchronization positions. API keys are generated
// with SHA256 hashing and base62 encoding, and all operations maintain
// audit logs for authentication and rate-limit events.
package serverdb
