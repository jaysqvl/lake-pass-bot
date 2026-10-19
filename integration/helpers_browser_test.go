//go:build integration

package integration_test

import (
	"path/filepath"
	"testing"
)

func TestBrowserPathUsesPinnedPlaywrightByDefault(t *testing.T) {
	for _, configured := range []string{"", "   "} {
		t.Setenv("LAKE_PASS_E2E_BROWSER_EXECUTABLE", configured)
		if got := browserPath(); got != "" {
			t.Fatalf("default browser override = %q; want the locked Playwright browser", got)
		}
	}
}

func TestBrowserPathPreservesExplicitOperatorOverride(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "operator-browser")
	t.Setenv("LAKE_PASS_E2E_BROWSER_EXECUTABLE", "  "+executable+"  ")
	if got := browserPath(); got != executable {
		t.Fatalf("explicit browser override = %q, want %q", got, executable)
	}
}
