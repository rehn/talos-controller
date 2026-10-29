// Package api exposes the HTTP API and serves the web UI.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/lars/talos-controller/internal/auth"
	"github.com/lars/talos-controller/internal/cluster"
	"github.com/lars/talos-controller/internal/jobs"
	"github.com/lars/talos-controller/internal/releases"
	"github.com/lars/talos-controller/internal/settings"
)

// Server wires handlers together.
type Server struct {
	// Version is the build version shown in the UI.
	Version string

	settings *settings.Store
	auth     *auth.Auth
	jobs     *jobs.Manager
	releases *releases.Checker
	web      fs.FS

	ovMu   sync.Mutex
	ovAt   time.Time
	ovData *cluster.Overview

	connMu  sync.RWMutex
	conn    *cluster.Cluster
	connErr error
}

// New creates the server.
// It tries to connect using the stored settings, but starts fine without any.
func New(ctx context.Context, st *settings.Store, a *auth.Auth, j *jobs.Manager, r *releases.Checker, web fs.FS) *Server {
	s := &Server{settings: st, auth: a, jobs: j, releases: r, web: web}
	_ = s.reconnect(ctx)

	return s
}

// Close releases the cluster connection.
func (s *Server) Close() {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	if s.conn != nil {
		_ = s.conn.Close()
	}
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	api := http.NewServeMux()

	api.HandleFunc("GET /api/overview", s.handleOverview)
	api.HandleFunc("GET /api/nodes", s.handleNodes)
	api.HandleFunc("GET /api/nodes/{node}", s.handleNode)
	api.HandleFunc("GET /api/nodes/{node}/services", s.handleServices)
	api.HandleFunc("POST /api/nodes/{node}/services/{id}/restart", s.handleServiceRestart)
	api.HandleFunc("GET /api/nodes/{node}/logs/{service}", s.handleLogs)
	api.HandleFunc("GET /api/nodes/{node}/dmesg", s.handleDmesg)
	api.HandleFunc("GET /api/nodes/{node}/config", s.handleConfig)
	api.HandleFunc("POST /api/nodes/{node}/reboot", s.handleReboot)
	api.HandleFunc("GET /api/etcd", s.handleEtcd)
	api.HandleFunc("GET /api/workloads", s.handleWorkloads)
	api.HandleFunc("POST /api/config/patch", s.handlePatch)
	api.HandleFunc("POST /api/config/rollout", s.handleRollout)
	api.HandleFunc("GET /api/drift", s.handleDrift)
	api.HandleFunc("GET /api/upgrades", s.handleUpgrades)
	api.HandleFunc("POST /api/upgrades/talos", s.handleTalosUpgrade)
	api.HandleFunc("GET /api/jobs", s.handleJobs)
	api.HandleFunc("GET /api/jobs/{id}", s.handleJob)
	api.HandleFunc("POST /api/jobs/{id}/cancel", s.handleJobCancel)
	api.HandleFunc("GET /api/settings", s.handleSettings)
	api.HandleFunc("POST /api/settings/talosconfig", s.handleSaveTalosconfig)
	api.HandleFunc("POST /api/settings/talosconfig/delete", s.handleDeleteTalosconfig)
	api.HandleFunc("POST /api/settings/connection", s.handleSaveConnection)
	api.HandleFunc("POST /api/settings/kubeconfig", s.handleSaveKubeconfig)
	api.HandleFunc("POST /api/settings/kubeconfig/delete", s.handleDeleteKubeconfig)
	api.HandleFunc("POST /api/settings/test", s.handleTestConnection)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", s.auth.LoginHandler)
	mux.HandleFunc("POST /api/auth/logout", s.auth.LogoutHandler)
	mux.HandleFunc("GET /api/auth/me", s.auth.MeHandler)
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": s.Version})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.Handle("/api/", s.auth.Middleware(api))
	mux.Handle("/", s.static())

	return logRequests(securityHeaders(mux))
}

// static serves the exported Next.js app.
func (s *Server) static() http.Handler {
	fileServer := http.FileServerFS(s.web)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		if p == "" || p == "." {
			fileServer.ServeHTTP(w, r)

			return
		}

		// Exact file, directory index, or "<route>.html".
		if _, err := fs.Stat(s.web, p); err == nil {
			if strings.HasPrefix(p, "_next/static/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}

			fileServer.ServeHTTP(w, r)

			return
		}

		if _, err := fs.Stat(s.web, p+".html"); err == nil {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/" + p + ".html"
			http.ServeFileFS(w, r2, s.web, p+".html")

			return
		}

		w.WriteHeader(http.StatusNotFound)

		if b, err := fs.ReadFile(s.web, "404.html"); err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(b)
		}
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, code: 200}
		next.ServeHTTP(rec, r)

		if strings.HasPrefix(r.URL.Path, "/api/") {
			slog.Debug("http", "method", r.Method, "path", r.URL.Path, "status", rec.code, "dur", time.Since(start))
		}
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()

	if err := dec.Decode(v); err != nil {
		return errors.New("invalid request body: " + err.Error())
	}

	return nil
}

func reqCtx(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
