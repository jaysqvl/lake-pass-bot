package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
)

// SystemGetNetworkSettings reads installation-wide settings. ErrNotFound means
// the administrator has not saved an override of the deployment defaults.
func (s *Store) SystemGetNetworkSettings(ctx context.Context) (model.NetworkSettings, error) {
	var settings model.NetworkSettings
	var allowedHosts string
	err := s.db.QueryRowContext(ctx, `
		SELECT host_check_enabled, allowed_hosts FROM network_settings WHERE id = 1
	`).Scan(&settings.HostCheckEnabled, &allowedHosts)
	if errors.Is(err, sql.ErrNoRows) {
		return model.NetworkSettings{}, ErrNotFound
	}
	if err != nil {
		return model.NetworkSettings{}, fmt.Errorf("read network settings: %w", err)
	}
	if err := json.Unmarshal([]byte(allowedHosts), &settings.AllowedHosts); err != nil {
		return model.NetworkSettings{}, fmt.Errorf("read allowed hostnames: %w", err)
	}
	normalized, err := settings.Normalize()
	if err != nil {
		return model.NetworkSettings{}, fmt.Errorf("read network settings: %w", err)
	}
	return normalized, nil
}

// SystemSaveNetworkSettings atomically replaces the installation-wide settings.
// Callers must authorize administrator access before invoking this method.
func (s *Store) SystemSaveNetworkSettings(ctx context.Context, settings model.NetworkSettings) (model.NetworkSettings, error) {
	normalized, err := settings.Normalize()
	if err != nil {
		return model.NetworkSettings{}, err
	}
	allowedHosts, err := json.Marshal(normalized.AllowedHosts)
	if err != nil {
		return model.NetworkSettings{}, fmt.Errorf("encode allowed hostnames: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO network_settings(id, host_check_enabled, allowed_hosts)
		VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			host_check_enabled = excluded.host_check_enabled,
			allowed_hosts = excluded.allowed_hosts
	`, normalized.HostCheckEnabled, string(allowedHosts))
	if err != nil {
		return model.NetworkSettings{}, fmt.Errorf("save network settings: %w", err)
	}
	return normalized, nil
}
