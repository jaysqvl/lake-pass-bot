package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIErrorsDiscardPrivateDiagnosticsAndPreserveStatusHeaders(t *testing.T) {
	server := &Server{}
	for _, status := range []int{400, 401, 403, 404, 405, 413, 429, 500, 502, 503, 504} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			response := httptest.NewRecorder()
			server.apiErrors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Allow", "GET")
				w.Header().Set("Retry-After", "2")
				w.Header().Set("Content-Security-Policy", "default-src 'self'")
				w.Header().Set("ETag", "private-tag")
				http.Error(w, "private-provider-diagnostic", status)
			})).ServeHTTP(response, apiRequest("GET", "http://example.test/jobs/1?return=https://untrusted.invalid", nil))
			var page struct {
				Page string
				Data browserErrorPage
			}
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if response.Code != status || page.Page != "error" || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("response %d %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "private-provider-diagnostic") || strings.Contains(response.Body.String(), "untrusted.invalid") || response.Header().Get("ETag") != "" {
				t.Fatal("private diagnostics or representation headers leaked")
			}
			if response.Header().Get("Retry-After") != "2" || response.Header().Get("Allow") != "GET" || response.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("original headers lost")
			}
			if status >= 500 && (page.Data.ReturnURL != "/jobs" || !strings.Contains(page.Data.Message, "Check Jobs")) {
				t.Fatal("uncertain job result must direct user to Jobs")
			}
		})
	}
}

func TestAPIErrorWrapperPreservesValidationRedirectsAndStreams(t *testing.T) {
	server := &Server{}
	for _, test := range []struct {
		contentType, body string
		status            int
	}{
		{"application/json", `{"Page":"form","Data":{"FormError":"Keep this validation error"}}`, 422},
		{"text/event-stream", "event: state\ndata: {}\n\n", 200},
		{"application/json", "", 303},
	} {
		response := httptest.NewRecorder()
		server.apiErrors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", test.contentType)
			w.WriteHeader(test.status)
			_, _ = w.Write([]byte(test.body))
		})).ServeHTTP(response, apiRequest("GET", "http://example.test/jobs/1/events", nil))
		if response.Code != test.status || response.Body.String() != test.body {
			t.Fatalf("original representation changed: %d %s", response.Code, response.Body.String())
		}
	}
}
