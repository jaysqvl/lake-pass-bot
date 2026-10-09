package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jaysqvl/lake-pass-bot/internal/buildinfo"
)

type Flash struct{ Kind, Message, ActionLabel, ActionURL string }

// BaseData is the authenticated context returned with each API resource.
// Tokens stay in memory in the client; session cookies remain HttpOnly.
type BaseData struct {
	Title         string
	Authenticated bool
	Username      string
	IsAdmin       bool
	CSRFToken     string
	CurrentPath   string
	Flash         *Flash
}

type pageResponse struct {
	Page  string
	Data  any
	Build buildDisplay
}

func (s *Server) respond(w http.ResponseWriter, status int, page string, data any) {
	raw, err := json.Marshal(pageResponse{Page: page, Data: data, Build: applicationBuild(buildinfo.Version, buildinfo.Revision)})
	if err != nil {
		slog.Error("encode API response", "resource", page, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// The React client follows explicit navigation responses so URL fragments survive
// Fetch, which does not expose fragments from an automatic HTTP redirect.
// Other HTTP clients retain the normal API redirect contract.
func apiRedirect(w http.ResponseWriter, r *http.Request, target string, status int) {
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
		http.Error(w, "invalid redirect", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Header.Get("X-Lake-Pass-Navigation") == "manual" {
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct{ Redirect string }{target})
		return
	}
	http.Redirect(w, r, "/api"+target, status)
}
