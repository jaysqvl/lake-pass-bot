package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
)

func TestRetireSavedSchedulesPreservesQueuedExecutionAndHistoricalRecords(t *testing.T) {
	ctx := context.Background()
	database := ownedTestStore(t)
	resources := database.ForUser(testUserID)
	profile, legacy := fixtureProfileAndBooking(t, database, "retire-schedule")
	legacy.ScheduleEnabled = true
	legacy, err := resources.updateLegacyBookingFixture(ctx, legacy)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := resources.EnqueueJob(ctx, EnqueueJobParams{BookingRequestID: &legacy.ID, Command: model.CommandBook, RunMode: model.RunModeManual})
	if err != nil {
		t.Fatal(err)
	}
	old := legacy
	old.Name, old.TargetDate = "Historical booking", "2031-02-01"
	old, err = resources.createLegacyBookingFixture(ctx, old)
	if err != nil {
		t.Fatal(err)
	}
	historical, err := resources.EnqueueJob(ctx, EnqueueJobParams{BookingRequestID: &old.ID, Command: model.CommandBook, RunMode: model.RunModeManual})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.SystemTransitionJob(ctx, historical.ID, []model.JobStatus{model.JobQueued}, model.JobRunning, JobTransition{}); err != nil {
		t.Fatal(err)
	}
	if _, err = database.SystemTransitionJob(ctx, historical.ID, []model.JobStatus{model.JobRunning}, model.JobSucceeded, JobTransition{ConfirmationStarted: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = database.SystemAppendJobEvent(ctx, JobEventInput{JobID: historical.ID, Kind: "history", Message: "Retained history"}); err != nil {
		t.Fatal(err)
	}
	// An archived row must remain archived, and its history may still be needed.
	if _, err = database.db.ExecContext(ctx, "UPDATE booking_requests SET kind='archived',enabled=0,schedule_enabled=0 WHERE id=?", old.ID); err != nil {
		t.Fatal(err)
	}
	request := legacy
	request.Name, request.TargetDate = "Snapshot", "2031-03-01"
	if _, err = resources.EnqueueBookingRequest(ctx, request, EnqueueJobParams{Command: model.CommandDryRun}); err != nil {
		t.Fatal(err)
	}
	unused := legacy
	unused.Name = "Unqueued legacy request"
	if _, err = resources.createLegacyBookingFixture(ctx, unused); err != nil {
		t.Fatal(err)
	}
	beforeBookings, err := resources.ListBookingRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	beforeJobs, err := resources.ListJobs(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	beforeEvents, err := resources.ListJobEvents(ctx, historical.ID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	beforeCredentials, err := resources.GetProfileCredentials(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	var reservationsBefore string
	const reservationsSQL = `SELECT json_group_array(json_array(profile_id,target_date,job_id)) FROM (SELECT * FROM booking_reservations ORDER BY profile_id,target_date)`
	if err = database.db.QueryRowContext(ctx, reservationsSQL).Scan(&reservationsBefore); err != nil {
		t.Fatal(err)
	}
	if _, err = database.db.ExecContext(ctx, "DELETE FROM schema_migrations WHERE version=14"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = database.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if version, err := database.SchemaVersion(ctx); err != nil || version != 14 {
		t.Fatalf("schema=%d %v", version, err)
	}
	for i := range beforeBookings {
		if beforeBookings[i].Kind == model.BookingKindSaved {
			beforeBookings[i].ScheduleEnabled = false
		}
	}
	bookings, err := resources.ListBookingRequests(ctx)
	if err != nil || !reflect.DeepEqual(bookings, beforeBookings) {
		t.Fatalf("migration changed booking inputs: %+v %v", bookings, err)
	}
	jobs, err := resources.ListJobs(ctx, 20)
	if err != nil || !reflect.DeepEqual(jobs, beforeJobs) {
		t.Fatalf("migration changed jobs: %+v %v", jobs, err)
	}
	events, err := resources.ListJobEvents(ctx, historical.ID, 0, 20)
	if err != nil || !reflect.DeepEqual(events, beforeEvents) {
		t.Fatalf("migration changed events: %+v %v", events, err)
	}
	credentials, err := resources.GetProfileCredentials(ctx, profile.ID)
	if err != nil || !reflect.DeepEqual(credentials, beforeCredentials) {
		t.Fatal("migration changed credentials")
	}
	var reservationsAfter string
	if err = database.db.QueryRowContext(ctx, reservationsSQL).Scan(&reservationsAfter); err != nil || reservationsAfter != reservationsBefore {
		t.Fatalf("migration changed reservations: %s %v", reservationsAfter, err)
	}
	if _, err = resources.EnqueueBookingRequest(ctx, old, EnqueueJobParams{Command: model.CommandBook, RunMode: model.RunModeManual}); !errors.Is(err, ErrConflict) {
		t.Fatalf("historical reservation no longer blocks duplicate: %v", err)
	}
	claimed, err := database.SystemClaimNextDueJobAt(ctx, "retirement-check", time.Now().UTC().Add(time.Second))
	if err != nil || claimed.ID != queued.ID {
		t.Fatalf("already-queued legacy job no longer executes: %+v %v", claimed, err)
	}
}
