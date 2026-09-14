package store

import (
	"context"
	"reflect"
	"testing"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
)

func TestBookingSnapshotMigrationRetiresSchedulesAndPreservesExecution(t *testing.T) {
	ctx := context.Background()
	database := ownedTestStore(t)
	_, booking := fixtureProfileAndBooking(t, database, "snapshot-migration")
	booking.ScheduleEnabled = true
	booking, err := database.ForUser(testUserID).updateLegacyBookingFixture(ctx, booking)
	if err != nil {
		t.Fatal(err)
	}
	job, err := database.ForUser(testUserID).EnqueueJob(ctx, EnqueueJobParams{
		BookingRequestID: &booking.ID, Command: model.CommandBook, RunMode: model.RunModeManual,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct version 11 without touching its saved request, active job,
	// reservation, account credentials or schedule configuration.
	if _, err := database.db.ExecContext(ctx, `
		DROP TRIGGER lake_booking_profile_insert;
		DROP TRIGGER lake_booking_profile_update;
		DROP TRIGGER lake_booking_profile_identity;
		ALTER TABLE lake_settings DROP COLUMN booking_profile_id;
		ALTER TABLE account_settings DROP COLUMN default_confirmation_mode;
		DROP TRIGGER jobs_prune_booking_snapshot;
		DROP TRIGGER booking_requests_user_limit;
		ALTER TABLE booking_requests DROP COLUMN kind;
		CREATE TRIGGER booking_requests_user_limit
		BEFORE INSERT ON booking_requests
		WHEN (SELECT count(*) FROM booking_requests WHERE user_id = NEW.user_id) >= 64
		BEGIN
			SELECT RAISE(ABORT, 'per-user booking request limit reached');
		END;
		DELETE FROM schema_migrations WHERE version >= 12;
	`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := database.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	booking.ScheduleEnabled = false
	requests, err := database.ForUser(testUserID).ListBookingRequests(ctx)
	if err != nil || len(requests) != 1 || !reflect.DeepEqual(requests[0], booking) {
		t.Fatalf("migration changed saved request: %+v err=%v", requests, err)
	}
	retained, err := database.ForUser(testUserID).GetJob(ctx, job.ID)
	if err != nil || !reflect.DeepEqual(retained, job) {
		t.Fatalf("migration changed pending execution: %+v err=%v", retained, err)
	}
	var reservationJobID int64
	if err := database.db.QueryRowContext(ctx, "SELECT job_id FROM booking_reservations WHERE profile_id=? AND target_date=?", booking.ProfileID, booking.TargetDate).Scan(&reservationJobID); err != nil || reservationJobID != job.ID {
		t.Fatalf("migration lost reservation: job=%d err=%v", reservationJobID, err)
	}

}
