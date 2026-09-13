package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
)

func TestNetworkSettingsPersistenceAndAtomicReplacement(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "network.db")
	box := testEncryptor(t)
	database, err := OpenMigrated(ctx, path, box)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if got, err := database.SystemGetNetworkSettings(ctx); !errors.Is(err, ErrNotFound) || got.HostCheckEnabled {
		t.Fatalf("unsaved settings=%+v err=%v", got, err)
	}
	want := model.NetworkSettings{HostCheckEnabled: true, AllowedHosts: []string{"lake.example", "192.0.2.10:8091"}}
	saved, err := database.SystemSaveNetworkSettings(ctx, model.NetworkSettings{
		HostCheckEnabled: true, AllowedHosts: []string{" Lake.EXAMPLE ", "lake.example", "192.0.2.10:8091"},
	})
	if err != nil || !reflect.DeepEqual(saved, want) {
		t.Fatalf("saved settings=%+v err=%v", saved, err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = OpenMigrated(ctx, path, box)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := database.SystemGetNetworkSettings(ctx); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("reopened settings=%+v err=%v", got, err)
	}
	if _, err := database.SystemSaveNetworkSettings(ctx, model.NetworkSettings{AllowedHosts: []string{"https://invalid.example"}}); err == nil {
		t.Fatal("invalid settings persisted")
	}
	if got, err := database.SystemGetNetworkSettings(ctx); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("failed save changed existing policy: %+v err=%v", got, err)
	}
	if _, err := database.SystemSaveNetworkSettings(ctx, model.NetworkSettings{}); err != nil {
		t.Fatal(err)
	}
	want = model.NetworkSettings{AllowedHosts: []string{}}
	if got, err := database.SystemGetNetworkSettings(ctx); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("disabled settings=%+v err=%v", got, err)
	}
	var count int
	if err := database.db.QueryRowContext(ctx, "SELECT count(*) FROM network_settings").Scan(&count); err != nil || count != 1 {
		t.Fatalf("singleton rows=%d err=%v", count, err)
	}
}

func TestNetworkSettingsDatabaseConstraintsAndReadValidation(t *testing.T) {
	ctx := context.Background()
	database := testStore(t)
	for _, statement := range []string{
		`INSERT INTO network_settings VALUES (2, 0, '[]')`,
		`INSERT INTO network_settings VALUES (1, 2, '[]')`,
		`INSERT INTO network_settings VALUES (1, 0, '{}')`,
	} {
		if _, err := database.db.ExecContext(ctx, statement); err == nil {
			t.Fatalf("invalid database state accepted: %s", statement)
		}
	}
	for _, hosts := range []string{`[1]`, `["https://invalid.example"]`} {
		if _, err := database.db.ExecContext(ctx, `
			INSERT INTO network_settings VALUES (1, 1, ?)
			ON CONFLICT(id) DO UPDATE SET allowed_hosts = excluded.allowed_hosts
		`, hosts); err != nil {
			t.Fatal(err)
		}
		if _, err := database.SystemGetNetworkSettings(ctx); err == nil {
			t.Fatalf("invalid persisted hostnames accepted: %s", hosts)
		}
	}
}

func TestNetworkSettingsMigrationPreservesAccountAndBookingState(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "v10.db"), testEncryptor(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.db.ExecContext(ctx, `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		var version int
		if _, err := fmt.Sscanf(entry.Name(), "%04d_", &version); err != nil {
			t.Fatal(err)
		}
		if version > 10 {
			continue
		}
		script, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := database.applyMigration(ctx, version, entry.Name(), script); err != nil {
			t.Fatal(err)
		}
	}
	admin, err := database.SetupAdmin(ctx, "network-admin", testAdminPassword)
	if err != nil {
		t.Fatal(err)
	}
	member, err := database.CreateMember(ctx, CreateUserInput{Username: "network-member", Password: testMemberPassword})
	if err != nil {
		t.Fatal(err)
	}
	_, profile, booking := createOwnedResources(t, database, member.ID, "network-migration")
	settings := model.DefaultAccountSettings()
	settings.PrepMinutesBefore = 45
	savedSettings, err := database.ForUser(member.ID).SaveAccountSettings(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := database.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if version, err := database.SchemaVersion(ctx); err != nil || version != 11 {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
	if _, err := database.SystemGetNetworkSettings(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("migration replaced deployment defaults: %v", err)
	}
	for _, user := range []model.User{admin, member} {
		if got, err := database.GetUser(ctx, user.ID); err != nil || !reflect.DeepEqual(got, user) {
			t.Fatalf("migration changed user: %+v err=%v", got, err)
		}
	}
	if got, err := database.ForUser(member.ID).GetAccountSettings(ctx); err != nil || !reflect.DeepEqual(got, savedSettings) {
		t.Fatalf("migration changed account settings: %+v err=%v", got, err)
	}
	if got, err := database.ForUser(member.ID).GetProfile(ctx, profile.ID); err != nil || !reflect.DeepEqual(got, profile) {
		t.Fatalf("migration changed profile: %+v err=%v", got, err)
	}
	if got, err := database.ForUser(member.ID).GetBookingRequest(ctx, booking.ID); err != nil || !reflect.DeepEqual(got, booking) {
		t.Fatalf("migration changed booking: %+v err=%v", got, err)
	}
}
