// Package serve provides the local HTTP API server for td, implementing
// RESTful endpoints for issue CRUD operations, status transitions, health
// monitoring, and real-time updates via Server-Sent Events (SSE).
//
// The server includes middleware for authentication, CORS, logging, and
// error recovery. It manages web session lifecycle, cross-platform port
// file coordination for process discovery, and structured request/response
// serialization with DTOs and validation helpers.
package serve
