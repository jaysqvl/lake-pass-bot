package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
	"github.com/jaysqvl/lake-pass-bot/internal/store"
)

func networkLoginStatus(f webFixture, host string) int {
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://"+host+"/login", nil))
	return w.Code
}

func TestNetworkSettingsToggleAppliesImmediatelyAndAfterServerRestart(t *testing.T) {
	f := newWebFixture(t)
	f.server.config.HostCheckEnabled = false
	cookies := loginCookies(t, f)
	if got := networkLoginStatus(f, "unlisted.example"); got != http.StatusOK {
		t.Fatalf("initial unchecked hostname = %d", got)
	}
	form := url.Values{
		"csrf_token": {csrfFrom(cookies)}, "host_check_enabled": {"on"},
		"allowed_hosts": {"EXAMPLE.TEST\nadditional.example:8091\nexample.test"},
	}
	w := serveForm(f, http.MethodPost, "/settings/network", cookies, form)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/settings/network?ok=updated" {
		t.Fatalf("save network settings = %d: %s", w.Code, w.Body.String())
	}
	saved, err := f.store.SystemGetNetworkSettings(context.Background())
	if err != nil || !saved.HostCheckEnabled || !slices.Equal(saved.AllowedHosts, []string{"example.test", "additional.example:8091"}) {
		t.Fatalf("persisted settings = %+v, %v", saved, err)
	}
	assertEnabled := func() {
		t.Helper()
		for _, tc := range []struct {
			host string
			want int
		}{
			{"example.test", http.StatusOK},
			{"additional.example:8091", http.StatusOK},
			{"additional.example:8092", http.StatusBadRequest},
			{"container.internal", http.StatusBadRequest}, // Old deployment seed no longer controls the saved list.
			{"unlisted.example", http.StatusBadRequest},
		} {
			if got := networkLoginStatus(f, tc.host); got != tc.want {
				t.Errorf("hostname %s = %d, want %d", tc.host, got, tc.want)
			}
		}
	}
	restart := func() {
		t.Helper()
		s, err := NewServer(f.cfg, f.store, f.server.engine)
		if err != nil {
			t.Fatal(err)
		}
		f.server, f.handler = s, s.Handler()
	}
	assertEnabled()
	restart()
	assertEnabled()
	form.Del("host_check_enabled")
	w = serveForm(f, http.MethodPost, "/settings/network", cookies, form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("disable hostname checks = %d: %s", w.Code, w.Body.String())
	}
	if got := networkLoginStatus(f, "unlisted.example"); got != http.StatusOK {
		t.Fatalf("disabled hostname = %d", got)
	}
	restart()
	if got := networkLoginStatus(f, "unlisted.example"); got != http.StatusOK {
		t.Fatalf("persisted off must override deployment seed after restart: %d", got)
	}
}

