package web

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/jaysqvl/lake-pass-bot/internal/destinations"
	"github.com/jaysqvl/lake-pass-bot/internal/model"
	"github.com/jaysqvl/lake-pass-bot/internal/store"
)

func latestQueuedBooking(t *testing.T, fixture webFixture, ownerID int64) model.BookingRequest {
	t.Helper()
	resources := fixture.store.ForUser(ownerID)
	jobs, err := resources.ListJobs(context.Background(), 1)
	if err != nil || len(jobs) != 1 || jobs[0].BookingRequestID == nil {
		t.Fatalf("queued booking missing: %+v err=%v", jobs, err)
	}
	booking, err := resources.GetBookingRequest(context.Background(), *jobs[0].BookingRequestID)
	if err != nil {
		t.Fatal(err)
	}
	return booking
}

func TestBookingUsesOwnedDefaultsAndKeepsExecutionSnapshots(t *testing.T) {
	fixture := newWebFixture(t)
	ctx := context.Background()
	profile, existing := createImmediateWebBooking(t, fixture, fixture.admin.ID, "Existing lake visit", true)
	lake, _ := destinations.Resolve(destinations.DefaultLakeID)
	settings := model.DefaultLakeSettings(lake.WithOrigin(fixture.cfg.YodelOrigins[0]))
	settings.Timezone, settings.ReleaseTime, settings.ReleaseDaysBefore = "Europe/London", "09:45", 3
	settings.AllDayPassURL, settings.HalfDayPassURL = "https://example.test/personal-all", "https://example.test/personal-half"
	settings.PreferredPasses = []model.PassType{model.PassMorning, model.PassAllDay}
	settings.VehicleKeyword = "Personal lake vehicle"
	account := model.DefaultAccountSettings()
	account.PrepMinutesBefore, account.AuthDeadlineMinutesBefore = 45, 10
	account.PollDeadlineSeconds, account.PollMinSeconds, account.PollMaxSeconds = 300, 0.05, 1.45
	resources := fixture.store.ForUser(fixture.admin.ID)
	if _, err := resources.SaveAccountSettings(ctx, account); err != nil {
		t.Fatal(err)
	}
	if _, err := resources.SaveLakeSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	member, err := fixture.store.CreateMember(ctx, store.CreateUserInput{Username: "other-lake-defaults", Password: "another long password"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, _ := createImmediateWebBooking(t, fixture, member.ID, "Private other account", true)
	cookies := loginCookies(t, fixture)
	page := serveForm(fixture, http.MethodGet, "/bookings/new?lake_id=buntzen", cookies, nil)
	if page.Code != http.StatusOK {
		t.Fatalf("booking form=%d %s", page.Code, page.Body.String())
	}
	assertBookingPassChoices(t, page.Body.String(), []string{"morning", "all_day", ""})
	if strings.Contains(page.Body.String(), foreign.Name) || !strings.Contains(page.Body.String(), profile.Name) {
		t.Fatal("booking connection did not stay scoped to the current account")
	}
	form := url.Values{
		"csrf_token": {csrfFrom(cookies)}, "lake_id": {"buntzen"}, "target_date": {"2030-07-20"},
		"pass_priority_1": {"morning"}, "pass_priority_2": {"all_day"},
		// Stale or crafted forms cannot move lake/account policy back into a visit.
		"profile_id": {stringID(foreign.ID)}, "vehicle_keyword": {"Untrusted vehicle"},
		"timezone": {"UTC"}, "release_time": {"12:00"}, "release_days_before": {"0"},
		"prep_minutes_before": {"not a number"}, "poll_max_seconds": {"60"},
		"all_day_pass_url": {"https://attacker.example/pass"}, "confirmation_mode": {"auto"},
	}
	response := serveForm(fixture, http.MethodPost, "/bookings/new", cookies, form)
	if response.Code != http.StatusSeeOther || !strings.HasPrefix(response.Header().Get("Location"), "/jobs/") {
		t.Fatalf("queue=%d %s", response.Code, response.Body.String())
	}
	snapshot := latestQueuedBooking(t, fixture, fixture.admin.ID)
	if snapshot.Kind != model.BookingKindSnapshot || snapshot.ProfileID != profile.ID || snapshot.Timezone != settings.Timezone ||
		snapshot.ReleaseTime != settings.ReleaseTime || snapshot.EffectiveReleaseDaysBefore() != 3 || snapshot.VehicleKeyword != settings.VehicleKeyword ||
		snapshot.AllDayPassURL != settings.AllDayPassURL || snapshot.PrepMinutesBefore != 45 || snapshot.PollMinSeconds != 0.05 ||
		snapshot.PollMaxSeconds != 1.45 || snapshot.ConfirmationMode != model.RunModeManual || !slices.Equal(snapshot.PassOrder(), settings.PreferredPasses) {
		t.Fatalf("visit escaped its owned saved defaults: %+v", snapshot)
	}
	settings.Timezone, settings.ReleaseDaysBefore, settings.VehicleKeyword = "UTC", 0, "Different lake vehicle"
	account.PrepMinutesBefore, account.PollMaxSeconds = 60, 2
	if _, err := resources.SaveLakeSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if _, err := resources.SaveAccountSettings(ctx, account); err != nil {
		t.Fatal(err)
	}
	retained, err := resources.GetBookingRequest(ctx, snapshot.ID)
	if err != nil || retained.Timezone != snapshot.Timezone || retained.VehicleKeyword != snapshot.VehicleKeyword || retained.PrepMinutesBefore != snapshot.PrepMinutesBefore || retained.PollMaxSeconds != snapshot.PollMaxSeconds {
		t.Fatalf("changing defaults altered queued execution: %+v err=%v", retained, err)
	}
	legacy, err := resources.GetBookingRequest(ctx, existing.ID)
	if err != nil || legacy.Timezone != existing.Timezone || legacy.VehicleKeyword != existing.VehicleKeyword || legacy.PrepMinutesBefore != existing.PrepMinutesBefore {
		t.Fatalf("changing defaults altered legacy saved request: %+v err=%v", legacy, err)
	}
	form.Set("target_date", "2030-07-21")
	response = serveForm(fixture, http.MethodPost, "/bookings/new", cookies, form)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("second visit=%d %s", response.Code, response.Body.String())
	}
	latest := latestQueuedBooking(t, fixture, fixture.admin.ID)
	if latest.ID == snapshot.ID || latest.Timezone != "UTC" || latest.EffectiveReleaseDaysBefore() != 0 || latest.VehicleKeyword != settings.VehicleKeyword || latest.PrepMinutesBefore != 60 || latest.PollMaxSeconds != 2 {
		t.Fatalf("next visit did not use changed defaults: %+v", latest)
	}
	otherCookies := loginCookiesAs(t, fixture, member.Username, "another long password")
	otherPage := serveForm(fixture, http.MethodGet, "/bookings/new?lake_id=buntzen", otherCookies, nil)
	if otherPage.Code != http.StatusOK || strings.Contains(otherPage.Body.String(), profile.Name) {
		t.Fatal("private connection leaked to another account")
	}
	form.Set("csrf_token", csrfFrom(otherCookies))
	response = serveForm(fixture, http.MethodPost, "/bookings/new", otherCookies, form)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("member visit=%d %s", response.Code, response.Body.String())
	}
	other := latestQueuedBooking(t, fixture, member.ID)
	if other.ProfileID != foreign.ID || other.Timezone != lake.Timezone || other.PrepMinutesBefore != 30 || other.PollMaxSeconds != 3.6 || other.VehicleKeyword != foreign.DefaultVehicle {
		t.Fatalf("personal defaults leaked to another account: %+v", other)
	}
}

func TestBookingConnectionCannotBeChosenThroughQueryParameters(t *testing.T) {
	fixture := newWebFixture(t)
	profile, _ := createImmediateWebBooking(t, fixture, fixture.admin.ID, "Selected connection", true)
	disabled, _ := createImmediateWebBooking(t, fixture, fixture.admin.ID, "Disabled connection", false)
	cookies := loginCookies(t, fixture)
	page := serveForm(fixture, http.MethodGet, "/bookings/new?lake_id=buntzen&profile_id="+stringID(disabled.ID), cookies, nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), profile.Name) || strings.Contains(page.Body.String(), disabled.Name) || strings.Contains(page.Body.String(), `name="profile_id"`) {
		t.Fatalf("query changed the lake's connection: %d %s", page.Code, page.Body.String())
	}
}
