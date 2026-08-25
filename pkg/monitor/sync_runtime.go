package monitor

import (
	"context"
	"log/slog"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/td/pkg/tdsync"
)

// SyncOptions configures the monitor-owned background sync loop. The zero
// value enables sync whenever the project's tdsync gate is open.
type SyncOptions struct {
	Disabled bool
	Interval time.Duration
	Logger   *slog.Logger
	OnStatus func(SyncStatus)
}

// SyncStatus reports the latest background sync outcome to an embedder.
type SyncStatus struct {
	Gate   tdsync.Gate
	Result tdsync.Result
	Error  error
}

type monitorSyncResultMsg struct {
	result tdsync.Result
	err    error
}

type startMonitorSyncMsg struct{}

// syncRuntime is pointer-owned because Bubble Tea copies Model values. It
// owns the goroutine, cancellation, result channel, and database release so
// Close remains safe and idempotent no matter which model copy receives it.
type syncRuntime struct {
	gate     func() tdsync.Gate
	once     func(context.Context) (tdsync.Result, error)
	interval time.Duration
	logger   *slog.Logger
	onStatus func(SyncStatus)
	release  func() error

	wake    chan struct{}
	results chan monitorSyncResultMsg
	done    chan struct{}

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

func newSyncRuntime(syncer *tdsync.Syncer, opts SyncOptions, release func() error) *syncRuntime {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	r := &syncRuntime{
		interval: opts.Interval, logger: logger,
		onStatus: opts.OnStatus, release: release,
		wake: make(chan struct{}, 1), results: make(chan monitorSyncResultMsg, 1),
		done: make(chan struct{}),
	}
	if syncer != nil {
		r.gate = syncer.Gate
		r.once = syncer.Once
	}
	return r
}

func (r *syncRuntime) start() {
	if r == nil || r.once == nil || r.gate == nil {
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

	// The first round trip happens only after Init has returned and Bubble Tea
	// executes the asynchronous wait command.
	r.runOnce(ctx)

	var ticker *time.Ticker
	var tick <-chan time.Time
	if r.interval > 0 {
		ticker = time.NewTicker(r.interval)
		tick = ticker.C
		defer ticker.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
			r.runOnce(ctx)
		case <-tick:
			r.runOnce(ctx)
		}
	}
}

func (r *syncRuntime) runOnce(ctx context.Context) {
	gate := r.gate()
	var result tdsync.Result
	var err error
	if gate.Open {
		result, err = r.once(ctx)
		if err != nil && ctx.Err() == nil {
			r.logger.Debug("monitor: autosync", "err", err)
		}
	}
	if r.onStatus != nil {
		r.onStatus(SyncStatus{Gate: gate, Result: result, Error: err})
	}
	r.publish(monitorSyncResultMsg{result: result, err: err})
}

func (r *syncRuntime) publish(msg monitorSyncResultMsg) {
	select {
	case r.results <- msg:
		return
	default:
	}
	// Preserve evidence that a pull occurred if Bubble Tea has not consumed the
	// prior result yet. There is only one producer (the runtime goroutine).
	select {
	case old := <-r.results:
		msg.result.Pulled += old.result.Pulled
	default:
	}
	select {
	case r.results <- msg:
	default:
	}
}

func (r *syncRuntime) wakeSync() {
	if r == nil {
		return
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *syncRuntime) waitCmd() tea.Cmd {
	if r == nil || r.once == nil || r.gate == nil {
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

func (r *syncRuntime) closeDone() {
	r.doneOnce.Do(func() { close(r.done) })
}
