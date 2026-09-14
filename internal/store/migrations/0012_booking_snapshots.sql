-- Keep existing requests and schedules intact. New lake bookings instead keep
-- an immutable execution snapshot for exactly as long as its job history.
ALTER TABLE booking_requests ADD COLUMN kind TEXT NOT NULL DEFAULT 'saved'
    CHECK (kind IN ('saved', 'snapshot', 'archived'));

DROP TRIGGER booking_requests_user_limit;
CREATE TRIGGER booking_requests_user_limit
BEFORE INSERT ON booking_requests
WHEN NEW.kind = 'saved'
    AND (SELECT count(*) FROM booking_requests WHERE user_id = NEW.user_id AND kind = 'saved') >= 64
BEGIN
    SELECT RAISE(ABORT, 'per-user booking request limit reached');
END;

-- Snapshot insertion and job admission share one transaction. The retained-job
-- quota therefore also bounds execution snapshots. Reservations intentionally
-- remain independent of this cleanup and continue protecting profile/date.
CREATE TRIGGER jobs_prune_booking_snapshot
AFTER DELETE ON jobs
WHEN OLD.booking_request_id IS NOT NULL
BEGIN
    DELETE FROM booking_requests
    WHERE id = OLD.booking_request_id AND kind IN ('snapshot', 'archived')
        AND NOT EXISTS (SELECT 1 FROM jobs WHERE booking_request_id = OLD.booking_request_id);
END;
