package web

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jaysqvl/lake-pass-bot/internal/destinations"
	"github.com/jaysqvl/lake-pass-bot/internal/engine"
	"github.com/jaysqvl/lake-pass-bot/internal/model"
	"github.com/jaysqvl/lake-pass-bot/internal/store"
)

const autoQueueOffNotice = "Auto-queueing is off for this server. New jobs must be queued manually. Already queued jobs remain scheduled; use Cancel job to stop one."

func lakeName(id string) string {
	lake, err := destinations.Resolve(id)
	if err != nil {
		return "Unsupported lake"
	}
	return lake.Name
}

func passOptionLabel(pass string) string {
	switch model.PassType(pass) {
	case model.PassAllDay:
		return "All-day"
	case model.PassAfternoon:
		return "Afternoon"
	case model.PassMorning:
		return "Morning"
	default:
		return strings.ReplaceAll(pass, "_", " ")
	}
}

func passNames(values []model.PassType) []string {
	result := make([]string, len(values))
	for i, value := range values {
		switch value {
		case model.PassAllDay:
			result[i] = "All-day"
		case model.PassMorning:
			result[i] = "Morning"
		case model.PassAfternoon:
			result[i] = "Afternoon"
		default:
			result[i] = strings.ReplaceAll(string(value), "_", " ")
		}
	}
	return result
}

func (s *Server) bookingRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	booking, err := s.userStore(r).GetBookingRequest(r.Context(), id)
	if err != nil {
		s.notFoundOrInternal(w, err)
		return
	}
	if booking.Kind != model.BookingKindSaved {
		http.NotFound(w, r)
		return
	}
	command := model.JobCommand(r.Form.Get("command"))
	if !command.Valid() {
		redirectNotice(w, r, "/bookings", "booking-action")
		return
	}
	mode := model.RunMode("")
	if command == model.CommandDryRun {
		mode = model.RunModeDryRun
	}
	timing := r.Form.Get("timing")
	if timing != "" && (timing != "now" || command != model.CommandBook) {
		redirectNotice(w, r, "/bookings", "booking-action")
		return
	}
	userID := requestAuth(r).Authenticated.User.ID
	var job model.Job
	if timing == "now" {
		job, err = s.engine.QueueBookingNow(r.Context(), userID, id)
	} else {
		job, err = s.engine.QueueBooking(r.Context(), userID, id, command, mode)
	}
	if err != nil {
		slog.Warn("booking job could not be queued", "booking_id", id, "command", command, "error", err)
		s.bookingRunFailure(w, r, booking, command, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/jobs/%d?ok=queued", job.ID), http.StatusSeeOther)
}

func (s *Server) bookingRunFailure(w http.ResponseWriter, r *http.Request, booking model.BookingRequest, command model.JobCommand, cause error) {
	code := "queue-unavailable"
	var existingJob *model.Job
	switch {
	case errors.Is(cause, store.ErrResourceLimit):
		// Resource limits wrap ErrConflict, but do not mean a duplicate exists.
		code = "queue-full"
	case errors.Is(cause, engine.ErrBookingNotReleased):
		code = "booking-not-released"
	case errors.Is(cause, engine.ErrBookingDatePassed):
		code = "booking-date-passed"
	case errors.Is(cause, engine.ErrBookingWindowEnded):
		code = "booking-window-ended"
	case errors.Is(cause, store.ErrConflict):
		conflict, err := s.userStore(r).BookingConflict(r.Context(), booking.ID, command)
		if err != nil {
			slog.Warn("booking conflict lookup failed", "booking_id", booking.ID, "error", err)
			break
		}
		existingJob = conflict.Job
		if existingJob != nil && !existingJob.Status.Terminal() {
			code = "queue-pending"
		} else if conflict.Reservation && (existingJob == nil || existingJob.Status == model.JobSucceeded || existingJob.Status == model.JobOutcomeUnknown || existingJob.ConfirmationStartedAt != nil) {
			code = "queue-review"
		}
	default:
		if !booking.Enabled {
			code = "booking-disabled"
		} else if profile, err := s.userStore(r).GetProfile(r.Context(), booking.ProfileID); err == nil && !profile.Enabled {
			code = "profile-disabled"
		}
	}
	query := url.Values{"notice": {code}}
	if existingJob != nil && (code == "queue-pending" || code == "queue-review") {
		query.Set("job", strconv.FormatInt(existingJob.ID, 10))
	}
	location := url.URL{Path: "/bookings", RawQuery: query.Encode()}
	http.Redirect(w, r, location.String(), http.StatusSeeOther)
}
