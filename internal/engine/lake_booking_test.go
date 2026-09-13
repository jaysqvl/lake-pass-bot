package engine

import (
	"errors"
	"testing"
	"time"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
	"github.com/jaysqvl/lake-pass-bot/internal/scheduler"
)

func TestLakeBookingChoosesTimingAtReleaseBoundary(t *testing.T) {
	request := model.BookingRequest{
		Name: "Example visit", ProfileID: 1, VehicleKeyword: "Example vehicle",
		TargetDate: "2031-06-15", Timezone: "America/Vancouver", ReleaseTime: "07:00",
		PrepMinutesBefore: 30, AuthDeadlineMinutesBefore: 5, PollDeadlineSeconds: 120,
		PollMinSeconds: 1, PollMaxSeconds: 2, AllDayPassURL: "https://example.test/all-day", CheckAllDay: true,
		ConfirmationMode: model.RunModeAuto,
	}
	window, err := scheduler.WindowFor(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		now       time.Time
		immediate bool
		mode      model.RunMode
	}{
		{"before preparation", window.PrepAt.Add(-time.Hour), false, model.RunModeAuto},
		{"during preparation", window.ReleaseAt.Add(-time.Minute), false, model.RunModeAuto},
		{"exact release", window.ReleaseAt, true, model.RunModeManual},
		{"after release window", window.PollEndsAt.Add(time.Hour), true, model.RunModeManual},
	} {
		t.Run(test.name, func(t *testing.T) {
			params, err := lakeBookingParams(request, test.now)
			if err != nil {
				t.Fatal(err)
			}
			if params.BookingRequestID != nil || params.Command != model.CommandBook || params.RunImmediately != test.immediate || params.RunMode != test.mode {
				t.Fatalf("wrong booking policy: %+v", params)
			}
			if test.immediate {
				if !params.DueAt.Equal(test.now) || params.ExpiresAt == nil || !params.ExpiresAt.Equal(test.now.Add(15*time.Minute)) {
					t.Fatalf("immediate deadline: %+v", params)
				}
			} else if params.DueAt.Before(window.PrepAt) || params.ExpiresAt == nil || !params.ExpiresAt.Equal(window.PollEndsAt) {
				t.Fatalf("release window: %+v", params)
			}
		})
	}
	location, _ := time.LoadLocation(request.Timezone)
	dayAfterVisit := time.Date(2031, 6, 16, 0, 0, 0, 0, location)
	if _, err := lakeBookingParams(request, dayAfterVisit); !errors.Is(err, ErrBookingDatePassed) {
		t.Fatalf("past date was not rejected: %v", err)
	}
}
