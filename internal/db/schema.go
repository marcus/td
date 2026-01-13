package db

// SchemaVersion is the current database schema version
const SchemaVersion = 11

const schema = `
-- Issues table
CREATE TABLE IF NOT EXISTS issues (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open',
    type TEXT NOT NULL DEFAULT 'task',
    priority TEXT NOT NULL DEFAULT 'P2',
    points INTEGER DEFAULT 0,
    labels TEXT DEFAULT '',
    parent_id TEXT DEFAULT '',
    acceptance TEXT DEFAULT '',
    implementer_session TEXT DEFAULT '',
    reviewer_session TEXT DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    closed_at DATETIME,
    deleted_at DATETIME,
    minor INTEGER DEFAULT 0,
    created_branch TEXT DEFAULT '',
    FOREIGN KEY (parent_id) REFERENCES issues(id)
);

-- Logs table
CREATE TABLE IF NOT EXISTS logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id TEXT DEFAULT '',
    session_id TEXT NOT NULL,
    work_session_id TEXT DEFAULT '',
    message TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'progress',
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Handoffs table
CREATE TABLE IF NOT EXISTS handoffs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    done TEXT DEFAULT '[]',
    remaining TEXT DEFAULT '[]',
    decisions TEXT DEFAULT '[]',
    uncertain TEXT DEFAULT '[]',
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (issue_id) REFERENCES issues(id)
);

-- Git snapshots table
CREATE TABLE IF NOT EXISTS git_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id TEXT NOT NULL,
    event TEXT NOT NULL,
    commit_sha TEXT NOT NULL,
    branch TEXT NOT NULL,
    dirty_files INTEGER DEFAULT 0,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (issue_id) REFERENCES issues(id)
);

-- Issue files table
CREATE TABLE IF NOT EXISTS issue_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id TEXT NOT NULL,
    file_path TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'implementation',
    linked_sha TEXT DEFAULT '',
    linked_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (issue_id) REFERENCES issues(id),
    UNIQUE(issue_id, file_path)
);

-- Issue dependencies table
CREATE TABLE IF NOT EXISTS issue_dependencies (
    issue_id TEXT NOT NULL,
    depends_on_id TEXT NOT NULL,
    relation_type TEXT NOT NULL DEFAULT 'depends_on',
    PRIMARY KEY (issue_id, depends_on_id),
    FOREIGN KEY (issue_id) REFERENCES issues(id),
    FOREIGN KEY (depends_on_id) REFERENCES issues(id)
);

-- Work sessions table
CREATE TABLE IF NOT EXISTS work_sessions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    session_id TEXT NOT NULL,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME,
    start_sha TEXT DEFAULT '',
    end_sha TEXT DEFAULT ''
);

-- Work session issues junction table
CREATE TABLE IF NOT EXISTS work_session_issues (
    work_session_id TEXT NOT NULL,
    issue_id TEXT NOT NULL,
    tagged_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (work_session_id, issue_id),
    FOREIGN KEY (work_session_id) REFERENCES work_sessions(id),
    FOREIGN KEY (issue_id) REFERENCES issues(id)
);

-- Comments table
CREATE TABLE IF NOT EXISTS comments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    text TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (issue_id) REFERENCES issues(id)
);

-- Sessions table for tracking session history
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    name TEXT DEFAULT '',
    context_id TEXT NOT NULL,
    previous_session_id TEXT DEFAULT '',
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME
);

-- Schema info table for version tracking
CREATE TABLE IF NOT EXISTS schema_info (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_issues_status ON issues(status);
CREATE INDEX IF NOT EXISTS idx_issues_priority ON issues(priority);
CREATE INDEX IF NOT EXISTS idx_issues_type ON issues(type);
CREATE INDEX IF NOT EXISTS idx_issues_parent ON issues(parent_id);
CREATE INDEX IF NOT EXISTS idx_issues_deleted ON issues(deleted_at);
CREATE INDEX IF NOT EXISTS idx_logs_issue ON logs(issue_id);
CREATE INDEX IF NOT EXISTS idx_handoffs_issue ON handoffs(issue_id);
CREATE INDEX IF NOT EXISTS idx_git_snapshots_issue ON git_snapshots(issue_id);
CREATE INDEX IF NOT EXISTS idx_issue_files_issue ON issue_files(issue_id);
CREATE INDEX IF NOT EXISTS idx_comments_issue ON comments(issue_id);
`

// Migration defines a database migration
type Migration struct {
	Version     int
	Description string
	SQL         string
}

