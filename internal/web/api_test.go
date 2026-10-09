package web

import (
	"encoding/json"
	"testing"
)

func apiData[T any](t *testing.T, body, page string) T {
	t.Helper()
	var response struct {
		Page string
		Data T
	}
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatalf("decode API response: %v", err)
	}
	if page != "" && response.Page != page {
		t.Fatalf("API resource = %q; want %q", response.Page, page)
	}
	return response.Data
}

func apiFields(t *testing.T, body string) map[string]formField {
	t.Helper()
	data := apiData[struct {
		Sections      []formSection
		Visit, Passes formSection
	}](t, body, "")
	data.Sections = append(data.Sections, data.Visit, data.Passes)
	fields := make(map[string]formField)
	for _, section := range data.Sections {
		for _, field := range section.Fields {
			if _, exists := fields[field.Name]; exists {
				t.Fatalf("duplicate API field %q", field.Name)
			}
			fields[field.Name] = field
		}
	}
	return fields
}

func apiField(t *testing.T, body, name string) formField {
	t.Helper()
	field, found := apiFields(t, body)[name]
	if !found {
		t.Fatalf("API form missing field %q", name)
	}
	return field
}

func apiCards(t *testing.T, body string) []listCard {
	t.Helper()
	data := apiData[struct{ Cards, Profiles []listCard }](t, body, "")
	return append(data.Cards, data.Profiles...)
}

func hasPostAction(t *testing.T, body, target string) bool {
	t.Helper()
	for _, card := range apiCards(t, body) {
		for _, action := range card.PostActions {
			if action.URL == target {
				return true
			}
		}
	}
	return false
}

func hasCardLink(t *testing.T, body, target string) bool {
	t.Helper()
	for _, card := range apiCards(t, body) {
		for _, action := range card.Actions {
			if action.URL == target {
				return true
			}
		}
	}
	return false
}

func hasFormHelpLink(t *testing.T, body, target string) bool {
	t.Helper()
	data := apiData[formData](t, body, "form")
	if data.Flash != nil && data.Flash.ActionURL == target {
		return true
	}
	for _, section := range data.Sections {
		if section.HelpURL == target {
			return true
		}
	}
	return false
}
