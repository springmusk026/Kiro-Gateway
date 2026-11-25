package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/logger"
	"github.com/springmusk026/Kiro-Gateway/storage"
)

func databaseURL() string {
	for _, k := range []string{"DATABASE_URL", "KIRO_DATABASE_URL"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// openDatabase connects to PostgreSQL when DATABASE_URL is set and installs it
// as the config backend. On the first start against an empty database it
// imports the existing config.json (and stored responses) automatically
// unless KIRO_AUTO_IMPORT=false, leaving the JSON files in place: the admin
// console then offers to verify the transfer and delete them.
func openDatabase(configPath string) *storage.DB {
	url := databaseURL()
	if url == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := connectWithRetry(ctx, url)
	if err != nil {
		logger.Fatalf("Database: %v", err)
	}
	cs := storage.NewConfigStore(db)
	empty, err := cs.Empty(ctx)
	if err != nil {
		logger.Fatalf("Database: %v", err)
	}
	if empty && autoImportEnabled() {
		if _, statErr := os.Stat(configPath); statErr == nil {
			res, err := storage.ImportJSON(ctx, db, configPath, false)
			if err != nil {
				logger.Fatalf("Database: importing %s failed: %v", configPath, err)
			}
			logger.Infof("Database: imported %s (%d accounts, %d API keys, %d stored responses; %d skipped). The JSON file is kept until you confirm the migration in the admin console.",
				configPath, res.Accounts, res.APIKeys, res.Responses, res.Skipped)
		}
	}
	config.UseStore(cs)
	return db
}

// autoImportEnabled reports whether config.json is imported automatically
// into an empty database at startup (KIRO_AUTO_IMPORT, default on).
func autoImportEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KIRO_AUTO_IMPORT"))) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

// connectWithRetry tolerates a database that is still starting (docker compose).
func connectWithRetry(ctx context.Context, url string) (*storage.DB, error) {
	var lastErr error
	for {
		db, err := storage.Open(ctx, url)
		if err == nil {
			return db, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, lastErr
		case <-time.After(2 * time.Second):
		}
	}
}

func runCommand(cmd string, args []string, configPath string) int {
	switch cmd {
	case "migrate":
		fs := flag.NewFlagSet("migrate", flag.ExitOnError)
		from := fs.String("from", configPath, "config.json to import")
		to := fs.String("to", databaseURL(), "PostgreSQL URL (default $DATABASE_URL)")
		force := fs.Bool("force", false, "replace configuration already in the database")
		fs.Parse(args)
		if *to == "" {
			fmt.Fprintln(os.Stderr, "migrate: set DATABASE_URL or pass --to postgres://…")
			return 2
		}
		ctx := context.Background()
		db, err := storage.Open(ctx, *to)
		if err != nil {
			fmt.Fprintln(os.Stderr, "migrate:", err)
			return 1
		}
		defer db.Close()
		res, err := storage.ImportJSON(ctx, db, *from, *force)
		if errors.Is(err, storage.ErrDatabaseNotEmpty) {
			fmt.Fprintln(os.Stderr, "migrate:", err)
			return 1
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "migrate:", err)
			return 1
		}
		fmt.Printf("Imported %s → PostgreSQL: %d accounts, %d API keys, %d stored responses (%d skipped).\n",
			*from, res.Accounts, res.APIKeys, res.Responses, res.Skipped)
		fmt.Println("The JSON file was not modified; keep it as a backup.")
		return 0
	case "export":
		fs := flag.NewFlagSet("export", flag.ExitOnError)
		from := fs.String("from", databaseURL(), "PostgreSQL URL (default $DATABASE_URL)")
		out := fs.String("out", "config.export.json", "file to write")
		decrypt := fs.Bool("decrypt", false, "write credentials in plaintext (needs KIRO_ENCRYPTION_KEY when they are encrypted)")
		fs.Parse(args)
		if *from == "" {
			fmt.Fprintln(os.Stderr, "export: set DATABASE_URL or pass --from postgres://…")
			return 2
		}
		ctx := context.Background()
		db, err := storage.Open(ctx, *from)
		if err != nil {
			fmt.Fprintln(os.Stderr, "export:", err)
			return 1
		}
		defer db.Close()
		c, err := storage.ExportJSON(ctx, db, *out, *decrypt)
		if err != nil {
			fmt.Fprintln(os.Stderr, "export:", err)
			return 1
		}
		switch {
		case *decrypt:
			fmt.Printf("Exported %d accounts and %d API key hashes to %s with credentials in PLAINTEXT — keep it private.\n", len(c.Accounts), len(c.ApiKeys), *out)
		case config.EncryptionEnabled():
			fmt.Printf("Exported %d accounts and %d API key hashes to %s (credentials encrypted; loading it needs the same %s).\n", len(c.Accounts), len(c.ApiKeys), *out, config.EncryptionKeyEnv)
		default:
			fmt.Printf("Exported %d accounts and %d API key hashes to %s (contains secrets — keep it private).\n", len(c.Accounts), len(c.ApiKeys), *out)
		}
		return 0
	case "help", "-h", "--help":
		fmt.Println(`Usage:
  kiro-gateway                       start the gateway
  kiro-gateway migrate [--from config.json] [--to postgres://…] [--force]
                                copy config.json and stored responses into PostgreSQL
  kiro-gateway export  [--from postgres://…] [--out file.json] [--decrypt]
                                write the database configuration back to a JSON file
                                (encrypted credentials stay encrypted unless --decrypt;
                                 API keys are exported as hashes, never as key values)

Environment: CONFIG_PATH (default data/config.json), DATABASE_URL (enables PostgreSQL),
KIRO_ENCRYPTION_KEY (encrypts credentials at rest).`)
		return 0
	}
	fmt.Fprintf(os.Stderr, "unknown command %q (try: kiro-gateway help)\n", cmd)
	return 2
}
