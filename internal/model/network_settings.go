package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jaysqvl/lake-pass-bot/internal/origin"
)

const (
	MaxNetworkAllowedHosts = 64
	MaxNetworkHostBytes    = 512
)

// NetworkSettings apply to the entire installation, rather than one account.
// The zero value leaves private HTTP hostname checks disabled.
type NetworkSettings struct {
	HostCheckEnabled bool
	AllowedHosts     []string
}

func (s NetworkSettings) Normalize() (NetworkSettings, error) {
	if len(s.AllowedHosts) > MaxNetworkAllowedHosts {
		return NetworkSettings{}, fmt.Errorf("enter no more than %d allowed hostnames", MaxNetworkAllowedHosts)
	}
	normalized := NetworkSettings{HostCheckEnabled: s.HostCheckEnabled, AllowedHosts: []string{}}
	seen := make(map[string]bool, len(s.AllowedHosts))
	for _, value := range s.AllowedHosts {
		if len(value) > MaxNetworkHostBytes {
			return NetworkSettings{}, fmt.Errorf("each allowed hostname must be no longer than %d bytes", MaxNetworkHostBytes)
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		host, err := origin.Host(value)
		if err != nil {
			return NetworkSettings{}, errors.New("enter exact hostnames or IP addresses with an optional port, without URLs, paths, or wildcards")
		}
		if !seen[host] {
			normalized.AllowedHosts = append(normalized.AllowedHosts, host)
			seen[host] = true
		}
	}
	return normalized, nil
}
