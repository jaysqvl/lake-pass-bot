package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
)

func TestDeleteUnusedBookingIsOwnerScopedAndRemovesItsRow(t *testing.T) {
	ctx := context.Background()
	database, ownerID, otherID := ownershipStore(t)
	_, _, booking := createOwnedResources(t, database, ownerID, "booking-delete-owner")
	if err := database.DeleteBookingRequest(ctx, 0, booking.ID); !errors.Is(err, ErrUserRequired) {
		t.Fatalf("missing owner error=%v", err)
	}
	if err := database.ForUser(otherID).DeleteBookingRequest(ctx, booking.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign booking deletion error=%v", err)
	}
	if _, err := database.ForUser(ownerID).GetBookingRequest(ctx, booking.ID); err != nil {
		t.Fatalf("foreign deletion changed the owner's booking: %v", err)
	}
	if err := database.ForUser(ownerID).DeleteBookingRequest(ctx, booking.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SystemGetBookingRequest(ctx, booking.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unused booking was retained: %v", err)
	}
}

func TestDeleteBookingRejectsPendingJobsWithoutChangingInputs(t *testing.T) {
	for _, test := range []struct {
		name    string
		command model.JobCommand
		status  model.JobStatus
	}{
		{"queued booking", model.CommandBook, model.JobQueued},
		{"running booking", model.CommandBook, model.JobRunning},
		{"awaiting approval", model.CommandBook, model.JobAwaitingApproval},
		{"queued dry run", model.CommandDryRun, model.JobQueued},
		{"queued sign in", model.CommandAuthCheck, model.JobQueued},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			database := ownedTestStore(t)
			_, booking := fixtureProfileAndBooking(t, database, "pending-delete")
			booking.ScheduleEnabled = true
			booking, err := database.ForUser(testUserID).UpdateBookingRequest(ctx, booking)
			if err != nil {
				t.Fatal(err)
			}
			job, err := database.ForUser(testUserID).EnqueueJob(ctx, EnqueueJobParams{
				BookingRequestID: &booking.ID, Command: test.command, RunMode: model.RunModeManual,
			})
			if err != nil {
				t.Fatal(err)
			}
			if test.status != model.JobQueued {
				if _, err := database.SystemTransitionJob(ctx, job.ID, []model.JobStatus{model.JobQueued}, model.JobRunning, JobTransition{}); err != nil {
					t.Fatal(err)
				}
			}
			if test.status == model.JobAwaitingApproval {
				if _, err := database.SystemTransitionJob(ctx, job.ID, []model.JobStatus{model.JobRunning}, model.JobAwaitingApproval, JobTransition{}); err != nil {
					t.Fatal(err)
				}
			}
			if err := database.ForUser(testUserID).DeleteBookingRequest(ctx, booking.ID); !errors.Is(err, ErrConflict) {
				t.Fatalf("pending booking deletion error=%v", err)
			}
			retained, err := database.SystemGetBookingRequest(ctx, booking.ID)
			if err != nil || retained.Kind != model.BookingKindSaved || !retained.Enabled || !retained.ScheduleEnabled || retained.TargetDate != booking.TargetDate {
				t.Fatalf("rejected deletion changed booking: %+v err=%v", retained, err)
			}
			retainedJob, err := database.ForUser(testUserID).GetJob(ctx, job.ID)
			if err != nil || retainedJob.Status != test.status || retainedJob.CancelRequested {
				t.Fatalf("rejected deletion changed job: %+v err=%v", retainedJob, err)
			}
		})
	}
}

