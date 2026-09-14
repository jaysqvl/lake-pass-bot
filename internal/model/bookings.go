package model

import (
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jaysqvl/lake-pass-bot/internal/destinations"
	"github.com/jaysqvl/lake-pass-bot/internal/origin"
)

type PassType string

const (
	PassAllDay    PassType = "all_day"
	PassAfternoon PassType = "afternoon"
	PassMorning   PassType = "morning"
)

// Saved and archived records retain legacy job references. Snapshots are
// immutable inputs to new jobs; legacy fields are not assumed to be the
// original inputs to every historical job.
type BookingKind string

const (
	BookingKindSaved    BookingKind = "saved"
	BookingKindSnapshot BookingKind = "snapshot"
	BookingKindArchived BookingKind = "archived"
)

type BookingRequest struct {
	ID              int64
	UserID          int64
	Name            string
	Kind            BookingKind
	LakeID          string
	ProfileID       int64
	VehicleKeyword  string
	Enabled         bool
	ScheduleEnabled bool
	TargetDate      string
	Timezone        string
	ReleaseTime     string
	// Nil supports callers predating per-lake defaults. Persistence snapshots
	// the catalog value; an explicit zero means release on the visit date.
	ReleaseDaysBefore         *int
	PrepMinutesBefore         int
	AuthDeadlineMinutesBefore int
	PollDeadlineSeconds       int
	PollMinSeconds            float64
	PollMaxSeconds            float64
	ConfirmationMode          RunMode
	// LoginProbeURL retains the legacy database value for migration/history.
	// Authentication uses Profile.LoginProbeURL exclusively.
	LoginProbeURL   string
	AllDayPassURL   string
	HalfDayPassURL  string
	PreferredPasses []PassType
	// Legacy input flags are used only when PreferredPasses is nil.
	CheckAllDay    bool
	CheckAfternoon bool
	CheckMorning   bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (r BookingRequest) EffectiveLakeID() string {
	if r.LakeID == "" {
		return destinations.DefaultLakeID
	}
	return r.LakeID
}

func (r BookingRequest) EffectiveReleaseDaysBefore() int {
	if r.ReleaseDaysBefore != nil {
		return *r.ReleaseDaysBefore
	}
	lake, _ := destinations.Resolve(r.LakeID)
	return lake.ReleaseDaysBefore
}

func (r BookingRequest) PassOrder() []PassType {
	if r.PreferredPasses != nil {
		return slices.Clone(r.PreferredPasses)
	}
	result := make([]PassType, 0, 3)
	if r.CheckAllDay {
		result = append(result, PassAllDay)
	}
	if r.CheckAfternoon {
		result = append(result, PassAfternoon)
	}
	if r.CheckMorning {
		result = append(result, PassMorning)
	}
	return result
}

func (r BookingRequest) Validate() error {
	var problems []string
	if r.Kind != "" && r.Kind != BookingKindSaved && r.Kind != BookingKindSnapshot && r.Kind != BookingKindArchived {
		problems = append(problems, "booking kind is invalid")
	}
	if strings.TrimSpace(r.Name) == "" {
		problems = append(problems, "name is required")
	} else if len(r.Name) > MaxResourceNameBytes {
		problems = append(problems, "name is too long")
	}
	if r.ProfileID <= 0 {
		problems = append(problems, "profile is required")
	}
	if strings.TrimSpace(r.VehicleKeyword) == "" {
		problems = append(problems, "vehicle is required")
	}
	if _, err := time.Parse(time.DateOnly, r.TargetDate); err != nil {
		problems = append(problems, "target date must use YYYY-MM-DD")
	}
	problems = append(problems, r.lakeSettings().validationProblems()...)
	problems = append(problems, r.preparationProblems()...)
	if !r.ConfirmationMode.Valid() || r.ConfirmationMode == RunModeDryRun {
		problems = append(problems, "confirmation mode must be manual or auto")
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func (r BookingRequest) preparationProblems() []string {
	return (preparationTiming{
		PrepMinutesBefore: r.PrepMinutesBefore, AuthDeadlineMinutesBefore: r.AuthDeadlineMinutesBefore,
		PollDeadlineSeconds: r.PollDeadlineSeconds, PollMinSeconds: r.PollMinSeconds, PollMaxSeconds: r.PollMaxSeconds,
	}).validationProblems()
}

// A request keeps a snapshot of its lake preferences. Validate those fields
// using the same policy as saved defaults, without requiring another booking.
func (r BookingRequest) lakeSettings() LakeSettings {
	return LakeSettings{
		LakeID: r.LakeID, VehicleKeyword: r.VehicleKeyword,
		Timezone: r.Timezone, ReleaseTime: r.ReleaseTime,
		ReleaseDaysBefore: r.EffectiveReleaseDaysBefore(),
		AllDayPassURL:     r.AllDayPassURL, HalfDayPassURL: r.HalfDayPassURL,
		PreferredPasses: r.PassOrder(),
	}
}

// ValidateForOrigins applies the operator-controlled credential boundary on
// top of the persistence-level booking validation. Booking records may choose
// paths on an approved Yodel site, but they cannot choose which origin receives
// credentials or OTPs.
func (r BookingRequest) ValidateForOrigins(allowedOrigins []string) error {
	if err := r.Validate(); err != nil {
		return err
	}
	return validateYodelURLs(allowedOrigins,
		yodelURL{r.AllDayPassURL, "all-day pass URL"},
		yodelURL{r.HalfDayPassURL, "half-day pass URL"},
	)
}

type yodelURL struct{ value, label string }

func validateYodelURLs(allowedOrigins []string, checks ...yodelURL) error {
	approved := make(map[string]struct{}, len(allowedOrigins))
	for _, value := range allowedOrigins {
		canonical, err := origin.Canonical(value)
		if err != nil || !strings.HasPrefix(canonical, "https://") {
			return errors.New("configured Yodel origin is invalid")
		}
		approved[canonical] = struct{}{}
	}
	if len(approved) == 0 {
		return errors.New("no approved Yodel origin is configured")
	}
	for _, check := range checks {
		if strings.TrimSpace(check.value) == "" {
			continue
		}
		canonical, err := origin.FromURL(check.value)
		if err != nil || !strings.HasPrefix(canonical, "https://") {
			return errors.New(check.label + " must be an absolute HTTPS URL")
		}
		if _, ok := approved[canonical]; !ok {
			return errors.New(check.label + " must use an approved Yodel origin")
		}
	}
	return nil
}

func validateHTTPURL(value, label string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New(label + " is required")
	}
	if len(value) > 2048 {
		return errors.New(label + " is too long")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New(label + " must be an absolute HTTP(S) URL")
	}
	if parsed.User != nil {
		return errors.New(label + " must not contain credentials")
	}
	return nil
}
