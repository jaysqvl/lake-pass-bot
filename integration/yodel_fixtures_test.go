//go:build integration

package integration_test

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"
)

// Keep the fake provider's DOM readable as HTML. Template data selects a test
// scenario explicitly; it must not depend on the position of matching strings.
//
//go:embed testdata/yodel/*.html
var yodelFixtureFiles embed.FS

var yodelFixturePages = template.Must(template.ParseFS(yodelFixtureFiles, "testdata/yodel/*.html"))

type yodelFixtureData struct {
	BearerToken  string
	Phone        string
	OTP          string
	VehicleLabel string
	CartQuantity int
}

func writeYodelPage(response http.ResponseWriter, name string, data yodelFixtureData) {
	var page bytes.Buffer
	if err := yodelFixturePages.ExecuteTemplate(&page, name, data); err != nil {
		http.Error(response, "render synthetic Yodel page: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeHTML(response, page.String())
}
