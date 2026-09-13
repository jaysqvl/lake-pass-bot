package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/jaysqvl/lake-pass-bot/internal/buildinfo"
	"github.com/jaysqvl/lake-pass-bot/internal/config"
	secretcrypto "github.com/jaysqvl/lake-pass-bot/internal/crypto"
	"github.com/jaysqvl/lake-pass-bot/internal/observability"
	"github.com/jaysqvl/lake-pass-bot/internal/store"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	// Build identification must also work before setup or when runtime
	// configuration is invalid, without opening or modifying appdata.
	if args[0] == "version" {
		if len(args) != 1 {
			return errors.New("usage: lake-pass-bot version")
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]string{
			"version":  buildinfo.Version,
			"revision": buildinfo.Revision,
		})
	}
	// Reject malformed commands before opening appdata, creating a key, or
	// applying migrations. Parsing a command must not modify an installation.
	var job jobCommand
	var err error
	switch args[0] {
	case "serve", "doctor", "migrate":
		if len(args) != 1 {
			return fmt.Errorf("usage: lake-pass-bot %s", args[0])
		}
	case "admin-password":
		if len(args) != 2 || args[1] != "reset" {
			return errors.New("usage: lake-pass-bot admin-password reset")
		}
	case "auth-check", "dry-run", "book":
		job, err = parseJobCommand(args[0], args[1:])
		if err != nil {
			return err
		}
	default:
		return usageError()
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := observability.Configure(cfg.EffectiveLogLevel()); err != nil {
		return fmt.Errorf("configure logging: %w", err)
	}
	slog.Debug("runtime configuration loaded",
		"command", args[0],
		"log_level", cfg.EffectiveLogLevel(),
		"appdata_dir", cfg.AppDataDir,
	)
	if err := cfg.EnsureDirectories(); err != nil {
		return err
	}
	box, err := secretcrypto.LoadForDatabase(cfg.EncryptionKeyPath, cfg.DatabasePath, cfg.MasterKeyExplicit)
	if err != nil {
		return fmt.Errorf("load encryption key: %w", err)
	}
	database, err := store.OpenMigrated(ctx, cfg.DatabasePath, box)
	if err != nil {
		return err
	}
	defer database.Close()

	switch args[0] {
	case "migrate":
		version, err := database.SchemaVersion(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("database migrated to schema %04d\n", version)
		return nil
	case "doctor":
		return runDoctor(ctx, cfg, database)
	case "serve":
		return runServe(ctx, cfg, database)
	case "auth-check", "dry-run", "book":
		return runJobCommand(ctx, cfg, database, job)
	case "admin-password":
		return resetAdministratorPassword(ctx, database)
	default:
		return usageError()
	}
}

func resetAdministratorPassword(ctx context.Context, database *store.Store) error {
	password := config.Env("LAKE_PASS_ADMIN_PASSWORD")
	if password == "" {
		return errors.New("LAKE_PASS_ADMIN_PASSWORD must contain the new password")
	}
	admin, err := database.ResetAdministratorPassword(ctx, password)
	if err != nil {
		return err
	}
	fmt.Printf("administrator %q password reset; existing sessions were revoked\n", admin.Username)
	return nil
}

func usageError() error {
	return errors.New("usage: lake-pass-bot {serve|doctor|version|migrate|auth-check|dry-run|book|admin-password reset}; auth-check, dry-run, and book require --booking ID")
}
