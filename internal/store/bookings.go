package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jaysqvl/lake-pass-bot/internal/destinations"
	"github.com/jaysqvl/lake-pass-bot/internal/model"
)

func (s *Store) prepareBookingRequest(ctx context.Context, userID int64, request model.BookingRequest) (model.BookingRequest, error) {
	request.UserID = userID
	request = normalizeBooking(request)
	profile, err := s.bookingProfile(ctx, userID, request)
	if err != nil {
		return model.BookingRequest{}, err
	}
	if request.VehicleKeyword == "" {
		request.VehicleKeyword = strings.TrimSpace(profile.DefaultVehicle)
	}
	if err := request.Validate(); err != nil {
		return model.BookingRequest{}, err
	}
	if request.LoginProbeURL == "" {
		// Keep the historical column populated for its original SQL constraint;
		// authentication now reads the profile's login URL directly.
		request.LoginProbeURL = profile.LoginProbeURL
	}
	return request, nil
}

// Execution inputs are inserted together with a job in a transaction.
type bookingExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *Store) insertBookingRequest(ctx context.Context, executor bookingExecer, userID int64, request model.BookingRequest) (int64, error) {
	now := s.now()
	result, err := executor.ExecContext(ctx, `
		INSERT INTO booking_requests(
			user_id, name, profile_id, enabled, schedule_enabled, target_date, timezone, release_time,
			prep_minutes_before, auth_deadline_minutes_before, poll_deadline_seconds,
			poll_min_seconds, poll_max_seconds, confirmation_mode, login_probe_url,
			all_day_pass_url, half_day_pass_url, check_all_day, check_afternoon, check_morning,
			pass_order, created_at, updated_at, lake_id, release_days_before, vehicle_keyword, kind
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, request.Name, request.ProfileID, request.Enabled, request.ScheduleEnabled,
		request.TargetDate, request.Timezone, request.ReleaseTime, request.PrepMinutesBefore,
		request.AuthDeadlineMinutesBefore, request.PollDeadlineSeconds, request.PollMinSeconds,
		request.PollMaxSeconds, request.ConfirmationMode, request.LoginProbeURL,
		request.AllDayPassURL, request.HalfDayPassURL, request.CheckAllDay,
		request.CheckAfternoon, request.CheckMorning, passOrderCSV(request.PreferredPasses), formatTime(now), formatTime(now), request.LakeID, request.EffectiveReleaseDaysBefore(), request.VehicleKeyword, request.Kind)
	if err != nil {
		return 0, mapWriteError(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read booking request id: %w", err)
	}
	return id, nil
}

func (s *Store) GetBookingRequest(ctx context.Context, userID, id int64) (model.BookingRequest, error) {
	if userID <= 0 {
		return model.BookingRequest{}, ErrUserRequired
	}
	return scanBooking(s.db.QueryRowContext(ctx, bookingSelect+" WHERE id = ? AND user_id = ?", id, userID))
}

func (s *Store) ListBookingRequests(ctx context.Context, userID int64) ([]model.BookingRequest, error) {
	if userID <= 0 {
		return nil, ErrUserRequired
	}
	rows, err := s.db.QueryContext(ctx, bookingSelect+" WHERE user_id = ? ORDER BY name, id", userID)
	if err != nil {
		return nil, fmt.Errorf("list booking requests: %w", err)
	}
	defer rows.Close()
	var result []model.BookingRequest
	for rows.Next() {
		request, err := scanBooking(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, request)
	}
	return result, rows.Err()
}

// SystemGetBookingRequest is for trusted CLI/worker code. HTTP handlers
// must use ForUser so a cross-owner ID remains indistinguishable from missing.
func (s *Store) SystemGetBookingRequest(ctx context.Context, id int64) (model.BookingRequest, error) {
	return scanBooking(s.db.QueryRowContext(ctx, bookingSelect+" WHERE id = ?", id))
}

const bookingSelect = `
	SELECT id, user_id, name, profile_id, enabled, schedule_enabled, target_date, timezone, release_time,
		prep_minutes_before, auth_deadline_minutes_before, poll_deadline_seconds,
		poll_min_seconds, poll_max_seconds, confirmation_mode, login_probe_url,
		all_day_pass_url, half_day_pass_url, check_all_day, check_afternoon, check_morning,
		pass_order, created_at, updated_at, lake_id, release_days_before, vehicle_keyword, kind
	FROM booking_requests`

func scanBooking(scanner rowScanner) (model.BookingRequest, error) {
	var request model.BookingRequest
	var passOrder, created, updated string
	if err := scanner.Scan(&request.ID, &request.UserID, &request.Name, &request.ProfileID, &request.Enabled,
		&request.ScheduleEnabled, &request.TargetDate, &request.Timezone, &request.ReleaseTime,
		&request.PrepMinutesBefore, &request.AuthDeadlineMinutesBefore,
		&request.PollDeadlineSeconds, &request.PollMinSeconds, &request.PollMaxSeconds,
		&request.ConfirmationMode, &request.LoginProbeURL, &request.AllDayPassURL,
		&request.HalfDayPassURL, &request.CheckAllDay, &request.CheckAfternoon,
		&request.CheckMorning, &passOrder, &created, &updated, &request.LakeID, &request.ReleaseDaysBefore, &request.VehicleKeyword, &request.Kind); errors.Is(err, sql.ErrNoRows) {
		return model.BookingRequest{}, ErrNotFound
	} else if err != nil {
		return model.BookingRequest{}, fmt.Errorf("scan booking request: %w", err)
	}
	var err error
	for _, pass := range strings.Split(passOrder, ",") {
		request.PreferredPasses = append(request.PreferredPasses, model.PassType(pass))
	}
	if request.CreatedAt, err = parseTime(created); err != nil {
		return model.BookingRequest{}, err
	}
	if request.UpdatedAt, err = parseTime(updated); err != nil {
		return model.BookingRequest{}, err
	}
	return request, nil
}

func normalizeBooking(request model.BookingRequest) model.BookingRequest {
	request.Name = strings.TrimSpace(request.Name)
	request.VehicleKeyword = strings.TrimSpace(request.VehicleKeyword)
	request.LakeID = request.EffectiveLakeID()
	days := request.EffectiveReleaseDaysBefore()
	request.ReleaseDaysBefore = &days
	request.TargetDate = strings.TrimSpace(request.TargetDate)
	request.Timezone = strings.TrimSpace(request.Timezone)
	request.ReleaseTime = strings.TrimSpace(request.ReleaseTime)
	request.LoginProbeURL = strings.TrimSpace(request.LoginProbeURL)
	request.AllDayPassURL = strings.TrimSpace(request.AllDayPassURL)
	request.HalfDayPassURL = strings.TrimSpace(request.HalfDayPassURL)
	request.PreferredPasses = request.PassOrder()
	request.CheckAllDay = slices.Contains(request.PreferredPasses, model.PassAllDay)
	request.CheckAfternoon = slices.Contains(request.PreferredPasses, model.PassAfternoon)
	request.CheckMorning = slices.Contains(request.PreferredPasses, model.PassMorning)
	return request
}

func (s *Store) bookingProfile(ctx context.Context, userID int64, request model.BookingRequest) (model.Profile, error) {
	profile, err := s.GetProfile(ctx, userID, request.ProfileID)
	if err != nil {
		return model.Profile{}, err
	}
	lake, err := destinations.Resolve(request.LakeID)
	if err != nil {
		return model.Profile{}, err
	}
	if profile.EffectiveProviderID() != lake.ProviderID {
		return model.Profile{}, fmt.Errorf("%w: choose a sign-in for the selected lake's booking provider", ErrConflict)
	}
	return profile, nil
}

func passOrderCSV(order []model.PassType) string {
	values := make([]string, len(order))
	for i, pass := range order {
		values[i] = string(pass)
	}
	return strings.Join(values, ",")
}
