package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRebrandPreservesLegacySessionsAndLogout(t *testing.T) {
	for _, public := range []bool{false, true} {
		t.Run(map[bool]string{false: "private", true: "public"}[public], func(t *testing.T) {
			fixture := newWebFixture(t)
			if public {
				fixture = publicFixture(t)
			}
			var issued []*http.Cookie
			if public {
				issued = publicLoginCookies(t, fixture)
			} else {
				issued = loginCookies(t, fixture)
			}
			var cookies []*http.Cookie
			var csrf, session string
			for _, cookie := range issued {
				switch cookie.Name {
				case fixture.server.cookieName(sessionCookie):
					session = cookie.Value
					cookies = append(cookies, &http.Cookie{Name: fixture.server.cookieName("buntzen_session"), Value: cookie.Value})
				case fixture.server.cookieName(csrfCookie):
					csrf = cookie.Value
					cookies = append(cookies, &http.Cookie{Name: fixture.server.cookieName("buntzen_csrf"), Value: cookie.Value})
				}
			}
			request := func(method, path string, values url.Values) *http.Request {
				r := authenticatedRequest(method, "http://example.test"+path, cookies, values)
				if public {
					r = publicRequest(method, "http://example.test"+path)
					for _, cookie := range cookies {
						r.AddCookie(cookie)
					}
					r.Header.Set("Origin", "https://example.test")
				} else {
					r.Header.Set("Origin", "http://example.test")
				}
				if values != nil {
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					r.Body = io.NopCloser(strings.NewReader(values.Encode()))
				}
				return r
			}
			w := httptest.NewRecorder()
			fixture.handler.ServeHTTP(w, request(http.MethodGet, "/account", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("old session rejected: %d", w.Code)
			}
			bad := request(http.MethodGet, "/account", nil)
			bad.AddCookie(&http.Cookie{Name: fixture.server.cookieName(sessionCookie), Value: "invalid-new-session"})
			w = httptest.NewRecorder()
			fixture.handler.ServeHTTP(w, bad)
			if w.Code != http.StatusSeeOther {
				t.Fatal("invalid new identity fell back to legacy session")
			}
			if public {
				bad = publicRequest(http.MethodGet, "http://example.test/account")
				bad.AddCookie(&http.Cookie{Name: "buntzen_session", Value: session})
				bad.AddCookie(&http.Cookie{Name: "buntzen_csrf", Value: csrf})
				w = httptest.NewRecorder()
				fixture.handler.ServeHTTP(w, bad)
				if w.Code != http.StatusSeeOther {
					t.Fatal("public mode accepted unprefixed legacy cookies")
				}
			}
			w = httptest.NewRecorder()
			fixture.handler.ServeHTTP(w, request(http.MethodPost, "/logout", url.Values{"csrf_token": {csrf}}))
			if w.Code != http.StatusSeeOther {
				t.Fatalf("legacy logout failed: %d", w.Code)
			}
			cleared := make(map[string]bool)
			for _, cookie := range w.Result().Cookies() {
				cleared[cookie.Name] = cookie.MaxAge == -1
			}
			for _, name := range []string{sessionCookie, csrfCookie, "buntzen_session", "buntzen_csrf"} {
				if !cleared[fixture.server.cookieName(name)] {
					t.Fatalf("cookie %s not cleared", name)
				}
			}
			if _, err := fixture.store.GetSession(context.Background(), session); err == nil {
				t.Fatal("logout retained server session")
			}
		})
	}
}

func TestLakeBookingSelectionRejectsUnsupportedLakesAndRetiredRequestRoutes(t *testing.T) {
	fixture := newWebFixture(t)
	_, saved := createImmediateWebBooking(t, fixture, fixture.admin.ID, "lake owner", true)
	cookies := loginCookies(t, fixture)
	page := serveForm(fixture, http.MethodGet, "/bookings", cookies, nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "/bookings/new?lake_id=buntzen") || !strings.Contains(page.Body.String(), "Buntzen Lake") {
		t.Fatal("bookings did not expose the supported lake")
	}
	path := "/bookings/" + stringID(saved.ID)
	page = serveForm(fixture, http.MethodGet, path, cookies, nil)
	if page.Code != http.StatusNotFound {
		t.Fatalf("retired request GET=%d", page.Code)
	}
	form := url.Values{"csrf_token": {csrfFrom(cookies)}, "lake_id": {"unsupported"}, "target_date": {"2030-01-15"}, "pass_priority_1": {"all_day"}}
	response := serveForm(fixture, http.MethodPost, path, cookies, form)
	if response.Code != http.StatusNotFound {
		t.Fatalf("removed request update route status=%d", response.Code)
	}
	retained, err := fixture.store.ForUser(fixture.admin.ID).GetBookingRequest(context.Background(), saved.ID)
	if err != nil || retained.LakeID != saved.LakeID || retained.TargetDate != saved.TargetDate {
		t.Fatal("removed update route changed the saved request")
	}
	response = serveForm(fixture, http.MethodPost, "/bookings/new", cookies, form)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unsupported booking lake status=%d", response.Code)
	}
	page = serveForm(fixture, http.MethodGet, "/bookings/new?lake_id=unsupported", cookies, nil)
	if page.Code != http.StatusNotFound {
		t.Fatal("unknown lake query silently defaulted")
	}
	jobs, err := fixture.store.ForUser(fixture.admin.ID).ListJobs(context.Background(), 10)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("invalid lake created jobs: %+v err=%v", jobs, err)
	}
}
