package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/marcus/td/internal/email"
	"github.com/marcus/td/internal/serverdb"
)

const invitationTTL = 7 * 24 * time.Hour

type CreateInvitationRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type InvitationResponse struct {
	ID         string  `json:"id"`
	ProjectID  string  `json:"project_id"`
	Email      string  `json:"email"`
	Role       string  `json:"role"`
	InvitedBy  string  `json:"invited_by"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
	ExpiresAt  string  `json:"expires_at"`
	AcceptedAt *string `json:"accepted_at,omitempty"`
}

func handleInvitationError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, serverdb.ErrInvitationNotFound):
		writeError(w, http.StatusNotFound, "not_found", "invitation not found")
	case errors.Is(err, serverdb.ErrInvitationEmailMismatch):
		writeError(w, http.StatusForbidden, "forbidden", "invitation does not belong to authenticated user")
	case errors.Is(err, serverdb.ErrInvitationExpired):
		writeError(w, http.StatusGone, "expired", "invitation has expired")
	case errors.Is(err, serverdb.ErrInvitationNotPending):
		writeError(w, http.StatusConflict, "conflict", "invitation is not pending")
	default:
		return false
	}
	return true
}

func invitationResponse(inv *serverdb.Invitation) InvitationResponse {
	var acceptedAt *string
	if inv.AcceptedAt != nil {
		v := inv.AcceptedAt.Format(time.RFC3339)
		acceptedAt = &v
	}
	return InvitationResponse{
		ID:         inv.ID,
		ProjectID:  inv.ProjectID,
		Email:      inv.Email,
		Role:       inv.Role,
		InvitedBy:  inv.InvitedBy,
		Status:     inv.Status,
		CreatedAt:  inv.CreatedAt.Format(time.RFC3339),
		ExpiresAt:  inv.ExpiresAt.Format(time.RFC3339),
		AcceptedAt: acceptedAt,
	}
}

func invitationResponses(invitations []*serverdb.Invitation) []InvitationResponse {
	resp := make([]InvitationResponse, 0, len(invitations))
	for _, inv := range invitations {
		resp = append(resp, invitationResponse(inv))
	}
	return resp
}

func validateInvitationRole(role string) bool {
	return role == serverdb.RoleOwner || role == serverdb.RoleWriter || role == serverdb.RoleReader
}

func generateInvitationToken() (plaintext, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	plaintext = hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(plaintext))
	return plaintext, hex.EncodeToString(sum[:]), nil
}

func inviteLoginBase(callbackURL string) string {
	const fallback = "http://localhost:5173/home/login"
	if callbackURL == "" {
		return fallback
	}
	u, err := url.Parse(callbackURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fallback
	}
	u.Path = "/home/login"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func buildInvitationLoginLink(callbackURL, invitationID, emailAddr, token string) string {
	u, err := url.Parse(inviteLoginBase(callbackURL))
	if err != nil {
		return inviteLoginBase("")
	}
	q := u.Query()
	q.Set("email", emailAddr)
	q.Set("invitation_id", invitationID)
	q.Set("invitation_token", token)
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *Server) handleCreateInvitation(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	user := getUserFromContext(r.Context())
	actor := getActingUserFromContext(r.Context())

	var req CreateInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	addr, err := mail.ParseAddress(req.Email)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "valid email is required")
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(addr.Address))
	if !validateInvitationRole(req.Role) {
		writeError(w, http.StatusBadRequest, "bad_request", "valid role is required")
		return
	}

	invitedBy := user.UserID
	if actor != nil && actor.UserID != "" {
		invitedBy = actor.UserID
	}

	plaintextToken, tokenHash, err := generateInvitationToken()
	if err != nil {
		logFor(r.Context()).Error("generate invitation token", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to generate invitation")
		return
	}

	inv, err := s.store.CreateInvitation(projectID, req.Email, req.Role, invitedBy, tokenHash, time.Now().UTC().Add(invitationTTL))
	if err != nil {
		logFor(r.Context()).Error("create invitation", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create invitation")
		return
	}

	linkURL := buildInvitationLoginLink(s.config.AuthWebCallbackURL, inv.ID, inv.Email, plaintextToken)
	textBody := fmt.Sprintf("You have been invited to a td-watch project as %s.\n\nSign in to accept: %s\n\nThis invitation expires in 7 days.", inv.Role, linkURL)
	htmlBody := fmt.Sprintf(
		`<p>You have been invited to a td-watch project as <strong>%s</strong>.</p>`+
			`<p><a href="%s" style="display:inline-block;padding:12px 24px;background:#0070f3;color:#fff;text-decoration:none;border-radius:4px;">Sign in to accept</a></p>`+
			`<p>Or copy this link into your browser (do not share it):</p>`+
			`<p style="word-break:break-all;">%s</p>`+
			`<p>This invitation expires in 7 days.</p>`,
		inv.Role, linkURL, linkURL,
	)

	if err := s.emailSender.SendLoginLink(r.Context(), email.LoginEmail{
		To:      inv.Email,
		Subject: "You're invited to td-watch",
		Text:    textBody,
		HTML:    htmlBody,
		Purpose: "project_invitation",
		TraceID: getRequestID(r.Context()),
	}); err != nil {
		logFor(r.Context()).Warn("send project invitation email failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to send invitation email")
		return
	}

	writeJSON(w, http.StatusCreated, invitationResponse(inv))
}

func (s *Server) handleListProjectInvitations(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	invitations, err := s.store.ListProjectInvitations(projectID)
	if err != nil {
		logFor(r.Context()).Error("list project invitations", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list invitations")
		return
	}
	writeJSON(w, http.StatusOK, invitationResponses(invitations))
}

func (s *Server) handleDeleteInvitation(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	invitationID := r.PathValue("invitationID")
	if err := s.store.DeleteInvitation(projectID, invitationID); err != nil {
		if handleInvitationError(w, err) {
			return
		}
		logFor(r.Context()).Error("delete invitation", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete invitation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListOwnInvitations(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r.Context())
	if user == nil || user.Email == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing auth user")
		return
	}
	invitations, err := s.store.ListPendingInvitationsForEmail(user.Email)
	if err != nil {
		logFor(r.Context()).Error("list own invitations", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list invitations")
		return
	}
	writeJSON(w, http.StatusOK, invitationResponses(invitations))
}

func (s *Server) handleAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r.Context())
	if user == nil || user.UserID == "" || user.Email == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing auth user")
		return
	}
	m, err := s.store.AcceptInvitation(r.PathValue("invitationID"), user.UserID, user.Email)
	if err != nil {
		if handleInvitationError(w, err) {
			return
		}
		logFor(r.Context()).Error("accept invitation", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to accept invitation")
		return
	}
	writeJSON(w, http.StatusOK, MemberResponse{
		ProjectID: m.ProjectID,
		UserID:    m.UserID,
		Role:      m.Role,
		InvitedBy: m.InvitedBy,
		CreatedAt: m.CreatedAt.Format(time.RFC3339),
	})
}

func (s *Server) handleDeclineInvitation(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r.Context())
	if user == nil || user.Email == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing auth user")
		return
	}
	if err := s.store.DeclineInvitation(r.PathValue("invitationID"), user.Email); err != nil {
		if handleInvitationError(w, err) {
			return
		}
		logFor(r.Context()).Error("decline invitation", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to decline invitation")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "declined"})
}
