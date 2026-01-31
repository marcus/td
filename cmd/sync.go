package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	tdsync "github.com/marcus/td/internal/sync"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/internal/syncconfig"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:     "sync",
	Short:   "Sync local data with remote server",
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		pushOnly, _ := cmd.Flags().GetBool("push")
		pullOnly, _ := cmd.Flags().GetBool("pull")
		statusOnly, _ := cmd.Flags().GetBool("status")

		if !syncconfig.IsAuthenticated() {
			output.Error("not logged in (run: td auth login)")
			return fmt.Errorf("not authenticated")
		}

		baseDir := getBaseDir()
		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("open database: %v", err)
			return err
		}
		defer database.Close()

		syncState, err := database.GetSyncState()
		if err != nil {
			output.Error("get sync state: %v", err)
			return err
		}
		if syncState == nil {
			output.Error("project not linked (run: td sync-project link <id>)")
			return fmt.Errorf("not linked")
		}

		deviceID, err := syncconfig.GetDeviceID()
		if err != nil {
			output.Error("get device id: %v", err)
			return err
		}

		serverURL := syncconfig.GetServerURL()
		apiKey := syncconfig.GetAPIKey()
		client := syncclient.New(serverURL, apiKey, deviceID)

		if statusOnly {
			return runSyncStatus(database, client, syncState)
		}

		if !pullOnly {
			if err := runPush(database, client, syncState, deviceID); err != nil {
				return err
			}
		}

		if !pushOnly {
			if err := runPull(database, client, syncState, deviceID); err != nil {
				return err
			}
		}

		return nil
	},
}

func runSyncStatus(database *db.DB, client *syncclient.Client, state *db.SyncState) error {
	pending, err := database.CountPendingEvents()
	if err != nil {
		output.Error("count pending: %v", err)
		return err
	}

	fmt.Printf("Project:     %s\n", state.ProjectID)
	fmt.Printf("Last pushed: action %d\n", state.LastPushedActionID)
	fmt.Printf("Last pulled: seq %d\n", state.LastPulledServerSeq)
	fmt.Printf("Pending:     %d events\n", pending)
	if state.LastSyncAt != nil {
		fmt.Printf("Last sync:   %s\n", state.LastSyncAt.Format(time.RFC3339))
	}

	serverStatus, err := client.SyncStatus(state.ProjectID)
	if err != nil {
		if errors.Is(err, syncclient.ErrUnauthorized) {
			output.Warning("unauthorized - re-login may be needed")
			return nil
		}
		output.Error("server status: %v", err)
		return err
	}

	fmt.Printf("\nServer:\n")
	fmt.Printf("  Events:    %d\n", serverStatus.EventCount)
	fmt.Printf("  Last seq:  %d\n", serverStatus.LastServerSeq)
	if serverStatus.LastEventTime != "" {
		fmt.Printf("  Last event: %s\n", serverStatus.LastEventTime)
	}
	return nil
}

