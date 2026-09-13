package model

import (
	"slices"
	"strings"
	"testing"
)

func TestNetworkSettingsNormalizeExactAuthorities(t *testing.T) {
	input := NetworkSettings{
		HostCheckEnabled: true,
		AllowedHosts: []string{
			" Lake.EXAMPLE. ", "lake.example", "", " \t", "192.0.2.10:8091",
			"[2001:DB8::1]:8091", "[::1]", "LAKE.EXAMPLE:8080",
		},
	}
	got, err := input.Normalize()
	want := []string{"lake.example", "192.0.2.10:8091", "[2001:db8::1]:8091", "[::1]", "lake.example:8080"}
	if err != nil || !got.HostCheckEnabled || !slices.Equal(got.AllowedHosts, want) {
		t.Fatalf("normalized settings=%+v err=%v", got, err)
	}
	input.AllowedHosts[0] = "mutated.example"
	if got.AllowedHosts[0] != "lake.example" {
		t.Fatal("normalized settings retained the caller's mutable host slice")
	}
	zero, err := (NetworkSettings{}).Normalize()
	if err != nil || zero.HostCheckEnabled || len(zero.AllowedHosts) != 0 {
		t.Fatalf("zero settings=%+v err=%v", zero, err)
	}
}

func TestNetworkSettingsRejectInvalidAndUnboundedAuthorities(t *testing.T) {
	for _, invalid := range []string{
		"https://lake.example", "lake.example/path", "lake.example?query=yes", "lake.example#fragment",
		"user@example.test", "*.example", "lake.example:0", "lake.example:65536", "lake.example:abc",
		"lake.example\\path", "lake.example\r\nX-Header: value", strings.Repeat("a", MaxNetworkHostBytes+1),
	} {
		t.Run(invalid, func(t *testing.T) {
			// Invalid hostnames must not become latent policy when checks are off.
			for _, enabled := range []bool{false, true} {
				if _, err := (NetworkSettings{HostCheckEnabled: enabled, AllowedHosts: []string{invalid}}).Normalize(); err == nil {
					t.Fatalf("invalid hostname accepted with checks enabled=%v", enabled)
				}
			}
		})
	}
	if _, err := (NetworkSettings{AllowedHosts: make([]string, MaxNetworkAllowedHosts+1)}).Normalize(); err == nil {
		t.Fatal("unbounded host list accepted")
	}
}
