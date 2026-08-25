package tdsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcus/td/internal/syncclient"
)

func liveSyncer(t *testing.T, handler http.Handler, statuses *[]Status, statusMu *sync.Mutex) (*Syncer, func()) {
	t.Helper()
	clearGateEnv(t)
	t.Setenv("TD_SYNC_AUTO_PULL", "true")
	baseDir, database := gateDB(t, "proj", false)
	server := httptest.NewServer(handler)
	t.Setenv("TD_SYNC_URL", server.URL)
	syncer, err := New(Options{
		BaseDir: baseDir, DB: database, Interval: 20 * time.Millisecond,
		OnStatus: func(status Status) {
			statusMu.Lock()
			*statuses = append(*statuses, status)
			statusMu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("new syncer: %v", err)
	}
	syncer.coalesceWindow = 30 * time.Millisecond
	syncer.streamIdle = 100 * time.Millisecond
	syncer.reconnectBase = 10 * time.Millisecond
	syncer.reconnectCap = 20 * time.Millisecond
	syncer.jitter = func(d time.Duration) time.Duration { return d }
	return syncer, func() { server.Close(); database.Close() }
}

func emptyPull(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(syncclient.PullResponse{})
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestLiveCoalescesEventBurstInFixedWindow(t *testing.T) {
	var pulls atomic.Int32
	streamReady := make(chan struct{})
	var readyOnce sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects/proj/sync/pull", func(w http.ResponseWriter, _ *http.Request) { pulls.Add(1); emptyPull(w) })
	mux.HandleFunc("/v1/projects/proj/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		readyOnce.Do(func() { close(streamReady) })
		for i := range 12 {
			_, _ = fmt.Fprintf(w, "event: refresh\nid: %d\ndata: {}\n\n", i+1)
		}
		flusher.Flush()
		<-r.Context().Done()
	})
	var statuses []Status
	var statusMu sync.Mutex
	syncer, cleanup := liveSyncer(t, mux, &statuses, &statusMu)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- syncer.Live(ctx, nil) }()
	<-streamReady
	waitFor(t, "coalesced pull", func() bool { return pulls.Load() >= 2 })
	time.Sleep(70 * time.Millisecond)
	if got := pulls.Load(); got != 2 {
		t.Fatalf("pull requests = %d, want initial plus one coalesced event pass", got)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Live cancellation = %v", err)
	}
}

func TestLiveReconnectUsesLastEventIDAsRefreshHintAndRunsGapPass(t *testing.T) {
	var pulls, streams atomic.Int32
	secondHeader := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects/proj/sync/pull", func(w http.ResponseWriter, _ *http.Request) { pulls.Add(1); emptyPull(w) })
	mux.HandleFunc("/v1/projects/proj/events", func(w http.ResponseWriter, r *http.Request) {
		attempt := streams.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		if attempt == 1 {
			_, _ = fmt.Fprint(w, "event: refresh\nid: token-7\ndata: {}\n\n")
			flusher.Flush()
			return
		}
		secondHeader <- r.Header.Get("Last-Event-ID")
		flusher.Flush()
		<-r.Context().Done()
	})
	var statuses []Status
	var statusMu sync.Mutex
	syncer, cleanup := liveSyncer(t, mux, &statuses, &statusMu)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- syncer.Live(ctx, nil) }()
	select {
	case got := <-secondHeader:
		if got != "token-7" {
			t.Fatalf("Last-Event-ID = %q, want refresh hint token-7", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second stream did not connect")
	}
	if got := pulls.Load(); got < 2 {
		t.Fatalf("gap-closing pulls = %d, want at least one before each connection", got)
	}
	cancel()
	<-done
}

func TestLiveBufferingStreamFallsBackToStatusProbe(t *testing.T) {
	var probes atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects/proj/sync/pull", func(w http.ResponseWriter, _ *http.Request) { emptyPull(w) })
	mux.HandleFunc("/v1/projects/proj/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("/v1/projects/proj/sync/status", func(w http.ResponseWriter, _ *http.Request) {
		probes.Add(1)
		_ = json.NewEncoder(w).Encode(syncclient.SyncStatusResponse{LastServerSeq: 4})
	})
	var statuses []Status
	var statusMu sync.Mutex
	syncer, cleanup := liveSyncer(t, mux, &statuses, &statusMu)
	defer cleanup()
	syncer.streamIdle = 25 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- syncer.Live(ctx, nil) }()
	waitFor(t, "probing fallback", func() bool { return probes.Load() > 0 })
	statusMu.Lock()
	found := false
	for _, status := range statuses {
		if status.Rung == RungProbing && errors.Is(status.Error, syncclient.ErrStreamStalled) {
			found = true
		}
	}
	statusMu.Unlock()
	if !found {
		t.Fatal("status stream did not report stalled live connection on probing rung")
	}
	cancel()
	<-done
}

