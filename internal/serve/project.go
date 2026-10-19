package serve

import (
	"net/http"
	"path/filepath"
)

// handleProject exposes local project metadata and optional API capabilities.
func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	minTitle, maxTitle := titleLengthLimitsFor(s.handlerContext())
	WriteSuccess(w, map[string]interface{}{
		"name": filepath.Base(s.baseDir), "path": s.baseDir,
		"session_id":       s.sessionID,
		"title_min_length": minTitle, "title_max_length": maxTitle,
		"capabilities": []string{"issue_revisions", "board_move", "atomic_label_updates"},
	}, http.StatusOK)
}
