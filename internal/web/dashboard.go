package web

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
)

type dashboardData struct {
	BaseData
	BookingCount int
	Jobs         []jobRow
	Connections  []lakeConnection
	Bookings     []dashboardBooking
}

type dashboardBooking struct{ Name, URL, Date string }

type dashboardCard struct {
	listCard
	CSRFToken string
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	connections, err := s.lakeConnections(r)
	if err != nil {
		s.internal(w)
		return
	}
	configured := false
	for _, connection := range connections {
		configured = configured || connection.Configured
	}
	if !configured {
		http.Redirect(w, r, "/lakes", http.StatusSeeOther)
		return
	}
	userStore := s.userStore(r)
	bookings, err := userStore.ListBookingRequests(r.Context())
	if err != nil {
		s.internal(w)
		return
	}
	jobs, err := userStore.ListJobs(r.Context(), 200)
	if err != nil {
		s.internal(w)
		return
	}
	recent := jobs
	if len(recent) > 10 {
		recent = recent[:10]
	}
	visits := upcomingVisits(bookings, jobs, time.Now())
	data := dashboardData{
		BaseData: base(r, "Home"), Jobs: s.jobRows(r.Context(), userStore, recent),
		Connections: connections, BookingCount: len(visits), Bookings: visits,
	}
	if len(data.Bookings) > 3 {
		data.Bookings = data.Bookings[:3]
	}
	s.render(w, http.StatusOK, "dashboard", data)
}

// A visit appears only after it has a pending or successful booking job. Older
// request records remain readable for those jobs, but do not create visits.
func upcomingVisits(bookings []model.BookingRequest, jobs []model.Job, now time.Time) []dashboardBooking {
	byID := make(map[int64]model.BookingRequest, len(bookings))
	for _, booking := range bookings {
		byID[booking.ID] = booking
	}
	upcoming := make([]dashboardBooking, 0)
	seen := make(map[int64]bool)
	for _, job := range jobs {
		if job.BookingRequestID == nil || job.Command != model.CommandBook || (job.Status.Terminal() && job.Status != model.JobSucceeded) {
			continue
		}
		booking, ok := byID[*job.BookingRequestID]
		if !ok || !booking.Enabled || seen[booking.ID] || job.UserID != booking.UserID || job.ProfileID != booking.ProfileID {
			continue
		}
		// Old saved requests could be edited after completion, so their current
		// date is not evidence of an upcoming confirmed visit.
		if job.Status.Terminal() && booking.Kind != model.BookingKindSnapshot {
			continue
		}
		location, err := time.LoadLocation(booking.Timezone)
		if err != nil {
			location = time.UTC
		}
		if booking.TargetDate < now.In(location).Format(time.DateOnly) {
			continue
		}
		seen[booking.ID] = true
		upcoming = append(upcoming, dashboardBooking{
			Name: lakeName(booking.EffectiveLakeID()), URL: fmt.Sprintf("/jobs/%d", job.ID), Date: booking.TargetDate,
		})
	}
	sort.SliceStable(upcoming, func(i, j int) bool { return upcoming[i].Date < upcoming[j].Date })
	return upcoming
}
