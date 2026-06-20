package serverdb

import (
	"testing"
)

// --- slugify tests ---

func TestSlugify(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// Basic cases
		{"hello world", "hello-world"},
		{"Hello World", "hello-world"},
		{"  hello  ", "hello"},
		{"", ""},
		{"   ", ""},

		// Punctuation collapses to single dash
		{"foo--bar", "foo-bar"},
		{"foo...bar", "foo-bar"},
		{"foo!@#bar", "foo-bar"},

		// Leading/trailing non-alphanumeric stripped
		{"--hello--", "hello"},
		{"!hello!", "hello"},
		{"123abc", "123abc"},
		{"abc123", "abc123"},

		// Already a slug
		{"my-project", "my-project"},
		{"my-project-2", "my-project-2"},

		// Unicode/non-ASCII collapses
		{"héllo", "h-llo"},
		{"こんにちは", ""},
		{"café", "caf"},

		// Numbers only
		{"123", "123"},

		// Mixed runs
		{"foo   bar---baz", "foo-bar-baz"},
		{"Project Name: v2.0!", "project-name-v2-0"},
	}

	for _, tc := range cases {
		got := slugify(tc.input)
		if got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSlugBase(t *testing.T) {
	// When name has a valid slug, use it
	got := slugBase("My Project", "p_abc123")
	if got != "my-project" {
		t.Errorf("slugBase with valid name: got %q, want my-project", got)
	}

	// When name slugifies to empty (pure unicode), fall back to id
	got = slugBase("こんにちは", "p_abc123")
	if got != "p_abc123" {
		t.Errorf("slugBase with empty slug name: got %q, want p_abc123", got)
	}

	// Empty name falls back to id
	got = slugBase("", "p_abc123")
	if got != "p_abc123" {
		t.Errorf("slugBase with empty name: got %q, want p_abc123", got)
	}
}

// --- uniqueSlug tests ---

func TestUniqueSlug_FirstProjectGetsBareSlug(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("first@test.com")
	p, err := db.CreateProject("My Project", "", u.ID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if p.Slug != "my-project" {
		t.Errorf("slug = %q, want my-project", p.Slug)
	}
}

func TestUniqueSlug_DuplicateNameGetsSuffix(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("dup@test.com")

	p1, err := db.CreateProject("Foo Project", "", u.ID)
	if err != nil {
		t.Fatalf("create p1: %v", err)
	}
	if p1.Slug != "foo-project" {
		t.Errorf("p1.Slug = %q, want foo-project", p1.Slug)
	}

	p2, err := db.CreateProject("Foo Project", "", u.ID)
	if err != nil {
		t.Fatalf("create p2: %v", err)
	}
	if p2.Slug != "foo-project-2" {
		t.Errorf("p2.Slug = %q, want foo-project-2", p2.Slug)
	}

	p3, err := db.CreateProject("Foo Project", "", u.ID)
	if err != nil {
		t.Fatalf("create p3: %v", err)
	}
	if p3.Slug != "foo-project-3" {
		t.Errorf("p3.Slug = %q, want foo-project-3", p3.Slug)
	}
}

func TestUniqueSlug_ExcludeProjectID(t *testing.T) {
	db := newTestDB(t)

	// uniqueSlug with excludeProjectID == project's own id should return the
	// project's existing slug (so re-running backfill doesn't re-increment).
	u, _ := db.CreateUser("excl@test.com")
	p, _ := db.CreateProject("Bar Project", "", u.ID)

	// Calling uniqueSlug with the project's own id excluded: since no OTHER
	// project has this slug, it should come back as the base slug.
	got, err := uniqueSlug(db.conn, "bar-project", p.ID)
	if err != nil {
		t.Fatalf("uniqueSlug: %v", err)
	}
	if got != "bar-project" {
		t.Errorf("uniqueSlug with own ID excluded: got %q, want bar-project", got)
	}
}

func TestUniqueSlug_ExcludeID_OtherConflictStillIncrement(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("ei@test.com")

	// p1 gets "qux"
	p1, _ := db.CreateProject("Qux", "", u.ID)
	if p1.Slug != "qux" {
		t.Fatalf("p1.Slug = %q, want qux", p1.Slug)
	}

	// p2 would be "qux-2"
	p2, _ := db.CreateProject("Qux", "", u.ID)
	if p2.Slug != "qux-2" {
		t.Fatalf("p2.Slug = %q, want qux-2", p2.Slug)
	}

	// Now if we call uniqueSlug("qux", p2.ID) — excluding p2 itself,
	// p1 still holds "qux", so result is still "qux-2".
	got, err := uniqueSlug(db.conn, "qux", p2.ID)
	if err != nil {
		t.Fatalf("uniqueSlug: %v", err)
	}
	if got != "qux-2" {
		t.Errorf("uniqueSlug with conflict from other project: got %q, want qux-2", got)
	}
}

// --- CreateProjectWithID slug tests ---

func TestCreateProjectWithID_SetsSlug(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("cwid@test.com")

	p, err := db.CreateProjectWithID("p_custom01", "Custom Project", "desc", u.ID)
	if err != nil {
		t.Fatalf("create project with id: %v", err)
	}
	if p.Slug != "custom-project" {
		t.Errorf("slug = %q, want custom-project", p.Slug)
	}

	// Round-trip via GetProject
	got, err := db.GetProject("p_custom01", false)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if got.Slug != "custom-project" {
		t.Errorf("GetProject slug = %q, want custom-project", got.Slug)
	}
}

// --- BackfillProjectSlugs tests ---

func TestBackfillProjectSlugs_FillsNullSlugs(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("bf@test.com")

	// Create two projects normally (they get slugs)
	p1, _ := db.CreateProject("Backfill One", "", u.ID)
	p2, _ := db.CreateProject("Backfill Two", "", u.ID)

	// Manually null out the slugs to simulate pre-migration rows
	if _, err := db.conn.Exec(`UPDATE projects SET slug = NULL WHERE id IN (?, ?)`, p1.ID, p2.ID); err != nil {
		t.Fatalf("null slugs: %v", err)
	}

	// Verify they are now null
	var slug1 *string
	_ = db.conn.QueryRow(`SELECT slug FROM projects WHERE id = ?`, p1.ID).Scan(&slug1)
	if slug1 != nil {
		t.Fatalf("expected null slug for p1, got %v", *slug1)
	}

	if err := db.BackfillProjectSlugs(); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	got1, _ := db.GetProject(p1.ID, false)
	got2, _ := db.GetProject(p2.ID, false)

	if got1.Slug == "" {
		t.Errorf("p1 slug still empty after backfill")
	}
	if got2.Slug == "" {
		t.Errorf("p2 slug still empty after backfill")
	}
	if got1.Slug == got2.Slug {
		t.Errorf("p1 and p2 got the same slug: %q", got1.Slug)
	}
}

func TestBackfillProjectSlugs_Idempotent(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("idem@test.com")

	p, _ := db.CreateProject("Idempotent Project", "", u.ID)
	originalSlug := p.Slug

	// Run backfill again — project already has a slug, should not change
	if err := db.BackfillProjectSlugs(); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if err := db.BackfillProjectSlugs(); err != nil {
		t.Fatalf("third backfill: %v", err)
	}

	got, _ := db.GetProject(p.ID, false)
	if got.Slug != originalSlug {
		t.Errorf("slug changed after repeated backfill: got %q, want %q", got.Slug, originalSlug)
	}
}

func TestBackfillProjectSlugs_DeterministicByCreatedAt(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("det@test.com")

	// Create two projects that would share a base slug, but null both out.
	p1, _ := db.CreateProject("Overlap Project", "", u.ID)
	p2, _ := db.CreateProject("Overlap Project", "", u.ID)

	// Null both slugs out
	if _, err := db.conn.Exec(`UPDATE projects SET slug = NULL WHERE id IN (?, ?)`, p1.ID, p2.ID); err != nil {
		t.Fatalf("null slugs: %v", err)
	}
	// Also drop the unique index temporarily to permit both NULLs
	// (SQLite UNIQUE allows multiple NULLs, so this is fine already)

	if err := db.BackfillProjectSlugs(); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	got1, _ := db.GetProject(p1.ID, false)
	got2, _ := db.GetProject(p2.ID, false)

	// Earlier created_at wins the base slug
	if got1.Slug != "overlap-project" {
		t.Errorf("p1 (earlier) slug = %q, want overlap-project", got1.Slug)
	}
	if got2.Slug != "overlap-project-2" {
		t.Errorf("p2 (later) slug = %q, want overlap-project-2", got2.Slug)
	}
}

func TestBackfillProjectSlugs_EmptyNameFallsBackToID(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("emptyname@test.com")

	p, _ := db.CreateProject("Valid Name", "", u.ID)

	// Force the name to be one that slugifies to empty and null the slug
	if _, err := db.conn.Exec(`UPDATE projects SET name = 'こんにちは', slug = NULL WHERE id = ?`, p.ID); err != nil {
		t.Fatalf("force name: %v", err)
	}

	if err := db.BackfillProjectSlugs(); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	got, _ := db.GetProject(p.ID, false)
	// When slugify(name) == "", we use the project id as base
	if got.Slug == "" {
		t.Errorf("slug should not be empty when name slugifies to empty")
	}
	// The slug should start with the id (or be the id itself)
	if got.Slug != p.ID {
		t.Errorf("slug = %q, want %q (project id fallback)", got.Slug, p.ID)
	}
}

// --- ListProjectsForUser slug test ---

func TestListProjectsForUser_IncludesSlug(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("list@test.com")

	p1, _ := db.CreateProject("List Alpha", "", u.ID)
	p2, _ := db.CreateProject("List Beta", "", u.ID)

	projects, err := db.ListProjectsForUser(u.ID)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2, got %d", len(projects))
	}

	slugs := map[string]string{}
	for _, p := range projects {
		slugs[p.ID] = p.Slug
	}
	if slugs[p1.ID] != "list-alpha" {
		t.Errorf("p1 slug = %q, want list-alpha", slugs[p1.ID])
	}
	if slugs[p2.ID] != "list-beta" {
		t.Errorf("p2 slug = %q, want list-beta", slugs[p2.ID])
	}
}
