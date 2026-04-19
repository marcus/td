// Package api: Perch-shape REST routes that wrap td-serve's pure handler
// functions against per-project project.db handles.
//
// This file is the Stream 2.3 deliverable for the td-watch rich UI plan
// (docs/td-watch-rich-ui-plan.md §6). It registers the
// `/v1/projects/{id}/{issues,boards,focus,stats,sessions}` surface alongside
// the existing /v1/projects/{id}/sync/* CLI sync routes, composing:
//
//	requireAuth -> requireProjectMembership -> resolveTdWatchSession
//	  -> wrapServeHandler(serve.HandleXxx)
//
// wrapServeHandler acquires the per-project project.db from
// ProjectLivePool, builds a serve.HandlerContext, re-keys URL path values so
// the serve handlers can read their familiar names (`id`, `comment_id`, etc.)
// instead of the project-scoped names (`iid`, `cid`, ...), and invokes the
// pure handler. After successful mutations, a TODO marker indicates where
// Stream 3 will add post-commit action_log -> events.db promotion.
package api

import (
	"net/http"

	"github.com/marcus/td/internal/serve"
	"github.com/marcus/td/internal/serverdb"
)

// wrapServeHandler returns an http.HandlerFunc that adapts a pure
// serve.HandleXxx function so it can be served against a per-project
// project.db acquired from s.projectLivePool.
//
// The optional pathRemap argument lets a route translate its own URL path
// variable names (e.g. {iid} for the issue inside /v1/projects/{id}/issues/{iid})
// into the names the serve handler expects (e.g. "id"). This avoids having to
// change every serve handler to know about the project-scoped names.
func (s *Server) wrapServeHandler(
	h func(serve.HandlerContext, http.ResponseWriter, *http.Request),
	pathRemap ...[2]string,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("id")
		if projectID == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "missing project id")
			return
		}

		liveDB, err := s.projectLivePool.Acquire(projectID)
		if err != nil {
			logFor(r.Context()).Error("project_live_pool acquire", "err", err, "pid", projectID)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to open project database")
			return
		}
		defer s.projectLivePool.Release(projectID)

		// Translate project-scoped path var names to the names the serve
		// handlers expect. Done before invoking the handler so PathValue
		// lookups inside the handler resolve to the right value.
		for _, pair := range pathRemap {
			if v := r.PathValue(pair[0]); v != "" {
				r.SetPathValue(pair[1], v)
			}
		}

		ctx := serve.HandlerContext{
			DB:        liveDB,
			SessionID: TdWatchSessionFromCtx(r.Context()),
			BaseDir:   "", // td-sync mode: no on-disk td root
			Config:    serve.HandlerConfig{
				// NotifyChange intentionally nil: td-sync uses post-commit
				// action_log -> events.db promotion (Stream 3) instead of the
				// SSE/autosync notification path used by local `td serve`.
			},
		}

		h(ctx, w, r)

		// Stream 3.1: promote any new action_log rows produced by the
		// handler into the project's events.db. Owned by
		// action_log_promotion.go; runs ATTACH + insert + synced_at flip in
		// a single transaction so partial work is impossible.
		//
		// Skipped for read-only methods (GET/HEAD/OPTIONS) — they can't
		// produce action_log rows. We deliberately do NOT inspect the
		// handler's status code: a 4xx that still wrote to action_log
		// (rare, but possible if a handler logs first and then returns an
		// error) should still be promoted; conversely, a 2xx that wrote
		// nothing is a cheap no-op.
		//
		// Errors here are logged but never bubble up to the response: the
		// handler's reply has already been written and rolling back would
		// be impossible anyway. The action_log row stays synced_at IS NULL
		// so the next request into the same project retries promotion —
		// that's the recovery valve called out in plan §7.1.
		if shouldPromote(r.Method) {
			if n, err := s.promoteActionLog(projectID, liveDB); err != nil {
				logFor(r.Context()).Error("action_log promotion",
					"err", err, "pid", projectID, "method", r.Method, "path", r.URL.Path)
			} else if n > 0 {
				logFor(r.Context()).Debug("action_log promoted",
					"count", n, "pid", projectID, "method", r.Method)
			}
		}
	}
}

// projectMutateChain composes the mutation middleware stack for a Perch-shape
// project route: requireAuth -> requireProjectMembership(writer) ->
// resolveTdWatchSession -> handler. Returns an http.HandlerFunc the ServeMux
// can register directly.
func (s *Server) projectMutateChain(h http.HandlerFunc) http.HandlerFunc {
	mw := s.requireProjectMembership(serverdb.RoleWriter)
	wrapped := mw(s.resolveTdWatchSession(h))
	return wrapped.ServeHTTP
}

// projectReadChain composes the read middleware stack: requireAuth ->
// requireProjectMembership(reader) -> resolveTdWatchSession -> handler.
// Reads still get a session id stamped on the context for symmetry, even
// though pure-read handlers don't write to action_log.
func (s *Server) projectReadChain(h http.HandlerFunc) http.HandlerFunc {
	mw := s.requireProjectMembership(serverdb.RoleReader)
	wrapped := mw(s.resolveTdWatchSession(h))
	return wrapped.ServeHTTP
}

