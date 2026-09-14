-- Keep reusable lake setup separate from each booking attempt's snapshot.
ALTER TABLE lake_settings ADD COLUMN booking_profile_id INTEGER
    REFERENCES profiles(id) ON DELETE RESTRICT;

CREATE TRIGGER lake_booking_profile_insert
BEFORE INSERT ON lake_settings
WHEN NEW.booking_profile_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM profiles
    WHERE id = NEW.booking_profile_id AND user_id = NEW.user_id AND lake_id = NEW.lake_id
)
BEGIN
    SELECT RAISE(ABORT, 'booking account must belong to the lake and its owner');
END;

CREATE TRIGGER lake_booking_profile_update
BEFORE UPDATE OF booking_profile_id, user_id, lake_id ON lake_settings
WHEN NEW.booking_profile_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM profiles
    WHERE id = NEW.booking_profile_id AND user_id = NEW.user_id AND lake_id = NEW.lake_id
)
BEGIN
    SELECT RAISE(ABORT, 'booking account must belong to the lake and its owner');
END;

-- Account edits must not invalidate another record's ownership or lake link.
-- Disabling remains allowed: booking then asks the user to repair the choice.
CREATE TRIGGER lake_booking_profile_identity
BEFORE UPDATE OF user_id, lake_id, provider_id ON profiles
WHEN (NEW.user_id <> OLD.user_id OR NEW.lake_id <> OLD.lake_id OR NEW.provider_id <> OLD.provider_id)
    AND EXISTS (SELECT 1 FROM lake_settings WHERE booking_profile_id = OLD.id)
BEGIN
    SELECT RAISE(ABORT, 'an account used for bookings cannot change its owner, lake, or provider');
END;

ALTER TABLE account_settings ADD COLUMN default_confirmation_mode TEXT NOT NULL DEFAULT 'manual'
    CHECK (default_confirmation_mode IN ('manual', 'auto'));
