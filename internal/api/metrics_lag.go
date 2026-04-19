// Package api: periodic apply-cursor lag sampler.
//
// Plan §9.3 (b): expose, per project currently held open in the live pool,
// the gap between events.db head (MAX(server_seq) over events) and project.db
// applied_events cursor (MAX(server_seq) over applied_events). Lag is the
// canonical drift signal for the post-commit promotion + push-apply paths
// (Streams 3.1 / 3.2). Steady-state value is 0; a sustained > 0 lag means
// project.db is falling behind events.db.
//
// This file deliberately avoids introducing a metrics dependency. Instead it
// emits one structured slog line per project per tick, which downstream
// scraping (Loki / Vector / etc.) can convert into a gauge. The plan calls
// out an alert at lag > 100 for > 30s; that alerting lives outside this
// process, this file just emits the raw signal.
package api

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"time"

	tddb "github.com/marcus/td/internal/db"
)

// lagSampleInterval is how often the sampler iterates the live-pool. 30s is
// the cadence called out in plan §9.3 — fast enough to catch a regression
// before the alert window expires, slow enough that the per-project SELECTs
// don't show up as a noticeable load even with hundreds of projects open.
const lagSampleInterval = 30 * time.Second

// lagQueryTimeout caps how long a single project's read-only MAX(server_seq)
// queries may take. The sampler is best-effort by design: a project whose
// events.db is locked by a long-running transaction must NOT block the
// sampler from moving on to the next project. 2s is comfortably above the
// SQLite busy_timeout (5s default for tddb.OpenSQLite) only as an upper
// bound; we want the sampler to give up well before busy_timeout would, so
// a wedged project surfaces as a missing sample rather than a stuck loop.
const lagQueryTimeout = 2 * time.Second

// startLagSampler launches a background goroutine that, on every
// lagSampleInterval tick, snapshots the live project pool and emits one
// "project_apply_lag" log line per project containing events_head, applied,
// and lag. The goroutine exits when ctx is cancelled.
//
// Wired from Server.Start so shutdown's CancelFunc tears it down with the
// other periodic tasks. Tests may call this directly with a controlled ctx.
func (s *Server) startLagSampler(ctx context.Context) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("lag sampler panic", "panic", r)
			}
		}()
		ticker := time.NewTicker(lagSampleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.sampleLagOnce(ctx)
			}
		}
	}()
}

// sampleLagOnce runs one pass over the live pool. Exposed (lowercase, but
// reachable from tests in-package) so tests can drive a single iteration
// without spinning the ticker.
func (s *Server) sampleLagOnce(ctx context.Context) {
	if s.projectLivePool == nil {
		return
	}
	for _, projectID := range s.projectLivePool.Snapshot() {
		// Re-check cancellation between projects so a shutdown doesn't have
		// to wait on every project in a large pool.
		if ctx.Err() != nil {
			return
		}
		eventsHead, applied, err := s.readProjectLag(ctx, projectID)
		if err != nil {
			// errMissing is benign (brand-new project, not yet sync-pushed).
			// Anything else is operationally interesting — log and continue.
			if errors.Is(err, errLagMissingDB) {
				continue
			}
			slog.Warn("project_apply_lag_error",
				"project", projectID,
				"err", err,
			)
			continue
		}
		lag := eventsHead - applied
		slog.Info("project_apply_lag",
			"project", projectID,
			"lag", lag,
			"events_head", eventsHead,
			"applied", applied,
		)
	}
}

// errLagMissingDB is returned by readProjectLag when one of the two on-disk
// databases doesn't exist yet. Callers treat it as "nothing to report" rather
// than an error worth logging.
var errLagMissingDB = errors.New("lag: project db file missing")

// readProjectLag opens events.db and project.db read-only for the given
// project, queries the head of each, and returns (eventsHead, applied).
// Each query runs under a per-project lagQueryTimeout so a wedged DB cannot
// stall the whole sampler.
func (s *Server) readProjectLag(parentCtx context.Context, projectID string) (int64, int64, error) {
	eventsPath := s.eventsDBPathFor(projectID)
	projectPath := s.projectLivePool.projectDBPath(projectID)

	if _, err := os.Stat(eventsPath); err != nil {
		if os.IsNotExist(err) {
			return 0, 0, errLagMissingDB
		}
		return 0, 0, err
	}
	if _, err := os.Stat(projectPath); err != nil {
		if os.IsNotExist(err) {
			return 0, 0, errLagMissingDB
		}
		return 0, 0, err
	}

	eventsHead, err := readMaxSeq(parentCtx, eventsPath, `SELECT COALESCE(MAX(server_seq), 0) FROM events`)
	if err != nil {
		return 0, 0, err
	}
	applied, err := readMaxSeq(parentCtx, projectPath, `SELECT COALESCE(MAX(server_seq), 0) FROM applied_events`)
	if err != nil {
		return 0, 0, err
	}
	return eventsHead, applied, nil
}

// readMaxSeq opens path read-only, executes the single-row query under a
// short context timeout, and returns the int64 result. The connection is
// closed before return.
func readMaxSeq(parentCtx context.Context, path, query string) (int64, error) {
	conn, err := tddb.OpenSQLite(path, tddb.OpenOptions{ReadOnly: true})
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(parentCtx, lagQueryTimeout)
	defer cancel()

	var n sql.NullInt64
	if err := conn.QueryRowContext(ctx, query).Scan(&n); err != nil {
		return 0, err
	}
	if !n.Valid {
		return 0, nil
	}
	return n.Int64, nil
}