// Migrations is the list of all database migrations in order
var Migrations = []Migration{
	// Version 1 is the initial schema - no migration needed
	{
		Version:     2,
		Description: "Add action_log table for undo support",
		SQL: `
CREATE TABLE IF NOT EXISTS action_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    action_type TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    previous_data TEXT DEFAULT '',
    new_data TEXT DEFAULT '',
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    undone INTEGER DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_action_log_session ON action_log(session_id);
CREATE INDEX IF NOT EXISTS idx_action_log_timestamp ON action_log(timestamp);
`,
	},
	{
		Version:     3,
		Description: "Allow work session logs without issue_id",
		SQL: `
-- SQLite doesn't support ALTER COLUMN, so we need to recreate the table
CREATE TABLE logs_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id TEXT DEFAULT '',
    session_id TEXT NOT NULL,
    work_session_id TEXT DEFAULT '',
    message TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'progress',
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO logs_new SELECT * FROM logs;
DROP TABLE logs;
ALTER TABLE logs_new RENAME TO logs;
CREATE INDEX IF NOT EXISTS idx_logs_issue ON logs(issue_id);
CREATE INDEX IF NOT EXISTS idx_logs_work_session ON logs(work_session_id);
`,
	},
	{
		Version:     4,
		Description: "Add minor flag to issues for self-reviewable tasks",
		SQL:         `ALTER TABLE issues ADD COLUMN minor INTEGER DEFAULT 0;`,
	},
	{
		Version:     5,
		Description: "Add created_branch to issues",
		SQL:         `ALTER TABLE issues ADD COLUMN created_branch TEXT DEFAULT '';`,
	},
	{
		Version:     6,
		Description: "Add creator_session for review enforcement",
		SQL:         `ALTER TABLE issues ADD COLUMN creator_session TEXT DEFAULT '';`,
	},
	{
		Version:     7,
		Description: "Add session history for review enforcement",
		SQL: `CREATE TABLE IF NOT EXISTS issue_session_history (
    id TEXT PRIMARY KEY,
    issue_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    action TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (issue_id) REFERENCES issues(id)
);
CREATE INDEX IF NOT EXISTS idx_ish_issue ON issue_session_history(issue_id);
CREATE INDEX IF NOT EXISTS idx_ish_session ON issue_session_history(session_id);`,
	},
	{
		Version:     8,
		Description: "Add timestamp indexes for activity queries",
		SQL: `CREATE INDEX IF NOT EXISTS idx_handoffs_timestamp ON handoffs(timestamp);
CREATE INDEX IF NOT EXISTS idx_issues_deleted_status ON issues(deleted_at, status);`,
	},
	{
		Version:     9,
		Description: "Add boards and board_issues tables",
		SQL: `
-- Boards table
CREATE TABLE IF NOT EXISTS boards (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL COLLATE NOCASE UNIQUE,
    last_viewed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Board-Issue membership with ordering
CREATE TABLE IF NOT EXISTS board_issues (
    board_id TEXT NOT NULL,
    issue_id TEXT NOT NULL,
    position INTEGER NOT NULL,
    added_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (board_id, issue_id),
    FOREIGN KEY (board_id) REFERENCES boards(id) ON DELETE CASCADE,
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_board_issues_position ON board_issues(board_id, position);
`,
	},
	{
		Version:     10,
		Description: "Query-based boards with sparse ordering and sprint field",
		SQL: `
-- Add query and is_builtin columns to boards
ALTER TABLE boards ADD COLUMN query TEXT NOT NULL DEFAULT '';
ALTER TABLE boards ADD COLUMN is_builtin INTEGER NOT NULL DEFAULT 0;

-- Rename board_issues to board_issue_positions for semantic clarity
DROP INDEX IF EXISTS idx_board_issues_position;
ALTER TABLE board_issues RENAME TO board_issue_positions;

-- Recreate index on positions
CREATE UNIQUE INDEX IF NOT EXISTS idx_board_positions_position
    ON board_issue_positions(board_id, position);

-- Add sprint field to issues
ALTER TABLE issues ADD COLUMN sprint TEXT DEFAULT '';

-- Create built-in "All Issues" board (empty query = all issues)
INSERT INTO boards (id, name, query, is_builtin, created_at, updated_at)
VALUES ('bd-all-issues', 'All Issues', '', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(name) DO UPDATE SET
    query = excluded.query,
    is_builtin = 1,
    updated_at = CURRENT_TIMESTAMP;
`,
	},
	{
		Version:     11,
		Description: "Add view_mode to boards for swimlanes/backlog toggle",
		SQL:         `ALTER TABLE boards ADD COLUMN view_mode TEXT NOT NULL DEFAULT 'swimlanes';`,
	},
}
