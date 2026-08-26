package tdsync

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/internal/syncconfig"
)

// Rung identifies the active live-sync transport or terminal gate state.
type Rung string

const (
	RungLive    Rung = "live"
	RungProbing Rung = "probing"
	RungTimed   Rung = "timed"
	RungOff     Rung = "off"
	RungExpired Rung = "expired"
)

// Status is one observable live-sync outcome. Result is populated for a
// completed push/pull pass. Error describes the transport failure that caused
// a degradation; it is not sticky after a later successful outcome.
type Status struct {
	Rung   Rung
	Gate   Gate
	Result Result
	Error  error
	Reason string
}

type liveConnection struct {
	database *db.DB
	closeDB  bool
	project  string
	identity requestIdentity
	client   *syncclient.Client
}

var errCredentialGenerationChanged = errors.New("credential generation changed during request")

type fallbackMode int

const (
	fallbackReconnect fallbackMode = iota
	fallbackTimed
	fallbackExpired
)

// RequestSync asks a running Live ladder to perform a prompt pass. Requests
// coalesce while a pass is already pending, which is appropriate for monitor
// mutations: the database is the queue and Once drains it.
func (s *Syncer) RequestSync() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Live owns steady-state cadence for one project. It performs an initial
// push/pull, prefers SSE refresh hints, degrades through cheap status probes to
// timed full sync, and stops all network traffic after an exact 401 until the
// server/key/project fingerprint changes. onChange means pulled data changed
// the local database; consumers should refresh their projection.
func (s *Syncer) Live(ctx context.Context, onChange func()) error {
	if ctx == nil {
		ctx = context.Background()
	}
	lastEventID := ""
	for {
		gate := s.Gate()
		if !gate.Open {
			// A closed gate is a state, not an end state. Authentication, project
			// linking, and the autosync flags all change while a monitor is open —
			// td's own in-TUI link prompt is one such path — so wait for the gate
			// rather than retiring the ladder until the process restarts.
			rung := RungOff
			if gate.Reason == "credential expired" {
				rung = RungExpired
			}
			s.emit(Status{Rung: rung, Gate: gate, Reason: gate.Reason})
			if !s.waitForGateOpen(ctx) {
				return ctx.Err()
			}
			continue
		}

		if err := s.runConnectedLadder(ctx, &lastEventID, onChange); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return ctx.Err()
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func (s *Syncer) runConnectedLadder(ctx context.Context, lastEventID *string, onChange func()) error {
	var probeHead int64 = -1
	backoff := time.Duration(0)
	failures := 0
	for {
		if backoff > 0 {
			mode, err := s.waitWithProbes(ctx, backoff, &probeHead, onChange)
			if err != nil {
				return err
			}
			switch mode {
			case fallbackTimed:
				return s.runTimed(ctx, onChange)
			case fallbackExpired:
				return nil
			}
		}

		// A full pass before every (re)connect closes the missed-event window.
		if _, err := s.syncPass(ctx, RungProbing, onChange); err != nil {
			if errors.Is(err, syncclient.ErrUnauthorized) {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
		connectedAt := time.Now()
		err := s.consumeStream(ctx, lastEventID, onChange)
		if time.Since(connectedAt) >= 4*s.reconnectBase {
			failures = 0
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, syncclient.ErrUnauthorized) {
			if s.Gate().Reason != "credential expired" {
				s.emit(Status{Rung: RungProbing, Gate: s.Gate(), Error: errCredentialGenerationChanged, Reason: "credentials changed during event request"})
				continue
			}
			s.emitExpired()
			return nil
		}
		if syncclient.IsHTTPStatus(err, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented) {
			s.emit(Status{Rung: RungTimed, Gate: s.Gate(), Error: err, Reason: "live endpoints unavailable"})
			return s.runTimed(ctx, onChange)
		}
		s.emit(Status{Rung: RungProbing, Gate: s.Gate(), Error: err, Reason: "event stream unavailable"})

		mode, probeErr := s.probe(ctx, &probeHead, onChange)
		if probeErr != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		switch mode {
		case fallbackTimed:
			return s.runTimed(ctx, onChange)
		case fallbackExpired:
			return nil
		}
		// First retry is immediate; subsequent failures use jittered exponential
		// backoff capped at two minutes.
		if failures == 0 {
			backoff = 0
		} else {
			backoff = s.reconnectBase << (failures - 1)
			if backoff > s.reconnectCap || backoff <= 0 {
				backoff = s.reconnectCap
			}
			if s.jitter != nil {
				backoff = s.jitter(backoff)
			}
		}
		failures++
	}
}

func (s *Syncer) consumeStream(ctx context.Context, lastEventID *string, onChange func()) error {
	conn, err := s.connection(ctx)
	if err != nil {
		return err
	}
	if conn.closeDB {
		defer conn.database.Close()
	}
	events := make(chan syncclient.ProjectEvent, 1)
	opened := make(chan struct{}, 1)
	streamErr := make(chan error, 1)
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		streamErr <- conn.client.Events(streamCtx, conn.project, *lastEventID, s.streamIdle,
			func() {
				select {
				case opened <- struct{}{}:
				default:
				}
			},
			func(event syncclient.ProjectEvent) {
				select {
				case events <- event:
				default:
				}
			})
	}()

	var timer *time.Timer
	var window <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-opened:
			s.emit(Status{Rung: RungLive, Gate: s.Gate(), Reason: "event stream connected"})
		case event := <-events:
			if event.ID != "" {
				*lastEventID = event.ID
			}
			if timer == nil {
				timer = time.NewTimer(s.coalesceWindow)
				window = timer.C
			}
		case <-window:
			timer = nil
			window = nil
			if _, err := s.syncPass(ctx, RungLive, onChange); err != nil {
				return err
			}
		case <-s.wake:
			if _, err := s.syncPass(ctx, RungLive, onChange); err != nil {
				return err
			}
		case err := <-streamErr:
			select {
			case event := <-events:
				if event.ID != "" {
					*lastEventID = event.ID
				}
			default:
			}
			if timer != nil {
				timer.Stop()
			}
			if errors.Is(err, syncclient.ErrUnauthorized) {
				if !s.latchExpired(conn.identity) {
					return errCredentialGenerationChanged
				}
			}
			return err
		}
	}
}

func (s *Syncer) runTimed(ctx context.Context, onChange func()) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		retryLive := false
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			retryLive = true
		case <-s.wake:
		}
		if _, err := s.syncPass(ctx, RungTimed, onChange); err != nil {
			if errors.Is(err, syncclient.ErrUnauthorized) {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
		// The timed rung is a fallback, not a permanent downgrade. Its existing
		// cadence is also the retry cadence for newer server capabilities.
		if retryLive {
			return nil
		}
	}
}

