-- No initial row: deployment defaults remain in effect until an administrator
-- saves installation-wide network settings in the application.
CREATE TABLE network_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    host_check_enabled INTEGER NOT NULL CHECK (host_check_enabled IN (0, 1)),
    allowed_hosts TEXT NOT NULL CHECK (
        length(CAST(allowed_hosts AS BLOB)) <= 33000
        AND json_valid(allowed_hosts)
        AND json_type(allowed_hosts) = 'array'
        AND json_array_length(allowed_hosts) <= 64
    )
);
