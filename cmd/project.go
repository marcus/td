package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/internal/syncconfig"
	"github.com/spf13/cobra"
)

var validRoles = map[string]bool{"owner": true, "writer": true, "reader": true}

var syncProjectCmd = &cobra.Command{
	Use:     "sync-project",
	Aliases: []string{"sp"},
	Short:   "Manage sync projects",
	GroupID: "system",
}

var syncProjectCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a remote sync project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !syncconfig.IsAuthenticated() {
			output.Error("not logged in (run: td auth login)")
			return fmt.Errorf("not authenticated")
		}

		name := args[0]
		description, _ := cmd.Flags().GetString("description")

		serverURL := syncconfig.GetServerURL()
		apiKey := syncconfig.GetAPIKey()
		client := syncclient.New(serverURL, apiKey, "")

		project, err := client.CreateProject(name, description)
		if err != nil {
			output.Error("create project: %v", err)
			return err
		}

		// Auto-link local project
		baseDir := getBaseDir()
		database, err := db.Open(baseDir)
		if err == nil {
			defer database.Close()
			if err := database.SetSyncState(project.ID); err == nil {
				output.Success("Created and linked to project %s (%s)", project.Name, project.ID)
				return nil
			} else {
				output.Success("Created project %s (%s)", project.Name, project.ID)
				output.Warning("auto-link failed: %v", err)
				return nil
			}
		}

		output.Success("Created project %s (%s)", project.Name, project.ID)
		output.Warning("auto-link failed: %v", err)
		return nil
	},
}

var syncProjectLinkCmd = &cobra.Command{
	Use:   "link <project-id>",
	Short: "Link local project to remote sync project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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

		projectID := args[0]
		force, _ := cmd.Flags().GetBool("force")

		// Check if already linked to a different project
		currentState, err := database.GetSyncState()
		if err != nil {
			output.Error("get sync state: %v", err)
			return err
		}

		if currentState != nil && currentState.ProjectID != projectID {
			syncedCount, err := database.CountSyncedEvents()
			if err != nil {
				output.Error("count synced events: %v", err)
				return err
			}

			if syncedCount > 0 {
				if !force {
					reader := bufio.NewReader(os.Stdin)
					fmt.Printf("You have %d events synced to previous project. Reset sync state to push to new project? [y/N] ", syncedCount)
					line, _ := reader.ReadString('\n')
					line = strings.TrimSpace(strings.ToLower(line))
					if line != "y" && line != "yes" {
						output.Warning("link cancelled")
						return nil
					}
				}

				cleared, err := database.ClearActionLogSyncState()
				if err != nil {
					output.Error("clear sync state: %v", err)
					return err
				}
				output.Success("Reset %d events for re-sync", cleared)
			}
		}

		if err := database.SetSyncState(projectID); err != nil {
			output.Error("link project: %v", err)
			return err
		}

		output.Success("Linked to project %s", projectID)
		return nil
	},
}

var syncProjectUnlinkCmd = &cobra.Command{
	Use:   "unlink",
	Short: "Unlink local project from remote sync",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()
		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("open database: %v", err)
			return err
		}
		defer database.Close()

		force, _ := cmd.Flags().GetBool("force")

		// Check for synced events and offer to clear sync state
		syncedCount, err := database.CountSyncedEvents()
		if err != nil {
			output.Error("count synced events: %v", err)
			return err
		}

		if syncedCount > 0 {
			if !force {
				reader := bufio.NewReader(os.Stdin)
				fmt.Printf("You have %d synced events. Clear sync state so they can be pushed to a new project? [y/N] ", syncedCount)
				line, _ := reader.ReadString('\n')
				line = strings.TrimSpace(strings.ToLower(line))
				if line == "y" || line == "yes" {
					cleared, err := database.ClearActionLogSyncState()
					if err != nil {
						output.Error("clear sync state: %v", err)
						return err
					}
					output.Success("Reset %d events for re-sync", cleared)
				}
			} else {
				cleared, err := database.ClearActionLogSyncState()
				if err != nil {
					output.Error("clear sync state: %v", err)
					return err
				}
				output.Success("Reset %d events for re-sync", cleared)
			}
		}

		if err := database.ClearSyncState(); err != nil {
			output.Error("unlink project: %v", err)
			return err
		}

		output.Success("Unlinked from sync project")
		return nil
	},
}

var syncProjectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List remote sync projects",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !syncconfig.IsAuthenticated() {
			output.Error("not logged in (run: td auth login)")
			return fmt.Errorf("not authenticated")
		}

		serverURL := syncconfig.GetServerURL()
		apiKey := syncconfig.GetAPIKey()
		client := syncclient.New(serverURL, apiKey, "")

		projects, err := client.ListProjects()
		if err != nil {
			output.Error("list projects: %v", err)
			return err
		}

		if len(projects) == 0 {
			fmt.Println("No projects.")
			return nil
		}

		fmt.Printf("%-36s  %-20s  %s\n", "ID", "NAME", "CREATED")
		for _, p := range projects {
			fmt.Printf("%-36s  %-20s  %s\n", p.ID, p.Name, p.CreatedAt)
		}
		return nil
	},
}

var syncProjectMembersCmd = &cobra.Command{
	Use:   "members",
	Short: "List project members",
	RunE: func(cmd *cobra.Command, args []string) error {
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
		if err != nil || syncState == nil {
			output.Error("project not linked (run: td sync-project link <id>)")
			return fmt.Errorf("not linked")
		}

		client := syncclient.New(syncconfig.GetServerURL(), syncconfig.GetAPIKey(), "")
		members, err := client.ListMembers(syncState.ProjectID)
		if err != nil {
			output.Error("list members: %v", err)
			return err
		}

		if len(members) == 0 {
			fmt.Println("No members.")
			return nil
		}

		fmt.Printf("%-36s  %-10s  %s\n", "USER ID", "ROLE", "ADDED")
		for _, m := range members {
			fmt.Printf("%-36s  %-10s  %s\n", m.UserID, m.Role, m.CreatedAt)
		}
		return nil
	},
}

var syncProjectInviteCmd = &cobra.Command{
	Use:   "invite <email> [role]",
	Short: "Invite a user to the project by email",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
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
		if err != nil || syncState == nil {
			output.Error("project not linked (run: td sync-project link <id>)")
			return fmt.Errorf("not linked")
		}

		email := args[0]
		role := "writer"
		if len(args) > 1 {
			role = args[1]
		}
		if !validRoles[role] {
			output.Error("invalid role %q (must be owner, writer, or reader)", role)
			return fmt.Errorf("invalid role: %s", role)
		}

		client := syncclient.New(syncconfig.GetServerURL(), syncconfig.GetAPIKey(), "")
		m, err := client.AddMember(syncState.ProjectID, email, role)
		if err != nil {
			output.Error("invite member: %v", err)
			return err
		}

		output.Success("Invited %s as %s (user %s)", email, m.Role, m.UserID)
		return nil
	},
}

var syncProjectKickCmd = &cobra.Command{
	Use:   "kick <user-id>",
	Short: "Remove a member from the project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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
		if err != nil || syncState == nil {
			output.Error("project not linked (run: td sync-project link <id>)")
			return fmt.Errorf("not linked")
		}

		client := syncclient.New(syncconfig.GetServerURL(), syncconfig.GetAPIKey(), "")
		if err := client.RemoveMember(syncState.ProjectID, args[0]); err != nil {
			output.Error("remove member: %v", err)
			return err
		}

		output.Success("Removed member %s", args[0])
		return nil
	},
}

