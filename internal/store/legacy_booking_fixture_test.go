package store

import (
	"context"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
	"github.com/jaysqvl/lake-pass-bot/internal/testutil/bookingfixture"
)

const MaxBookingRequestsPerUser = 64 // Historical schema limit; snapshots use job retention.

// These methods construct historical database states for tests. They are not
// compiled into the application and do not represent supported CRUD features.
func (s *Store) createLegacyBookingFixture(ctx context.Context, userID int64, request model.BookingRequest) (model.BookingRequest, error) {
	result, err := bookingfixture.Create(ctx, s.path, s.ForUser(userID), request)
	return result, mapWriteError(err)
}
func (u UserStore) createLegacyBookingFixture(ctx context.Context, request model.BookingRequest) (model.BookingRequest, error) {
	return u.store.createLegacyBookingFixture(ctx, u.userID, request)
}
func (s *Store) updateLegacyBookingFixture(ctx context.Context, userID int64, request model.BookingRequest) (model.BookingRequest, error) {
	return bookingfixture.Update(ctx, s.path, s.ForUser(userID), request)
}
func (u UserStore) updateLegacyBookingFixture(ctx context.Context, request model.BookingRequest) (model.BookingRequest, error) {
	return bookingfixture.Update(ctx, u.store.path, u, request)
}
func (u UserStore) removeLegacyBookingFixture(ctx context.Context, id int64) error {
	return bookingfixture.Remove(ctx, u.store.path, u, id)
}
