package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/internal/syncconfig"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:     "auth",
	Short:   "Manage sync authentication",
	GroupID: "system",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to sync server",
	RunE: func(cmd *cobra.Command, args []string) error {
		serverURL := syncconfig.GetServerURL()
		client := syncclient.New(serverURL, "", "")

		fmt.Print("Email: ")
		reader := bufio.NewReader(os.Stdin)
		email, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read email: %w", err)
		}
		email = strings.TrimSpace(email)
		if email == "" {
			return fmt.Errorf("email required")
		}

		resp, err := client.LoginStart(email)
		if err != nil {
			output.Error("login start: %v", err)
			return err
		}

		fmt.Printf("Open %s and enter code: %s\n", resp.VerificationURI, resp.UserCode)

		interval := time.Duration(resp.Interval) * time.Second
		if interval < time.Second {
			interval = 5 * time.Second
		}

		for {
			time.Sleep(interval)

			poll, err := client.LoginPoll(resp.DeviceCode)
			if err != nil {
				output.Error("login poll: %v", err)
				return err
			}

			switch poll.Status {
			case "pending":
				fmt.Print(".")
				continue
			case "complete":
				fmt.Println()

				deviceID, err := syncconfig.GetDeviceID()
				if err != nil {
					return fmt.Errorf("get device id: %w", err)
				}

				creds := &syncconfig.AuthCredentials{
					ServerURL: serverURL,
					Email:     email,
					DeviceID:  deviceID,
				}
				if poll.APIKey != nil {
					creds.APIKey = *poll.APIKey
				}
				if poll.UserID != nil {
					creds.UserID = *poll.UserID
				}
				if poll.Email != nil {
					creds.Email = *poll.Email
				}
				if poll.ExpiresAt != nil {
					creds.ExpiresAt = *poll.ExpiresAt
				}

				if err := syncconfig.SaveAuth(creds); err != nil {
					output.Error("save credentials: %v", err)
					return err
				}

				output.Success("Logged in as %s", creds.Email)
				return nil
			default:
				return fmt.Errorf("unexpected poll status: %s", poll.Status)
			}
		}
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log out from sync server",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := syncconfig.ClearAuth(); err != nil {
			output.Error("logout: %v", err)
			return err
		}
		fmt.Println("Logged out.")
		return nil
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show authentication status",
	RunE: func(cmd *cobra.Command, args []string) error {
		creds, err := syncconfig.LoadAuth()
		if err != nil {
			output.Error("load auth: %v", err)
			return err
		}

		if creds == nil || creds.APIKey == "" {
			fmt.Println("Not logged in.")
			return nil
		}

		keyPrefix := creds.APIKey
		if len(keyPrefix) > 12 {
			keyPrefix = keyPrefix[:12] + "..."
		}

		fmt.Printf("Email:  %s\n", creds.Email)
		fmt.Printf("Server: %s\n", creds.ServerURL)
		fmt.Printf("Key:    %s\n", keyPrefix)
		return nil
	},
}

func init() {
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authStatusCmd)
	AddFeatureGatedCommand(features.SyncCLI.Name, authCmd)
}
