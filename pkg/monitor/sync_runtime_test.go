package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/pkg/tdsync"
)

func testSyncRuntime(
	gate func() tdsync.Gate,
	once func(context.Context) (tdsync.Result, error),
	release func() error,
) *syncRuntime {
	return newSyncRuntime(&testSyncService{gate: gate, once: once, wake: make(chan struct{}, 1)}, SyncOptions{}, release)
}

type testSyncService struct {
	gate   func() tdsync.Gate
	once   func(context.Context) (tdsync.Result, error)
	wake   chan struct{}
	status func(tdsync.Status)
}

func (s *testSyncService) Gate() tdsync.Gate                               { return s.gate() }
func (s *testSyncService) Once(ctx context.Context) (tdsync.Result, error) { return s.once(ctx) }
func (s *testSyncService) SetStatusHandler(handler func(tdsync.Status))    { s.status = handler }
func (s *testSyncService) RequestSync() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *testSyncService) Live(ctx context.Context, onChange func()) error {
	for {
		gate := s.gate()
		if !gate.Open {
			if s.status != nil {
				s.status(tdsync.Status{Rung: tdsync.RungOff, Gate: gate})
			}
			return nil
		}
		result, err := s.once(ctx)
		if s.status != nil {
			s.status(tdsync.Status{Rung: tdsync.RungTimed, Gate: gate, Result: result, Error: err})
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.wake:
		}
	}
}

