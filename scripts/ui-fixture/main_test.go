package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func fixtureRequest(handler http.Handler, method, target string, cookies []*http.Cookie, form url.Values, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:18092"+target, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestFixtureAuthenticationRemainsEnabledByDefault(t *testing.T) {
	handler, cleanup, err := newFixture(false)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	w := fixtureRequest(handler, http.MethodGet, "/api/lakes", nil, nil, "")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/api/setup" {
		t.Fatalf("want setup redirect, got %d %q", w.Code, w.Header().Get("Location"))
	}
}

func TestDevelopmentSignInSurvivesFreshFixtureAndPreservesCSRF(t *testing.T) {
	var cookies []*http.Cookie
	for restart := 0; restart < 2; restart++ {
		handler, cleanup, err := newFixture(true)
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer cleanup()
			// The second fixture receives cookies from the deleted first database.
			w := fixtureRequest(handler, http.MethodGet, "/api/lakes", cookies, nil, "")
			if w.Code != http.StatusOK {
				t.Fatalf("restart %d: want authenticated page, got %d: %s", restart, w.Code, w.Body.String())
			}
			var page struct {
				Data struct {
					Authenticated bool
					Username      string
					CSRFToken     string
				}
			}
			if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if !page.Data.Authenticated || page.Data.Username != "preview-admin" || page.Data.CSRFToken == "" {
				t.Fatal("automatic sign-in did not produce an administrator session and CSRF token")
			}
			cookies = w.Result().Cookies()
			if len(cookies) != 2 {
				t.Fatalf("want session and CSRF cookies, got %d", len(cookies))
			}
			for _, cookie := range cookies {
				if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
					t.Fatal("development cookies lost normal protections")
				}
			}
			reused := fixtureRequest(handler, http.MethodGet, "/api/lakes", cookies, nil, "")
			if reused.Code != http.StatusOK || len(reused.Result().Cookies()) != 0 {
				t.Fatal("a valid session should be reused without issuing more cookies")
			}
			for _, attempt := range []struct {
				name, token, origin string
			}{
				{"missing CSRF", "", "http://127.0.0.1:18092"},
				{"cross origin", page.Data.CSRFToken, "http://example.invalid"},
			} {
				rejected := fixtureRequest(handler, http.MethodPost, "/api/logout", cookies, url.Values{"csrf_token": {attempt.token}}, attempt.origin)
				if rejected.Code != http.StatusForbidden {
					t.Fatalf("%s: want 403, got %d", attempt.name, rejected.Code)
				}
			}
			// A valid mutation still reaches the real authenticated handler.
			accepted := fixtureRequest(handler, http.MethodPost, "/api/logout", cookies, url.Values{"csrf_token": {page.Data.CSRFToken}}, "http://127.0.0.1:18092")
			if accepted.Code != http.StatusSeeOther || accepted.Header().Get("Location") != "/api/login" {
				t.Fatalf("valid sign out: got %d %q", accepted.Code, accepted.Header().Get("Location"))
			}
			// Logout/revocation also recovers automatically on the next GET.
			if next := fixtureRequest(handler, http.MethodGet, "/api/lakes", cookies, nil, ""); next.Code != http.StatusOK {
				t.Fatalf("automatic sign-in after revocation: got %d", next.Code)
			}
		}()
	}
}
