package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
	"github.com/jaysqvl/lake-pass-bot/internal/scheduler"
	"github.com/jaysqvl/lake-pass-bot/internal/store"
)

var (
	ErrBookingNotReleased = errors.New("parking passes have not been released for this target date")
	ErrBookingDatePassed  = errors.New("the booking target date has passed")
	ErrBookingWindowEnded = errors.New("the booking release window has ended")
)

func immediateBookingEnqueueParams(booking model.BookingRequest, now time.Time) (store.EnqueueJobParams, error) {
	if err := validateImmediateBookingDate(booking, now); err != nil {
		return store.EnqueueJobParams{}, err
	}
	expiresAt := now.Add(model.MaxImmediateBookingLifetime).UTC()
	return store.EnqueueJobParams{
		BookingRequestID: &booking.ID,
		Command:          model.CommandBook,
		RunMode:          model.RunModeManual,
		RunImmediately:   true,
		DueAt:            now.UTC(),
		ExpiresAt:        &expiresAt,
	}, nil
}

func validateImmediateBookingDate(booking model.BookingRequest, now time.Time) error {
	window, err := scheduler.WindowFor(booking)
	if err != nil {
		return err
	}
	if now.Before(window.ReleaseAt) {
		return ErrBookingNotReleased
	}
	if now.In(window.ReleaseAt.Location()).Format(time.DateOnly) > booking.TargetDate {
		return ErrBookingDatePassed
	}
	return nil
}

// bookingStartTiming checks admission again immediately before launching the
// browser, since provider setup and queueing can consume the remaining window.
func bookingStartTiming(job model.Job, booking model.BookingRequest, now time.Time) (map[string]any, error) {
	if err := job.ValidateImmediateRun(); err != nil {
		return nil, err
	}
	if job.Command != model.CommandBook {
		return nil, nil
	}
	timing := make(map[string]any)
	if job.RunImmediately {
		if err := validateImmediateBookingDate(booking, now); err != nil {
			return nil, err
		}
		if now.Before(job.DueAt) {
			return nil, errors.New("the immediate booking became runnable before its enqueue time")
		}
		timing["auth_deadline_at"] = job.ExpiresAt.Format(time.RFC3339Nano)
	} else {
		window, err := scheduler.WindowFor(booking)
		if err != nil {
			return nil, err
		}
		if now.Before(window.PrepAt) {
			return nil, errors.New("the booking job became runnable before its bounded preparation window")
		}
		if !now.Before(window.PollEndsAt) {
			return nil, errors.New("the booking release window ended before the action could start")
		}
		if now.Before(window.ReleaseAt) {
			timing["release_at"] = window.ReleaseAt.Format(time.RFC3339)
		}
		timing["auth_deadline_at"] = window.AuthDeadlineAt.Format(time.RFC3339)
	}
	if job.ExpiresAt != nil {
		remaining := int(math.Ceil(job.ExpiresAt.Sub(now).Seconds()))
		if remaining < 1 {
			return nil, errors.New("the booking window expired before the action could start")
		}
		if remaining < booking.PollDeadlineSeconds {
			timing["poll_deadline_seconds"] = remaining
		}
	}
	return timing, nil
}

// SystemQueueBooking supports the host-authorized CLI replay path. The
// persisted owner is derived from the booking request, never supplied here.
func (e *Engine) SystemQueueBooking(ctx context.Context, bookingID int64, command model.JobCommand, mode model.RunMode) (model.Job, error) {
	booking, err := e.store.SystemGetBookingRequest(ctx, bookingID)
	if err != nil {
		return model.Job{}, err
	}
	if err := booking.ValidateForOrigins(e.config.YodelOrigins); err != nil {
		return model.Job{}, err
	}
	params, err := bookingEnqueueParams(booking, command, mode, time.Now().UTC())
	if err != nil {
		return model.Job{}, err
	}
	return e.store.SystemEnqueueJob(ctx, params)
}

func bookingEnqueueParams(
	booking model.BookingRequest,
	command model.JobCommand,
	mode model.RunMode,
	now time.Time,
) (store.EnqueueJobParams, error) {
	switch command {
	case model.CommandDryRun:
		mode = model.RunModeDryRun
	case model.CommandAuthCheck:
		mode = model.RunModeManual
	case model.CommandBook:
		if mode == "" {
			mode = booking.ConfirmationMode
		}
		if mode != model.RunModeManual && mode != model.RunModeAuto {
			return store.EnqueueJobParams{}, errors.New("book run mode must be manual or auto")
		}
	default:
		return store.EnqueueJobParams{}, fmt.Errorf("invalid job command %q", command)
	}
	params := store.EnqueueJobParams{
		BookingRequestID: &booking.ID,
		Command:          command,
		RunMode:          mode,
		DueAt:            now.UTC(),
	}
	if command != model.CommandBook {
		return params, nil
	}
	window, err := scheduler.WindowFor(booking)
	if err != nil {
		return store.EnqueueJobParams{}, err
	}
	if !now.Before(window.PollEndsAt) {
		return store.EnqueueJobParams{}, ErrBookingWindowEnded
	}
	if now.Before(window.PrepAt) {
		params.DueAt = window.PrepAt.UTC()
	}
	expiresAt := window.PollEndsAt.UTC()
	params.ExpiresAt = &expiresAt
	return params, nil
}
