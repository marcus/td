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
	Long: "Log in to the sync server using email approval.\n\n" +
		"You enter your email, td emails you an approval link, and the login\n" +
		"completes only after you click that link. The login cannot be approved\n" +
		"from the terminal alone.",
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

		// Generate a local PKCE pair. The verifier never leaves this process
		// until we poll; only the S256 challenge is sent in DeviceStart. This is
		// what prevents a different process (which lacks the verifier) from
		// completing the login even if it observes the device_code.
		pkce, err := syncclient.GeneratePKCE()
		if err != nil {
			output.Error("generate pkce: %v", err)
			return err
		}

		deviceName := deviceLoginName()

		resp, err := client.DeviceStart(email, pkce.Challenge, pkce.Method, deviceName)
		if err != nil {
			output.Error("login start: %v", err)
			return err
		}

		fmt.Println("Check your email and click the link to approve this login.")
		fmt.Println("Waiting for approval...")

		interval := time.Duration(resp.Interval) * time.Second
		if interval < time.Second {
			interval = 5 * time.Second
		}

		// Stop polling once the device_code can no longer be approved.
		expiresIn := time.Duration(resp.ExpiresIn) * time.Second
		if expiresIn <= 0 {
			expiresIn = 15 * time.Minute
		}
		deadline := time.Now().Add(expiresIn)

		for {
			if time.Now().After(deadline) {
				fmt.Println()
				output.Error("login expired before approval — run `td auth login` again")
				return fmt.Errorf("login expired before approval")
			}

			time.Sleep(interval)

			poll, err := client.DevicePoll(resp.DeviceCode, pkce.Verifier)
			if err != nil {
				// The server returns 410 Gone once the request has expired or
				// the key has already been issued; surface it cleanly rather
				// than as a raw HTTP error.
				fmt.Println()
				output.Error("login could not be completed: %v", err)
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

// deviceLoginName returns a human-readable label for this device, used so the
// approval email/audit log can identify where the login came from. Falls back
// to "td-cli" when the hostname is unavailable.
func deviceLoginName() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "td-cli"
	}
	return "td-cli@" + host
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
