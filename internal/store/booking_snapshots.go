package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
)

// EnqueueBookingRequest stores the selected date, passes and current defaults
// together with their job. A rejected admission never leaves a request behind.
func (s *Store) EnqueueBookingRequest(ctx context.Context, userID int64, request model.BookingRequest, params EnqueueJobParams) (model.Job, error) {
	if userID <= 0 {
		return model.Job{}, ErrUserRequired
	}
	if params.Command != model.CommandBook && params.Command != model.CommandDryRun {
		return model.Job{}, errors.New("booking snapshots require a book or dry-run job")
	}
	if params.BookingRequestID != nil && *params.BookingRequestID != 0 {
		return model.Job{}, errors.New("a new booking cannot use an existing request ID")
	}
	request.ID = 0
	request.Kind = model.BookingKindSnapshot
	request.Enabled = true
	request.ScheduleEnabled = false
	request, err := s.prepareBookingRequest(ctx, userID, request)
	if err != nil {
		return model.Job{}, err
	}
	request.Name, err = snapshotBookingName(request.Name)
	if err != nil {
		return model.Job{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Job{}, fmt.Errorf("begin booking transaction: %w", err)
	}
	defer tx.Rollback()
	id, err := s.insertBookingRequest(ctx, tx, userID, request)
	if err != nil {
		return model.Job{}, err
	}
	params.BookingRequestID = &id
	job, err := s.enqueueJobTx(ctx, tx, userID, params)
	if err != nil {
		return model.Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Job{}, fmt.Errorf("commit booking transaction: %w", err)
	}
	return job, nil
}

// Saved request names remain unique per account. Snapshots use an internal
// suffix so retries and removed requests never reserve a user-facing name.
// The UI labels execution snapshots using their lake and visit date.
func snapshotBookingName(name string) (string, error) {
	var token [8]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("create booking snapshot name: %w", err)
	}
	suffix := " · " + hex.EncodeToString(token[:])
	name = strings.ToValidUTF8(strings.TrimSpace(name), "\uFFFD")
	limit := model.MaxResourceNameBytes - len(suffix)
	if len(name) > limit {
		for !utf8.RuneStart(name[limit]) {
			limit--
		}
		name = name[:limit]
	}
	return strings.TrimSpace(name) + suffix, nil
}
