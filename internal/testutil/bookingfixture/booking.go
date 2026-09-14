// Package bookingfixture constructs historical booking rows for tests. Production
// code creates immutable visit inputs only through EnqueueBookingRequest.
package bookingfixture

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
)

type Resources interface {
	UserID() int64
	GetProfile(context.Context, int64) (model.Profile, error)
	GetBookingRequest(context.Context, int64) (model.BookingRequest, error)
}

// Create inserts a legacy row without exercising a removed product feature.
func Create(ctx context.Context, path string, resources Resources, request model.BookingRequest) (model.BookingRequest, error) {
	profile, err := resources.GetProfile(ctx, request.ProfileID)
	if err != nil {
		return model.BookingRequest{}, err
	}
	request.UserID = resources.UserID()
	request.LakeID = request.EffectiveLakeID()
	if request.Kind == "" {
		request.Kind = model.BookingKindSaved
	}
	if request.VehicleKeyword == "" {
		request.VehicleKeyword = profile.DefaultVehicle
	}
	if request.LoginProbeURL == "" {
		request.LoginProbeURL = profile.LoginProbeURL
	}
	request.PreferredPasses = request.PassOrder()
	request.CheckAllDay = slices.Contains(request.PreferredPasses, model.PassAllDay)
	request.CheckAfternoon = slices.Contains(request.PreferredPasses, model.PassAfternoon)
	request.CheckMorning = slices.Contains(request.PreferredPasses, model.PassMorning)
	order := make([]string, len(request.PreferredPasses))
	for i, pass := range request.PreferredPasses {
		order[i] = string(pass)
	}
	db, err := open(path)
	if err != nil {
		return model.BookingRequest{}, err
	}
	defer db.Close()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
	result, err := db.ExecContext(ctx, `INSERT INTO booking_requests(
		user_id,name,profile_id,enabled,schedule_enabled,target_date,timezone,release_time,
		prep_minutes_before,auth_deadline_minutes_before,poll_deadline_seconds,poll_min_seconds,poll_max_seconds,
		confirmation_mode,login_probe_url,all_day_pass_url,half_day_pass_url,check_all_day,check_afternoon,check_morning,
		pass_order,created_at,updated_at,lake_id,release_days_before,vehicle_keyword,kind)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		request.UserID, request.Name, request.ProfileID, request.Enabled, request.ScheduleEnabled, request.TargetDate, request.Timezone, request.ReleaseTime,
		request.PrepMinutesBefore, request.AuthDeadlineMinutesBefore, request.PollDeadlineSeconds, request.PollMinSeconds, request.PollMaxSeconds,
		request.ConfirmationMode, request.LoginProbeURL, request.AllDayPassURL, request.HalfDayPassURL, request.CheckAllDay, request.CheckAfternoon, request.CheckMorning,
		strings.Join(order, ","), now, now, request.LakeID, request.EffectiveReleaseDaysBefore(), request.VehicleKeyword, request.Kind)
	if err != nil {
		return model.BookingRequest{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.BookingRequest{}, err
	}
	return resources.GetBookingRequest(ctx, id)
}

// Update changes fixture inputs directly; it is deliberately not a product API
// and must not be used to assert authorization or mutation-guard behavior.
func Update(ctx context.Context, path string, resources Resources, request model.BookingRequest) (model.BookingRequest, error) {
	db, err := open(path)
	if err != nil {
		return model.BookingRequest{}, err
	}
	defer db.Close()
	order := request.PassOrder()
	passes := make([]string, len(order))
	for i, pass := range order {
		passes[i] = string(pass)
	}
	_, err = db.ExecContext(ctx, `UPDATE booking_requests SET name=?,profile_id=?,enabled=?,schedule_enabled=?,target_date=?,timezone=?,release_time=?,
		prep_minutes_before=?,auth_deadline_minutes_before=?,poll_deadline_seconds=?,poll_min_seconds=?,poll_max_seconds=?,confirmation_mode=?,
		login_probe_url=?,all_day_pass_url=?,half_day_pass_url=?,check_all_day=?,check_afternoon=?,check_morning=?,pass_order=?,lake_id=?,release_days_before=?,vehicle_keyword=?
		WHERE id=? AND user_id=?`, request.Name, request.ProfileID, request.Enabled, request.ScheduleEnabled, request.TargetDate, request.Timezone, request.ReleaseTime,
		request.PrepMinutesBefore, request.AuthDeadlineMinutesBefore, request.PollDeadlineSeconds, request.PollMinSeconds, request.PollMaxSeconds, request.ConfirmationMode,
		request.LoginProbeURL, request.AllDayPassURL, request.HalfDayPassURL, slices.Contains(order, model.PassAllDay), slices.Contains(order, model.PassAfternoon), slices.Contains(order, model.PassMorning),
		strings.Join(passes, ","), request.EffectiveLakeID(), request.EffectiveReleaseDaysBefore(), request.VehicleKeyword, request.ID, resources.UserID())
	if err != nil {
		return model.BookingRequest{}, err
	}
	return resources.GetBookingRequest(ctx, request.ID)
}

// Remove deletes unused fixture setup, never a user's saved request.
func Remove(ctx context.Context, path string, resources Resources, id int64) error {
	db, err := open(path)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, "DELETE FROM booking_requests WHERE id=? AND user_id=?", id, resources.UserID())
	return err
}

func open(path string) (*sql.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("fixture database path is required")
	}
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	return sql.Open("sqlite", uri+"?_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
}
