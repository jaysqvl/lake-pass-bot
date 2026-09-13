package model

import (
	"errors"
	"testing"

	"github.com/jaysqvl/lake-pass-bot/internal/destinations"
)

func TestResolveLakeBookingProfile(t *testing.T) {
	lake, _ := destinations.Resolve("")
	first := Profile{ID: 1, UserID: 7, LakeID: lake.ID, ProviderID: lake.ProviderID, Enabled: true}
	second := first
	second.ID = 2
	disabled := first
	disabled.Enabled = false
	foreign := first
	foreign.UserID = 8
	otherLake := first
	otherLake.LakeID = "other-lake"
	otherProvider := first
	otherProvider.ProviderID = "other-provider"
	for _, test := range []struct {
		name      string
		preferred int64
		profiles  []Profile
		wantID    int64
		wantErr   error
	}{
		{"sole connected account", 0, []Profile{first}, 1, nil},
		{"explicit account", 2, []Profile{first, second}, 2, nil},
		{"no connection", 0, nil, 0, ErrLakeConnectionRequired},
		{"ambiguous accounts", 0, []Profile{first, second}, 0, ErrLakeConnectionAmbiguous},
		{"disabled choice does not switch accounts", 1, []Profile{disabled, second}, 0, ErrLakeConnectionUnavailable},
		{"deleted choice does not switch accounts", 1, []Profile{second}, 0, ErrLakeConnectionUnavailable},
		{"foreign account is not eligible", 0, []Profile{foreign}, 0, ErrLakeConnectionRequired},
		{"explicit foreign account is rejected", 1, []Profile{foreign}, 0, ErrLakeConnectionUnavailable},
		{"another lake is not connected", 0, []Profile{otherLake}, 0, ErrLakeConnectionRequired},
		{"another provider is incompatible", 0, []Profile{otherProvider}, 0, ErrLakeConnectionRequired},
		{"ineligible accounts do not cause ambiguity", 0, []Profile{foreign, otherLake, disabled, second}, 2, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := DefaultLakeSettings(lake)
			settings.UserID, settings.BookingProfileID = 7, test.preferred
			profile, err := ResolveLakeBookingProfile(lake, settings, test.profiles)
			if profile.ID != test.wantID || !errors.Is(err, test.wantErr) {
				t.Fatalf("resolved profile %d, error %v; want %d, %v", profile.ID, err, test.wantID, test.wantErr)
			}
		})
	}
}

func TestAccountConfirmationPreferenceDoesNotRewriteSavedBooking(t *testing.T) {
	settings := DefaultAccountSettings()
	if settings.DefaultConfirmationMode != RunModeManual {
		t.Fatal("new accounts must default to manual approval")
	}
	settings.DefaultConfirmationMode = RunModeAuto
	if err := settings.Validate(); err != nil {
		t.Fatal(err)
	}
	booking := settings.ApplyToBooking(BookingRequest{ConfirmationMode: RunModeManual})
	if booking.ConfirmationMode != RunModeManual {
		t.Fatal("applying timing defaults rewrote the saved confirmation policy")
	}
	settings.DefaultConfirmationMode = "invalid"
	if err := settings.Validate(); err == nil {
		t.Fatal("invalid confirmation mode accepted")
	}
}
