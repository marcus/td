package monitor

import (
	"context"
	"log/slog"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/td/pkg/tdsync"
)

// SyncOptions configures the monitor-owned background sync ladder. The zero
// value enables sync whenever the project's tdsync gate is open.
type SyncOptions struct {
	Disabled bool
	Interval time.Duration
	Logger   *slog.Logger
	OnStatus func(SyncStatus)
}

// SyncStatus reports the current live-sync rung and latest outcome.
type SyncStatus struct {
	Rung   tdsync.Rung
	Gate   tdsync.Gate
	Result tdsync.Result
	Error  error
	Reason string
}

type monitorSyncResultMsg struct {
	result  tdsync.Result
	err     error
	changed bool
}

type startMonitorSyncMsg struct{}

// syncService is the owned live-sync contract consumed by the monitor. Tests
// replace this seam without reimplementing HTTP or credential policy.
type syncService interface {
	Live(context.Context, func()) error
	RequestSync()
	SetStatusHandler(func(tdsync.Status))
}

type syncRuntime struct {
	service  syncService
	logger   *slog.Logger
	onStatus func(SyncStatus)
	release  func() error
	results  chan monitorSyncResultMsg
	done     chan struct{}

	startOnce sync.Once
	closeOnce sync.Once
	doneOnce  sync.Once
	mu        sync.Mutex
	started   bool
	closed    bool
	wg        sync.WaitGroup
	cancel    context.CancelFunc
	closeErr  error
}

func newSyncRuntime(service syncService, opts SyncOptions, release func() error) *syncRuntime {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &syncRuntime{
		service: service, logger: logger, onStatus: opts.OnStatus, release: release,
		results: make(chan monitorSyncResultMsg, 1), done: make(chan struct{}),
	}
}

func (r *syncRuntime) start() {
	if r == nil || r.service == nil {
		return
	}
	r.startOnce.Do(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.closed {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		r.cancel = cancel
		r.started = true
		r.wg.Add(1)
		go r.run(ctx)
	})
}

func (r *syncRuntime) run(ctx context.Context) {
	defer r.wg.Done()
	defer r.closeDone()
	r.service.SetStatusHandler(r.handleStatus)
	err := r.service.Live(ctx, func() { r.publish(monitorSyncResultMsg{changed: true}) })
	if err != nil && ctx.Err() == nil {
		r.logger.Debug("monitor: live sync", "err", err)
		r.publish(monitorSyncResultMsg{err: err})
	}
}

func (r *syncRuntime) handleStatus(status tdsync.Status) {
	if r.onStatus != nil {
		r.onStatus(SyncStatus{Rung: status.Rung, Gate: status.Gate, Result: status.Result, Error: status.Error, Reason: status.Reason})
	}
	r.publish(monitorSyncResultMsg{result: status.Result, err: status.Error})
}

func (r *syncRuntime) publish(msg monitorSyncResultMsg) {
	select {
	case r.results <- msg:
		return
	default:
	}
	select {
	case old := <-r.results:
		msg.result.Pulled += old.result.Pulled
		msg.changed = msg.changed || old.changed
	default:
	}
	select {
	case r.results <- msg:
	default:
	}
}

func (r *syncRuntime) wakeSync() {
	if r != nil && r.service != nil {
		r.service.RequestSync()
	}
}

func (r *syncRuntime) waitCmd() tea.Cmd {
	if r == nil || r.service == nil {
		return nil
	}
	return func() tea.Msg {
		r.start()
		select {
		case msg := <-r.results:
			return msg
		case <-r.done:
			return nil
		}
	}
}

func (m Model) syncWaitCmd() tea.Cmd {
	if m.syncRuntime == nil {
		return nil
	}
	return m.syncRuntime.waitCmd()
}

func (m Model) wakeSync() {
	if m.AutoSyncFunc == nil && m.syncRuntime != nil {
		m.syncRuntime.wakeSync()
	}
}

func (r *syncRuntime) close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		if r.cancel != nil {
			r.cancel()
		}
		started := r.started
		r.mu.Unlock()
		r.wg.Wait()
		if !started {
			r.closeDone()
		}
		if r.release != nil {
			r.closeErr = r.release()
		}
	})
	return r.closeErr
}

func (r *syncRuntime) closeDone() { r.doneOnce.Do(func() { close(r.done) }) }