// registerProjectRoutes wires all Perch-shape /v1/projects/{id}/* routes onto
// the supplied mux. The full inventory below mirrors td-serve's route table
// and matches the plan §6.2 list.
func (s *Server) registerProjectRoutes(mux *http.ServeMux) {
	// Path-var remappings:
	//   project routes use {iid} for the issue id inside /issues/{iid}
	//   serve handlers expect {id}.
	issueRemap := [2]string{"iid", "id"}
	commentRemap := [2]string{"cid", "comment_id"}
	depRemap := [2]string{"did", "dep_id"}

	// Issues — list/create
	mux.HandleFunc("GET /v1/projects/{id}/issues",
		s.projectReadChain(s.wrapServeHandler(serve.HandleListIssues)))
	mux.HandleFunc("POST /v1/projects/{id}/issues",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleCreateIssue)))

	// Issues — read/update/delete by id
	mux.HandleFunc("GET /v1/projects/{id}/issues/{iid}",
		s.projectReadChain(s.wrapServeHandler(serve.HandleGetIssue, issueRemap)))
	mux.HandleFunc("PATCH /v1/projects/{id}/issues/{iid}",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleUpdateIssue, issueRemap)))
	mux.HandleFunc("DELETE /v1/projects/{id}/issues/{iid}",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleDeleteIssue, issueRemap)))

	// Issue transitions
	mux.HandleFunc("POST /v1/projects/{id}/issues/{iid}/start",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleStart, issueRemap)))
	mux.HandleFunc("POST /v1/projects/{id}/issues/{iid}/review",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleReview, issueRemap)))
	mux.HandleFunc("POST /v1/projects/{id}/issues/{iid}/approve",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleApprove, issueRemap)))
	mux.HandleFunc("POST /v1/projects/{id}/issues/{iid}/reject",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleReject, issueRemap)))
	mux.HandleFunc("POST /v1/projects/{id}/issues/{iid}/block",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleBlock, issueRemap)))
	mux.HandleFunc("POST /v1/projects/{id}/issues/{iid}/unblock",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleUnblock, issueRemap)))
	mux.HandleFunc("POST /v1/projects/{id}/issues/{iid}/close",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleClose, issueRemap)))
	mux.HandleFunc("POST /v1/projects/{id}/issues/{iid}/reopen",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleReopen, issueRemap)))

	// Comments
	mux.HandleFunc("POST /v1/projects/{id}/issues/{iid}/comments",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleAddComment, issueRemap)))
	mux.HandleFunc("DELETE /v1/projects/{id}/issues/{iid}/comments/{cid}",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleDeleteComment, issueRemap, commentRemap)))

	// Dependencies
	mux.HandleFunc("POST /v1/projects/{id}/issues/{iid}/dependencies",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleAddDependency, issueRemap)))
	mux.HandleFunc("DELETE /v1/projects/{id}/issues/{iid}/dependencies/{did}",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleDeleteDependency, issueRemap, depRemap)))

	// Boards — list/create
	mux.HandleFunc("GET /v1/projects/{id}/boards",
		s.projectReadChain(s.wrapServeHandler(serve.HandleListBoards)))
	mux.HandleFunc("POST /v1/projects/{id}/boards",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleCreateBoard)))

	// Boards — read/update/delete by id
	// HandleGetBoard etc. read PathValue("id") for the board, so remap {bid}->id.
	boardRemap := [2]string{"bid", "id"}
	mux.HandleFunc("GET /v1/projects/{id}/boards/{bid}",
		s.projectReadChain(s.wrapServeHandler(serve.HandleGetBoard, boardRemap)))
	mux.HandleFunc("PATCH /v1/projects/{id}/boards/{bid}",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleUpdateBoard, boardRemap)))
	mux.HandleFunc("DELETE /v1/projects/{id}/boards/{bid}",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleDeleteBoard, boardRemap)))

	// Board issue positions. HandleSetBoardPosition reads PathValue("id") for
	// the board; HandleRemoveBoardPosition reads "id" + "issue_id".
	boardIssueRemap := [2]string{"iid", "issue_id"}
	mux.HandleFunc("POST /v1/projects/{id}/boards/{bid}/issues",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleSetBoardPosition, boardRemap)))
	mux.HandleFunc("DELETE /v1/projects/{id}/boards/{bid}/issues/{iid}",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleRemoveBoardPosition, boardRemap, boardIssueRemap)))

	// Focus / stats / sessions
	mux.HandleFunc("PUT /v1/projects/{id}/focus",
		s.projectMutateChain(s.wrapServeHandler(serve.HandleSetFocus)))
	mux.HandleFunc("GET /v1/projects/{id}/stats",
		s.projectReadChain(s.wrapServeHandler(serve.HandleStats)))
	mux.HandleFunc("GET /v1/projects/{id}/sessions",
		s.projectReadChain(s.wrapServeHandler(serve.HandleListSessions)))
}
