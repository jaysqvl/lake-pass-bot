package web

import (
	"net/http"
	"net/url"
	"strings"
)

// Only fixed application messages are accepted from redirect URLs. Provider
// errors, credentials, arbitrary text and return URLs never enter notifications.
func noticeFor(code string) *Flash {
	if code == "otp-default-updated" {
		return &Flash{Kind: "success", Message: "Default OTP source updated. Already queued jobs keep their original source."}
	}
	if code == "lake-defaults-reset" {
		return &Flash{Kind: "success", Message: "Lake defaults reset. Existing Yodel sign-ins and queued jobs keep their saved settings."}
	}
	messages := map[string]string{
		"preview-read-only":    "This action is unavailable in the sample preview. You can test queueing from Bookings; other changes belong in your main app.",
		"queue-full":           "This account has reached its job limit. Review existing jobs before starting another.",
		"provider-unavailable": "The OTP source connection test failed. Check its server address and credentials, and make sure the provider is running.",
		"pairing-unavailable":  "Pairing could not start. Check your account in the lake's Connection section, then try again.",
	}
	if message := messages[code]; message != "" {
		return &Flash{Kind: "error", Message: message}
	}
	return nil
}

// path is a fixed in-app destination chosen by the handler, never a submitted
// return URL or Referer. Redirecting after POST also makes refresh safe.
func redirectNotice(w http.ResponseWriter, r *http.Request, path, code string) {
	path, fragment, _ := strings.Cut(path, "#")
	location := url.URL{Path: path, Fragment: fragment, RawQuery: url.Values{"notice": {code}}.Encode()}
	http.Redirect(w, r, location.String(), http.StatusSeeOther)
}
