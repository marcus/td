package cmd

import "github.com/marcus/td/internal/db"

// reviewableByFilterForModeParity is a test-only alias that lets the parity
// test in cmd/parity_surface_test.go reach db.ReviewableByFilterForMode
// without importing internal/db transitively into other non-test code paths.
func reviewableByFilterForModeParity(sessionID, mode string) (string, []interface{}) {
	return db.ReviewableByFilterForMode(sessionID, mode)
}