func TestDeleteBookingRetainsHistoryAndReservationGuards(t *testing.T) {
	for _, status := range []model.JobStatus{model.JobSucceeded, model.JobOutcomeUnknown} {
		t.Run(string(status), func(t *testing.T) {
			ctx := context.Background()
			database := ownedTestStore(t)
			resources := database.ForUser(testUserID)
			_, booking := fixtureProfileAndBooking(t, database, "retained-delete")
			booking.ScheduleEnabled = true
			booking, err := resources.UpdateBookingRequest(ctx, booking)
			if err != nil {
				t.Fatal(err)
			}
			job, err := resources.EnqueueJob(ctx, EnqueueJobParams{
				BookingRequestID: &booking.ID, Command: model.CommandBook, RunMode: model.RunModeManual,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.SystemTransitionJob(ctx, job.ID, []model.JobStatus{model.JobQueued}, model.JobRunning, JobTransition{}); err != nil {
				t.Fatal(err)
			}
			if _, err := database.SystemTransitionJob(ctx, job.ID, []model.JobStatus{model.JobRunning}, status, JobTransition{ConfirmationStarted: true}); err != nil {
				t.Fatal(err)
			}
			event, err := database.SystemAppendJobEvent(ctx, JobEventInput{JobID: job.ID, Kind: "result", Message: "Retained booking result"})
			if err != nil {
				t.Fatal(err)
			}
			if err := resources.DeleteBookingRequest(ctx, booking.ID); err != nil {
				t.Fatal(err)
			}
			visible, err := resources.ListSavedBookingRequests(ctx)
			if err != nil || len(visible) != 0 {
				t.Fatalf("deleted booking remains visible: %+v err=%v", visible, err)
			}
			retained, err := database.SystemGetBookingRequest(ctx, booking.ID)
			if err != nil || retained.Kind != model.BookingKindArchived || retained.Enabled || retained.ScheduleEnabled ||
				retained.ProfileID != booking.ProfileID || retained.TargetDate != booking.TargetDate || retained.VehicleKeyword != booking.VehicleKeyword {
				t.Fatalf("history inputs were not retained: %+v err=%v", retained, err)
			}
			retainedJob, err := resources.GetJob(ctx, job.ID)
			if err != nil || retainedJob.Status != status || retainedJob.BookingRequestID == nil || *retainedJob.BookingRequestID != booking.ID {
				t.Fatalf("job history was not retained: %+v err=%v", retainedJob, err)
			}
			events, err := resources.ListJobEvents(ctx, job.ID, 0, 10)
			if err != nil || len(events) != 1 || events[0].ID != event.ID {
				t.Fatalf("job events were not retained: %+v err=%v", events, err)
			}
			if err := resources.DeleteBookingRequest(ctx, booking.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("archived request could be deleted directly: %v", err)
			}
			forged := retained
			forged.Kind = model.BookingKindSaved
			forged.Enabled = true
			if _, err := resources.UpdateBookingRequest(ctx, forged); !errors.Is(err, ErrConflict) {
				t.Fatalf("archived request could be restored through a forged update: %v", err)
			}
			booking.ID = 0
			replacement, err := resources.CreateBookingRequest(ctx, booking)
			if err != nil {
				t.Fatalf("deleted booking's name was not reusable: %v", err)
			}
			params := EnqueueJobParams{BookingRequestID: &replacement.ID, Command: model.CommandBook, RunMode: model.RunModeManual}
			if _, err := resources.EnqueueJob(ctx, params); !errors.Is(err, ErrConflict) {
				t.Fatalf("deletion removed the reservation guard: %v", err)
			}
			if status == model.JobSucceeded {
				// History pruning must also remove the unused snapshot while the
				// durable profile/date reservation continues to prevent duplicates.
				if _, err := database.db.ExecContext(ctx, "DELETE FROM jobs WHERE id = ?", job.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := database.SystemGetBookingRequest(ctx, retained.ID); !errors.Is(err, ErrNotFound) {
					t.Fatalf("pruned history left an unused snapshot: %v", err)
				}
				if _, err := resources.EnqueueJob(ctx, params); !errors.Is(err, ErrConflict) {
					t.Fatalf("snapshot cleanup removed the reservation guard: %v", err)
				}
			}
		})
	}
}

func TestDeleteBookingWithHistoryFreesSavedBookingQuota(t *testing.T) {
	ctx := context.Background()
	database := ownedTestStore(t)
	resources := database.ForUser(testUserID)
	_, booking := fixtureProfileAndBooking(t, database, "delete-quota")
	job, err := resources.EnqueueJob(ctx, EnqueueJobParams{
		BookingRequestID: &booking.ID, Command: model.CommandDryRun, RunMode: model.RunModeDryRun,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := resources.RequestJobCancellation(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	for index := 1; index < MaxBookingRequestsPerUser; index++ {
		copy := booking
		copy.ID = 0
		copy.Name = fmt.Sprintf("saved booking %d", index)
		if _, err := resources.CreateBookingRequest(ctx, copy); err != nil {
			t.Fatal(err)
		}
	}
	additional := booking
	additional.ID = 0
	additional.Name = "over quota"
	if _, err := resources.CreateBookingRequest(ctx, additional); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("saved quota control error=%v", err)
	}
	if err := resources.DeleteBookingRequest(ctx, booking.ID); err != nil {
		t.Fatal(err)
	}
	additional.Name = booking.Name
	if _, err := resources.CreateBookingRequest(ctx, additional); err != nil {
		t.Fatalf("retained history consumed the removed booking's quota: %v", err)
	}
}

func TestBookingDeletionAndJobAdmissionAreAtomicAcrossStores(t *testing.T) {
	ctx := context.Background()
	database := ownedTestStore(t)
	_, booking := fixtureProfileAndBooking(t, database, "delete-race")
	peer, err := OpenMigrated(ctx, database.path, testEncryptor(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	start := make(chan struct{})
	deletion := make(chan error, 1)
	admission := make(chan error, 1)
	go func() {
		<-start
		deletion <- database.ForUser(testUserID).DeleteBookingRequest(ctx, booking.ID)
	}()
	go func() {
		<-start
		_, err := peer.ForUser(testUserID).EnqueueJob(ctx, EnqueueJobParams{
			BookingRequestID: &booking.ID, Command: model.CommandBook, RunMode: model.RunModeManual,
		})
		admission <- err
	}()
	close(start)
	deleteErr, enqueueErr := <-deletion, <-admission
	switch {
	case deleteErr == nil && errors.Is(enqueueErr, ErrNotFound):
		if jobs, err := database.ForUser(testUserID).ListJobs(ctx, 10); err != nil || len(jobs) != 0 {
			t.Fatalf("deletion admitted orphan work: %+v err=%v", jobs, err)
		}
	case errors.Is(deleteErr, ErrConflict) && enqueueErr == nil:
		if _, err := database.SystemGetBookingRequest(ctx, booking.ID); err != nil {
			t.Fatalf("admitted work lost its execution inputs: %v", err)
		}
	default:
		t.Fatalf("delete error=%v enqueue error=%v", deleteErr, enqueueErr)
	}
}
