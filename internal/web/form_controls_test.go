package web

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/jaysqvl/lake-pass-bot/internal/destinations"
	"github.com/jaysqvl/lake-pass-bot/internal/model"
)

func TestRenderedFormControlsPreserveValuesAndAccessibleLabels(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []string{"form", "settings"} {
		t.Run(page, func(t *testing.T) {
			section := formSection{Fields: []formField{
				{Name: "enabled", Label: "Enabled", Type: "checkbox", Checked: true, Help: "Enable new requests."},
				{Name: "retries", Label: "Retries", Type: "number", Value: "3", Required: true, Min: "0", Max: "10", Step: "1", Help: "Number of attempts."},
				{Name: "channel", Label: "Browser", Type: "select", Required: true, Help: "Choose a browser.", Options: []selectOption{
					{Value: "bundled", Label: "Bundled"},
					{Value: "chrome", Label: "Chrome", Selected: true},
				}},
			}}
			var data any = formData{Sections: []formSection{section}}
			if page == "settings" {
				data = settingsPageData{Sections: []formSection{section}}
			}
			response := httptest.NewRecorder()
			if err := renderer.Render(response, http.StatusOK, page, data); err != nil {
				t.Fatal(err)
			}
			body := response.Body.String()
			for _, field := range section.Fields {
				control := regexp.MustCompile(`<(?:input|select)\b[^>]*id="field-` + field.Name + `"[^>]*>`).FindString(body)
				if control == "" || !strings.Contains(control, `name="`+field.Name+`"`) || !strings.Contains(control, `aria-describedby="help-`+field.Name+`"`) {
					t.Fatalf("missing named control or help association: %s", control)
				}
				if strings.Contains(control, " checked") != field.Checked || strings.Contains(control, " required") != field.Required {
					t.Fatalf("control lost checked/required state: %s", control)
				}
				if !strings.Contains(body, `for="field-`+field.Name+`"`) || !strings.Contains(body, `id="help-`+field.Name+`">`+field.Help+`</small>`) {
					t.Fatalf("control %s lost its visible label/help", field.Name)
				}
				if field.Type == "number" {
					for _, attribute := range []string{`value="3"`, `min="0"`, `max="10"`, `step="1"`} {
						if !strings.Contains(control, attribute) {
							t.Fatalf("number control lost %s: %s", attribute, control)
						}
					}
				}
			}
			if !regexp.MustCompile(`<option\b[^>]*value="chrome"[^>]*\bselected[^>]*>Chrome</option>`).MatchString(body) {
				t.Fatal("selected browser option was not retained")
			}
		})
	}
}

func TestRenderedFormPreservesPlaceholderAndLakeDefaults(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	const defaults = `{"id":"synthetic","vehicleKeyword":"Vehicle <two>"}`
	data := formData{Sections: []formSection{{Fields: []formField{
		{Name: "vehicle_keyword", Label: "Vehicle", Type: "text", Placeholder: "Example <vehicle>"},
		{Name: "lake_id", Label: "Lake", Type: "select", Options: []selectOption{
			{Value: "synthetic", Label: "Synthetic lake", LakeDefaults: defaults},
		}},
	}}}}
	response := httptest.NewRecorder()
	if err := renderer.Render(response, http.StatusOK, "form", data); err != nil {
		t.Fatal(err)
	}
	body := response.Body.String()
	if !strings.Contains(body, `placeholder="Example &lt;vehicle&gt;"`) {
		t.Fatal("vehicle placeholder was not preserved and escaped")
	}
	match := regexp.MustCompile(`data-lake-defaults="([^"]+)"`).FindStringSubmatch(body)
	if len(match) != 2 || html.UnescapeString(match[1]) != defaults || !json.Valid([]byte(html.UnescapeString(match[1]))) {
		t.Fatalf("lake defaults did not survive HTML rendering: %v", match)
	}
}

func TestLakeAdvancedFieldsDoNotDependOnSectionTitle(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	lake, err := destinations.Resolve(destinations.DefaultLakeID)
	if err != nil {
		t.Fatal(err)
	}
	sections := lakeSettingsSections(lake, model.DefaultLakeSettings(lake))
	for index := range sections {
		sections[index].Title = "Updated section label"
	}
	response := httptest.NewRecorder()
	data := lakePageData{Lake: lake, Sections: sections}
	if err := renderer.Render(response, http.StatusOK, "lake", data); err != nil {
		t.Fatal(err)
	}
	body := response.Body.String()
	advanced := regexp.MustCompile(`(?s)<details\b[^>]*class="form-advanced"[^>]*>(.*?)</details>`).FindString(body)
	for _, name := range []string{"all_day_pass_url", "half_day_pass_url"} {
		if !strings.Contains(advanced, `name="`+name+`"`) || strings.Count(body, `name="`+name+`"`) != 1 {
			t.Fatalf("advanced field %s moved or duplicated after a copy edit", name)
		}
	}
	if strings.Contains(advanced, `name="vehicle_keyword"`) || strings.Count(body, `name="vehicle_keyword"`) != 1 {
		t.Fatal("vehicle setting did not remain outside Advanced settings")
	}
}
