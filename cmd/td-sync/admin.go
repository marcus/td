package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/marcus/td/internal/api"
	"github.com/marcus/td/internal/serverdb"
)

func runAdmin(args []string) {
	if len(args) == 0 {
		printAdminUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "grant":
		runAdminGrant(args[1:])
	case "revoke":
		runAdminRevoke(args[1:])
	case "create-key":
		runAdminCreateKey(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown admin command: %s\n", args[0])
		printAdminUsage()
		os.Exit(1)
	}
}

func printAdminUsage() {
	fmt.Fprintln(os.Stderr, `Usage: td-sync admin <command> [flags]

Commands:
  grant       Grant admin privileges to a user
  revoke      Revoke admin privileges from a user
  create-key  Create an API key for an admin user`)
}

func openDB(dbPath string) *serverdb.ServerDB {
	if dbPath == "" {
		cfg := api.LoadConfig()
		dbPath = cfg.ServerDBPath
	}
	store, err := serverdb.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open database: %v\n", err)
		os.Exit(1)
	}
	return store
}

func runAdminGrant(args []string) {
	fs := flag.NewFlagSet("admin grant", flag.ExitOnError)
	email := fs.String("email", "", "user email address")
	dbPath := fs.String("db", "", "path to server.db (default: from SYNC_SERVER_DB_PATH or ./data/server.db)")
	fs.Parse(args)

	if *email == "" {
		fmt.Fprintln(os.Stderr, "error: --email is required")
		fs.Usage()
		os.Exit(1)
	}

	store := openDB(*dbPath)
	defer store.Close()

	if err := store.SetUserAdmin(*email, true); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("granted admin to %s\n", strings.ToLower(strings.TrimSpace(*email)))
}

func runAdminRevoke(args []string) {
	fs := flag.NewFlagSet("admin revoke", flag.ExitOnError)
	email := fs.String("email", "", "user email address")
	dbPath := fs.String("db", "", "path to server.db (default: from SYNC_SERVER_DB_PATH or ./data/server.db)")
	fs.Parse(args)

	if *email == "" {
		fmt.Fprintln(os.Stderr, "error: --email is required")
		fs.Usage()
		os.Exit(1)
	}

	store := openDB(*dbPath)
	defer store.Close()

	// Check if this would remove the last admin
	count, err := store.CountAdmins()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Verify user is currently an admin before checking count
	user, err := store.GetUserByEmail(*email)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if user == nil {
		fmt.Fprintf(os.Stderr, "error: user not found: %s\n", *email)
		os.Exit(1)
	}
	if user.IsAdmin && count <= 1 {
		fmt.Fprintln(os.Stderr, "error: cannot revoke last admin")
		os.Exit(1)
	}

	if err := store.SetUserAdmin(*email, false); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("revoked admin from %s\n", strings.ToLower(strings.TrimSpace(*email)))
}

func runAdminCreateKey(args []string) {
	fs := flag.NewFlagSet("admin create-key", flag.ExitOnError)
	email := fs.String("email", "", "admin user email address")
	scopes := fs.String("scopes", "", "comma-separated scopes (e.g. admin:read:server,sync)")
	name := fs.String("name", "", "key name (e.g. td-watch)")
	dbPath := fs.String("db", "", "path to server.db (default: from SYNC_SERVER_DB_PATH or ./data/server.db)")
	fs.Parse(args)

	if *email == "" {
		fmt.Fprintln(os.Stderr, "error: --email is required")
		fs.Usage()
		os.Exit(1)
	}
	if *name == "" {
		fmt.Fprintln(os.Stderr, "error: --name is required")
		fs.Usage()
		os.Exit(1)
	}

	store := openDB(*dbPath)
	defer store.Close()

	// Look up user
	user, err := store.GetUserByEmail(*email)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if user == nil {
		fmt.Fprintf(os.Stderr, "error: user not found: %s\n", *email)
		os.Exit(1)
	}
	if !user.IsAdmin {
		fmt.Fprintf(os.Stderr, "error: user %s is not an admin\n", *email)
		os.Exit(1)
	}

	// Validate scopes
	scopeStr := *scopes
	if scopeStr == "" {
		scopeStr = "sync"
	}
	if err := api.ValidateScopes(scopeStr); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	plaintext, ak, err := store.GenerateAPIKey(user.ID, *name, scopeStr, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("created API key for %s\n", user.Email)
	fmt.Printf("  name:   %s\n", ak.Name)
	fmt.Printf("  scopes: %s\n", ak.Scopes)
	fmt.Printf("  key:    %s\n", plaintext)
	fmt.Println("\nSave this key now -- it will not be shown again.")
}
