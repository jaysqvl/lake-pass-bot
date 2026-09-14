package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jaysqvl/lake-pass-bot/internal/destinations"
	"github.com/jaysqvl/lake-pass-bot/internal/model"
)

func TestLakeBookingAccountOwnershipIdentityAndDeletion(t *testing.T) {
	ctx := context.Background()
	database, firstID, secondID := ownershipStore(t)
	source, profile, booking := createOwnedResources(t, database, firstID, "booking-default-owner")
	_, foreign, _ := createOwnedResources(t, database, secondID, "booking-default-foreign")
	resources := database.ForUser(firstID)
	lake, _ := destinations.Resolve("")
	settings := model.DefaultLakeSettings(lake)
	settings.BookingProfileID = profile.ID
	saved, err := resources.SaveLakeSettings(ctx, settings)
	if err != nil || saved.BookingProfileID != profile.ID {
		t.Fatalf("save preferred account: %+v, %v", saved, err)
	}
	settings.BookingProfileID = foreign.ID
	if _, err := resources.SaveLakeSettings(ctx, settings); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign account accepted by store: %v", err)
	}
	otherLake, err := resources.CreateProfile(ctx, ProfileInput{
		Name: "Other lake", LakeID: "other-lake", LoginProbeURL: profile.LoginProbeURL,
		OTPSourceID: source.ID, Headless: true, DefaultTimeoutMS: 15000, Enabled: true,
		Credentials: &model.ProfileCredentials{Phone: "5559876543"},
	})
	if err != nil {
		t.Fatal(err)
	}
	settings.BookingProfileID = otherLake.ID
	if _, err := resources.SaveLakeSettings(ctx, settings); !errors.Is(err, ErrConflict) {
		t.Fatalf("another lake's account accepted by store: %v", err)
	}
	for _, id := range []int64{foreign.ID, otherLake.ID} {
		if _, err := database.db.ExecContext(ctx, `UPDATE lake_settings SET booking_profile_id = ? WHERE user_id = ? AND lake_id = ?`, id, firstID, lake.ID); err == nil {
			t.Fatalf("direct write accepted unrelated profile %d", id)
		}
	}
	if _, err := database.db.ExecContext(ctx, `
		INSERT INTO lake_settings(user_id, lake_id, timezone, release_time, release_days_before,
			all_day_pass_url, half_day_pass_url, pass_order, vehicle_keyword, booking_profile_id, updated_at)
		SELECT ?, lake_id, timezone, release_time, release_days_before,
			all_day_pass_url, half_day_pass_url, pass_order, vehicle_keyword, booking_profile_id, updated_at
		FROM lake_settings WHERE user_id = ? AND lake_id = ?
	`, secondID, firstID, lake.ID); err == nil {
		t.Fatal("direct insert accepted another owner's preferred account")
	}
	for _, change := range []string{"lake_id = 'other-lake'", "provider_id = 'other-provider'"} {
		if _, err := database.db.ExecContext(ctx, "UPDATE profiles SET "+change+" WHERE id = ?", profile.ID); err == nil {
			t.Fatalf("preferred profile identity changed: %s", change)
		}
	}
	if _, err := database.db.ExecContext(ctx, `UPDATE profiles SET enabled = 0 WHERE id = ?`, profile.ID); err != nil {
		t.Fatalf("preference blocked disabling a profile: %v", err)
	}
	retained, err := resources.GetLakeSettings(ctx, lake.ID)
	if err != nil || !reflect.DeepEqual(retained, saved) {
		t.Fatalf("failed writes or disabling lost the explicit choice: %+v, %v", retained, err)
	}
	if err := resources.removeLegacyBookingFixture(ctx, booking.ID); err != nil {
		t.Fatal(err)
	}
	// Another eligible identity must not become the default by deleting the
	// explicitly chosen one. The choice must be changed deliberately first.
	if _, err := database.db.ExecContext(ctx, `UPDATE profiles SET lake_id = ? WHERE id = ?`, lake.ID, otherLake.ID); err != nil {
		t.Fatal(err)
	}
	if err := resources.DeleteProfile(ctx, profile.ID); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "choose another booking account") {
		t.Fatalf("deleting chosen account was not blocked with an actionable error: %v", err)
	}
	if _, err := database.db.ExecContext(ctx, `DELETE FROM profiles WHERE id = ?`, profile.ID); err == nil {
		t.Fatal("direct deletion cleared the explicit account choice")
	}
	retained, err = resources.GetLakeSettings(ctx, lake.ID)
	if err != nil || retained.BookingProfileID != profile.ID {
		t.Fatalf("failed deletion lost the explicit preference: %+v, %v", retained, err)
	}
	profiles, err := resources.ListProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.ResolveLakeBookingProfile(lake, retained, profiles); !errors.Is(err, model.ErrLakeConnectionUnavailable) {
		t.Fatalf("failed deletion silently switched to another account: %v", err)
	}
	retained.BookingProfileID = 0
	if _, err := resources.SaveLakeSettings(ctx, retained); err != nil {
		t.Fatal(err)
	}
	if err := resources.DeleteProfile(ctx, profile.ID); err != nil {
		t.Fatalf("deliberately clearing the choice did not allow deletion: %v", err)
	}
}

func TestBookingDefaultsMigrationPreservesSettingsAndDefaultsToManual(t *testing.T) {
	ctx := context.Background()
	database := ownedTestStore(t)
	resources := database.ForUser(testUserID)
	_, booking := fixtureProfileAndBooking(t, database, "booking-default-migration")
	lake, _ := destinations.Resolve("")
	lakeSettings := model.DefaultLakeSettings(lake)
	lakeSettings.VehicleKeyword, lakeSettings.ReleaseTime = "Saved car", "08:30"
	lakeSettings, err := resources.SaveLakeSettings(ctx, lakeSettings)
	if err != nil {
		t.Fatal(err)
	}
	account := model.DefaultAccountSettings()
	account.PrepMinutesBefore = 45
	account, err = resources.SaveAccountSettings(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct v12 without changing its records, then apply the real upgrade.
	if _, err := database.db.ExecContext(ctx, `
		DROP TRIGGER lake_booking_profile_insert;
		DROP TRIGGER lake_booking_profile_update;
		DROP TRIGGER lake_booking_profile_identity;
		ALTER TABLE lake_settings DROP COLUMN booking_profile_id;
		ALTER TABLE account_settings DROP COLUMN default_confirmation_mode;
		DELETE FROM schema_migrations WHERE version = 13;
	`); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	gotLake, err := resources.GetLakeSettings(ctx, lake.ID)
	if err != nil || !reflect.DeepEqual(gotLake, lakeSettings) {
		t.Fatalf("migration changed lake settings: %+v, %v", gotLake, err)
	}
	gotAccount, err := resources.GetAccountSettings(ctx)
	if err != nil || !reflect.DeepEqual(gotAccount, account) || gotAccount.DefaultConfirmationMode != model.RunModeManual {
		t.Fatalf("migration changed account settings: %+v, %v", gotAccount, err)
	}
	gotBooking, err := resources.GetBookingRequest(ctx, booking.ID)
	if err != nil || !reflect.DeepEqual(gotBooking, booking) {
		t.Fatalf("migration changed an existing booking: %+v, %v", gotBooking, err)
	}
	gotAccount.DefaultConfirmationMode = model.RunModeAuto
	if got, err := resources.SaveAccountSettings(ctx, gotAccount); err != nil || got.DefaultConfirmationMode != model.RunModeAuto {
		t.Fatalf("confirmation mode did not persist: %+v, %v", got, err)
	}
}
