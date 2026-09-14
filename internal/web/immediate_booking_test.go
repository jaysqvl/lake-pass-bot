package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
	"github.com/jaysqvl/lake-pass-bot/internal/store"
)

func createImmediateWebBooking(t *testing.T, fixture webFixture, ownerID int64, name string, enabled bool) (model.Profile, model.BookingRequest) {
	t.Helper()
	ctx := context.Background()
	resources := fixture.store.ForUser(ownerID)
	source, err := resources.CreateOTPSource(ctx, store.OTPSourceInput{Name: name + " inbox", Provider: model.OTPProviderTwilio, Identity: "twilio:" + name, ProviderConfig: map[string]string{"auth_token": "synthetic-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := resources.CreateProfile(ctx, store.ProfileInput{Name: name + " profile", DefaultVehicle: "Example Vehicle", LoginProbeURL: "https://example.test/login", OTPSourceID: source.ID, Headless: true, DefaultTimeoutMS: 15000, Enabled: enabled, Credentials: &model.ProfileCredentials{Phone: "5559876543"}})
	if err != nil {
		t.Fatal(err)
	}
	booking, err := createLegacyBooking(ctx, fixture, ownerID, model.BookingRequest{
		Name: name + " booking", ProfileID: profile.ID, Enabled: true, ScheduleEnabled: true,
		TargetDate: time.Now().UTC().Format(time.DateOnly), Timezone: "UTC", ReleaseTime: "07:00",
		PrepMinutesBefore: 30, AuthDeadlineMinutesBefore: 5, PollDeadlineSeconds: 120, PollMinSeconds: 1, PollMaxSeconds: 2,
		ConfirmationMode: model.RunModeAuto, AllDayPassURL: "https://example.test/all", CheckAllDay: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return profile, booking
}

func createReadyWebLake(t *testing.T, fixture webFixture, ownerID int64, name string) model.Profile {
	t.Helper()
	profile := createLakeBookingAccount(t, fixture, ownerID, name, "buntzen", true)
	settings := settingsPageDefaults(t, fixture)
	settings.VehicleKeyword, settings.Timezone = "Example vehicle", "UTC"
	if _, err := fixture.store.ForUser(ownerID).SaveLakeSettings(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	return profile
}

func TestBookNowHTTPForcesManualApprovalAndRejectsDuplicate(t *testing.T) {
	fixture := newWebFixture(t)
	createReadyWebLake(t, fixture, fixture.admin.ID, "immediate-http")
	settings := model.DefaultAccountSettings()
	settings.DefaultConfirmationMode = model.RunModeAuto
	if _, err := fixture.store.ForUser(fixture.admin.ID).SaveAccountSettings(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	cookies := loginCookies(t, fixture)
	form := url.Values{"csrf_token": {csrfFrom(cookies)}, "lake_id": {"buntzen"}, "target_date": {time.Now().UTC().Format(time.DateOnly)}, "pass_priority_1": {"all_day"}, "mode": {"auto"}}
	response := serveForm(fixture, http.MethodPost, "/bookings/new", cookies, form)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("book now response=%d body=%q", response.Code, response.Body.String())
	}
	jobs, err := fixture.store.ForUser(fixture.admin.ID).ListJobs(context.Background(), 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	job := jobs[0]
	if !job.RunImmediately || job.Command != model.CommandBook || job.RunMode != model.RunModeManual || job.ExpiresAt == nil || job.ExpiresAt.Sub(job.DueAt) != 15*time.Minute {
		t.Fatalf("posted automatic mode escaped manual immediate policy: %+v", job)
	}
	response = serveForm(fixture, http.MethodPost, "/bookings/new", cookies, form)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "already has a booking or pending job for that date") {
		t.Fatalf("duplicate response=%d", response.Code)
	}
}

func TestRetiredBookingRequestsHaveNoUIOrRoutesForAnyOwner(t *testing.T) {
	fixture := newWebFixture(t)
	_, booking := createImmediateWebBooking(t, fixture, fixture.admin.ID, "selected", true)
	member, err := fixture.store.CreateMember(context.Background(), store.CreateUserInput{Username: "another-owner", Password: "a long member password"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, foreignBooking := createImmediateWebBooking(t, fixture, member.ID, "foreign", true)
	cookies := loginCookies(t, fixture)
	page := serveForm(fixture, http.MethodGet, "/bookings", cookies, nil)
	for _, want := range []string{"Buntzen Lake", "/bookings/new?lake_id=buntzen"} {
		if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), want) {
			t.Fatalf("booking page missing %q: %d body=%q", want, page.Code, page.Body.String())
		}
	}
	for _, unwanted := range []string{booking.Name, foreign.Name, foreignBooking.Name, "Saved requests", "View request", "Delete saved request", `name="timing"`} {
		if strings.Contains(page.Body.String(), unwanted) {
			t.Fatalf("bookings retained old UI or exposed another account: %s", unwanted)
		}
	}
	form := url.Values{"csrf_token": {csrfFrom(cookies)}, "command": {"book"}, "timing": {"now"}}
	for _, request := range []model.BookingRequest{booking, foreignBooking} {
		for _, suffix := range []string{"", "/run", "/delete"} {
			method := http.MethodPost
			if suffix == "" {
				method = http.MethodGet
			}
			response := serveForm(fixture, method, fmt.Sprintf("/bookings/%d%s", request.ID, suffix), cookies, form)
			if response.Code != http.StatusNotFound {
				t.Fatalf("retired booking %d %s %s=%d", request.ID, method, suffix, response.Code)
			}
		}
		retained, err := fixture.store.ForUser(request.UserID).GetBookingRequest(context.Background(), request.ID)
		jobs, jobsErr := fixture.store.ForUser(request.UserID).ListJobs(context.Background(), 10)
		if err != nil || jobsErr != nil || !reflect.DeepEqual(retained, request) || len(jobs) != 0 {
			t.Fatalf("retired route changed records: request=%+v jobs=%+v errors=%v/%v", retained, jobs, err, jobsErr)
		}
	}
}

func TestExecutionSnapshotCannotBeReplayedThroughLegacyRunRoute(t *testing.T) {
	fixture := newWebFixture(t)
	createImmediateWebBooking(t, fixture, fixture.admin.ID, "snapshot replay", true)
	cookies := loginCookies(t, fixture)
	form := url.Values{
		"csrf_token": {csrfFrom(cookies)}, "lake_id": {"buntzen"}, "target_date": {"2030-07-20"},
		"pass_priority_1": {"all_day"},
	}
	response := serveForm(fixture, http.MethodPost, "/bookings/new", cookies, form)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("queue visit=%d %s", response.Code, response.Body.String())
	}
	snapshot := latestQueuedBooking(t, fixture, fixture.admin.ID)
	form = url.Values{"csrf_token": {csrfFrom(cookies)}, "command": {"dry-run"}}
	response = serveForm(fixture, http.MethodPost, fmt.Sprintf("/bookings/%d/run", snapshot.ID), cookies, form)
	if response.Code != http.StatusNotFound {
		t.Fatalf("execution snapshot replay status=%d", response.Code)
	}
	jobs, err := fixture.store.ForUser(fixture.admin.ID).ListJobs(context.Background(), 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("legacy route created another execution for a snapshot: %+v err=%v", jobs, err)
	}
}

func TestRetiredRequestRoutesPreservePendingAndCompletedJobHistory(t *testing.T) {
	fixture := newWebFixture(t)
	_, booking := createImmediateWebBooking(t, fixture, fixture.admin.ID, "action-state", true)
	ctx := context.Background()
	resources := fixture.store.ForUser(fixture.admin.ID)
	job, err := resources.EnqueueJob(ctx, store.EnqueueJobParams{BookingRequestID: &booking.ID, Command: model.CommandAuthCheck})
	if err != nil {
		t.Fatal(err)
	}
	cookies := loginCookies(t, fixture)
	form := url.Values{"csrf_token": {csrfFrom(cookies)}}
	for _, completed := range []bool{false, true} {
		if completed {
			if err := resources.RequestJobCancellation(ctx, job.ID); err != nil {
				t.Fatal(err)
			}
		}
		before, err := resources.GetJob(ctx, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		events, err := resources.ListJobEvents(ctx, job.ID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, suffix := range []string{"", "/delete", "/run"} {
			method := http.MethodPost
			if suffix == "" {
				method = http.MethodGet
			}
			response := serveForm(fixture, method, fmt.Sprintf("/bookings/%d%s", booking.ID, suffix), cookies, form)
			if response.Code != http.StatusNotFound {
				t.Fatalf("retired route %s=%d", suffix, response.Code)
			}
		}
		retained, err := resources.GetJob(ctx, job.ID)
		retainedEvents, eventsErr := resources.ListJobEvents(ctx, job.ID, 0, 100)
		if err != nil || eventsErr != nil || !reflect.DeepEqual(retained, before) || !reflect.DeepEqual(retainedEvents, events) {
			t.Fatalf("retired route changed job history: job=%+v events=%+v errors=%v/%v", retained, retainedEvents, err, eventsErr)
		}
		page := serveForm(fixture, http.MethodGet, fmt.Sprintf("/jobs/%d", job.ID), cookies, nil)
		if page.Code != http.StatusOK {
			t.Fatalf("job history inaccessible: %d %s", page.Code, page.Body.String())
		}
	}
}

func TestBookingAutomaticallySchedulesFutureDatesAndRejectsPastDates(t *testing.T) {
	for _, test := range []struct {
		name   string
		days   int
		status int
	}{
		{"unreleased", 2, http.StatusSeeOther},
		{"past", -1, http.StatusUnprocessableEntity},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWebFixture(t)
			createReadyWebLake(t, fixture, fixture.admin.ID, "booking-dates")
			cookies := loginCookies(t, fixture)
			form := url.Values{"csrf_token": {csrfFrom(cookies)}, "lake_id": {"buntzen"}, "target_date": {time.Now().UTC().AddDate(0, 0, test.days).Format(time.DateOnly)}, "pass_priority_1": {"all_day"}}
			response := serveForm(fixture, http.MethodPost, "/bookings/new", cookies, form)
			if response.Code != test.status {
				t.Fatalf("date response=%d: %s", response.Code, response.Body.String())
			}
			jobs, err := fixture.store.ForUser(fixture.admin.ID).ListJobs(context.Background(), 10)
			if err != nil {
				t.Fatal(err)
			}
			if test.days > 0 {
				if len(jobs) != 1 || jobs[0].RunImmediately || !jobs[0].DueAt.After(time.Now()) {
					t.Fatalf("future date was not scheduled: %+v", jobs)
				}
			} else if len(jobs) != 0 || !strings.Contains(response.Body.String(), "Choose today or a future visit date") || !strings.Contains(response.Body.String(), `role="alert"`) {
				t.Fatalf("past date was not rejected: jobs=%+v body=%s", jobs, response.Body.String())
			}
		})
	}
}
