package serverdb

// ServerSchemaVersion is the current server database schema version
const ServerSchemaVersion = 3

const serverSchema = `
-- Users table
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    email_verified_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- API keys table
CREATE TABLE IF NOT EXISTS api_keys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    key_hash TEXT UNIQUE NOT NULL,
    key_prefix TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    scopes TEXT NOT NULL DEFAULT 'sync',
    expires_at DATETIME,
    last_used_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Projects table
CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);

-- Memberships table
CREATE TABLE IF NOT EXISTS memberships (
    project_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('owner', 'writer', 'reader')),
    invited_by TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (project_id, user_id),
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Sync cursors table
CREATE TABLE IF NOT EXISTS sync_cursors (
    project_id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    last_event_id BIGINT NOT NULL DEFAULT 0,
    last_sync_at DATETIME,
    PRIMARY KEY (project_id, client_id),
    FOREIGN KEY (project_id) REFERENCES projects(id)
);

-- Schema info table
CREATE TABLE IF NOT EXISTS schema_info (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_api_keys_user ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(key_prefix);
CREATE INDEX IF NOT EXISTS idx_memberships_user ON memberships(user_id);
CREATE INDEX IF NOT EXISTS idx_projects_deleted ON projects(deleted_at);
`

// Migration defines a server database migration
type Migration struct {
	Version     int
	Description string
	SQL         string
}

// Migrations is the list of all server database migrations in order
var Migrations = []Migration{
	// Version 1 is the initial schema - no migration needed
	{
		Version:     2,
		Description: "Add auth_requests table for device auth flow",
		SQL: `CREATE TABLE IF NOT EXISTS auth_requests (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL,
			device_code TEXT UNIQUE NOT NULL,
			user_code TEXT UNIQUE NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			user_id TEXT,
			api_key_id TEXT,
			expires_at DATETIME NOT NULL,
			verified_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_auth_requests_device_code ON auth_requests(device_code);
		CREATE INDEX IF NOT EXISTS idx_auth_requests_user_code ON auth_requests(user_code);
		CREATE INDEX IF NOT EXISTS idx_auth_requests_status ON auth_requests(status);
		CREATE INDEX IF NOT EXISTS idx_auth_requests_cleanup ON auth_requests(status, expires_at);`,
	},
	{
		Version:     3,
		Description: "Add encryption tables for end-to-end encrypted sync",
		SQL: `CREATE TABLE IF NOT EXISTS user_public_keys (
			user_id TEXT NOT NULL,
			public_key BLOB NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id)
		);

		CREATE TABLE IF NOT EXISTS encrypted_private_keys (
			user_id TEXT NOT NULL,
			encrypted_key BLOB NOT NULL,
			salt BLOB NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id)
		);

		CREATE TABLE IF NOT EXISTS project_key_epochs (
			project_id TEXT NOT NULL,
			epoch INTEGER NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			created_by TEXT NOT NULL,
			PRIMARY KEY (project_id, epoch)
		);

		CREATE TABLE IF NOT EXISTS wrapped_project_keys (
			project_id TEXT NOT NULL,
			epoch INTEGER NOT NULL,
			user_id TEXT NOT NULL,
			wrapped_key BLOB NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (project_id, epoch, user_id)
		);`,
	},
}
