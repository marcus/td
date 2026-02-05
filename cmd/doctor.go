package cmd

import (
	"errors"
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/internal/syncconfig"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:     "doctor",
	Short:   "Run diagnostic checks for sync setup",
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		runDoctor()
		return nil
	},
}

func runDoctor() {
	// 1. Auth config
	auth, err := syncconfig.LoadAuth()
	authOK := err == nil && auth != nil && auth.APIKey != ""
	if authOK {
		fmt.Printf("Auth config ............ OK (%s)\n", auth.Email)
	} else if err != nil {
		fmt.Printf("Auth config ............ FAIL (%v)\n", err)
	} else {
		fmt.Printf("Auth config ............ FAIL (not logged in)\n")
	}

	// 2. Server reachable
	serverURL := syncconfig.GetServerURL()
	var client *syncclient.Client
	serverOK := false
	if !authOK {
		// Still try server check even without auth - healthz doesn't need auth
		client = syncclient.New(serverURL, "", "")
	} else {
		deviceID, _ := syncconfig.GetDeviceID()
		client = syncclient.New(serverURL, auth.APIKey, deviceID)
	}
	_, err = client.HealthCheck()
	if err == nil {
		serverOK = true
		fmt.Printf("Server reachable ....... OK (%s)\n", serverURL)
	} else {
		fmt.Printf("Server reachable ....... FAIL (%v)\n", err)
	}

	// 3. Auth valid
	authValid := false
	if !authOK || !serverOK {
		fmt.Printf("Auth valid ............. SKIP\n")
	} else {
		_, err = client.ListProjects()
		if err == nil {
			authValid = true
			fmt.Printf("Auth valid ............. OK\n")
		} else if errors.Is(err, syncclient.ErrUnauthorized) {
			fmt.Printf("Auth valid ............. FAIL (invalid or expired API key)\n")
		} else {
			fmt.Printf("Auth valid ............. FAIL (%v)\n", err)
		}
	}
	_ = authValid // used for documentation; no dependent checks currently gate on this

	// 4. Local database
	baseDir := getBaseDir()
	database, err := db.Open(baseDir)
	dbOK := err == nil
	if dbOK {
		defer database.Close()
		fmt.Printf("Local database ......... OK\n")
	} else {
		fmt.Printf("Local database ......... FAIL (%v)\n", err)
	}

	// 5. Sync linked
	var syncState *db.SyncState
	if !dbOK {
		fmt.Printf("Sync linked ............ SKIP\n")
	} else {
		syncState, err = database.GetSyncState()
		if err != nil {
			fmt.Printf("Sync linked ............ FAIL (%v)\n", err)
		} else if syncState == nil {
			fmt.Printf("Sync linked ............ WARN (not linked to a project)\n")
		} else {
			fmt.Printf("Sync linked ............ OK (project %s)\n", syncState.ProjectID)
		}
	}

	// 6. Pending events
	if !dbOK {
		fmt.Printf("Pending events ......... SKIP\n")
	} else {
		count, err := database.CountPendingEvents()
		if err != nil {
			fmt.Printf("Pending events ......... FAIL (%v)\n", err)
		} else if count > 0 {
			fmt.Printf("Pending events ......... %d\n", count)
		} else {
			fmt.Printf("Pending events ......... 0\n")
		}
	}
}

func init() {
	AddFeatureGatedCommand(features.SyncCLI.Name, doctorCmd)
}
