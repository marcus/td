package api

import (
	"encoding/json"
	"net/http"
)

// AddMemberRequest is the JSON body for POST /v1/projects/{id}/members.
type AddMemberRequest struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
}

// MemberResponse is the JSON representation of a membership.
type MemberResponse struct {
	ProjectID string `json:"project_id"`
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	InvitedBy string `json:"invited_by"`
	CreatedAt string `json:"created_at"`
}

// UpdateMemberRequest is the JSON body for PATCH /v1/projects/{id}/members/{userID}.
type UpdateMemberRequest struct {
	Role string `json:"role"`
}

// handleAddMember handles POST /v1/projects/{id}/members.
func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	user := getUserFromContext(r.Context())

	var req AddMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}

	// Resolve email to user ID if email provided without user_id
	if req.Email != "" && req.UserID == "" {
		user, err := s.store.GetUserByEmail(req.Email)
		if err != nil {
			logFor(r.Context()).Error("lookup user by email", "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to look up user")
			return
		}
		if user == nil {
			user, err = s.store.CreateUser(req.Email)
			if err != nil {
				logFor(r.Context()).Error("create user by email", "err", err)
				writeError(w, http.StatusInternalServerError, "internal_error", "failed to create user")
				return
			}
		}
		req.UserID = user.ID
	}

	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "user_id or email is required")
		return
	}
	if req.Role == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "role is required")
		return
	}

	m, err := s.store.AddMember(projectID, req.UserID, req.Role, user.UserID)
	if err != nil {
		logFor(r.Context()).Error("add member", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to add member")
		return
	}

	writeJSON(w, http.StatusCreated, MemberResponse{
		ProjectID: m.ProjectID,
		UserID:    m.UserID,
		Role:      m.Role,
		InvitedBy: m.InvitedBy,
		CreatedAt: m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

// handleListMembers handles GET /v1/projects/{id}/members.
func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	members, err := s.store.ListMembers(projectID)
	if err != nil {
		logFor(r.Context()).Error("list members", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list members")
		return
	}

	resp := make([]MemberResponse, 0, len(members))
	for _, m := range members {
		resp = append(resp, MemberResponse{
			ProjectID: m.ProjectID,
			UserID:    m.UserID,
			Role:      m.Role,
			InvitedBy: m.InvitedBy,
			CreatedAt: m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleUpdateMember handles PATCH /v1/projects/{id}/members/{userID}.
func (s *Server) handleUpdateMember(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	targetUserID := r.PathValue("userID")

	var req UpdateMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}

	if req.Role == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "role is required")
		return
	}

	if err := s.store.UpdateMemberRole(projectID, targetUserID, req.Role); err != nil {
		logFor(r.Context()).Error("update member", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update member")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleRemoveMember handles DELETE /v1/projects/{id}/members/{userID}.
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	targetUserID := r.PathValue("userID")

	if err := s.store.RemoveMember(projectID, targetUserID); err != nil {
		logFor(r.Context()).Error("remove member", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to remove member")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