func (s *Syncer) waitWithProbes(ctx context.Context, delay time.Duration, baseline *int64, onChange func()) (fallbackMode, error) {
	reconnect := time.NewTimer(delay)
	defer reconnect.Stop()
	probe := time.NewTicker(s.interval)
	defer probe.Stop()
	for {
		select {
		case <-ctx.Done():
			return fallbackReconnect, ctx.Err()
		case <-reconnect.C:
			return fallbackReconnect, nil
		case <-s.wake:
			if _, err := s.syncPass(ctx, RungProbing, onChange); errors.Is(err, syncclient.ErrUnauthorized) {
				return fallbackExpired, nil
			}
		case <-probe.C:
			mode, err := s.probe(ctx, baseline, onChange)
			if mode != fallbackReconnect || err != nil {
				return mode, err
			}
		}
	}
}

func (s *Syncer) probe(ctx context.Context, baseline *int64, onChange func()) (fallbackMode, error) {
	conn, err := s.connection(ctx)
	if err != nil {
		return fallbackReconnect, err
	}
	if conn.closeDB {
		defer conn.database.Close()
	}
	status, err := conn.client.SyncStatusContext(ctx, conn.project)
	if err != nil {
		if errors.Is(err, syncclient.ErrUnauthorized) {
			if !s.latchExpired(conn.identity) {
				s.emit(Status{Rung: RungProbing, Gate: s.Gate(), Error: errCredentialGenerationChanged, Reason: "credentials changed during status probe"})
				return fallbackReconnect, nil
			}
			s.emitExpired()
			return fallbackExpired, nil
		}
		if syncclient.IsHTTPStatus(err, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented) {
			s.emit(Status{Rung: RungTimed, Gate: s.Gate(), Error: err, Reason: "status probe unavailable"})
			return fallbackTimed, nil
		}
		s.emit(Status{Rung: RungProbing, Gate: s.Gate(), Error: err, Reason: "status probe failed"})
		return fallbackReconnect, nil
	}
	if *baseline < 0 {
		state, stateErr := conn.database.GetSyncState()
		if stateErr != nil {
			return fallbackReconnect, stateErr
		}
		*baseline = 0
		if state != nil {
			*baseline = state.LastPulledServerSeq
		}
	}
	if status.LastServerSeq > *baseline {
		if _, err := s.syncPass(ctx, RungProbing, onChange); err != nil {
			if errors.Is(err, syncclient.ErrUnauthorized) {
				return fallbackExpired, nil
			}
			return fallbackReconnect, nil
		}
		// The status head includes this device's own events, which Pull excludes.
		// Advancing to the observed head prevents an own push from causing an
		// endless probe/full-sync loop.
		*baseline = status.LastServerSeq
	}
	s.emit(Status{Rung: RungProbing, Gate: s.Gate(), Reason: "status probe connected"})
	return fallbackReconnect, nil
}

