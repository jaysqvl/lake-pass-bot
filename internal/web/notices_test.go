package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
	"github.com/jaysqvl/lake-pass-bot/internal/store"
)

func TestBookingFailureExplainsRetainedConfirmationWithoutAnotherJob(t *testing.T) {
	fixture := newWebFixture(t)
	ctx := context.Background()
	resources := fixture.store.ForUser(fixture.admin.ID)
	_, booking := createImmediateWebBooking(t, fixture, fixture.admin.ID, "retained-notice", true)
	booking.TargetDate = "2030-09-10"
	job, err := fixture.server.engine.QueueLakeBooking(ctx, fixture.admin.ID, booking)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.SystemTransitionJob(ctx, job.ID, []model.JobStatus{model.JobQueued}, model.JobRunning, store.JobTransition{}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SystemMarkConfirmationStarted(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.SystemTransitionJob(ctx, job.ID, []model.JobStatus{model.JobRunning}, model.JobOutcomeUnknown, store.JobTransition{}); err != nil {
		t.Fatal(err)
	}
	cookies := loginCookies(t, fixture)
	response := serveForm(fixture, http.MethodPost, "/bookings/new", cookies,
		url.Values{"csrf_token": {csrfFrom(cookies)}, "lake_id": {"buntzen"}, "target_date": {booking.TargetDate}, "pass_priority_1": {"all_day"}})
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "already has a booking or pending job for that date") || !strings.Contains(response.Body.String(), `role="alert"`) {
		t.Fatalf("retained confirmation response=%d body=%s", response.Code, response.Body.String())
	}
	if jobs, err := resources.ListJobs(ctx, 10); err != nil || len(jobs) != 1 || jobs[0].Status != model.JobOutcomeUnknown {
		t.Fatalf("notification changed retained booking protection: jobs=%+v err=%v", jobs, err)
	}
}

func TestFullQueueDoesNotClaimThatBookingIsAlreadyQueued(t *testing.T) {
	fixture := newWebFixture(t)
	ctx := context.Background()
	profile, _ := createImmediateWebBooking(t, fixture, fixture.admin.ID, "capacity-notice", true)
	for range store.MaxPendingJobsPerUser {
		if _, err := fixture.store.ForUser(fixture.admin.ID).EnqueueJob(ctx, store.EnqueueJobParams{
			ProfileID: profile.ID, Command: model.CommandAuthCheck, DueAt: time.Now().Add(time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	cookies := loginCookies(t, fixture)
	response := serveForm(fixture, http.MethodPost, "/bookings/new", cookies,
		url.Values{"csrf_token": {csrfFrom(cookies)}, "lake_id": {"buntzen"}, "target_date": {"2030-09-10"}, "pass_priority_1": {"all_day"}})
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "Your job queue is full") || strings.Contains(response.Body.String(), "already has a booking") {
		t.Fatalf("capacity notification=%d %s", response.Code, response.Body.String())
	}
	jobs, err := fixture.store.ForUser(fixture.admin.ID).ListJobs(ctx, 100)
	if err != nil || len(jobs) != store.MaxPendingJobsPerUser {
		t.Fatalf("full queue changed after rejected booking: jobs=%+v err=%v", jobs, err)
	}
}

func TestNotificationCannotLinkAnotherOwnersJobOrEchoArbitraryText(t *testing.T) {
	fixture := newWebFixture(t)
	ctx := context.Background()
	member, err := fixture.store.CreateMember(ctx, store.CreateUserInput{Username: "notice-member", Password: "long member password"})
	if err != nil {
		t.Fatal(err)
	}
	_, booking := createImmediateWebBooking(t, fixture, member.ID, "private-notice", true)
	job, err := fixture.server.engine.QueueLakeBooking(ctx, member.ID, booking)
	if err != nil {
		t.Fatal(err)
	}
	cookies := loginCookies(t, fixture)
	for _, code := range []string{"queue-pending", "queue-review", `<script>alert("injected")</script>`} {
		query := url.Values{"notice": {code}, "job": {fmt.Sprint(job.ID)}, "return_to": {"https://attacker.invalid/"}}
		page := serveForm(fixture, http.MethodGet, "/bookings?"+query.Encode(), cookies, nil)
		if page.Code != http.StatusOK {
			t.Fatalf("notice page=%d", page.Code)
		}
		for _, unwanted := range []string{fmt.Sprintf(`href="/jobs/%d"`, job.ID), "View existing job", "private-notice", "attacker.invalid", "injected"} {
			if strings.Contains(page.Body.String(), unwanted) {
				t.Fatalf("notification exposed %q", unwanted)
			}
		}
	}
}
