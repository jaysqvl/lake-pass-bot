package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestFrontendHistoryFallbackAssetTypesAndCachePolicy(t *testing.T) {
	files := fstest.MapFS{"index.html": {Data: []byte(`<!doctype html><div id="root"></div><script type="module" src="/assets/app-123.js"></script>`)}, "assets/app-123.js": {Data: []byte("export const app = true")}, "favicon.svg": {Data: []byte("<svg/>")}}
	handler := newFrontendHandler(files)
	for _, test := range []struct {
		method, path string
		status       int
		kind, cache  string
	}{
		{"GET", "/", 200, "text/html", "no-store"}, {"GET", "/jobs/123", 200, "text/html", "no-store"},
		{"HEAD", "/account", 200, "text/html", "no-store"}, {"GET", "/assets/app-123.js", 200, "text/javascript", "public, max-age=31536000, immutable"},
		{"GET", "/assets/missing.js", 404, "text/plain", ""}, {"GET", "/api/missing", 404, "text/plain", ""},
		{"GET", "/static/htmx.min.js", 404, "text/plain", ""}, {"POST", "/login", 405, "text/plain", ""},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if response.Code != test.status || !strings.HasPrefix(response.Header().Get("Content-Type"), test.kind) || response.Header().Get("Cache-Control") != test.cache {
				t.Fatalf("response %d %v", response.Code, response.Header())
			}
			if test.method == "HEAD" && response.Body.Len() != 0 {
				t.Fatal("HEAD returned a body")
			}
		})
	}
}

func TestFrontendShellContainsNoAuthenticatedData(t *testing.T) {
	fixture := newWebFixture(t)
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, httptest.NewRequest("GET", "http://example.test/jobs/123", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `id="root"`) || strings.Contains(response.Body.String(), "CSRFToken") || strings.Contains(response.Body.String(), "long-test-password") {
		t.Fatal("frontend shell missing or contains private context")
	}
	api := httptest.NewRecorder()
	fixture.handler.ServeHTTP(api, apiRequest("GET", "http://example.test/jobs/123", nil))
	if api.Code != 303 || api.Header().Get("Location") != "/api/login" {
		t.Fatal("API no longer enforces authentication")
	}
}
