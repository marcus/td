package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/internal/syncconfig"
	"github.com/spf13/cobra"
)

var syncInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive guided setup for sync",
	RunE: func(cmd *cobra.Command, args []string) error {
		reader := bufio.NewReader(os.Stdin)

		// Step 1: Server URL
		fmt.Println("=== Sync Setup ===")
		fmt.Println()

		currentURL := syncconfig.GetServerURL()
		newURL := promptLine(reader, fmt.Sprintf("Server URL [%s]: ", currentURL), "")
		if newURL != "" && newURL != currentURL {
			cfg, err := syncconfig.LoadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			cfg.Sync.URL = newURL
			if err := syncconfig.SaveConfig(cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			currentURL = newURL
			output.Success("Server URL set to %s", currentURL)
		} else {
			fmt.Printf("Using server: %s\n", currentURL)
		}
		fmt.Println()

		// Step 2: Authentication
		if !syncconfig.IsAuthenticated() {
			output.Error("Not authenticated. Run 'td auth login' first, then re-run 'td sync init'.")
			return fmt.Errorf("not authenticated")
		}

		creds, err := syncconfig.LoadAuth()
		if err != nil {
			return fmt.Errorf("load auth: %w", err)
		}
		fmt.Printf("Authenticated as: %s\n", creds.Email)
		fmt.Println()

		// Step 3: Project
		baseDir := getBaseDir()
		database, err := db.Open(baseDir)
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer database.Close()

		syncState, err := database.GetSyncState()
		if err != nil {
			return fmt.Errorf("get sync state: %w", err)
		}

		var projectID string

		if syncState != nil && syncState.ProjectID != "" {
			keep := promptLine(reader, fmt.Sprintf("Already linked to %s. Keep? [Y/n]: ", syncState.ProjectID), "Y")
			if strings.ToLower(keep) != "n" {
				projectID = syncState.ProjectID
			}
		}

		if projectID == "" {
			choice := promptLine(reader, "Create new project or join existing? [c/j]: ", "")
			choice = strings.ToLower(strings.TrimSpace(choice))

			apiKey := syncconfig.GetAPIKey()
			client := syncclient.New(currentURL, apiKey, "")

			switch choice {
			case "c":
				name := promptLine(reader, "Project name: ", "")
				if name == "" {
					return fmt.Errorf("project name required")
				}

				project, err := client.CreateProject(name, "")
				if err != nil {
					output.Error("create project: %v", err)
					return err
				}

				if err := database.SetSyncState(project.ID); err != nil {
					output.Error("link project: %v", err)
					return err
				}

				projectID = project.ID
				output.Success("Created and linked to project %s (%s)", project.Name, project.ID)

			case "j":
				projects, err := client.ListProjects()
				if err != nil {
					output.Error("list projects: %v", err)
					return err
				}
				if len(projects) == 0 {
					output.Error("no projects found")
					return fmt.Errorf("no projects found")
				}

				fmt.Println("Available projects:")
				for i, p := range projects {
					fmt.Printf("  %d) %s (%s)\n", i+1, p.Name, p.ID)
				}

				input := promptLine(reader, "Select project number: ", "")
				num, err := strconv.Atoi(input)
				if err != nil || num < 1 || num > len(projects) {
					output.Error("invalid selection %q", input)
					return fmt.Errorf("invalid selection")
				}

				selected := projects[num-1]
				if err := database.SetSyncState(selected.ID); err != nil {
					output.Error("link project: %v", err)
					return err
				}

				projectID = selected.ID
				output.Success("Linked to project %s (%s)", selected.Name, selected.ID)

			default:
				return fmt.Errorf("invalid choice %q (expected 'c' or 'j')", choice)
			}
		}

		// Step 4: Summary
		fmt.Println()
		fmt.Println("=== Setup Complete ===")
		fmt.Printf("Server:  %s\n", currentURL)
		fmt.Printf("Email:   %s\n", creds.Email)
		fmt.Printf("Project: %s\n", projectID)
		fmt.Println()
		output.Success("Ready to sync! Run 'td sync' to start.")

		return nil
	},
}

// promptLine prints a prompt and reads a line. Returns defaultVal if input is blank.
func promptLine(reader *bufio.Reader, prompt string, defaultVal string) string {
	fmt.Print(prompt)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}

func init() {
	syncCmd.AddCommand(syncInitCmd)
}