func TestSyncRuntimeClosedGateMakesNoSyncCall(t *testing.T) {
	var calls atomic.Int32
	r := testSyncRuntime(
		func() tdsync.Gate { return tdsync.Gate{Reason: "not linked"} },
		func(context.Context) (tdsync.Result, error) {
			calls.Add(1)
			return tdsync.Result{}, nil
		}, nil,
	)

	msg := r.waitCmd()()
	if _, ok := msg.(monitorSyncResultMsg); !ok {
		t.Fatalf("wait command returned %T, want monitorSyncResultMsg", msg)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("closed gate made %d sync calls, want 0", got)
	}
	if err := r.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestSyncRuntimeProjectsLadderRungToEmbedder(t *testing.T) {
	statusCh := make(chan SyncStatus, 1)
	service := &testSyncService{
		gate: func() tdsync.Gate { return tdsync.Gate{Open: true} },
		once: func(context.Context) (tdsync.Result, error) { return tdsync.Result{}, nil },
		wake: make(chan struct{}, 1),
	}
	r := newSyncRuntime(service, SyncOptions{OnStatus: func(status SyncStatus) { statusCh <- status }}, nil)
	r.start()
	select {
	case status := <-statusCh:
		if status.Rung != tdsync.RungTimed {
			t.Fatalf("projected rung = %q, want timed", status.Rung)
		}
	case <-time.After(time.Second):
		t.Fatal("embedder did not receive ladder status")
	}
	if err := r.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestSyncRuntimeWakeCoalescesAndRetainsFollowUpPass(t *testing.T) {
	var calls atomic.Int32
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	r := testSyncRuntime(
		func() tdsync.Gate { return tdsync.Gate{Open: true} },
		func(ctx context.Context) (tdsync.Result, error) {
			if calls.Add(1) == 1 {
				close(firstStarted)
				select {
				case <-releaseFirst:
				case <-ctx.Done():
					return tdsync.Result{}, ctx.Err()
				}
			}
			return tdsync.Result{}, nil
		}, nil,
	)
	r.start()
	<-firstStarted
	for range 20 {
		r.wakeSync()
	}
	close(releaseFirst)

	deadline := time.Now().Add(time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("sync calls = %d, want initial pass plus one coalesced follow-up", got)
	}
	if err := r.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestSyncRuntimeCloseCancelsWaitsThenReleasesOnceAcrossModelCopies(t *testing.T) {
	started := make(chan struct{})
	passExited := make(chan struct{})
	var releases atomic.Int32
	r := testSyncRuntime(
		func() tdsync.Gate { return tdsync.Gate{Open: true} },
		func(ctx context.Context) (tdsync.Result, error) {
			close(started)
			<-ctx.Done()
			close(passExited)
			return tdsync.Result{}, ctx.Err()
		},
		func() error {
			select {
			case <-passExited:
			default:
				t.Fatal("database released before in-flight sync exited")
			}
			releases.Add(1)
			return nil
		},
	)
	m := Model{syncRuntime: r}
	copyOfModel := m
	r.start()
	<-started

	if err := m.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := copyOfModel.Close(); err != nil {
		t.Fatalf("copy close: %v", err)
	}
	if got := releases.Load(); got != 1 {
		t.Fatalf("release count = %d, want 1", got)
	}
}

func TestAutoSyncFuncSuppressesBuiltInInitCommand(t *testing.T) {
	var calls atomic.Int32
	r := testSyncRuntime(
		func() tdsync.Gate { return tdsync.Gate{Open: true} },
		func(context.Context) (tdsync.Result, error) {
			calls.Add(1)
			return tdsync.Result{}, nil
		}, nil,
	)
	withoutOverride := Model{syncRuntime: r}
	withOverride := withoutOverride
	withOverride.AutoSyncFunc = func() {}

	withoutBatch, ok := withoutOverride.Init()().(tea.BatchMsg)
	if !ok {
		t.Fatal("Init without override did not return tea.BatchMsg")
	}
	withBatch, ok := withOverride.Init()().(tea.BatchMsg)
	if !ok {
		t.Fatal("Init with override did not return tea.BatchMsg")
	}
	if len(withoutBatch) != len(withBatch)+1 {
		t.Fatalf("built-in command counts: default=%d override=%d, want override to suppress one", len(withoutBatch), len(withBatch))
	}
	startMsg := withoutBatch[len(withoutBatch)-1]()
	if _, ok := startMsg.(startMonitorSyncMsg); !ok {
		t.Fatalf("last Init command returned %T, want deferred start message", startMsg)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("Init performed %d network passes before first-frame message", got)
	}
	_, wait := withoutOverride.Update(startMsg)
	if _, ok := wait().(monitorSyncResultMsg); !ok {
		t.Fatal("deferred start did not yield a sync result")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("deferred start passes = %d, want 1", got)
	}
	_ = r.close()
}

func TestPulledSyncResultSchedulesImmediateRefreshAndNextWait(t *testing.T) {
	r := testSyncRuntime(
		func() tdsync.Gate { return tdsync.Gate{Open: true} },
		func(context.Context) (tdsync.Result, error) { return tdsync.Result{}, nil }, nil,
	)
	m := Model{syncRuntime: r}
	_, cmd := m.Update(monitorSyncResultMsg{result: tdsync.Result{Pulled: 1}, changed: true})
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("sync result command returned %T, want tea.BatchMsg", cmd())
	}
	if len(batch) != 2 {
		t.Fatalf("scheduled %d commands, want next wait plus immediate data refresh", len(batch))
	}
	_ = r.close()
}

func TestMonitorDefaultRuntimePushesAndPullsLinkedProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TD_AUTH_KEY", "key")
	t.Setenv("TD_SYNC_AUTO", "")
	t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "")
	t.Setenv("TD_SYNC_AUTO_PULL", "true")

	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := database.SetSyncState("proj"); err != nil {
		t.Fatalf("link: %v", err)
	}
	if _, err := database.Conn().Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, timestamp) VALUES ('al-monitor', 'ses-monitor', 'create', 'issues', 'td-monitor', '{"title":"test","status":"open"}', ?)`, time.Now().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert action: %v", err)
	}

	var pushes, pulls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects/proj/sync/push", func(w http.ResponseWriter, req *http.Request) {
		pushes.Add(1)
		var body syncclient.PushRequest
		_ = json.NewDecoder(req.Body).Decode(&body)
		acks := make([]syncclient.AckResponse, 0, len(body.Events))
		for i, event := range body.Events {
			acks = append(acks, syncclient.AckResponse{ClientActionID: event.ClientActionID, ServerSeq: int64(i + 1)})
		}
		_ = json.NewEncoder(w).Encode(syncclient.PushResponse{Accepted: len(acks), Acks: acks})
	})
	mux.HandleFunc("/v1/projects/proj/sync/pull", func(w http.ResponseWriter, _ *http.Request) {
		pulls.Add(1)
		_ = json.NewEncoder(w).Encode(syncclient.PullResponse{})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	t.Setenv("TD_SYNC_URL", server.URL)

	m := NewModel(database, "ses-monitor", time.Second, "", baseDir)
	if _, ok := m.syncRuntime.waitCmd()().(monitorSyncResultMsg); !ok {
		t.Fatal("default runtime did not report its initial pass")
	}
	if got := pushes.Load(); got != 1 {
		t.Fatalf("push requests = %d, want 1", got)
	}
	if got := pulls.Load(); got != 1 {
		t.Fatalf("pull requests = %d, want 1", got)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestEmbeddedMonitorUnlinkedProjectMakesNoNetworkCalls(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TD_AUTH_KEY", "key")
	t.Setenv("TD_SYNC_AUTO", "")
	t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "")
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close initializer: %v", err)
	}

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()
	t.Setenv("TD_SYNC_URL", server.URL)

	m, err := NewEmbeddedWithOptions(EmbeddedOptions{BaseDir: baseDir, Interval: time.Second})
	if err != nil {
		t.Fatalf("new embedded: %v", err)
	}
	if _, ok := m.syncRuntime.waitCmd()().(monitorSyncResultMsg); !ok {
		t.Fatal("default runtime did not report its gate result")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("unlinked monitor made %d network requests, want 0", got)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestSuccessfulMonitorMutationWakesRuntime(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	issue := &models.Issue{ID: "td-delete", Title: "delete me", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	var calls atomic.Int32
	r := testSyncRuntime(
		func() tdsync.Gate { return tdsync.Gate{Open: true} },
		func(context.Context) (tdsync.Result, error) {
			calls.Add(1)
			return tdsync.Result{}, nil
		}, database.Close,
	)
	m := NewModel(database, "ses-monitor", time.Second, "", baseDir)
	m.syncRuntime = r
	r.start()
	select {
	case <-r.results:
	case <-time.After(time.Second):
		t.Fatal("initial sync did not finish")
	}
	m.ConfirmIssueID = issue.ID
	_, _ = m.executeDelete()

	deadline := time.Now().Add(time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("sync calls = %d, want initial plus mutation wake", got)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
