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
	booking, err := resources.CreateBookingRequest(ctx, model.BookingRequest{
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

func TestBookNowHTTPForcesManualApprovalAndRejectsDuplicate(t *testing.T) {
	fixture := newWebFixture(t)
	_, booking := createImmediateWebBooking(t, fixture, fixture.admin.ID, "immediate-http", true)
	cookies := loginCookies(t, fixture)
	form := url.Values{"csrf_token": {csrfFrom(cookies)}, "command": {"book"}, "timing": {"now"}, "mode": {"auto"}}
	path := fmt.Sprintf("/bookings/%d/run", booking.ID)
	response := serveForm(fixture, http.MethodPost, path, cookies, form)
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
	response = serveForm(fixture, http.MethodPost, path, cookies, form)
	if response.Code != http.StatusSeeOther || !strings.Contains(response.Header().Get("Location"), "notice=queue-pending") {
		t.Fatalf("duplicate response=%d", response.Code)
	}
	for _, invalid := range []url.Values{
		{"csrf_token": {csrfFrom(cookies)}, "command": {"auth-check"}, "timing": {"now"}},
		{"csrf_token": {csrfFrom(cookies)}, "command": {"book"}, "timing": {"whenever"}},
	} {
		response = serveForm(fixture, http.MethodPost, path, cookies, invalid)
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/bookings?notice=booking-action" {
			t.Fatalf("invalid timing response=%d body=%q", response.Code, response.Body.String())
		}
	}
}

func TestBookingPagesKeepLegacyRequestsAndActionsOwnerScoped(t *testing.T) {
	fixture := newWebFixture(t)
	_, booking := createImmediateWebBooking(t, fixture, fixture.admin.ID, "selected", true)
	member, err := fixture.store.CreateMember(context.Background(), store.CreateUserInput{Username: "another-owner", Password: "a long member password"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, foreignBooking := createImmediateWebBooking(t, fixture, member.ID, "foreign", true)
	cookies := loginCookies(t, fixture)
	page := serveForm(fixture, http.MethodGet, "/bookings", cookies, nil)
	for _, want := range []string{"Buntzen Lake", "/bookings/new?lake_id=buntzen", booking.Name, fmt.Sprintf(`href="/bookings/%d"`, booking.ID)} {
		if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), want) {
			t.Fatalf("booking page missing %q: %d body=%q", want, page.Code, page.Body.String())
		}
	}
	if strings.Contains(page.Body.String(), foreign.Name) || strings.Contains(page.Body.String(), foreignBooking.Name) || strings.Contains(page.Body.String(), `name="timing"`) {
		t.Fatal("bookings exposed another account or retained the old direct-run form")
	}
	form := url.Values{"csrf_token": {csrfFrom(cookies)}, "command": {"book"}, "timing": {"now"}}
	for _, suffix := range []string{"/run", "/delete"} {
		response := serveForm(fixture, http.MethodPost, fmt.Sprintf("/bookings/%d%s", foreignBooking.ID, suffix), cookies, form)
		if response.Code != http.StatusNotFound {
			t.Fatalf("foreign booking %s POST=%d", suffix, response.Code)
		}
	}
	page = serveForm(fixture, http.MethodGet, fmt.Sprintf("/bookings/%d", foreignBooking.ID), cookies, nil)
	if page.Code != http.StatusNotFound {
		t.Fatalf("foreign saved request GET=%d", page.Code)
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

func TestSavedBookingDetailsExplainPendingDeletionAndRetainHistory(t *testing.T) {
	fixture := newWebFixture(t)
	_, booking := createImmediateWebBooking(t, fixture, fixture.admin.ID, "action-state", true)
	ctx := context.Background()
	resources := fixture.store.ForUser(fixture.admin.ID)
	job, err := resources.EnqueueJob(ctx, store.EnqueueJobParams{BookingRequestID: &booking.ID, Command: model.CommandAuthCheck})
	if err != nil {
		t.Fatal(err)
	}
	cookies := loginCookies(t, fixture)
	path := fmt.Sprintf("/bookings/%d", booking.ID)
	page := serveForm(fixture, http.MethodGet, path, cookies, nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), booking.Name) || !strings.Contains(page.Body.String(), "before deleting this request") || !strings.Contains(page.Body.String(), `type="submit" disabled`) || !strings.Contains(page.Body.String(), fmt.Sprintf(`href="/jobs/%d"`, job.ID)) {
		t.Fatalf("pending request deletion was not explained: %d %s", page.Code, page.Body.String())
	}
	form := url.Values{"csrf_token": {csrfFrom(cookies)}}
	response := serveForm(fixture, http.MethodPost, path+"/delete", cookies, form)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "pending job") {
		t.Fatalf("pending deletion did not explain the conflict: %d %s", response.Code, response.Body.String())
	}
	if err := resources.RequestJobCancellation(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	page = serveForm(fixture, http.MethodGet, path, cookies, nil)
	if page.Code != http.StatusOK || strings.Contains(page.Body.String(), `type="submit" disabled`) {
		t.Fatalf("completed job still blocked deleting saved request: %d %s", page.Code, page.Body.String())
	}
	response = serveForm(fixture, http.MethodPost, path+"/delete", cookies, form)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("delete completed request=%d %s", response.Code, response.Body.String())
	}
	if retained, err := resources.GetJob(ctx, job.ID); err != nil || retained.Status != model.JobCancelled {
		t.Fatalf("deleting request changed completed job: %+v err=%v", retained, err)
	}
	page = serveForm(fixture, http.MethodGet, path, cookies, nil)
	if page.Code != http.StatusNotFound {
		t.Fatalf("removed request remained accessible as saved settings: %d", page.Code)
	}
}

func TestBookNowExplainsUnreleasedAndExpiredDates(t *testing.T) {
	fixture := newWebFixture(t)
	_, booking := createImmediateWebBooking(t, fixture, fixture.admin.ID, "immediate-dates", true)
	cookies := loginCookies(t, fixture)
	form := url.Values{"csrf_token": {csrfFrom(cookies)}, "command": {"book"}, "timing": {"now"}}
	for _, test := range []struct {
		name    string
		days    int
		message string
	}{
		{"unreleased", 2, "Queue for release instead"},
		{"past", -1, "Choose today or a future date"},
	} {
		t.Run(test.name, func(t *testing.T) {
			booking.TargetDate = time.Now().UTC().AddDate(0, 0, test.days).Format(time.DateOnly)
			var err error
			booking, err = fixture.store.ForUser(fixture.admin.ID).UpdateBookingRequest(context.Background(), booking)
			if err != nil {
				t.Fatal(err)
			}
			response := serveForm(fixture, http.MethodPost, fmt.Sprintf("/bookings/%d/run", booking.ID), cookies, form)
			if response.Code != http.StatusSeeOther {
				t.Fatalf("date response=%d", response.Code)
			}
			page := serveForm(fixture, http.MethodGet, response.Header().Get("Location"), cookies, nil)
			if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), test.message) || !strings.Contains(page.Body.String(), `role="alert"`) {
				t.Fatalf("date notification=%d body=%q", page.Code, page.Body.String())
			}
		})
	}
	jobs, err := fixture.store.ForUser(fixture.admin.ID).ListJobs(context.Background(), 10)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("invalid dates queued jobs=%+v err=%v", jobs, err)
	}
}
