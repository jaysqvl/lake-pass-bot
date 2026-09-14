package web

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jaysqvl/lake-pass-bot/internal/destinations"
	"github.com/jaysqvl/lake-pass-bot/internal/engine"
	"github.com/jaysqvl/lake-pass-bot/internal/model"
	"github.com/jaysqvl/lake-pass-bot/internal/store"
)

type lakeBookingSetup struct {
	Lake     destinations.Lake
	Settings model.LakeSettings
	Account  model.AccountSettings
	Profile  model.Profile
}

// Setup issues are actionable on Lakes; storage failures remain server errors.
func (s *Server) lakeBookingSetup(r *http.Request, lakeID string) (lakeBookingSetup, string, error) {
	var setup lakeBookingSetup
	lake, settings, _, err := s.effectiveLakeSettings(r, lakeID)
	if err != nil {
		return setup, "", err
	}
	setup.Lake, setup.Settings = lake, settings
	resources := s.userStore(r)
	profiles, err := resources.ListProfiles(r.Context())
	if err != nil {
		return setup, "", err
	}
	setup.Profile, err = model.ResolveLakeBookingProfile(lake, settings, profiles)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrLakeConnectionAmbiguous):
			return setup, "Choose which account to use for bookings on this lake’s page.", nil
		case errors.Is(err, model.ErrLakeConnectionRequired):
			return setup, "Connect your account on this lake’s page before booking.", nil
		default:
			return setup, "Update the booking account selected on this lake’s page.", nil
		}
	}
	if err := setup.Profile.ValidateForOrigins(s.config.YodelOrigins); err != nil {
		return setup, "Update your lake connection before booking.", nil
	}
	if _, err := resources.GetDefaultOTPSource(r.Context()); errors.Is(err, store.ErrNotFound) {
		return setup, "Choose a default OTP source, then finish connecting this lake.", nil
	} else if err != nil {
		return setup, "", err
	}
	if strings.TrimSpace(settings.VehicleKeyword) == "" {
		return setup, "Set your vehicle keyword on this lake’s page before booking.", nil
	}
	if err := settings.ValidateForOrigins(s.config.YodelOrigins); err != nil {
		return setup, "Review the saved preferences on this lake’s page before booking.", nil
	}
	setup.Account, err = resources.GetAccountSettings(r.Context())
	return setup, "", err
}

type bookingLakeCard struct {
	Lake    destinations.Lake
	Ready   bool
	Problem string
}

type lakeBookingsData struct {
	BaseData
	Lakes []bookingLakeCard
	Saved []dashboardCard
}

func (s *Server) lakeBookingsPage(w http.ResponseWriter, r *http.Request) {
	data := lakeBookingsData{BaseData: base(r, "Bookings")}
	for _, lake := range destinations.List() {
		_, problem, err := s.lakeBookingSetup(r, lake.ID)
		if err != nil {
			s.internal(w)
			return
		}
		data.Lakes = append(data.Lakes, bookingLakeCard{
			Lake: lake, Ready: problem == "", Problem: problem,
		})
	}
	resources := s.userStore(r)
	if data.Flash != nil {
		switch r.URL.Query().Get("notice") {
		case "queue-pending", "queue-review":
			if job, err := resources.GetJob(r.Context(), parseInt64(r.URL.Query().Get("job"))); err == nil {
				data.Flash.ActionLabel = "View existing job"
				data.Flash.ActionURL = fmt.Sprintf("/jobs/%d", job.ID)
			}
		case "queue-full", "queue-unavailable":
			data.Flash.ActionLabel, data.Flash.ActionURL = "View jobs", "/jobs"
		}
	}
	saved, err := resources.ListSavedBookingRequests(r.Context())
	if err != nil {
		s.internal(w)
		return
	}
	for _, request := range saved {
		card := listCard{
			Title: request.Name, URL: fmt.Sprintf("/bookings/%d", request.ID), Subtitle: lakeName(request.LakeID), Status: request.TargetDate,
			Fields:  []labelValue{{Label: "Pass choices", Value: strings.Join(passNames(request.PassOrder()), " → ")}},
			Actions: []cardAction{{Label: "View request", URL: fmt.Sprintf("/bookings/%d", request.ID)}},
		}
		if request.ScheduleEnabled && request.Enabled {
			card.Description = "Saved for automatic queueing when server scheduling is enabled."
		}
		card.Actions = append(card.Actions, cardAction{Label: "Delete", URL: fmt.Sprintf("/bookings/%d#delete", request.ID), Class: "ghost"})
		data.Saved = append(data.Saved, dashboardCard{listCard: card, CSRFToken: data.CSRFToken})
	}
	s.render(w, http.StatusOK, "bookings", data)
}

