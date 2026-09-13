package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestNetworkSettingsRendersEditableAndManagedState(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		enabled bool
		managed string
	}{
		{name: "default off"},
		{name: "enabled", enabled: true},
		{name: "managed off", managed: "Managed by deployment configuration."},
		{name: "managed on", enabled: true, managed: "Managed by the public HTTPS configuration."},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := struct {
				BaseData
				HostCheckEnabled bool
				AllowedHosts     string
				CurrentHost      string
				ManagedReason    string
				FormError        string
			}{
				BaseData:         BaseData{Authenticated: true, IsAdmin: true, CurrentPath: "/settings/network", CSRFToken: "network-csrf"},
				HostCheckEnabled: test.enabled,
				AllowedHosts:     "lake-pass.example:8080\nbackup.example:8080",
				CurrentHost:      "lake-pass.example:8080",
				ManagedReason:    test.managed,
			}
			response := httptest.NewRecorder()
			if err := renderer.Render(response, http.StatusOK, "network_settings", data); err != nil {
				t.Fatal(err)
			}
			body := response.Body.String()
			checkbox := regexp.MustCompile(`<input\b[^>]*id="host-check-enabled"[^>]*>`).FindString(body)
			if checkbox == "" || !strings.Contains(checkbox, `role="switch"`) || strings.Contains(checkbox, " checked") != test.enabled {
				t.Fatalf("toggle does not represent saved state: %s", checkbox)
			}
			textarea := regexp.MustCompile(`<textarea\b[^>]*id="allowed-hosts"[^>]*>`).FindString(body)
			save := regexp.MustCompile(`<button\b[^>]*>Save network settings</button>`).FindString(body)
			for _, control := range []string{checkbox, textarea, save} {
				if control == "" || strings.Contains(control, " disabled") != (test.managed != "") {
					t.Fatalf("control does not respect deployment management: %s", control)
				}
			}
			if test.managed != "" && !strings.Contains(body, test.managed) {
				t.Fatal("managed controls have no explanation")
			}
			for _, required := range []string{
				`action="/settings/network" method="post"`,
				`name="csrf_token" value="network-csrf"`,
				data.AllowedHosts,
				`Current address: <code>` + data.CurrentHost + `</code>`,
			} {
				if !strings.Contains(body, required) {
					t.Fatalf("missing network form content %q", required)
				}
			}
		})
	}
}

func TestSettingsNavigationShowsNetworkOnlyToAdministrators(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	for _, admin := range []bool{false, true} {
		var output bytes.Buffer
		if err := renderer.pages["network_settings"].ExecuteTemplate(&output, "settings-nav", BaseData{IsAdmin: admin, CurrentPath: "/settings/network"}); err != nil {
			t.Fatal(err)
		}
		body := output.String()
		if strings.Contains(body, `href="/settings/network" aria-current="page"`) != admin || strings.Contains(body, `href="/admin/users"`) != admin {
			t.Fatalf("settings navigation does not respect admin role: %s", body)
		}
		if admin {
			previous := -1
			for _, section := range []string{"General", "Network", "Account", "Users"} {
				position := strings.Index(body, ">"+section+"</a>")
				if position <= previous {
					t.Fatalf("settings navigation order is incorrect: %s", body)
				}
				previous = position
			}
		}
	}
}