func TestProbeDoesNotConsumeAdvancedHeadWhenRecoveryPassFails(t *testing.T) {
	var pulls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects/proj/sync/status", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(syncclient.SyncStatusResponse{LastServerSeq: 9})
	})
	mux.HandleFunc("/v1/projects/proj/sync/pull", func(w http.ResponseWriter, _ *http.Request) {
		if pulls.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		emptyPull(w)
	})
	var statuses []Status
	var statusMu sync.Mutex
	syncer, cleanup := liveSyncer(t, mux, &statuses, &statusMu)
	defer cleanup()
	baseline := int64(-1)
	mode, err := syncer.probe(context.Background(), &baseline, nil)
	if err != nil || mode != fallbackReconnect {
		t.Fatalf("first probe = (%v, %v)", mode, err)
	}
	if baseline != 0 {
		t.Fatalf("baseline after failed recovery = %d, want local head 0", baseline)
	}
	mode, err = syncer.probe(context.Background(), &baseline, nil)
	if err != nil || mode != fallbackReconnect {
		t.Fatalf("second probe = (%v, %v)", mode, err)
	}
	if baseline != 9 {
		t.Fatalf("baseline after successful recovery = %d, want 9", baseline)
	}
	if got := pulls.Load(); got != 2 {
		t.Fatalf("recovery pulls = %d, want failed and successful attempts", got)
	}
}

func TestLiveUnsupportedEndpointsUseTimedFallback(t *testing.T) {
	var pulls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects/proj/sync/pull", func(w http.ResponseWriter, _ *http.Request) { pulls.Add(1); emptyPull(w) })
	mux.HandleFunc("/v1/projects/proj/events", func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
	var statuses []Status
	var statusMu sync.Mutex
	syncer, cleanup := liveSyncer(t, mux, &statuses, &statusMu)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- syncer.Live(ctx, nil) }()
	waitFor(t, "timed second pass", func() bool { return pulls.Load() >= 2 })
	statusMu.Lock()
	found := false
	for _, status := range statuses {
		if status.Rung == RungTimed {
			found = true
		}
	}
	statusMu.Unlock()
	if !found {
		t.Fatal("timed rung was not reported")
	}
	cancel()
	<-done
}

func TestLiveUnauthorizedLatchesExactlyOnceAndStopsRequests(t *testing.T) {
	var requests atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects/proj/sync/pull", func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "expired", http.StatusUnauthorized)
	})
	var statuses []Status
	var statusMu sync.Mutex
	syncer, cleanup := liveSyncer(t, mux, &statuses, &statusMu)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- syncer.Live(ctx, nil) }()
	waitFor(t, "expired status", func() bool {
		statusMu.Lock()
		defer statusMu.Unlock()
		for _, status := range statuses {
			if status.Rung == RungExpired {
				return true
			}
		}
		return false
	})
	time.Sleep(80 * time.Millisecond)
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests after 401 = %d, want exactly 1", got)
	}
	statusMu.Lock()
	expired := 0
	for _, status := range statuses {
		if status.Rung == RungExpired {
			expired++
		}
	}
	statusMu.Unlock()
	if expired != 1 {
		t.Fatalf("expired status emissions = %d, want exactly 1", expired)
	}
	if _, err := syncer.Once(context.Background()); err != nil {
		t.Fatalf("latched Once should skip without error: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("latched Once issued request; total = %d", got)
	}
	cancel()
	<-done
}
