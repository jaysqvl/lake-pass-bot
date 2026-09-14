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

func TestQuickBookingJourneyKeepsEachVisitsSettingsAndHistory(t *testing.T) {
	f := newWebFixture(t)
	ctx := context.Background()
	resources := f.store.ForUser(f.admin.ID)
	profile := createLakeBookingAccount(t, f, f.admin.ID, "My account", "buntzen", true)
	settings := settingsPageDefaults(t, f)
	settings.VehicleKeyword = "First vehicle"
	settings.Timezone = "America/Vancouver"
	if _, err := resources.SaveLakeSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	cookies := loginCookies(t, f)
	date := time.Now().UTC().AddDate(0, 0, 7).Format(time.DateOnly)
	form := url.Values{"csrf_token": {csrfFrom(cookies)}, "lake_id": {"buntzen"}, "target_date": {date}, "pass_priority_1": {"afternoon"}}
	response := serveForm(f, http.MethodPost, "/bookings/new", cookies, form)
	jobs, err := resources.ListJobs(ctx, 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%+v err=%v response=%d %s", jobs, err, response.Code, response.Body.String())
	}
	first := jobs[0]
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != fmt.Sprintf("/jobs/%d?ok=queued", first.ID) {
		t.Fatalf("did not open new job: %d %s", response.Code, response.Header().Get("Location"))
	}
	if first.ProfileID != profile.ID || first.BookingRequestID == nil || first.RunImmediately {
		t.Fatalf("wrong booking identity or timing: %+v", first)
	}
	response = serveForm(f, http.MethodPost, "/bookings/new", cookies, form)
	requests, err := resources.ListBookingRequests(ctx)
	if response.Code != http.StatusUnprocessableEntity || err != nil || len(requests) != 1 {
		t.Fatalf("duplicate left extra requests: response=%d requests=%+v err=%v", response.Code, requests, err)
	}
	settings.VehicleKeyword = "Second vehicle"
	if _, err := resources.SaveLakeSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	form.Set("target_date", time.Now().UTC().AddDate(0, 0, 8).Format(time.DateOnly))
	form.Set("pass_priority_1", "morning")
	response = serveForm(f, http.MethodPost, "/bookings/new", cookies, form)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("second date could not book: %d %s", response.Code, response.Body.String())
	}
	firstRequest, err := resources.GetBookingRequest(ctx, *first.BookingRequestID)
	if err != nil || firstRequest.Kind != model.BookingKindSnapshot || firstRequest.VehicleKeyword != "First vehicle" || firstRequest.PassOrder()[0] != model.PassAfternoon {
		t.Fatalf("later defaults altered queued visit: %+v, %v", firstRequest, err)
	}
	page := serveForm(f, http.MethodGet, "/", cookies, nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), fmt.Sprintf(`href="/jobs/%d"`, first.ID)) || strings.Contains(page.Body.String(), firstRequest.Name) {
		t.Fatalf("upcoming visit lacks job link or leaked internal name: %d %s", page.Code, page.Body.String())
	}
	for _, status := range []model.JobStatus{model.JobRunning, model.JobSucceeded} {
		first, err = f.store.SystemTransitionJob(ctx, first.ID, []model.JobStatus{first.Status}, status, store.JobTransition{})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/jobs", fmt.Sprintf("/jobs/%d", first.ID)} {
		page = serveForm(f, http.MethodGet, path, cookies, nil)
		if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), date) || strings.Contains(page.Body.String(), firstRequest.Name) {
			t.Fatalf("snapshot history is not readable at %s: %d %s", path, page.Code, page.Body.String())
		}
	}
	page = serveForm(f, http.MethodGet, "/bookings", cookies, nil)
	if page.Code != http.StatusOK || strings.Contains(page.Body.String(), "Saved requests") || strings.Contains(page.Body.String(), firstRequest.Name) {
		t.Fatalf("executions became extra saved profiles: %d %s", page.Code, page.Body.String())
	}
	for _, path := range []string{fmt.Sprintf("/bookings/%d", firstRequest.ID), fmt.Sprintf("/bookings/%d/run", firstRequest.ID)} {
		method := http.MethodGet
		if strings.HasSuffix(path, "/run") {
			method = http.MethodPost
		}
		page = serveForm(f, method, path, cookies, form)
		if page.Code != http.StatusNotFound {
			t.Fatalf("snapshot exposed editable/replay route %s: %d", path, page.Code)
		}
	}
}
