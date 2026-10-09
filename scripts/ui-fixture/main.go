// Command ui-fixture serves the real embedded React app and API with temporary
// data for Playwright. It never starts workers or contacts booking providers.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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
	directory, err := os.MkdirTemp("", "lake-pass-ui-fixture-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	box, err := secretcrypto.LoadOrCreate(filepath.Join(directory, "master.key"))
	if err != nil {
		return err
	}
	database, err := store.OpenMigrated(context.Background(), filepath.Join(directory, "lake-pass-bot.db"), box)
	if err != nil {
		return err
	}
	defer database.Close()
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
		return err
	}
	server := &http.Server{Addr: "127.0.0.1:18092", Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Print("Isolated React UI fixture at http://127.0.0.1:18092; workers disabled")
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
