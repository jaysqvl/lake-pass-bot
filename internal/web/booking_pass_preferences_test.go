package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
)

func assertBookingPassChoices(t *testing.T, body string, choices []string) {
	t.Helper()
	for i, choice := range choices {
		field := fmt.Sprintf("pass_priority_%d", i+1)
		selectMarkup := regexp.MustCompile(`(?s)<select name="` + field + `"[^>]*>(.*?)</select>`).FindStringSubmatch(body)
		if len(selectMarkup) != 2 || !strings.Contains(selectMarkup[1], `value="`+choice+`" selected`) {
			t.Fatalf("%s did not retain selection %q: %v", field, choice, selectMarkup)
		}
	}
}

func TestBookingPassPriorityQueuesInOrderAndPreservesInvalidDraft(t *testing.T) {
	fixture := newWebFixture(t)
	createImmediateWebBooking(t, fixture, fixture.admin.ID, "priority owner", true)
	cookies := loginCookies(t, fixture)
	form := url.Values{
		"csrf_token": {csrfFrom(cookies)}, "lake_id": {"buntzen"}, "target_date": {"2030-01-15"},
		"pass_priority_1": {"morning"}, "pass_priority_2": {""}, "pass_priority_3": {"all_day"},
	}
	response := serveForm(fixture, http.MethodPost, "/bookings/new", cookies, form)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("queue custom order=%d body=%s", response.Code, response.Body.String())
	}
	snapshot := latestQueuedBooking(t, fixture, fixture.admin.ID)
	if !slices.Equal(snapshot.PassOrder(), []model.PassType{model.PassMorning, model.PassAllDay}) {
		t.Fatalf("None gap lost custom priority: %+v", snapshot)
	}
	for _, test := range []struct {
		name    string
		choices []string
		date    string
		message string
	}{
		{"duplicate choices", []string{"morning", "", "morning"}, "2030-01-16", "only be selected once"},
		{"no choices", []string{"", "", ""}, "2030-01-16", "at least one pass preference"},
		{"invalid date", []string{"morning", "", "all_day"}, "invalid", "target date"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for i, choice := range test.choices {
				form.Set(fmt.Sprintf("pass_priority_%d", i+1), choice)
			}
			form.Set("target_date", test.date)
			response := serveForm(fixture, http.MethodPost, "/bookings/new", cookies, form)
			if response.Code != http.StatusUnprocessableEntity || !strings.Contains(strings.ToLower(response.Body.String()), test.message) {
				t.Fatalf("validation response=%d body=%s", response.Code, response.Body.String())
			}
			assertBookingPassChoices(t, response.Body.String(), test.choices)
			if !strings.Contains(response.Body.String(), `name="target_date" value="`+test.date+`"`) {
				t.Fatal("validation failure discarded the submitted date")
			}
			resources := fixture.store.ForUser(fixture.admin.ID)
			jobs, err := resources.ListJobs(context.Background(), 10)
			if err != nil || len(jobs) != 1 {
				t.Fatalf("invalid visit created a job: %+v err=%v", jobs, err)
			}
			bookings, err := resources.ListBookingRequests(context.Background())
			if err != nil || len(bookings) != 2 {
				t.Fatalf("invalid visit created an orphan snapshot: %+v err=%v", bookings, err)
			}
			unchanged, err := resources.GetBookingRequest(context.Background(), snapshot.ID)
			if err != nil || !slices.Equal(unchanged.PassOrder(), snapshot.PassOrder()) {
				t.Fatalf("invalid visit changed existing work: %+v err=%v", unchanged, err)
			}
		})
	}
}
