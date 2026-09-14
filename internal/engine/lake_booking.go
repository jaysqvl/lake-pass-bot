package engine

import (
	"context"
	"time"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
	"github.com/jaysqvl/lake-pass-bot/internal/scheduler"
	"github.com/jaysqvl/lake-pass-bot/internal/store"
)

// QueueLakeBooking freezes this visit's resolved settings and creates its job
// together. Later edits to lake/account defaults cannot change queued work.
func (e *Engine) QueueLakeBooking(ctx context.Context, userID int64, request model.BookingRequest) (model.Job, error) {
	if err := request.ValidateForOrigins(e.config.YodelOrigins); err != nil {
		return model.Job{}, err
	}
	params, err := lakeBookingParams(request, time.Now().UTC())
	if err != nil {
		return model.Job{}, err
	}
	return e.store.ForUser(userID).EnqueueBookingRequest(ctx, request, params)
}

func lakeBookingParams(request model.BookingRequest, now time.Time) (store.EnqueueJobParams, error) {
	window, err := scheduler.WindowFor(request)
	if err != nil {
		return store.EnqueueJobParams{}, err
	}
	var params store.EnqueueJobParams
	if now.Before(window.ReleaseAt) {
		params, err = bookingEnqueueParams(request, model.CommandBook, request.ConfirmationMode, now)
	} else {
		params, err = immediateBookingEnqueueParams(request, now)
	}
	// The store allocates the snapshot ID inside the enqueue transaction.
	params.BookingRequestID = nil
	return params, err
}
