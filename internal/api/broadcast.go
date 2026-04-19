package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/marcus/td/internal/serve"
)

// changeToken generates a monotonic token for SSE event IDs. The issues table
// has no dedicated change_token or row_version column, so we use
// time.Now().UnixNano() as a monotonic fallback. This is safe for the
// single-server deployment model: UnixNano is strictly increasing within a
// process and fine-grained enough that back-to-back writes in the same
// nanosecond are vanishingly rare on real hardware.
func changeToken() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// broadcastIssueUpsert broadcasts an issue.upserted event to all SSE
// subscribers of projectID. Call this AFTER the DB write succeeds.
func (s *Server) broadcastIssueUpsert(projectID, issueID string) {
	s.sseHubs.Broadcast(projectID, ProjectEvent{
		Type:        EventIssueUpserted,
		IssueID:     issueID,
		ChangeToken: changeToken(),
		Timestamp:   time.Now().UTC(),
	})
}

// broadcastIssueDelete broadcasts an issue.deleted event to all SSE
// subscribers of projectID. Call this AFTER the DB write succeeds.
func (s *Server) broadcastIssueDelete(projectID, issueID string) {
	s.sseHubs.Broadcast(projectID, ProjectEvent{
		Type:        EventIssueDeleted,
		IssueID:     issueID,
		ChangeToken: changeToken(),
		Timestamp:   time.Now().UTC(),
	})
}

// wrapIssueUpsert wraps a serve.HandleXxx function for issue upsert routes
// (create / patch / transition). It captures the HTTP status written by the
// inner handler and, on any 2xx success, calls s.broadcastIssueUpsert. The
// issuePathVar argument names the path variable that carries the issue ID
// (e.g. "iid" for /issues/{iid}); for create routes where the issue ID is not
// in the URL pass "".
//
// pathRemap is forwarded to wrapServeHandler so caller path-var translations
// (e.g. {iid} -> {id}) are applied before the serve handler runs.
func (s *Server) wrapIssueUpsert(
	h func(serve.HandlerContext, http.ResponseWriter, *http.Request),
	issuePathVar string,
	pathRemap ...[2]string,
) http.HandlerFunc {
	inner := s.wrapServeHandler(h, pathRemap...)
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("id")
		// Capture the issue ID before the remap (path vars are mutated in-place
		// by wrapServeHandler, so we read the pre-remap name here).
		issueID := ""
		if issuePathVar != "" {
			issueID = r.PathValue(issuePathVar)
		}

		sc := &statusCapture{ResponseWriter: w, code: http.StatusOK}
		inner(sc, r)

		// Broadcast only on success. For create (no issuePathVar) the response
		// body contains the issue ID but we do not re-parse it here; we emit
		// an upsert with an empty IssueID which is enough for clients to
		// trigger a re-fetch of the issue list. For known-ID routes the ID is
		// available from the path.
		if sc.code < 400 {
			s.broadcastIssueUpsert(projectID, issueID)
		}
	}
}

// wrapIssueDelete wraps a serve.HandleXxx function for issue delete routes.
// On 2xx success it calls s.broadcastIssueDelete.
//
// pathRemap is forwarded to wrapServeHandler.
func (s *Server) wrapIssueDelete(
	h func(serve.HandlerContext, http.ResponseWriter, *http.Request),
	issuePathVar string,
	pathRemap ...[2]string,
) http.HandlerFunc {
	inner := s.wrapServeHandler(h, pathRemap...)
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("id")
		issueID := ""
		if issuePathVar != "" {
			issueID = r.PathValue(issuePathVar)
		}

		sc := &statusCapture{ResponseWriter: w, code: http.StatusOK}
		inner(sc, r)

		if sc.code < 400 {
			s.broadcastIssueDelete(projectID, issueID)
		}
	}
}
