package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	secretcrypto "github.com/jaysqvl/lake-pass-bot/internal/crypto"
	"github.com/jaysqvl/lake-pass-bot/internal/store"
)

func TestUnknownCommandDoesNotInitializeAppdata(t *testing.T) {
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "LAKE_PASS_") || strings.HasPrefix(name, "BUNTZEN_") {
			t.Setenv(name, "")
		}
	}
	directory := filepath.Join(t.TempDir(), "uninitialized")
	t.Setenv("APPDATA_DIR", directory)
	t.Setenv("MAX_CONCURRENT_JOBS", "1")
	t.Setenv("BLUEBUBBLES_URL", "http://bluebubbles.example:1234")
	if err := run(context.Background(), []string{"typo"}); err == nil || err.Error() != usageError().Error() {
		t.Fatalf("unknown command: %v", err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unknown command initialized appdata: %v", err)
	}
}

func TestAdminPasswordCommandRecoversSoleAdminAndRevokesSessions(t *testing.T) {
	ctx := context.Background()
	box, err := secretcrypto.New(bytes.Repeat([]byte{0x52}, 32))
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.OpenMigrated(ctx, filepath.Join(t.TempDir(), "buntzen.db"), box)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	admin, err := database.SetupAdmin(ctx, "renamed-owner", "original administrator password")
	if err != nil {
		t.Fatal(err)
	}
	session, err := database.NewSession(ctx, admin.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("LAKE_PASS_ADMIN_PASSWORD", "host recovered password")
	if err := resetAdministratorPassword(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := database.AuthenticateUser(ctx, "renamed-owner", "host recovered password"); err != nil || !ok {
		t.Fatalf("recovered authentication ok=%v err=%v", ok, err)
	}
	if _, err := database.GetSession(ctx, session.Token); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("session after recovery error = %v", err)
	}
}

func TestAdminPasswordCommandRequiresExplicitRecoverySecret(t *testing.T) {
	t.Setenv("LAKE_PASS_ADMIN_PASSWORD", "")
	err := resetAdministratorPassword(context.Background(), nil)
	if err == nil || err.Error() != "LAKE_PASS_ADMIN_PASSWORD must contain the new password" {
		t.Fatalf("error = %v", err)
	}
}

func TestMalformedCommandsAreRejectedBeforeRuntimeSetup(t *testing.T) {
	t.Setenv("MAX_CONCURRENT_JOBS", "invalid-runtime-setting")
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"serve", "extra"}, "usage: lake-pass-bot serve"},
		{[]string{"doctor", "extra"}, "usage: lake-pass-bot doctor"},
		{[]string{"migrate", "extra"}, "usage: lake-pass-bot migrate"},
		{[]string{"admin-password"}, "usage: lake-pass-bot admin-password reset"},
		{[]string{"admin-password", "reset", "extra"}, "usage: lake-pass-bot admin-password reset"},
		{[]string{"book"}, "book requires --booking ID"},
		{[]string{"book", "--booking", "1", "--mode", "manul"}, "book --mode must be manual or auto"},
		{[]string{"book", "--booking", "1", "extra"}, "book does not accept positional arguments"},
		{[]string{"dry-run", "--booking", "1", "--mode", "auto"}, "--mode is only valid for book"},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			if err := run(context.Background(), test.args); err == nil || err.Error() != test.want {
				t.Fatalf("command error = %v; want %q", err, test.want)
			}
		})
	}
}
