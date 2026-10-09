package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIResponsePreservesDraftAndEscapesExecutableContent(t *testing.T) {
	server := &Server{}
	response := httptest.NewRecorder()
	draft := formData{BaseData: BaseData{Title: "New source", CSRFToken: "test-csrf"}, FormError: "Choose a source", Sections: []formSection{{Title: "Source", Fields: []formField{{Name: "name", Type: "text", Value: `<script>alert("unsafe")</script>`}}}}}
	server.respond(response, 422, "form", draft)
	var result struct {
		Page  string
		Data  formData
		Build buildDisplay
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != 422 || result.Page != "form" || result.Data.Sections[0].Fields[0].Value != draft.Sections[0].Fields[0].Value || result.Data.CSRFToken != "test-csrf" {
		t.Fatal("draft or CSRF lost")
	}
	if strings.Contains(response.Body.String(), "<script>") || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unsafe or cacheable API representation")
	}
}

func TestFailedAPIEncodingDoesNotExposePartialResponse(t *testing.T) {
	response := httptest.NewRecorder()
	(&Server{}).respond(response, 200, "broken", map[string]any{"private": "partial content", "unsupported": func() {}})
	if response.Code != 500 || strings.Contains(response.Body.String(), "partial content") {
		t.Fatal("failed serialization emitted a partial success")
	}
}

func TestAPIRedirectStaysWithinAPI(t *testing.T) {
	for _, target := range []string{"/login", "/jobs/123?ok=queued", "/lakes/buntzen#connection"} {
		response := httptest.NewRecorder()
		apiRedirect(response, httptest.NewRequest("POST", "/api/login", nil), target, http.StatusSeeOther)
		if response.Code != 303 || response.Header().Get("Location") != "/api"+target || response.Body.Len() != 0 {
			t.Fatalf("unexpected redirect: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestAPIClientNavigationPreservesFragment(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/profiles/new", nil)
	request.Header.Set("X-Lake-Pass-Navigation", "manual")
	apiRedirect(response, request, "/lakes/buntzen?ok=created#connection", http.StatusSeeOther)
	var result struct{ Redirect string }
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || result.Redirect != "/lakes/buntzen?ok=created#connection" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Location") != "" {
		t.Fatal("client navigation lost its fragment or became cacheable")
	}
}
