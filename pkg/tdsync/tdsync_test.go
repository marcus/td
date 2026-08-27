package tdsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/syncclient"
)

func gateDB(t *testing.T, projectID string, disabled bool) (string, *db.DB) {
	t.Helper()
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if projectID != "" {
		if err := database.SetSyncState(projectID); err != nil {
			_ = database.Close()
			t.Fatalf("set sync state: %v", err)
		}
		if disabled {
			if _, err := database.Conn().Exec(`UPDATE sync_state SET sync_disabled = 1`); err != nil {
				_ = database.Close()
				t.Fatalf("disable sync: %v", err)
			}
		}
	}
	return baseDir, database
}

func clearGateEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TD_AUTH_KEY", "key")
	t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "")
	t.Setenv("TD_SYNC_AUTO", "")
}

func TestGateMatrix(t *testing.T) {
	tests := []struct {
		name           string
		authenticated  bool
		projectID      string
		disabled       bool
		killSwitch     bool
		featureEnv     string
		autoEnv        string
		explicit       *bool
		wantOpen       bool
		wantConfigured bool
	}{
		{name: "authenticated linked", authenticated: true, projectID: "proj", wantOpen: true, wantConfigured: true},
		{name: "unauthenticated", projectID: "proj", wantConfigured: true},
		{name: "unlinked", authenticated: true},
		{name: "project disabled", authenticated: true, projectID: "proj", disabled: true},
		{name: "global kill-switch", authenticated: true, projectID: "proj", killSwitch: true, wantConfigured: true},
		{name: "higher precedence feature enable overrides legacy auto disable", authenticated: true, projectID: "proj", featureEnv: "true", autoEnv: "false", wantOpen: true, wantConfigured: true},
		{name: "explicit project off", authenticated: true, projectID: "proj", explicit: boolPtr(false), wantConfigured: true},
		{name: "explicit project on", authenticated: true, projectID: "proj", explicit: boolPtr(true), wantOpen: true, wantConfigured: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearGateEnv(t)
			if !tc.authenticated {
				t.Setenv("TD_AUTH_KEY", "")
			}
			if tc.killSwitch {
				t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "false")
			}
			if tc.featureEnv != "" {
				t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", tc.featureEnv)
			}
			if tc.autoEnv != "" {
				t.Setenv("TD_SYNC_AUTO", tc.autoEnv)
			}
			baseDir, database := gateDB(t, tc.projectID, tc.disabled)
			defer func() { _ = database.Close() }()
			if tc.explicit != nil {
				if err := config.SetFeatureFlag(baseDir, features.SyncAutosync.Name, *tc.explicit); err != nil {
					t.Fatalf("set feature: %v", err)
				}
			}
			syncer, err := New(Options{BaseDir: baseDir, DB: database})
			if err != nil {
				t.Fatalf("new: %v", err)
			}
			gate := syncer.Gate()
			if gate.Open != tc.wantOpen || gate.Configured != tc.wantConfigured || gate.Authenticated != tc.authenticated || gate.KillSwitch != tc.killSwitch {
				t.Fatalf("Gate() = %+v", gate)
			}
		})
	}
}

func boolPtr(v bool) *bool { return &v }

func TestGateReusesSharedDB(t *testing.T) {
	clearGateEnv(t)
	_, database := gateDB(t, "proj", false)
	defer func() { _ = database.Close() }()
	syncer, _ := New(Options{BaseDir: filepath.Join(t.TempDir(), "missing"), DB: database})
	if gate := syncer.Gate(); !gate.Open || !gate.Configured {
		t.Fatalf("shared DB gate = %+v", gate)
	}
}

func TestOnceCollapsesConcurrentCallsAndNeverBootstraps(t *testing.T) {
	clearGateEnv(t)
	baseDir, database := gateDB(t, "proj", false)
	defer func() { _ = database.Close() }()
	t.Setenv("TD_SYNC_AUTO_PULL", "true")

	var pushes, pulls, snapshots int32
	pushStarted := make(chan struct{})
	releasePush := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects/proj/sync/push", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&pushes, 1)
		close(pushStarted)
		<-releasePush
		var req syncclient.PushRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		acks := make([]syncclient.AckResponse, 0, len(req.Events))
		for i, event := range req.Events {
			acks = append(acks, syncclient.AckResponse{ClientActionID: event.ClientActionID, ServerSeq: int64(i + 1)})
		}
		_ = json.NewEncoder(w).Encode(syncclient.PushResponse{Accepted: len(acks), Acks: acks})
	})
	mux.HandleFunc("/v1/projects/proj/sync/pull", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&pulls, 1)
		_ = json.NewEncoder(w).Encode(syncclient.PullResponse{})
	})
	mux.HandleFunc("/v1/projects/proj/snapshot", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&snapshots, 1)
		http.Error(w, "unexpected bootstrap", http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	t.Setenv("TD_SYNC_URL", server.URL)

	if _, err := database.Conn().Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, timestamp) VALUES ('al-test', 'ses-test', 'create', 'issues', 'td-test', '{"title":"test","status":"open"}', ?)`, time.Now().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert action: %v", err)
	}
	syncer, _ := New(Options{BaseDir: baseDir, DB: database})
	type outcome struct {
		result Result
		err    error
	}
	results := make(chan outcome, 2)
	go func() { result, err := syncer.Once(context.Background()); results <- outcome{result, err} }()
	<-pushStarted
	go func() { result, err := syncer.Once(context.Background()); results <- outcome{result, err} }()
	close(releasePush)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("Once errors: %v, %v", first.err, second.err)
	}
	if first.result != second.result {
		t.Fatalf("collapsed results differ: %+v vs %+v", first.result, second.result)
	}
	if got := atomic.LoadInt32(&pushes); got != 1 {
		t.Fatalf("pushes = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&pulls); got != 1 {
		t.Fatalf("pulls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&snapshots); got != 0 {
		t.Fatalf("snapshot requests = %d, want 0", got)
	}
}
