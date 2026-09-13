package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
	"github.com/jaysqvl/lake-pass-bot/internal/store"
)

func createLakeBookingAccount(t *testing.T, f webFixture, userID int64, name, lakeID string, enabled bool) model.Profile {
	t.Helper()
	source := createDefaultSignInSource(t, f, userID, name+" inbox")
	profile, err := f.store.ForUser(userID).CreateProfile(context.Background(), store.ProfileInput{
		Name: name, LakeID: lakeID, LoginProbeURL: "https://example.test/login", OTPSourceID: source.ID,
		Headless: true, DefaultTimeoutMS: 15000, Enabled: enabled,
		Credentials: &model.ProfileCredentials{Phone: "5559876543"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func TestLakeBookingAccountChoiceAndPreferenceEdits(t *testing.T) {
	f := newWebFixture(t)
	ctx := context.Background()
	resources := f.store.ForUser(f.admin.ID)
	first := createLakeBookingAccount(t, f, f.admin.ID, "First account", "buntzen", true)
	cookies := loginCookies(t, f)
	page := serveForm(f, http.MethodGet, "/lakes/buntzen", cookies, nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Used for bookings") || strings.Contains(page.Body.String(), "Use for bookings</button>") {
		t.Fatalf("sole account should need no choice: %d %s", page.Code, page.Body.String())
	}
	second := createLakeBookingAccount(t, f, f.admin.ID, "Second account", "buntzen", true)
	page = serveForm(f, http.MethodGet, "/lakes/buntzen", cookies, nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), model.ErrLakeConnectionAmbiguous.Error()) || strings.Count(page.Body.String(), "Use for bookings</button>") != 2 {
		t.Fatalf("multiple accounts should ask for a choice: %d %s", page.Code, page.Body.String())
	}
	selection := url.Values{"csrf_token": {csrfFrom(cookies)}, "booking_profile_id": {strconv.FormatInt(second.ID, 10)}}
	response := serveForm(f, http.MethodPost, "/lakes/buntzen/connection", cookies, selection)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/lakes/buntzen?ok=updated#connection" {
		t.Fatalf("choose account: %d %s", response.Code, response.Body.String())
	}
	saved, err := resources.GetLakeSettings(ctx, "buntzen")
	if err != nil || saved.BookingProfileID != second.ID {
		t.Fatalf("choice did not persist: %+v, %v", saved, err)
	}
	saved.VehicleKeyword = "Personal car"
	values := lakeSettingsPageValues(saved)
	values.Set("csrf_token", csrfFrom(cookies))
	values.Set("booking_profile_id", strconv.FormatInt(first.ID, 10)) // This form does not select a connection.
	response = serveForm(f, http.MethodPost, "/lakes/buntzen", cookies, values)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("save preferences: %d %s", response.Code, response.Body.String())
	}
	saved, err = resources.GetLakeSettings(ctx, "buntzen")
	if err != nil || saved.BookingProfileID != second.ID || saved.VehicleKeyword != "Personal car" {
		t.Fatalf("preference edit changed chosen account: %+v, %v", saved, err)
	}
	response = serveForm(f, http.MethodPost, "/lakes/buntzen/reset", cookies, url.Values{"csrf_token": {csrfFrom(cookies)}})
	if response.Code != http.StatusSeeOther {
		t.Fatalf("reset preferences: %d %s", response.Code, response.Body.String())
	}
	saved, err = resources.GetLakeSettings(ctx, "buntzen")
	if err != nil || saved.BookingProfileID != second.ID || saved.VehicleKeyword != "" {
		t.Fatalf("reset changed chosen account or retained vehicle override: %+v, %v", saved, err)
	}
	page = serveForm(f, http.MethodGet, "/lakes/buntzen", cookies, nil)
	if page.Code != http.StatusOK || strings.Contains(page.Body.String(), model.ErrLakeConnectionAmbiguous.Error()) || strings.Count(page.Body.String(), "Used for bookings") != 1 {
		t.Fatalf("chosen account is not clearly identified: %d %s", page.Code, page.Body.String())
	}
}

func TestLakeBookingAccountRejectsUnrelatedDisabledAndUnprotectedChoices(t *testing.T) {
	f := newWebFixture(t)
	ctx := context.Background()
	current := createLakeBookingAccount(t, f, f.admin.ID, "Current account", "buntzen", true)
	disabled := createLakeBookingAccount(t, f, f.admin.ID, "Disabled account", "buntzen", false)
	otherLake := createLakeBookingAccount(t, f, f.admin.ID, "Other lake account", "another-lake", true)
	member, err := f.store.CreateMember(ctx, store.CreateUserInput{Username: "booking-choice-member", Password: "synthetic booking choice password"})
	if err != nil {
		t.Fatal(err)
	}
	foreign := createLakeBookingAccount(t, f, member.ID, "Foreign account", "buntzen", true)
	settings := settingsPageDefaults(t, f)
	settings.BookingProfileID = current.ID
	if _, err := f.store.ForUser(f.admin.ID).SaveLakeSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	cookies := loginCookies(t, f)
	for _, id := range []int64{disabled.ID, otherLake.ID, foreign.ID, 0, -1, 999999} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			response := serveForm(f, http.MethodPost, "/lakes/buntzen/connection", cookies, url.Values{
				"csrf_token": {csrfFrom(cookies)}, "booking_profile_id": {strconv.FormatInt(id, 10)},
			})
			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("invalid account accepted: %d %s", response.Code, response.Body.String())
			}
		})
	}
	response := serveForm(f, http.MethodPost, "/lakes/buntzen/connection", cookies, url.Values{"booking_profile_id": {strconv.FormatInt(current.ID, 10)}})
	if response.Code != http.StatusForbidden {
		t.Fatalf("account choice bypassed CSRF: %d", response.Code)
	}
	saved, err := f.store.ForUser(f.admin.ID).GetLakeSettings(ctx, "buntzen")
	if err != nil || saved.BookingProfileID != current.ID {
		t.Fatalf("rejected request changed account choice: %+v, %v", saved, err)
	}
}

func TestGlobalBookingConfirmationPreference(t *testing.T) {
	f := newWebFixture(t)
	cookies := loginCookies(t, f)
	page := serveForm(f, http.MethodGet, "/settings", cookies, nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `name="default_confirmation_mode"`) || !strings.Contains(page.Body.String(), `value="manual" selected`) {
		t.Fatalf("manual confirmation default missing: %d %s", page.Code, page.Body.String())
	}
	settings := model.DefaultAccountSettings()
	settings.DefaultConfirmationMode = model.RunModeAuto
	values := accountSettingsPageValues(settings)
	values.Set("csrf_token", csrfFrom(cookies))
	response := serveForm(f, http.MethodPost, "/settings", cookies, values)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("save automatic confirmation preference: %d %s", response.Code, response.Body.String())
	}
	saved, err := f.store.ForUser(f.admin.ID).GetAccountSettings(context.Background())
	if err != nil || saved.DefaultConfirmationMode != model.RunModeAuto {
		t.Fatalf("automatic preference did not persist: %+v, %v", saved, err)
	}
	values.Set("default_confirmation_mode", "invalid")
	response = serveForm(f, http.MethodPost, "/settings", cookies, values)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid confirmation preference accepted: %d", response.Code)
	}
	saved, err = f.store.ForUser(f.admin.ID).GetAccountSettings(context.Background())
	if err != nil || saved.DefaultConfirmationMode != model.RunModeAuto {
		t.Fatalf("invalid save changed confirmation preference: %+v, %v", saved, err)
	}
}