func TestNetworkSettingsRequireAdminOriginAndCSRF(t *testing.T) {
	f := newWebFixture(t)
	adminCookies := loginCookies(t, f)
	member, err := f.store.CreateMember(context.Background(), store.CreateUserInput{Username: "member", Password: "long-member-password"})
	if err != nil {
		t.Fatal(err)
	}
	memberCookies := loginCookiesAs(t, f, member.Username, "long-member-password")
	for _, tc := range []struct {
		name, method, origin, token string
		cookies                     []*http.Cookie
		want                        int
	}{
		{"anonymous page", "GET", "", "", nil, http.StatusSeeOther},
		{"member page", "GET", "", "", memberCookies, http.StatusForbidden},
		{"member save", "POST", "http://example.test", csrfFrom(memberCookies), memberCookies, http.StatusForbidden},
		{"invalid CSRF", "POST", "http://example.test", "invalid", adminCookies, http.StatusForbidden},
		{"foreign origin", "POST", "http://foreign.example", csrfFrom(adminCookies), adminCookies, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{"csrf_token": {tc.token}, "allowed_hosts": {"example.test"}}
			r := authenticatedRequest(tc.method, "http://example.test/settings/network", tc.cookies, form)
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	if _, err := f.store.SystemGetNetworkSettings(context.Background()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unauthorized requests changed settings: %v", err)
	}
}

func TestNetworkSettingsPreventLockoutAndRejectInvalidHosts(t *testing.T) {
	f := newWebFixture(t)
	cookies := loginCookies(t, f)
	for _, hosts := range []string{"other.example", "https://example.test", "*.example", "example.test:99999"} {
		form := url.Values{"csrf_token": {csrfFrom(cookies)}, "host_check_enabled": {"on"}, "allowed_hosts": {hosts}}
		w := serveForm(f, http.MethodPost, "/settings/network", cookies, form)
		if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `id="form-error"`) {
			t.Fatalf("invalid list %q = %d: %s", hosts, w.Code, w.Body.String())
		}
	}
	if _, err := f.store.SystemGetNetworkSettings(context.Background()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("invalid list saved: %v", err)
	}
}

func TestNetworkSettingsDeploymentOverrideAndRecovery(t *testing.T) {
	f := newWebFixture(t)
	cookies := loginCookies(t, f)
	value := model.NetworkSettings{HostCheckEnabled: true, AllowedHosts: []string{"example.test", "saved.example"}}
	if _, err := f.store.SystemSaveNetworkSettings(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	f.server.config.HostCheckConfigured = true
	f.server.config.HostCheckEnabled = false
	if got := networkLoginStatus(f, "recovery.example"); got != http.StatusOK {
		t.Fatalf("off override must recover access: %d", got)
	}
	w := serveForm(f, http.MethodGet, "/settings/network", cookies, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "overridden by LAKE_PASS_HOST_CHECK_ENABLED") {
		t.Fatalf("managed settings page = %d: %s", w.Code, w.Body.String())
	}
	w = serveForm(f, http.MethodPost, "/settings/network", cookies, url.Values{"csrf_token": {csrfFrom(cookies)}, "allowed_hosts": {"changed.example"}})
	if w.Code != http.StatusConflict {
		t.Fatalf("UI must not alter overridden settings: %d", w.Code)
	}
	f.server.config.HostCheckEnabled = true
	f.server.config.AllowedHosts = []string{"example.test", "deployment.example"}
	if networkLoginStatus(f, "deployment.example") != http.StatusOK || networkLoginStatus(f, "saved.example") != http.StatusBadRequest {
		t.Fatal("enabled deployment override did not use deployment hostnames")
	}
	f.server.config.HostCheckConfigured = false
	if networkLoginStatus(f, "deployment.example") != http.StatusBadRequest || networkLoginStatus(f, "saved.example") != http.StatusOK {
		t.Fatal("clearing the override did not restore saved UI settings")
	}
}

func TestNetworkSettingsCannotWeakenPublicHTTPS(t *testing.T) {
	f := publicFixture(t)
	if _, err := f.store.SystemSaveNetworkSettings(context.Background(), model.NetworkSettings{}); err != nil {
		t.Fatal(err)
	}
	_, credentials, ok, err := f.store.AuthenticateAndCreateSessionInScope(context.Background(), "admin", "long-test-password", sessionLifetime, f.cfg.PublicOrigin)
	if err != nil || !ok {
		t.Fatalf("create public session: ok=%t, error=%v", ok, err)
	}
	r := publicRequest(http.MethodPost, "http://example.test/settings/network")
	r.Header.Set("Origin", "https://example.test")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	form := url.Values{"csrf_token": {credentials.CSRFToken}, "allowed_hosts": {"example.test"}}
	r.Body = io.NopCloser(strings.NewReader(form.Encode()))
	r.AddCookie(&http.Cookie{Name: "__Host-" + sessionCookie, Value: credentials.Token})
	r.AddCookie(&http.Cookie{Name: "__Host-" + csrfCookie, Value: credentials.CSRFToken})
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("public network settings mutation = %d: %s", w.Code, w.Body.String())
	}
	if networkLoginStatus(f, "other.example") != http.StatusBadRequest {
		t.Fatal("saved private off setting weakened public host enforcement")
	}
}

func TestNetworkSettingsReadFailureDoesNotSilentlyDisableChecks(t *testing.T) {
	f := newWebFixture(t)
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	if got := networkLoginStatus(f, "example.test"); got != http.StatusServiceUnavailable {
		t.Fatalf("unavailable policy = %d", got)
	}
}

func TestPublicNetworkPageUsesOnlyDeploymentPolicy(t *testing.T) {
	f := publicFixture(t)
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	// Exercise page rendering independently of authentication. Its public-mode
	// settings come entirely from the deployment, regardless of private storage.
	w := httptest.NewRecorder()
	f.server.networkSettingsPage(w, publicRequest(http.MethodGet, "http://example.test/settings/network"))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Public HTTPS is configured") {
		t.Fatalf("public policy display read private settings: %d: %s", w.Code, w.Body.String())
	}
}
