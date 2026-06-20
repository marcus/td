package serverdb

// ServerSchemaVersion is the current server database schema version
const ServerSchemaVersion = 5

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

-- Invitations table
CREATE TABLE IF NOT EXISTS invitations (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    email TEXT NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('owner', 'writer', 'reader')),
    invited_by TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK(status IN ('pending', 'accepted', 'declined', 'expired')),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    accepted_at DATETIME,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    FOREIGN KEY (invited_by) REFERENCES users(id) ON DELETE CASCADE
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
CREATE INDEX IF NOT EXISTS idx_invitations_project ON invitations(project_id);
CREATE INDEX IF NOT EXISTS idx_invitations_email_status ON invitations(email, status);
CREATE INDEX IF NOT EXISTS idx_invitations_cleanup ON invitations(status, expires_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_invitations_token_hash ON invitations(token_hash);
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
		Description: "Add is_admin, auth_events, rate_limit_events, project event caching",
		SQL: `ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT 0;

		CREATE TABLE IF NOT EXISTS auth_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			auth_request_id TEXT NOT NULL,
			email TEXT NOT NULL,
			event_type TEXT NOT NULL,
			metadata TEXT DEFAULT '{}',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_auth_events_type ON auth_events(event_type);
		CREATE INDEX IF NOT EXISTS idx_auth_events_email ON auth_events(email);
		CREATE INDEX IF NOT EXISTS idx_auth_events_created ON auth_events(created_at);

		CREATE TABLE IF NOT EXISTS rate_limit_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key_id TEXT,
			ip TEXT,
			endpoint_class TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_rle_created ON rate_limit_events(created_at);
		CREATE INDEX IF NOT EXISTS idx_rle_key ON rate_limit_events(key_id);

		ALTER TABLE projects ADD COLUMN event_count INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE projects ADD COLUMN last_event_at DATETIME;`,
	},
	{
		Version:     4,
		Description: "Add auth_email_challenges table for email-verified auth",
		SQL: `CREATE TABLE auth_email_challenges (
			id TEXT PRIMARY KEY,
			purpose TEXT NOT NULL CHECK (purpose IN ('web_login','device_login','email_verify','admin_login')),
			email TEXT NOT NULL,
			user_id TEXT,
			selector TEXT UNIQUE NOT NULL,
			token_hash TEXT NOT NULL,
			otp_hash TEXT,
			device_code_hash TEXT,
			code_challenge TEXT,
			code_challenge_method TEXT,
			redirect_uri TEXT,
			state_hash TEXT,
			status TEXT NOT NULL DEFAULT 'pending'
				CHECK (status IN ('pending','verified','consumed','expired','failed','suppressed')),
			attempts INTEGER NOT NULL DEFAULT 0,
			ip TEXT,
			user_agent TEXT,
			expires_at DATETIME NOT NULL,
			verified_at DATETIME,
			consumed_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL
		);
		CREATE INDEX idx_auth_email_challenges_email_created ON auth_email_challenges(email, created_at);
		CREATE INDEX idx_auth_email_challenges_device ON auth_email_challenges(device_code_hash);
		CREATE INDEX idx_auth_email_challenges_cleanup ON auth_email_challenges(status, expires_at);`,
	},
	{
		Version:     5,
		Description: "Add project invitations table",
		SQL: `CREATE TABLE IF NOT EXISTS invitations (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			email TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('owner', 'writer', 'reader')),
			invited_by TEXT NOT NULL,
			token_hash TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending'
				CHECK(status IN ('pending', 'accepted', 'declined', 'expired')),
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL,
			accepted_at DATETIME,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY (invited_by) REFERENCES users(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_invitations_project ON invitations(project_id);
		CREATE INDEX IF NOT EXISTS idx_invitations_email_status ON invitations(email, status);
		CREATE INDEX IF NOT EXISTS idx_invitations_cleanup ON invitations(status, expires_at);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_invitations_token_hash ON invitations(token_hash);`,
	},
}
