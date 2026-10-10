// Command ui-fixture serves the real embedded React app and API with temporary
// data for Playwright and development previews. It never starts workers or
// contacts booking providers.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jaysqvl/lake-pass-bot/internal/auth"
	"github.com/jaysqvl/lake-pass-bot/internal/config"
	"github.com/jaysqvl/lake-pass-bot/internal/control"
	secretcrypto "github.com/jaysqvl/lake-pass-bot/internal/crypto"
	"github.com/jaysqvl/lake-pass-bot/internal/engine"
	"github.com/jaysqvl/lake-pass-bot/internal/store"
	"github.com/jaysqvl/lake-pass-bot/internal/web"
)

func main() {
	if err := serve(); err != nil {
		log.Fatal(err)
	}
}

func serve() error {
	port := os.Getenv("LAKE_PASS_DEV_PORT")
	if port == "" {
		port = "18092"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("LAKE_PASS_DEV_PORT must be between 1 and 65535")
	}
	autoLogin := false
	if value := os.Getenv("LAKE_PASS_DEV_AUTO_LOGIN"); value != "" {
		autoLogin, err = strconv.ParseBool(value)
		if err != nil {
			return errors.New("LAKE_PASS_DEV_AUTO_LOGIN must be true or false")
		}
	}
	handler, cleanup, err := newFixture(autoLogin)
	if err != nil {
		return err
	}
	defer cleanup()
	address := "127.0.0.1:" + port
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("Isolated React UI fixture at http://%s; workers disabled; automatic development sign-in=%t", address, autoLogin)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func newFixture(autoLogin bool) (http.Handler, func(), error) {
	directory, err := os.MkdirTemp("", "lake-pass-ui-fixture-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	success := false
	defer func() {
		if !success {
			cleanup()
		}
	}()
	box, err := secretcrypto.LoadOrCreate(filepath.Join(directory, "master.key"))
	if err != nil {
		return nil, nil, err
	}
	database, err := store.OpenMigrated(context.Background(), filepath.Join(directory, "lake-pass-bot.db"), box)
	if err != nil {
		return nil, nil, err
	}
	cleanup = func() {
		_ = database.Close()
		_ = os.RemoveAll(directory)
	}
	cfg := config.Config{
		AppDataDir: directory, ProfilesDir: filepath.Join(directory, "profiles"), ArtifactsDir: filepath.Join(directory, "artifacts"),
		MaxConcurrentJobs: 1, PythonExecutable: "python3", PythonModule: "lake_pass_actions",
		YodelOrigins: []string{"https://example.invalid"},
		// A deterministic token is used only in this disposable test command.
		SetupToken: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	}
	runner := engine.New(cfg, database, control.NewHub())
	// Deliberately do not call runner.Start. Queuing a job cannot run a worker.
	app, err := web.NewServer(cfg, database, runner)
	if err != nil {
		return nil, nil, err
	}
	handler := app.Handler()
	if autoLogin {
		password, err := auth.NewToken()
		if err != nil {
			return nil, nil, err
		}
		admin, err := database.SetupAdmin(context.Background(), "preview-admin", password)
		if err != nil {
			return nil, nil, err
		}
		handler = developmentSignIn(handler, database, admin.ID)
	}
	success = true
	return handler, cleanup, nil
}

// This wrapper belongs only to the disposable development command. It issues a
// real session on GET; production middleware still checks ownership and CSRF.
// Never attach it to a server using real appdata or running booking workers.
func developmentSignIn(next http.Handler, database *store.Store, userID int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Lake-Pass-Development", "isolated-auto-login")
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if cookie, err := r.Cookie("lake_pass_session"); err == nil {
			if session, err := database.GetSession(r.Context(), cookie.Value); err == nil {
				if csrf, err := r.Cookie("lake_pass_csrf"); err == nil && store.ValidateCSRF(session.Session, csrf.Value) {
					next.ServeHTTP(w, r)
					return
				}
			} else if !errors.Is(err, store.ErrNotFound) {
				http.Error(w, "development sign-in unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		credentials, err := database.NewSession(r.Context(), userID, 24*time.Hour)
		if err != nil {
			http.Error(w, "development sign-in unavailable", http.StatusServiceUnavailable)
			return
		}
		r = r.Clone(r.Context())
		cookies := r.Cookies()
		r.Header.Del("Cookie")
		for _, cookie := range cookies {
			if cookie.Name != "lake_pass_session" && cookie.Name != "lake_pass_csrf" {
				r.AddCookie(cookie)
			}
		}
		for name, value := range map[string]string{"lake_pass_session": credentials.Token, "lake_pass_csrf": credentials.CSRFToken} {
			cookie := &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: 86400, HttpOnly: true, SameSite: http.SameSiteStrictMode}
			http.SetCookie(w, cookie)
			r.AddCookie(cookie)
		}
		next.ServeHTTP(w, r)
	})
}