type quickBookingData struct {
	BaseData
	Lake         destinations.Lake
	AccountName  string
	Problem      string
	FormError    string
	Visit        formSection
	Passes       formSection
	Confirmation string
}

func (s *Server) lakeBookingNew(w http.ResponseWriter, r *http.Request) {
	s.renderQuickBooking(w, r, "")
}

func (s *Server) renderQuickBooking(w http.ResponseWriter, r *http.Request, formError string) {
	lakeID := r.URL.Query().Get("lake_id")
	if r.Method == http.MethodPost {
		lakeID = r.Form.Get("lake_id")
	}
	if _, err := destinations.Resolve(lakeID); err != nil {
		http.NotFound(w, r)
		return
	}
	setup, problem, err := s.lakeBookingSetup(r, lakeID)
	if err != nil {
		s.internal(w)
		return
	}
	location, err := time.LoadLocation(setup.Settings.Timezone)
	if err != nil {
		s.internal(w)
		return
	}
	now := time.Now().In(location)
	date := now.AddDate(0, 0, 1).Format(time.DateOnly)
	if r.Method == http.MethodPost {
		date = r.Form.Get("target_date")
	}
	data := quickBookingData{
		BaseData: base(r, "Book "+setup.Lake.Name), Lake: setup.Lake,
		AccountName: setup.Profile.Name, Problem: problem, FormError: formError,
		Visit:        formSection{Title: "Your visit", Fields: []formField{{Name: "target_date", Label: "Visit date", Type: "date", Required: true, Value: date, Min: now.Format(time.DateOnly)}}},
		Passes:       formSection{Title: "Pass preferences", Help: "Try these in order. Choose None to skip a choice.", Class: "form-grid-pass-preferences"},
		Confirmation: "The job will ask for your approval before confirming the pass.",
	}
	if setup.Account.DefaultConfirmationMode == model.RunModeAuto {
		data.Confirmation = "Future release jobs confirm automatically. Already-released passes still ask for your approval."
	}
	for i, label := range []string{"First choice", "Second choice", "Third choice"} {
		selected := ""
		if i < len(setup.Settings.PreferredPasses) {
			selected = string(setup.Settings.PreferredPasses[i])
		}
		name := fmt.Sprintf("pass_priority_%d", i+1)
		if r.Method == http.MethodPost {
			selected = r.Form.Get(name)
		}
		field := formField{Name: name, Label: label, Type: "select"}
		for _, pass := range setup.Lake.SupportedPasses {
			field.Options = append(field.Options, selectOption{Value: pass, Label: passOptionLabel(pass), Selected: selected == pass})
		}
		if selected != "" && !slices.Contains(setup.Lake.SupportedPasses, selected) {
			field.Options = append(field.Options, selectOption{Value: selected, Label: "Unsupported choice: " + selected, Selected: true})
		}
		field.Options = append(field.Options, selectOption{Label: "None", Selected: selected == ""})
		data.Passes.Fields = append(data.Passes.Fields, field)
	}
	s.render(w, formStatus(formError), "quick_booking", data)
}

