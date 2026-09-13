package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/jaysqvl/lake-pass-bot/internal/model"
	"github.com/jaysqvl/lake-pass-bot/internal/origin"
	"github.com/jaysqvl/lake-pass-bot/internal/store"
)

var errNetworkSettingsUnavailable = errors.New("network settings unavailable")

type networkSettingsPageData struct {
	BaseData
	HostCheckEnabled bool
	AllowedHosts     string
	CurrentHost      string
	ManagedReason    string
	FormError        string
}

// Read the singleton directly so a saved setting applies atomically to the
// next request and survives restarts without a separate cache to synchronize.
func (s *Server) privateNetworkSettings(ctx context.Context) (model.NetworkSettings, error) {
	value := model.NetworkSettings{
		HostCheckEnabled: s.config.HostCheckEnabled,
		AllowedHosts:     slices.Clone(s.config.AllowedHosts),
	}
	if s.config.HostCheckConfigured {
		return value, nil
	}
	if s.store != nil {
		saved, err := s.store.SystemGetNetworkSettings(ctx)
		if err == nil {
			value = saved
		} else if !errors.Is(err, store.ErrNotFound) {
			return model.NetworkSettings{}, err
		}
	}
	return value, nil
}

func (s *Server) networkSettingsManagedReason() string {
	if s.config.PublicOrigin != "" {
		return "Public HTTPS is configured for " + s.config.PublicOrigin + ". Hostname checks are required in this mode and are managed in the deployment."
	}
	if s.config.HostCheckConfigured {
		return "Hostname checks are overridden by LAKE_PASS_HOST_CHECK_ENABLED in the deployment. Remove or clear that override to manage them here."
	}
	return ""
}

func (s *Server) networkSettingsPage(w http.ResponseWriter, r *http.Request) {
	var value model.NetworkSettings
	if s.config.PublicOrigin != "" {
		publicURL, _ := url.Parse(s.config.PublicOrigin) // Validated during startup.
		value = model.NetworkSettings{HostCheckEnabled: true, AllowedHosts: []string{publicURL.Host}}
	} else {
		var err error
		value, err = s.privateNetworkSettings(r.Context())
		if err != nil {
			s.internal(w)
			return
		}
	}
	if len(value.AllowedHosts) == 0 {
		// Make the opt-in usable on first visit while retaining the current
		// address. This is a form suggestion, not a persisted hostname rule.
		value.AllowedHosts = []string{r.Host}
	}
	s.renderNetworkSettingsPage(w, r, value, "")
}

func (s *Server) networkSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if reason := s.networkSettingsManagedReason(); reason != "" {
		http.Error(w, reason, http.StatusConflict)
		return
	}
	value := model.NetworkSettings{
		HostCheckEnabled: checked(r, "host_check_enabled"),
		AllowedHosts: strings.FieldsFunc(r.Form.Get("allowed_hosts"), func(c rune) bool {
			return c == '\n' || c == '\r' || c == ','
		}),
	}
	normalized, err := value.Normalize()
	if err != nil {
		s.renderNetworkSettingsPage(w, r, value, safeFormError(err))
		return
	}
	if normalized.HostCheckEnabled && !hostAllowedBy(r.Host, normalized.AllowedHosts) {
		s.renderNetworkSettingsPage(w, r, value, "Keep the current hostname in the allowed list before enabling checks so you can still access Settings.")
		return
	}
	if _, err := s.store.SystemSaveNetworkSettings(r.Context(), normalized); err != nil {
		s.internal(w)
		return
	}
	http.Redirect(w, r, "/settings/network?ok=updated", http.StatusSeeOther)
}

func (s *Server) renderNetworkSettingsPage(w http.ResponseWriter, r *http.Request, value model.NetworkSettings, problem string) {
	host, _ := origin.Host(r.Host)
	data := networkSettingsPageData{
		BaseData: base(r, "Network settings"), HostCheckEnabled: value.HostCheckEnabled,
		AllowedHosts: strings.Join(value.AllowedHosts, "\n"), CurrentHost: host,
		ManagedReason: s.networkSettingsManagedReason(), FormError: problem,
	}
	if r.Method == http.MethodPost {
		data.AllowedHosts = r.Form.Get("allowed_hosts")
	}
	s.render(w, formStatus(problem), "network_settings", data)
}