var syncProjectRoleCmd = &cobra.Command{
	Use:   "role <user-id> <role>",
	Short: "Change a member's role",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
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
		if err != nil || syncState == nil {
			output.Error("project not linked (run: td sync-project link <id>)")
			return fmt.Errorf("not linked")
		}

		if !validRoles[args[1]] {
			output.Error("invalid role %q (must be owner, writer, or reader)", args[1])
			return fmt.Errorf("invalid role: %s", args[1])
		}

		client := syncclient.New(syncconfig.GetServerURL(), syncconfig.GetAPIKey(), "")
		if err := client.UpdateMemberRole(syncState.ProjectID, args[0], args[1]); err != nil {
			output.Error("update role: %v", err)
			return err
		}

		output.Success("Updated %s to %s", args[0], args[1])
		return nil
	},
}

var syncProjectJoinCmd = &cobra.Command{
	Use:   "join [name-or-id]",
	Short: "Join a remote sync project by name or ID",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !syncconfig.IsAuthenticated() {
			output.Error("not logged in (run: td auth login)")
			return fmt.Errorf("not authenticated")
		}

		serverURL := syncconfig.GetServerURL()
		apiKey := syncconfig.GetAPIKey()
		client := syncclient.New(serverURL, apiKey, "")

		projects, err := client.ListProjects()
		if err != nil {
			output.Error("list projects: %v", err)
			return err
		}

		if len(projects) == 0 {
			output.Error("no projects found")
			return fmt.Errorf("no projects found")
		}

		var selected syncclient.ProjectResponse

		if len(args) == 0 {
			// Interactive: display numbered list, prompt for selection
			fmt.Println("Available projects:")
			for i, p := range projects {
				fmt.Printf("  %d) %s (%s)\n", i+1, p.Name, p.ID)
			}
			fmt.Print("Select project number: ")

			scanner := bufio.NewScanner(os.Stdin)
			if !scanner.Scan() {
				return fmt.Errorf("no input")
			}
			input := strings.TrimSpace(scanner.Text())

			num, err := strconv.Atoi(input)
			if err != nil || num < 1 || num > len(projects) {
				output.Error("invalid selection %q", input)
				return fmt.Errorf("invalid selection")
			}
			selected = projects[num-1]
		} else {
			// Match by name first, then by ID
			query := args[0]
			found := false
			for _, p := range projects {
				if p.Name == query {
					selected = p
					found = true
					break
				}
			}
			if !found {
				for _, p := range projects {
					if p.ID == query {
						selected = p
						found = true
						break
					}
				}
			}
			if !found {
				output.Error("no project matching %q", query)
				return fmt.Errorf("no project matching %q", query)
			}
		}

		// Link using existing logic
		baseDir := getBaseDir()
		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("open database: %v", err)
			return err
		}
		defer database.Close()

		if err := database.SetSyncState(selected.ID); err != nil {
			output.Error("link project: %v", err)
			return err
		}

		output.Success("Linked to project %s (%s)", selected.Name, selected.ID)
		return nil
	},
}

func init() {
	syncProjectCreateCmd.Flags().String("description", "", "Project description")
	syncProjectLinkCmd.Flags().BoolP("force", "f", false, "Skip confirmation prompts")
	syncProjectUnlinkCmd.Flags().BoolP("force", "f", false, "Skip confirmation prompts")

	syncProjectCmd.AddCommand(syncProjectCreateCmd)
	syncProjectCmd.AddCommand(syncProjectJoinCmd)
	syncProjectCmd.AddCommand(syncProjectLinkCmd)
	syncProjectCmd.AddCommand(syncProjectUnlinkCmd)
	syncProjectCmd.AddCommand(syncProjectListCmd)
	syncProjectCmd.AddCommand(syncProjectMembersCmd)
	syncProjectCmd.AddCommand(syncProjectInviteCmd)
	syncProjectCmd.AddCommand(syncProjectKickCmd)
	syncProjectCmd.AddCommand(syncProjectRoleCmd)
	AddFeatureGatedCommand(features.SyncCLI.Name, syncProjectCmd)
}
