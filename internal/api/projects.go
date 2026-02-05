package api

import (
	"encoding/json"
	"net/http"

	"github.com/marcus/td/internal/serverdb"
)

// CreateProjectRequest is the JSON body for POST /v1/projects.
type CreateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ProjectResponse is the JSON representation of a project.
type ProjectResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	DeletedAt   *string `json:"deleted_at,omitempty"`
}

// handleCreateProject handles POST /v1/projects.
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r.Context())

	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "name is required")
		return
	}

	// Generate project ID and create event DB first to avoid orphaned server record
	projectID := serverdb.NewID()
	if _, err := s.dbPool.Create(projectID); err != nil {
		logFor(r.Context()).Error("create project db", "project", projectID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to initialize project database")
		return
	}

	project, err := s.store.CreateProjectWithID(projectID, req.Name, req.Description, user.UserID)
	if err != nil {
		logFor(r.Context()).Error("create project", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create project")
		return
	}

	writeJSON(w, http.StatusCreated, projectToResponse(project))
}

// handleListProjects handles GET /v1/projects.
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r.Context())

	projects, err := s.store.ListProjectsForUser(user.UserID)
	if err != nil {
		logFor(r.Context()).Error("list projects", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list projects")
		return
	}

	resp := make([]ProjectResponse, 0, len(projects))
	for _, p := range projects {
		resp = append(resp, projectToResponse(p))
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleGetProject handles GET /v1/projects/{id}.
func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	project, err := s.store.GetProject(projectID, false)
	if err != nil {
		logFor(r.Context()).Error("get project", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to get project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	writeJSON(w, http.StatusOK, projectToResponse(project))
}

// UpdateProjectRequest is the JSON body for PATCH /v1/projects/{id}.
type UpdateProjectRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// handleUpdateProject handles PATCH /v1/projects/{id}.
func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	var req UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}

	// Get current project to fill in unchanged fields
	current, err := s.store.GetProject(projectID, false)
	if err != nil {
		logFor(r.Context()).Error("get project for update", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to get project")
		return
	}
	if current == nil {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	name := current.Name
	desc := current.Description
	if req.Name != nil {
		if *req.Name == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "name cannot be empty")
			return
		}
		name = *req.Name
	}
	if req.Description != nil {
		desc = *req.Description
	}

	updated, err := s.store.UpdateProject(projectID, name, desc)
	if err != nil {
		logFor(r.Context()).Error("update project", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update project")
		return
	}

	writeJSON(w, http.StatusOK, projectToResponse(updated))
}

// handleDeleteProject handles DELETE /v1/projects/{id}.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	if err := s.store.SoftDeleteProject(projectID); err != nil {
		logFor(r.Context()).Error("delete project", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete project")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func projectToResponse(p *serverdb.Project) ProjectResponse {
	resp := ProjectResponse{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		CreatedAt:   p.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:   p.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if p.DeletedAt != nil {
		s := p.DeletedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.DeletedAt = &s
	}
	return resp
}
