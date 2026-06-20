package api

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// TestProjectResponse_SlugPresent verifies that POST /v1/projects returns
// a non-empty "slug" field in the JSON response.
func TestProjectResponse_SlugPresent(t *testing.T) {
	h := newTestHarness(t)
	_, tok := h.CreateUser("slug-owner@test.com")

	resp := h.Do("POST", "/v1/projects", tok, CreateProjectRequest{
		Name:        "My Slug Project",
		Description: "testing slug field",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /v1/projects: expected 201, got %d: %s", resp.StatusCode, body)
	}

	var pr ProjectResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if pr.Slug == "" {
		t.Error("POST /v1/projects: slug field is empty")
	}
	if pr.Slug != "my-slug-project" {
		t.Errorf("POST /v1/projects: slug = %q, want my-slug-project", pr.Slug)
	}
}

// TestGetProject_SlugPresent verifies that GET /v1/projects/{id} returns slug.
func TestGetProject_SlugPresent(t *testing.T) {
	h := newTestHarness(t)
	// First user is auto-admin; use them as the project owner so they can GET.
	_, ownerTok := h.CreateUser("owner-slug@test.com")

	pid := h.CreateProject(ownerTok, "Get Slug Test")

	// Owner can GET their own project (they are a member).
	resp := h.Do("GET", "/v1/projects/"+pid, ownerTok, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /v1/projects/%s: expected 200, got %d: %s", pid, resp.StatusCode, body)
	}

	var pr ProjectResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if pr.Slug == "" {
		t.Error("GET /v1/projects/{id}: slug field is empty")
	}
	if pr.Slug != "get-slug-test" {
		t.Errorf("GET /v1/projects/{id}: slug = %q, want get-slug-test", pr.Slug)
	}
}

// TestListProjects_SlugPresent verifies that GET /v1/projects returns slug on
// every project entry.
func TestListProjects_SlugPresent(t *testing.T) {
	h := newTestHarness(t)
	_, tok := h.CreateUser("list-slug@test.com")

	h.CreateProject(tok, "List Alpha Slug")
	h.CreateProject(tok, "List Beta Slug")

	resp := h.Do("GET", "/v1/projects", tok, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /v1/projects: expected 200, got %d: %s", resp.StatusCode, body)
	}

	var projects []ProjectResponse
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}

	for _, p := range projects {
		if p.Slug == "" {
			t.Errorf("project %q has empty slug", p.Name)
		}
	}

	slugs := map[string]string{}
	for _, p := range projects {
		slugs[p.Name] = p.Slug
	}
	if slugs["List Alpha Slug"] != "list-alpha-slug" {
		t.Errorf("List Alpha Slug: slug = %q, want list-alpha-slug", slugs["List Alpha Slug"])
	}
	if slugs["List Beta Slug"] != "list-beta-slug" {
		t.Errorf("List Beta Slug: slug = %q, want list-beta-slug", slugs["List Beta Slug"])
	}
}

// TestProjectResponse_DuplicateNameSlugSuffix verifies that two projects with
// the same name get unique slugs via the -2, -3 suffix scheme.
func TestProjectResponse_DuplicateNameSlugSuffix(t *testing.T) {
	h := newTestHarness(t)
	_, tok := h.CreateUser("dup-slug@test.com")

	resp1 := h.Do("POST", "/v1/projects", tok, CreateProjectRequest{Name: "Dup Slug"})
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp1.Body)
		t.Fatalf("create p1: %d %s", resp1.StatusCode, body)
	}
	var p1 ProjectResponse
	if err := json.NewDecoder(resp1.Body).Decode(&p1); err != nil {
		t.Fatalf("decode p1: %v", err)
	}

	resp2 := h.Do("POST", "/v1/projects", tok, CreateProjectRequest{Name: "Dup Slug"})
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("create p2: %d %s", resp2.StatusCode, body)
	}
	var p2 ProjectResponse
	if err := json.NewDecoder(resp2.Body).Decode(&p2); err != nil {
		t.Fatalf("decode p2: %v", err)
	}

	if p1.Slug != "dup-slug" {
		t.Errorf("p1.Slug = %q, want dup-slug", p1.Slug)
	}
	if p2.Slug != "dup-slug-2" {
		t.Errorf("p2.Slug = %q, want dup-slug-2", p2.Slug)
	}
}
