package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
)

func TestEnqueueBookingRequestKeepsAnOwnedImmutableSnapshot(t *testing.T) {
	ctx := context.Background()
	database, ownerID, otherID := ownershipStore(t)
	_, _, saved := createOwnedResources(t, database, ownerID, "snapshot-owner")
	request := saved
	request.UserID = otherID
	request.Enabled = false
	request.ScheduleEnabled = true
	request.PreferredPasses = []model.PassType{model.PassMorning, model.PassAllDay}
	request.HalfDayPassURL = "https://example.test/half-day"
	resources := database.ForUser(ownerID)
	job, err := resources.EnqueueBookingRequest(ctx, request, EnqueueJobParams{
		Command: model.CommandBook, RunMode: model.RunModeManual,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.UserID != ownerID || job.BookingRequestID == nil || *job.BookingRequestID == saved.ID {
		t.Fatalf("job did not get its own owned snapshot: %+v", job)
	}
	snapshot, err := resources.GetBookingRequest(ctx, *job.BookingRequestID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Kind != model.BookingKindSnapshot || !snapshot.Enabled || snapshot.ScheduleEnabled || snapshot.Name == saved.Name ||
		snapshot.UserID != ownerID || snapshot.TargetDate != request.TargetDate || snapshot.VehicleKeyword != request.VehicleKeyword ||
		!reflect.DeepEqual(snapshot.PreferredPasses, request.PreferredPasses) {
		t.Fatalf("incorrect execution snapshot: %+v", snapshot)
	}
	if _, err := database.ForUser(otherID).GetBookingRequest(ctx, snapshot.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("snapshot is visible to another user: %v", err)
	}
	all, err := resources.ListBookingRequests(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("job presentation cannot read snapshot: %+v err=%v", all, err)
	}
	if err := resources.RequestJobCancellation(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	// A later submission receives separate inputs even if it carries an old ID.
	changed := snapshot
	changed.Kind = model.BookingKindSaved
	changed.TargetDate = "2031-01-16"
	changed.VehicleKeyword = "Another car"
	next, err := resources.EnqueueBookingRequest(ctx, changed, EnqueueJobParams{Command: model.CommandDryRun})
	if err != nil || *next.BookingRequestID == snapshot.ID {
		t.Fatalf("new visit reused old inputs: %+v %v", next, err)
	}
	retained, err := resources.GetBookingRequest(ctx, snapshot.ID)
	if err != nil || !reflect.DeepEqual(retained, snapshot) {
		t.Fatalf("later submission changed snapshot: %+v %v", retained, err)
	}

}

func TestEnqueueBookingRequestRollsBackRejectedAdmission(t *testing.T) {
	for _, scenario := range []string{"duplicate reservation", "full queue", "foreign profile", "invalid expiry", "disabled profile"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			database, ownerID, otherID := ownershipStore(t)
			_, profile, request := createOwnedResources(t, database, ownerID, "snapshot-rollback")
			resources := database.ForUser(ownerID)
			params := EnqueueJobParams{Command: model.CommandBook, RunMode: model.RunModeManual}
			wantErr := ErrConflict
			switch scenario {
			case "duplicate reservation":
				if _, err := resources.EnqueueBookingRequest(ctx, request, params); err != nil {
					t.Fatal(err)
				}
			case "full queue":
				for day := 1; day <= MaxPendingJobsPerUser; day++ {
					queued := request
					queued.TargetDate = fmt.Sprintf("2031-02-%02d", day)
					if _, err := resources.EnqueueBookingRequest(ctx, queued, params); err != nil {
						t.Fatal(err)
					}
				}
				wantErr = ErrResourceLimit
			case "foreign profile":
				_, foreign, _ := createOwnedResources(t, database, otherID, "snapshot-other")
				request.ProfileID = foreign.ID
				wantErr = ErrNotFound
			case "invalid expiry":
				expiry := time.Now().Add(-time.Minute)
				params.ExpiresAt = &expiry
				wantErr = nil
			case "disabled profile":
				if _, err := database.db.ExecContext(ctx, "UPDATE profiles SET enabled = 0 WHERE id = ?", profile.ID); err != nil {
					t.Fatal(err)
				}
				wantErr = ErrNotFound
			}
			beforeRequests, err := resources.ListBookingRequests(ctx)
			if err != nil {
				t.Fatal(err)
			}
			beforeJobs, err := resources.ListJobs(ctx, MaxRetainedJobsPerUser)
			if err != nil {
				t.Fatal(err)
			}
			_, err = resources.EnqueueBookingRequest(ctx, request, params)
			if err == nil || wantErr != nil && !errors.Is(err, wantErr) {
				t.Fatalf("enqueue error=%v, want %v", err, wantErr)
			}
			afterRequests, err := resources.ListBookingRequests(ctx)
			if err != nil || !reflect.DeepEqual(afterRequests, beforeRequests) {
				t.Fatalf("rejected admission changed requests: before=%+v after=%+v err=%v", beforeRequests, afterRequests, err)
			}
			afterJobs, err := resources.ListJobs(ctx, MaxRetainedJobsPerUser)
			if err != nil || !reflect.DeepEqual(afterJobs, beforeJobs) {
				t.Fatalf("rejected admission changed jobs: before=%+v after=%+v err=%v", beforeJobs, afterJobs, err)
			}
		})
	}
}

func TestConcurrentBookingSubmissionsKeepOnlyTheWinningSnapshot(t *testing.T) {
	ctx := context.Background()
	database := ownedTestStore(t)
	_, request := fixtureProfileAndBooking(t, database, "snapshot-race")
	peer, err := OpenMigrated(ctx, database.path, testEncryptor(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, connection := range []*Store{database, peer} {
		go func() {
			<-start
			_, err := connection.ForUser(testUserID).EnqueueBookingRequest(ctx, request, EnqueueJobParams{
				Command: model.CommandBook, RunMode: model.RunModeManual,
			})
			results <- err
		}()
	}
	close(start)
	accepted, rejected := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrConflict):
			rejected++
		default:
			t.Fatal(err)
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("accepted=%d rejected=%d", accepted, rejected)
	}
	requests, err := database.ForUser(testUserID).ListBookingRequests(ctx)
	if err != nil || len(requests) != 2 {
		t.Fatalf("rejected concurrent booking leaked a snapshot: %+v err=%v", requests, err)
	}
}

func TestSnapshotRetentionFollowsJobsWithoutUsingSavedRequestQuota(t *testing.T) {
	ctx := context.Background()
	database := ownedTestStore(t)
	_, request := fixtureProfileAndBooking(t, database, "snapshot-retention")
	resources := database.ForUser(testUserID)
	for index := 1; index < 64; index++ {
		saved := request
		saved.Name = fmt.Sprintf("Saved request %d", index)
		if _, err := resources.createLegacyBookingFixture(ctx, saved); err != nil {
			t.Fatal(err)
		}
	}
	var firstID, lastID int64
	for index := 0; index < MaxTerminalJobHistoryPerUser+5; index++ {
		job, err := resources.EnqueueBookingRequest(ctx, request, EnqueueJobParams{
			Command: model.CommandBook, RunMode: model.RunModeManual,
		})
		if err != nil {
			t.Fatalf("booking %d at saved-request limit: %v", index, err)
		}
		if index == 0 {
			firstID = *job.BookingRequestID
		}
		lastID = *job.BookingRequestID
		if err := resources.RequestJobCancellation(ctx, job.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := resources.GetBookingRequest(ctx, firstID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pruned job left an orphan snapshot: %v", err)
	}
	if _, err := resources.GetBookingRequest(ctx, lastID); err != nil {
		t.Fatalf("retained job lost its snapshot: %v", err)
	}
	requests, err := resources.ListBookingRequests(ctx)
	if err != nil || len(requests) != 64+MaxTerminalJobHistoryPerUser {
		t.Fatalf("retention requests=%d err=%v", len(requests), err)
	}
	var legacyCount int
	if err := database.db.QueryRowContext(ctx, "SELECT count(*) FROM booking_requests WHERE kind='saved'").Scan(&legacyCount); err != nil || legacyCount != 64 {
		t.Fatalf("retention changed legacy rows: %d %v", legacyCount, err)
	}

}

func TestSnapshotNamesPreserveUTF8AndFitTheNameLimit(t *testing.T) {
	ctx := context.Background()
	database := ownedTestStore(t)
	_, request := fixtureProfileAndBooking(t, database, "snapshot-name")
	request.Name = strings.Repeat("湖", 42)
	for range 2 {
		job, err := database.ForUser(testUserID).EnqueueBookingRequest(ctx, request, EnqueueJobParams{Command: model.CommandDryRun})
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := database.ForUser(testUserID).GetBookingRequest(ctx, *job.BookingRequestID)
		if err != nil || len(snapshot.Name) > model.MaxResourceNameBytes || !utf8.ValidString(snapshot.Name) {
			t.Fatalf("invalid snapshot name %q err=%v", snapshot.Name, err)
		}
	}
}
