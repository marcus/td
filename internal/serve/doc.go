// Package serve implements the local td HTTP server used by the TUI monitor
// and other local clients. It exposes REST endpoints for reading, creating,
// updating, and transitioning issues over HTTP, with token-based
// authentication and long-poll support for real-time updates.
package serve
