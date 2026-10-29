// Command talos-controller is a web UI for supervising a Talos Linux cluster.
package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lars/talos-controller/internal/api"
	"github.com/lars/talos-controller/internal/auth"
	"github.com/lars/talos-controller/internal/config"
	"github.com/lars/talos-controller/internal/jobs"
	"github.com/lars/talos-controller/internal/releases"
	"github.com/lars/talos-controller/internal/settings"
)

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

//go:embed all:web/dist
var webFS embed.FS

func main() {
	// Used by the Docker HEALTHCHECK (the runtime image has no shell or curl).
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}

	level := slog.LevelInfo
	if os.Getenv("DEBUG") != "" {
		level = slog.LevelDebug
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func healthcheck() int {
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	c := http.Client{Timeout: 3 * time.Second}

	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return 1
	}

	return 0
}

func run() error {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := auth.New(cfg.AdminUser, cfg.AdminPassword, cfg.AdminPasswordBcrypt, cfg.SessionSecret, cfg.SessionTTL, cfg.CookieSecure)
	if err != nil {
		return err
	}

	web, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		return err
	}

	// The talosconfig can come from TALOSCONFIG or be uploaded in the UI
	// (Settings); the app starts fine without one.
	store := settings.New(cfg.DataDir, cfg.TalosConfigPath, cfg.KubeconfigPath)

	srv := api.New(ctx, store, a, jobs.NewManager(cfg.DataDir), releases.New(cfg.GitHubToken), web)
	defer srv.Close()

	srv.Version = version

	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	slog.Info("talos-controller listening", "addr", cfg.ListenAddr, "version", version)

	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}
