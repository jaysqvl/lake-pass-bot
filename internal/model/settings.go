package model

import (
	"errors"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/jaysqvl/lake-pass-bot/internal/destinations"
)

const MaxReleaseDaysBefore = 365

var (
	ErrLakeConnectionRequired    = errors.New("connect an account on the lake page before booking")
	ErrLakeConnectionAmbiguous   = errors.New("choose an account to use for bookings on the lake page")
	ErrLakeConnectionUnavailable = errors.New("the selected booking account is unavailable; update the connection on the lake page")
)

// LakeSettings are one account's defaults. ApplyTo copies them into a booking;
// changing these defaults never changes an existing request or queued job.
type LakeSettings struct {
	UserID            int64
	LakeID            string
	BookingProfileID  int64
	VehicleKeyword    string
	Timezone          string
	ReleaseTime       string
	ReleaseDaysBefore int
	AllDayPassURL     string
	HalfDayPassURL    string
	PreferredPasses   []PassType
	UpdatedAt         time.Time
}

func DefaultLakeSettings(lake destinations.Lake) LakeSettings {
	settings := LakeSettings{
		LakeID: lake.ID, Timezone: lake.Timezone, ReleaseTime: lake.ReleaseTime,
		ReleaseDaysBefore: lake.ReleaseDaysBefore,
		AllDayPassURL:     lake.AllDayPassURL, HalfDayPassURL: lake.HalfDayPassURL,
	}
	for _, pass := range lake.SupportedPasses {
		settings.PreferredPasses = append(settings.PreferredPasses, PassType(pass))
	}
	return settings
}

func (s LakeSettings) ApplyTo(request BookingRequest) BookingRequest {
	days := s.ReleaseDaysBefore
	request.LakeID = s.LakeID
	request.VehicleKeyword = s.VehicleKeyword
	request.Timezone = s.Timezone
	request.ReleaseTime = s.ReleaseTime
	request.ReleaseDaysBefore = &days
	request.AllDayPassURL = s.AllDayPassURL
	request.HalfDayPassURL = s.HalfDayPassURL
	request.PreferredPasses = slices.Clone(s.PreferredPasses)
	return request
}