func (s *Syncer) syncPass(ctx context.Context, rung Rung, onChange func()) (Result, error) {
	result, err := s.Once(ctx)
	gate := s.Gate()
	if gate.Reason == "credential expired" {
		s.emit(Status{Rung: RungExpired, Gate: gate, Error: err, Reason: "credential expired"})
		return result, syncclient.ErrUnauthorized
	}
	if errors.Is(err, syncclient.ErrUnauthorized) {
		err = errCredentialGenerationChanged
	}
	s.emit(Status{Rung: rung, Gate: gate, Result: result, Error: err})
	if err == nil && result.Pulled > 0 && onChange != nil {
		onChange()
	}
	return result, err
}

func (s *Syncer) connection(ctx context.Context) (*liveConnection, error) {
	database, closeDB, err := s.database()
	if err != nil {
		return nil, err
	}
	if database == nil {
		return nil, errors.New("sync database unavailable")
	}
	state, err := database.GetSyncState()
	if err != nil || state == nil || state.ProjectID == "" {
		if closeDB {
			database.Close()
		}
		return nil, fmt.Errorf("get sync state: %w", err)
	}
	deviceID, err := syncconfig.GetDeviceID()
	if err != nil {
		if closeDB {
			database.Close()
		}
		return nil, err
	}
	serverURL, apiKey := syncconfig.GetServerURL(), syncconfig.GetAPIKey()
	identity := newRequestIdentity(serverURL, apiKey, state.ProjectID)
	client := syncclient.New(serverURL, apiKey, deviceID)
	client.HTTP.Timeout = httpTimeout
	return &liveConnection{database: database, closeDB: closeDB, project: state.ProjectID, identity: identity, client: client}, nil
}

func (s *Syncer) projectID() string {
	database, closeDB, err := s.database()
	if err != nil || database == nil {
		return ""
	}
	if closeDB {
		defer database.Close()
	}
	state, _ := database.GetSyncState()
	if state == nil {
		return ""
	}
	return state.ProjectID
}

func (s *Syncer) emit(status Status) {
	if status.Rung == RungExpired {
		project := s.projectID()
		fingerprint := s.fingerprint(project)
		s.stateMu.Lock()
		if s.expiredEmitted == fingerprint {
			s.stateMu.Unlock()
			return
		}
		s.expiredEmitted = fingerprint
		s.stateMu.Unlock()
	}
	s.onStatusMu.RLock()
	handler := s.onStatus
	s.onStatusMu.RUnlock()
	if handler != nil {
		handler(status)
	}
}

func (s *Syncer) emitExpired() {
	gate := s.Gate()
	s.emit(Status{Rung: RungExpired, Gate: gate, Reason: "credential expired"})
}

// waitForGateOpen blocks until this project may sync again, or until ctx is
// cancelled. It makes no network requests: the gate is local state (credentials,
// link status, autosync flags), so a wake request or a slow re-check is enough.
//
// Most projects are never linked, so this loop is the steady state for the
// majority of monitors. It costs one gate evaluation per gatePoll tick — a
// single indexed sync_state read plus config resolution — and RequestSync gives
// the paths that change the gate from inside td an immediate answer instead of
// making them wait for the tick.
func (s *Syncer) waitForGateOpen(ctx context.Context) bool {
	ticker := time.NewTicker(s.gatePoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-s.wake:
		case <-ticker.C:
		}
		if s.Gate().Open {
			return true
		}
	}
}

func defaultJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	// Uniform 75%-125% jitter avoids synchronized reconnect fleets while
	// retaining a predictable exponential envelope.
	var b [8]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return d
	}
	fraction := float64(binary.LittleEndian.Uint64(b[:])) / float64(^uint64(0))
	return time.Duration(float64(d) * (0.75 + 0.5*fraction))
}
