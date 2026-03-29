// Package api implements the td sync server HTTP API. It provides endpoints
// for authentication (device-code OAuth flow), project and membership
// management, event push/pull synchronization, snapshot distribution,
// and an admin interface. The server uses per-project SQLite databases
// for event storage, rate limiting, CORS, and API key authentication.
package api
