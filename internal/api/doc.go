// Package api implements the HTTP REST API server for td-sync, providing
// endpoints for project management, real-time event synchronization,
// authentication, and server administration.
//
// The server manages per-project SQLite databases for event logs, supports
// device-code authentication with API key validation, rate limiting, and
// CORS. Admin endpoints provide server monitoring, user/project management,
// event inspection, and snapshot querying.
//
// Core components:
//
//   - [Server] orchestrates HTTP routing, middleware, and lifecycle management.
//   - Sync endpoints handle event push/pull for multi-device data replication.
//   - Project and member endpoints manage access control with owner/writer/reader roles.
//   - Admin endpoints provide privileged access to server-wide state.
package api
