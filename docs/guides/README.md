# Guides Migration to Skills

This directory contains legacy and in-repo implementation guides.

The long-term direction is to keep **task workflow guidance** in skills and keep **project-specific architecture/design docs** in `docs/`.

## Where Skills Live

- Repo-local skill package: `td-task-management/SKILL.md`
- Repo-local skill references: `td-task-management/references/`
- Typical installed location (developer machine): `~/.claude/skills/<skill-name>/SKILL.md`

## Migration Status

Coverage check was done by comparing guide content to:

- `td-task-management/SKILL.md`
- `td-task-management/references/quick_reference.md`
- `td-task-management/references/ai_agent_workflows.md`

### Fully covered by skills (moved)

None yet.

### Not fully covered by skills (kept in `docs/guides/`)

| Guide | Skill mapping | Coverage result |
|---|---|---|
| `docs/guides/cli-commands-guide.md` | `td-task-management/*` | Not covered (CLI implementation internals not in skill) |
| `docs/guides/collaboration.md` | `td-task-management/*` | Not covered (sync auth/project/member workflows missing) |
| `docs/guides/declarative-modal-guide.md` | `td-task-management/*` | Not covered (modal API and monitor UI internals missing) |
| `docs/guides/lipgloss-table-guide.md` | `td-task-management/*` | Not covered (table rendering/hit-testing internals missing) |
| `docs/guides/monitor-shortcuts-guide.md` | `td-task-management/*` | Not covered (keymap/monitor shortcut implementation missing) |
| `docs/guides/query-guide.md` | `td-task-management/*` | Not covered (TDQ language reference missing) |
| `docs/guides/releasing-new-version.md` | `td-task-management/*` | Not covered (release workflow/Goreleaser process missing) |
| `docs/guides/shortcuts-implementation-guide.md` | `td-task-management/*` | Not covered (meta-guide for CLI/monitor shortcut implementation) |
| `docs/guides/sync-setup-guide.md` | `td-task-management/*` | Not covered (sync bootstrap and config workflows missing) |
| `docs/guides/deprecated/modal-system-guide.md` | `td-task-management/*` | Not covered (legacy modal architecture reference) |

## How To Use Skills (Quick Tutorial)

1. Open the skill metadata and instructions:
   - `td-task-management/SKILL.md`
2. Follow linked references as needed:
   - `td-task-management/references/quick_reference.md`
   - `td-task-management/references/ai_agent_workflows.md`
3. If using an agent that supports local skills, install the skill directory:
   - `cp -R td-task-management ~/.claude/skills/`
4. In your agent instructions, tell it to use the skill when working on td workflows:
   - Example: "Use `td-task-management` for start/log/handoff/review workflows."

## Link Policy During Migration

- Active, authoritative workflow guidance should point to skill paths when a skill fully replaces a guide.
- Historical/spec references should point to `docs/deprecated/guides/` **after** a covered guide is moved there.
- If a guide is not fully covered, keep links pointed at `docs/guides/`.
