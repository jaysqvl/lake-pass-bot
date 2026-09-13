package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
)

func TestSavedRequestAndJobPagesSeparateSchedulingFromQueuedWork(t *testing.T) {
	fixture := newWebFixture(t)
	ctx := context.Background()
	_, booking := createImmediateWebBooking(t, fixture, fixture.admin.ID, "queue-status", true)
	booking.Timezone = "America/Vancouver"
	booking.TargetDate = "2030-09-10"
	var err error
	booking, err = fixture.store.ForUser(fixture.admin.ID).UpdateBookingRequest(ctx, booking)
	if err != nil {
		t.Fatal(err)
	}
	cookies := loginCookies(t, fixture)
	form := serveForm(fixture, http.MethodGet, fmt.Sprintf("/bookings/%d", booking.ID), cookies, nil)
	for _, want := range []string{"Enabled when server scheduling is on", "Book another day", "Delete saved request"} {
		if form.Code != http.StatusOK || !strings.Contains(form.Body.String(), want) {
			t.Fatalf("booking form missing %q: %d %s", want, form.Code, form.Body.String())
		}
	}
	before := serveForm(fixture, http.MethodGet, "/", cookies, nil)
	if before.Code != http.StatusOK || !strings.Contains(before.Body.String(), "Auto-queueing is off for this server") || !strings.Contains(before.Body.String(), "Already queued jobs remain scheduled") {
		t.Fatalf("server scheduling notice: %d %s", before.Code, before.Body.String())
	}
	job, err := fixture.server.engine.QueueBooking(ctx, fixture.admin.ID, booking.ID, model.CommandBook, "")
	if err != nil {
		t.Fatal(err)
	}
	page := serveForm(fixture, http.MethodGet, fmt.Sprintf("/jobs/%d", job.ID), cookies, nil)
	for _, want := range []string{"Waiting to start", "Earliest start", "Mon, Sep 9, 2030 at 6:30 AM", "Automatic final confirmation", "Release window"} {
		if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), want) {
			t.Fatalf("queued booking missing %q: %d %s", want, page.Code, page.Body.String())
		}
	}
	if strings.Contains(page.Body.String(), "No booking queued") || strings.Contains(page.Body.String(), "Schedule paused") {
		t.Fatal("queued booking still described as absent or paused")
	}
	unchanged, err := fixture.store.ForUser(fixture.admin.ID).GetJob(ctx, job.ID)
	if err != nil || unchanged.Status != model.JobQueued || unchanged.RunMode != model.RunModeAuto || !unchanged.DueAt.Equal(time.Date(2030, 9, 9, 13, 30, 0, 0, time.UTC)) {
		t.Fatalf("viewing the page changed the saved release job: %+v err=%v", unchanged, err)
	}
	if err := fixture.store.ForUser(fixture.admin.ID).RequestJobCancellation(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	page = serveForm(fixture, http.MethodGet, fmt.Sprintf("/bookings/%d", booking.ID), cookies, nil)
	if page.Code != http.StatusOK || strings.Contains(page.Body.String(), "View pending job") || strings.Contains(page.Body.String(), fmt.Sprintf(`href="/jobs/%d"`, job.ID)) {
		t.Fatal("cancelled job still blocks the saved request")
	}
}

func TestJobDetailsUseImmediateJobsSavedManualMode(t *testing.T) {
	fixture := newWebFixture(t)
	_, booking := createImmediateWebBooking(t, fixture, fixture.admin.ID, "manual-status", true)
	job, err := fixture.server.engine.QueueBookingNow(context.Background(), fixture.admin.ID, booking.ID)
	if err != nil {
		t.Fatal(err)
	}
	page := serveForm(fixture, http.MethodGet, fmt.Sprintf("/jobs/%d", job.ID), loginCookies(t, fixture), nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Manual final approval") || strings.Contains(page.Body.String(), "Automatic final confirmation") || !strings.Contains(page.Body.String(), "Book now · manual approval") {
		t.Fatalf("immediate job shown with wrong confirmation policy: %d %s", page.Code, page.Body.String())
	}
}