func (s *Server) lakeBookingCreate(w http.ResponseWriter, r *http.Request) {
	lakeID := r.Form.Get("lake_id")
	if _, err := destinations.Resolve(lakeID); err != nil {
		http.NotFound(w, r)
		return
	}
	setup, problem, err := s.lakeBookingSetup(r, lakeID)
	if err != nil {
		s.internal(w)
		return
	}
	if problem != "" {
		s.renderQuickBooking(w, r, problem)
		return
	}
	request := setup.Account.ApplyToBooking(setup.Settings.ApplyTo(model.BookingRequest{
		Name: setup.Lake.Name, ProfileID: setup.Profile.ID, Enabled: true,
		TargetDate: strings.TrimSpace(r.Form.Get("target_date")), ConfirmationMode: setup.Account.DefaultConfirmationMode,
	}))
	if request.ConfirmationMode == "" {
		request.ConfirmationMode = model.RunModeManual
	}
	// The only trip input is date and pass order. Setup fields in an old or
	// crafted form cannot override the selected lake/account's saved policy.
	request.PreferredPasses = []model.PassType{}
	for i := 1; i <= 3; i++ {
		if value := r.Form.Get(fmt.Sprintf("pass_priority_%d", i)); value != "" {
			request.PreferredPasses = append(request.PreferredPasses, model.PassType(value))
		}
	}
	job, err := s.engine.QueueLakeBooking(r.Context(), s.userStore(r).UserID(), request)
	if err != nil {
		message := safeFormError(err)
		switch {
		case errors.Is(err, store.ErrResourceLimit):
			message = "Your job queue is full. Review Jobs before trying again."
		case errors.Is(err, store.ErrConflict):
			message = "This account already has a booking or pending job for that date. Review Jobs before trying again."
		case errors.Is(err, engine.ErrBookingDatePassed):
			message = "Choose today or a future visit date."
		}
		s.renderQuickBooking(w, r, message)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/jobs/%d?ok=queued", job.ID), http.StatusSeeOther)
}

type savedBookingData struct {
	BaseData
	Request                   model.BookingRequest
	LakeName, Passes, Problem string
	PendingJobURL             string
}

func (s *Server) savedBookingPage(w http.ResponseWriter, r *http.Request) {
	s.renderSavedBooking(w, r, "")
}

func (s *Server) renderSavedBooking(w http.ResponseWriter, r *http.Request, problem string) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	request, err := s.userStore(r).GetBookingRequest(r.Context(), id)
	if err != nil {
		s.notFoundOrInternal(w, err)
		return
	}
	if request.Kind != model.BookingKindSaved {
		http.NotFound(w, r)
		return
	}
	data := savedBookingData{BaseData: base(r, request.Name), Request: request, LakeName: lakeName(request.LakeID), Passes: strings.Join(passNames(request.PassOrder()), " → "), Problem: problem}
	job, err := s.pendingResourceJob(r, func(job model.Job) bool { return job.BookingRequestID != nil && *job.BookingRequestID == id })
	if err != nil {
		s.internal(w)
		return
	}
	if job != nil {
		data.PendingJobURL = fmt.Sprintf("/jobs/%d", job.ID)
	}
	s.render(w, formStatus(problem), "saved_booking", data)
}

func (s *Server) savedBookingDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.userStore(r).DeleteBookingRequest(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderSavedBooking(w, r, "This request has a pending job. Cancel it from Jobs or wait for it to finish, then delete the saved request.")
		} else {
			s.notFoundOrInternal(w, err)
		}
		return
	}
	http.Redirect(w, r, "/bookings?ok=deleted", http.StatusSeeOther)
}

func bookingDisplayName(request model.BookingRequest) string {
	if request.Kind == model.BookingKindSnapshot {
		return lakeName(request.LakeID) + " · " + request.TargetDate
	}
	return request.Name
}