func (s LakeSettings) Validate() error {
	if problems := s.validationProblems(); len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// ResolveLakeBookingProfile uses an explicit choice, or the sole enabled
// account connected to this lake. A disabled choice never switches identities
// implicitly, and sharing a provider does not connect another lake.
func ResolveLakeBookingProfile(lake destinations.Lake, settings LakeSettings, profiles []Profile) (Profile, error) {
	var eligible []Profile
	for _, profile := range profiles {
		if (settings.UserID > 0 && profile.UserID != settings.UserID) ||
			profile.EffectiveLakeID() != lake.ID || profile.EffectiveProviderID() != lake.ProviderID || !profile.Enabled {
			continue
		}
		if settings.BookingProfileID == profile.ID {
			return profile, nil
		}
		eligible = append(eligible, profile)
	}
	if settings.BookingProfileID != 0 {
		return Profile{}, ErrLakeConnectionUnavailable
	}
	switch len(eligible) {
	case 0:
		return Profile{}, ErrLakeConnectionRequired
	case 1:
		return eligible[0], nil
	default:
		return Profile{}, ErrLakeConnectionAmbiguous
	}
}

func (s LakeSettings) ValidateForOrigins(allowedOrigins []string) error {
	if err := s.Validate(); err != nil {
		return err
	}
	return validateYodelURLs(allowedOrigins,
		yodelURL{s.AllDayPassURL, "all-day pass URL"},
		yodelURL{s.HalfDayPassURL, "half-day pass URL"},
	)
}

func (s LakeSettings) validationProblems() []string {
	var problems []string
	if s.BookingProfileID < 0 {
		problems = append(problems, "booking account is invalid")
	}
	lake, lakeErr := destinations.Resolve(s.LakeID)
	if lakeErr != nil {
		problems = append(problems, lakeErr.Error())
	}
	// Defaults may be saved before a vehicle is selected. A booking separately
	// requires one, while both paths apply the same size limit.
	if len(s.VehicleKeyword) > MaxDefaultVehicleBytes {
		problems = append(problems, "vehicle is too long")
	}
	if len(s.Timezone) > MaxTimezoneBytes {
		problems = append(problems, "timezone is too long")
	} else if _, err := time.LoadLocation(s.Timezone); err != nil {
		problems = append(problems, "timezone is invalid")
	}
	if _, err := time.Parse("15:04", s.ReleaseTime); err != nil {
		problems = append(problems, "release time must use HH:MM")
	}
	if s.ReleaseDaysBefore < 0 || s.ReleaseDaysBefore > MaxReleaseDaysBefore {
		problems = append(problems, "release days before visit must be between 0 and 365")
	}
	if len(s.PreferredPasses) == 0 {
		problems = append(problems, "at least one pass preference is required")
	} else if len(s.PreferredPasses) > 3 {
		problems = append(problems, "at most three pass preferences are allowed")
	}
	seen := make(map[PassType]bool, len(s.PreferredPasses))
	for _, pass := range s.PreferredPasses {
		if lakeErr == nil && !slices.Contains(lake.SupportedPasses, string(pass)) {
			problems = append(problems, "pass preference is not supported by the selected lake")
		} else if seen[pass] {
			problems = append(problems, "each pass preference can only be selected once")
		}
		seen[pass] = true
	}
	if seen[PassAllDay] {
		if err := validateHTTPURL(s.AllDayPassURL, "all-day pass URL"); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if seen[PassAfternoon] || seen[PassMorning] {
		if err := validateHTTPURL(s.HalfDayPassURL, "half-day pass URL"); err != nil {
			problems = append(problems, err.Error())
		}
	}
	return problems
}

// AccountSettings supply browser defaults for new profiles and timing and
// confirmation preferences for new bookings. Existing records retain their
// saved values.
type AccountSettings struct {
	UserID                    int64
	DefaultConfirmationMode   RunMode
	Headless                  bool
	BrowserChannel            string
	DefaultTimeoutMS          int
	PrepMinutesBefore         int
	AuthDeadlineMinutesBefore int
	PollDeadlineSeconds       int
	PollMinSeconds            float64
	PollMaxSeconds            float64
	UpdatedAt                 time.Time
}

func DefaultAccountSettings() AccountSettings {
	return AccountSettings{
		DefaultConfirmationMode: RunModeManual,
		Headless:                true, DefaultTimeoutMS: 15_000,
		PrepMinutesBefore: 30, AuthDeadlineMinutesBefore: 5,
		PollDeadlineSeconds: 120, PollMinSeconds: 1.4, PollMaxSeconds: 3.6,
	}
}

func (s AccountSettings) ApplyToBooking(request BookingRequest) BookingRequest {
	request.PrepMinutesBefore = s.PrepMinutesBefore
	request.AuthDeadlineMinutesBefore = s.AuthDeadlineMinutesBefore
	request.PollDeadlineSeconds = s.PollDeadlineSeconds
	request.PollMinSeconds = s.PollMinSeconds
	request.PollMaxSeconds = s.PollMaxSeconds
	return request
}

func (s AccountSettings) Validate() error {
	if s.DefaultConfirmationMode != "" && s.DefaultConfirmationMode != RunModeManual && s.DefaultConfirmationMode != RunModeAuto {
		return errors.New("choose manual approval or automatic confirmation")
	}
	if len(s.BrowserChannel) > MaxBrowserChannelBytes {
		return errors.New("browser selection is too long")
	}
	switch strings.ToLower(strings.TrimSpace(s.BrowserChannel)) {
	case "", "chrome", "chrome-beta", "chrome-dev", "chrome-canary":
	default:
		return errors.New("choose bundled Chromium or a supported Chrome channel")
	}
	if s.DefaultTimeoutMS < 1_000 || s.DefaultTimeoutMS > 120_000 {
		return errors.New("default timeout must be between 1000 and 120000 milliseconds")
	}
	timing := preparationTiming{
		PrepMinutesBefore: s.PrepMinutesBefore, AuthDeadlineMinutesBefore: s.AuthDeadlineMinutesBefore,
		PollDeadlineSeconds: s.PollDeadlineSeconds, PollMinSeconds: s.PollMinSeconds, PollMaxSeconds: s.PollMaxSeconds,
	}
	if problems := timing.validationProblems(); len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// preparationTiming is the shared timing policy for account defaults and
// individual requests. It has no account, destination, or booking identity.
type preparationTiming struct {
	PrepMinutesBefore         int
	AuthDeadlineMinutesBefore int
	PollDeadlineSeconds       int
	PollMinSeconds            float64
	PollMaxSeconds            float64
}

func (t preparationTiming) validationProblems() []string {
	var problems []string
	if t.PrepMinutesBefore < 0 || t.AuthDeadlineMinutesBefore < 0 {
		problems = append(problems, "preparation offsets cannot be negative")
	} else if t.PrepMinutesBefore > MaxPrepMinutesBefore {
		problems = append(problems, "preparation window cannot exceed 180 minutes")
	}
	if t.AuthDeadlineMinutesBefore > t.PrepMinutesBefore {
		problems = append(problems, "auth deadline must fall within the preparation window")
	}
	if t.PollDeadlineSeconds <= 0 || t.PollDeadlineSeconds > 900 ||
		t.PollMinSeconds < 0.05 || t.PollMinSeconds > 60 ||
		t.PollMaxSeconds < t.PollMinSeconds || t.PollMaxSeconds > 60 ||
		math.IsNaN(t.PollMinSeconds) || math.IsNaN(t.PollMaxSeconds) ||
		math.IsInf(t.PollMinSeconds, 0) || math.IsInf(t.PollMaxSeconds, 0) {
		problems = append(problems, "poll timing must fit the worker bounds")
	}
	return problems
}
