package db

import (
	"database/sql"
	"fmt"
	"strings"
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
//	PRAGMA journal_mode = TRUNCATE
//	PRAGMA busy_timeout = 5000           (overridable via OpenOptions.BusyTimeout)
//	PRAGMA synchronous  = NORMAL
//	PRAGMA foreign_keys = ON             (unless OpenOptions.DisableForeignKeys)
//
// journal_mode is TRUNCATE, not WAL, on purpose. modernc's WAL shared-memory
// coordination repeatedly corrupted issues.db ("database disk image is
// malformed") when a long-lived embedded connection (Sidecar's td monitor)
// ran concurrently with bursts of short-lived CLI processes — see td-adbf16.
// The rollback-journal path uses plain POSIX file locks, has no -shm/-wal
// state to race on, and td's write volumes make the concurrency cost of
// writers briefly blocking readers irrelevant. Do not switch back to WAL
// without a multi-process soak test covering that embedded-monitor pattern.
//
// The pool is pinned with SetMaxOpenConns(1) unless OpenOptions.MaxOpenConns
// is set. ReadOnly connections open with mode=ro and skip write-only pragmas.
func OpenSQLite(path string, opts OpenOptions) (*sql.DB, error) {
	// _time_format=sqlite makes modernc write time.Time values using the
	// canonical "2006-01-02 15:04:05.999999999-07:00" layout, which its
	// read-side parser round-trips reliably. Without it, modernc's default
	// writer uses time.Time.String() — emitting a monotonic-clock suffix
	// ("m=+...") and a zone *name* ("PDT") that cannot be parsed back into a
	// time.Time, corrupting every timestamp column. See internal/db/timeutil.go.
	params := []string{"_time_format=sqlite"}
	if opts.ReadOnly {
		params = append(params, "mode=ro")
	}
	// Append params with the correct separator in case the caller already
	// passed a URI-style path containing a query string (e.g. ":memory:?cache=shared").
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	dsn := path + sep + strings.Join(params, "&")

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

	// Switching an existing WAL database to TRUNCATE checkpoints and removes
	// any -wal/-shm files; on an already-TRUNCATE database this is a no-op.
	if _, err := conn.Exec("PRAGMA journal_mode=TRUNCATE"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set journal mode: %w", err)
	}

	// In rollback-journal mode synchronous=NORMAL risks losing the last
	// transaction on power failure, never database corruption.
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
