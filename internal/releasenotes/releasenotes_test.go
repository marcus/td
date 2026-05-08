package releasenotes

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseSubjectScopedConventionalCommit(t *testing.T) {
	parsed := ParseSubject("feat(api): add release note endpoint")

	if !parsed.Conventional {
		t.Fatal("expected conventional subject")
	}
	if parsed.Type != "feat" {
		t.Fatalf("type = %q, want feat", parsed.Type)
	}
	if parsed.Scope != "api" {
		t.Fatalf("scope = %q, want api", parsed.Scope)
	}
	if parsed.Description != "add release note endpoint" {
		t.Fatalf("description = %q", parsed.Description)
	}
}

func TestClassifyUnknownSubjectAsImprovement(t *testing.T) {
	item, section, ok := Classify(Commit{
		ShortHash: "abc1234",
		Subject:   "Polish release checklist",
	})
	if !ok {
		t.Fatal("expected commit to be included")
	}
	if section != SectionImprovements {
		t.Fatalf("section = %q, want %q", section, SectionImprovements)
	}
	if item.Subject != "Polish release checklist" {
		t.Fatalf("subject = %q", item.Subject)
	}
	if item.Internal {
		t.Fatal("unknown subjects should not be treated as internal")
	}
}

func TestClassifyFiltersFixupSquashAndMergeSubjects(t *testing.T) {
	subjects := []string{
		"fixup! feat: add release notes",
		"squash! fix: repair output",
		"Merge branch 'main'",
	}
	for _, subject := range subjects {
		t.Run(subject, func(t *testing.T) {
			_, _, ok := Classify(Commit{ShortHash: "abc1234", Subject: subject})
			if ok {
				t.Fatalf("expected %q to be filtered", subject)
			}
		})
	}
}

func TestClassifyBreakingChangeFooter(t *testing.T) {
	item, section, ok := Classify(Commit{
		ShortHash: "abc1234",
		Subject:   "feat: replace config file format",
		Body:      "feat: replace config file format\n\nBREAKING CHANGE: old config files must be migrated",
	})
	if !ok {
		t.Fatal("expected commit to be included")
	}
	if section != SectionBreaking {
		t.Fatalf("section = %q, want %q", section, SectionBreaking)
	}
	if !item.Breaking {
		t.Fatal("expected breaking item")
	}
	if item.BreakingNote != "old config files must be migrated" {
		t.Fatalf("breaking note = %q", item.BreakingNote)
	}
}

func TestBuildDraftFiltersInternalUnlessRequested(t *testing.T) {
	commits := []Commit{
		{ShortHash: "aaa1111", Subject: "feat: add release notes"},
		{ShortHash: "bbb2222", Subject: "chore: tune release script"},
	}

	publicDraft := BuildDraft(commits, Draft{From: "v0.1.0", To: "HEAD"}, false)
	if publicDraft.CommitCount != 1 {
		t.Fatalf("public commit count = %d, want 1", publicDraft.CommitCount)
	}
	if got := sectionItems(publicDraft, SectionMaintenance); len(got) != 0 {
		t.Fatalf("maintenance items = %d, want 0", len(got))
	}

	internalDraft := BuildDraft(commits, Draft{From: "v0.1.0", To: "HEAD"}, true)
	if internalDraft.CommitCount != 2 {
		t.Fatalf("internal commit count = %d, want 2", internalDraft.CommitCount)
	}
	if got := sectionItems(internalDraft, SectionMaintenance); len(got) != 1 {
		t.Fatalf("maintenance items = %d, want 1", len(got))
	}
}

func TestRenderMarkdownUsesStableSectionsAndShortSHAs(t *testing.T) {
	draft := BuildDraft([]Commit{
		{ShortHash: "aaa1111", Subject: "feat(cli): add release notes"},
		{ShortHash: "bbb2222", Subject: "fix: handle empty range"},
	}, Draft{Version: "v0.2.0", Date: "2026-05-08", From: "v0.1.0", To: "HEAD"}, false)

	md := RenderMarkdown(draft)
	for _, want := range []string{
		"## v0.2.0 - 2026-05-08",
		"_Range: `v0.1.0..HEAD` (2 commits)_",
		"### Features",
		"- add release notes (aaa1111)",
		"### Bug Fixes",
		"- handle empty range (bbb2222)",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "feat(cli):") {
		t.Fatalf("markdown should strip conventional prefix and scope:\n%s", md)
	}
}

func TestJSONDraftShape(t *testing.T) {
	draft := BuildDraft([]Commit{
		{ShortHash: "aaa1111", Subject: "feat(cli): add release notes"},
	}, Draft{Version: "v0.2.0", From: "v0.1.0", To: "HEAD", Repository: "/repo"}, false)

	data, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Draft
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Version != "v0.2.0" || decoded.From != "v0.1.0" || decoded.To != "HEAD" {
		t.Fatalf("decoded draft has wrong range/version: %+v", decoded)
	}
	if decoded.CommitCount != 1 {
		t.Fatalf("commit count = %d, want 1", decoded.CommitCount)
	}
	if len(decoded.Sections) != len(orderedSections) {
		t.Fatalf("sections = %d, want %d", len(decoded.Sections), len(orderedSections))
	}
	items := sectionItems(&decoded, SectionFeatures)
	if len(items) != 1 {
		t.Fatalf("feature items = %d, want 1", len(items))
	}
	if items[0].SHA != "aaa1111" || items[0].Type != "feat" || items[0].Scope != "cli" {
		t.Fatalf("unexpected item: %+v", items[0])
	}
}

func sectionItems(draft *Draft, id string) []Item {
	for _, section := range draft.Sections {
		if section.ID == id {
			return section.Items
		}
	}
	return nil
}
