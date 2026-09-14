-- New bookings already create their durable job when the user clicks Book.
-- Retire automatic creation from legacy saved requests without changing the
-- inputs or enabled state needed by jobs that are already queued or running.
UPDATE booking_requests SET schedule_enabled = 0 WHERE kind = 'saved';
