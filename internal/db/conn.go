package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// OpenOptions configures a SQLite connection opened via OpenSQLite.
// Zero values select sensible defaults.
type OpenOptions struct {
	// ReadOnly opens the database with mode=ro. When true, OpenSQLite still
	// applies busy_timeout but skips pragmas that require write access
	// (journal_mode=WAL, synchronous, foreign_keys).
	ReadOnly bool

	// MaxOpenConns caps the SQL connection pool size. 0 selects the default
	// of 1, which matches SQLite's single-writer semantics and prevents the
	// pool from opening extra connections that could corrupt WAL/SHM files
	// under concurrent multi-process access.
	MaxOpenConns int

	// BusyTimeout controls PRAGMA busy_timeout. 0 selects the default of 5s.
	BusyTimeout time.Duration

	// DisableForeignKeys, when true, skips `PRAGMA foreign_keys = ON`. This is
	// a temporary escape hatch for the CLI issues.db, which currently ships
	// with FK enforcement OFF. Flipping FK enforcement on for that DB is the
	// responsibility of td-4846e6 (which also adds the orphan-cleanup
	// migration); this flag keeps td-d4a67c scope-limited to pragma
	// centralization. Remove once td-4846e6 lands.
	DisableForeignKeys bool
}

// OpenSQLite opens a SQLite database at path and applies td's standard pragma
// policy so every caller gets identical behaviour:
//
//	PRAGMA journal_mode = WAL
//	PRAGMA busy_timeout = 5000           (overridable via OpenOptions.BusyTimeout)
//	PRAGMA synchronous  = NORMAL
//	PRAGMA foreign_keys = ON             (unless OpenOptions.DisableForeignKeys)
//
// The pool is pinned with SetMaxOpenConns(1) unless OpenOptions.MaxOpenConns
// is set. ReadOnly connections open with mode=ro and skip write-only pragmas.
func OpenSQLite(path string, opts OpenOptions) (*sql.DB, error) {
	dsn := path
	if opts.ReadOnly {
		dsn = path + "?mode=ro"
	}

	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	maxOpen := opts.MaxOpenConns
	if maxOpen == 0 {
		maxOpen = 1
	}
	conn.SetMaxOpenConns(maxOpen)

	busy := opts.BusyTimeout
	if busy == 0 {
		busy = 5 * time.Second
	}
	busyMs := int(busy / time.Millisecond)
	if _, err := conn.Exec(fmt.Sprintf("PRAGMA busy_timeout=%d", busyMs)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	if opts.ReadOnly {
		// journal_mode/synchronous/foreign_keys are write-side concerns.
		return conn, nil
	}

	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	// synchronous=NORMAL is slightly faster and still safe under WAL.
	if _, err := conn.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set synchronous: %w", err)
	}

	if !opts.DisableForeignKeys {
		if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
			conn.Close()
			return nil, fmt.Errorf("enable foreign keys: %w", err)
		}
	}

	return conn, nil
}