func runPush(database *db.DB, client *syncclient.Client, state *db.SyncState, deviceID string) error {
	sess, err := session.Get(database)
	if err != nil {
		output.Error("get session: %v", err)
		return err
	}

	conn := database.Conn()
	tx, err := conn.Begin()
	if err != nil {
		output.Error("begin tx: %v", err)
		return err
	}
	defer tx.Rollback()

	events, err := tdsync.GetPendingEvents(tx, deviceID, sess.ID)
	if err != nil {
		output.Error("get pending events: %v", err)
		return err
	}

	if len(events) == 0 {
		fmt.Println("Nothing to push.")
		return nil
	}

	pushReq := &syncclient.PushRequest{
		DeviceID:  deviceID,
		SessionID: sess.ID,
	}
	for _, ev := range events {
		pushReq.Events = append(pushReq.Events, syncclient.EventInput{
			ClientActionID:  ev.ClientActionID,
			ActionType:      ev.ActionType,
			EntityType:      ev.EntityType,
			EntityID:        ev.EntityID,
			Payload:         ev.Payload,
			ClientTimestamp: ev.ClientTimestamp.Format(time.RFC3339),
		})
	}

	pushResp, err := client.Push(state.ProjectID, pushReq)
	if err != nil {
		if errors.Is(err, syncclient.ErrUnauthorized) {
			output.Error("unauthorized - re-login may be needed")
		} else {
			output.Error("push: %v", err)
		}
		return err
	}

	acks := make([]tdsync.Ack, len(pushResp.Acks))
	var maxActionID int64
	for i, a := range pushResp.Acks {
		acks[i] = tdsync.Ack{
			ClientActionID: a.ClientActionID,
			ServerSeq:      a.ServerSeq,
		}
		if a.ClientActionID > maxActionID {
			maxActionID = a.ClientActionID
		}
	}

	if err := tdsync.MarkEventsSynced(tx, acks); err != nil {
		output.Error("mark synced: %v", err)
		return err
	}

	// Update sync_state within the same transaction to avoid race
	if maxActionID > 0 {
		if _, err := tx.Exec(`UPDATE sync_state SET last_pushed_action_id = ?, last_sync_at = CURRENT_TIMESTAMP`, maxActionID); err != nil {
			output.Error("update sync state: %v", err)
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		output.Error("commit: %v", err)
		return err
	}

	fmt.Printf("Pushed %d events.\n", pushResp.Accepted)
	return nil
}

func runPull(database *db.DB, client *syncclient.Client, state *db.SyncState, deviceID string) error {
	lastSeq := state.LastPulledServerSeq
	totalPulled := 0
	totalApplied := 0

	for {
		pullResp, err := client.Pull(state.ProjectID, lastSeq, 1000, deviceID)
		if err != nil {
			if errors.Is(err, syncclient.ErrUnauthorized) {
				output.Error("unauthorized - re-login may be needed")
			} else {
				output.Error("pull: %v", err)
			}
			return err
		}

		if len(pullResp.Events) == 0 {
			break
		}

		// Convert pull events to sync events
		events := make([]tdsync.Event, len(pullResp.Events))
		for i, pe := range pullResp.Events {
			clientTS, _ := time.Parse(time.RFC3339, pe.ClientTimestamp)
			events[i] = tdsync.Event{
				ServerSeq:       pe.ServerSeq,
				DeviceID:        pe.DeviceID,
				SessionID:       pe.SessionID,
				ClientActionID:  pe.ClientActionID,
				ActionType:      pe.ActionType,
				EntityType:      pe.EntityType,
				EntityID:        pe.EntityID,
				Payload:         pe.Payload,
				ClientTimestamp: clientTS,
			}
		}

		conn := database.Conn()
		tx, err := conn.Begin()
		if err != nil {
			output.Error("begin tx: %v", err)
			return err
		}

		result, err := tdsync.ApplyRemoteEvents(tx, events, deviceID, nil)
		if err != nil {
			tx.Rollback()
			output.Error("apply events: %v", err)
			return err
		}

		// Update sync_state within the same transaction to avoid race
		if _, err := tx.Exec(`UPDATE sync_state SET last_pulled_server_seq = ?, last_sync_at = CURRENT_TIMESTAMP`, pullResp.LastServerSeq); err != nil {
			tx.Rollback()
			output.Error("update sync state: %v", err)
			return err
		}

		if err := tx.Commit(); err != nil {
			output.Error("commit: %v", err)
			return err
		}

		totalPulled += len(pullResp.Events)
		totalApplied += result.Applied
		lastSeq = pullResp.LastServerSeq

		if !pullResp.HasMore {
			break
		}
	}

	if totalPulled == 0 {
		fmt.Println("Nothing to pull.")
	} else {
		fmt.Printf("Pulled %d events (%d applied).\n", totalPulled, totalApplied)
	}
	return nil
}

func init() {
	syncCmd.Flags().Bool("push", false, "Push only")
	syncCmd.Flags().Bool("pull", false, "Pull only")
	syncCmd.Flags().Bool("status", false, "Show sync status only")
	rootCmd.AddCommand(syncCmd)
}
