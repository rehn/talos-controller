package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/lars/talos-controller/internal/cluster"
	"github.com/lars/talos-controller/internal/settings"
)

// errNotConfigured is returned while no usable talosconfig is set.
var errNotConfigured = errors.New("no talosconfig configured yet, open Settings to add one")

// reconnect (re)builds the cluster client from the stored settings.
func (s *Server) reconnect(ctx context.Context) error {
	cfg, err := s.settings.LoadTalosconfig()

	var c *cluster.Cluster

	if err == nil && cfg == nil {
		err = errNotConfigured
	}

	if err == nil {
		p := s.settings.Prefs()
		kube, _ := s.settings.KubeconfigPath()

		c, err = cluster.New(ctx, cluster.Options{
			Config:         cfg,
			Context:        p.Context,
			Endpoints:      p.Endpoints,
			Nodes:          p.Nodes,
			KubeconfigPath: kube,
		})
	}

	s.connMu.Lock()
	old := s.conn
	s.conn, s.connErr = c, err
	s.connMu.Unlock()

	s.ovMu.Lock()
	s.ovData = nil
	s.ovMu.Unlock()

	// Keep the old client alive while jobs may still be using it.
	if old != nil && old != c && s.jobs.Running() == 0 {
		_ = old.Close()
	}

	if err != nil {
		if !errors.Is(err, errNotConfigured) {
			slog.Warn("cluster connection not configured", "err", err)
		}

		return err
	}

	slog.Info("connected", "cluster", c.Name(), "endpoints", c.Endpoints())

	return nil
}

// cl returns the current cluster client or writes a 503 explaining why there is none.
func (s *Server) cl(w http.ResponseWriter) *cluster.Cluster {
	s.connMu.RLock()
	c, err := s.conn, s.connErr
	s.connMu.RUnlock()

	if c != nil {
		return c
	}

	if err == nil {
		err = errNotConfigured
	}

	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error(), "code": "not_configured"})

	return nil
}

type settingsView struct {
	Configured bool   `json:"configured"`
	Error      string `json:"error,omitempty"`

	Talosconfig struct {
		Source         string                 `json:"source"`
		Path           string                 `json:"path,omitempty"`
		DefaultContext string                 `json:"defaultContext"`
		Contexts       []settings.ContextInfo `json:"contexts"`
		Error          string                 `json:"error,omitempty"`
	} `json:"talosconfig"`

	Kubeconfig struct {
		Source string `json:"source"`
		Path   string `json:"path,omitempty"`
	} `json:"kubeconfig"`

	Prefs              settings.Prefs `json:"prefs"`
	ClusterName        string         `json:"clusterName,omitempty"`
	EffectiveEndpoints []string       `json:"effectiveEndpoints"`
}

func (s *Server) view() settingsView {
	var v settingsView

	path, src := s.settings.TalosconfigPath()
	v.Talosconfig.Source = src
	v.Talosconfig.Contexts = []settings.ContextInfo{}

	if src == settings.SourceFile {
		v.Talosconfig.Path = path
	}

	if cfg, err := s.settings.LoadTalosconfig(); err != nil {
		v.Talosconfig.Error = err.Error()
	} else if cfg != nil {
		v.Talosconfig.DefaultContext = cfg.Context
		v.Talosconfig.Contexts = settings.Contexts(cfg)
	}

	kpath, ksrc := s.settings.KubeconfigPath()
	v.Kubeconfig.Source = ksrc

	if ksrc == settings.SourceFile {
		v.Kubeconfig.Path = kpath
	}

	v.Prefs = s.settings.Prefs()
	v.EffectiveEndpoints = []string{}

	s.connMu.RLock()
	c, err := s.conn, s.connErr
	s.connMu.RUnlock()

	if c != nil {
		v.Configured = true
		v.ClusterName = c.Name()
		v.EffectiveEndpoints = c.Endpoints()
	} else if err != nil {
		v.Error = err.Error()
	}

	return v
}

func (s *Server) handleSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.view())
}

type settingsResult struct {
	settingsView
	Checks []cluster.EndpointCheck `json:"checks,omitempty"`
}

// afterChange reconnects and, when connected, probes the endpoints.
func (s *Server) afterChange(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := reqCtx(r, 20*time.Second)
	defer cancel()

	_ = s.reconnect(ctx)

	res := settingsResult{settingsView: s.view()}

	s.connMu.RLock()
	c := s.conn
	s.connMu.RUnlock()

	if c != nil {
		res.Checks = c.CheckEndpoints(ctx)
	}

	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSaveTalosconfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}

	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	if _, err := s.settings.SaveTalosconfig(body.Content); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	s.afterChange(w, r)
}

func (s *Server) handleDeleteTalosconfig(w http.ResponseWriter, r *http.Request) {
	if err := s.settings.DeleteTalosconfig(); err != nil {
		writeError(w, http.StatusInternalServerError, err)

		return
	}

	s.afterChange(w, r)
}

func (s *Server) handleSaveConnection(w http.ResponseWriter, r *http.Request) {
	var body settings.Prefs

	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	if _, err := s.settings.SavePrefs(body); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	s.afterChange(w, r)
}

func (s *Server) handleSaveKubeconfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}

	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	if err := s.settings.SaveKubeconfig(body.Content); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	s.afterChange(w, r)
}

func (s *Server) handleDeleteKubeconfig(w http.ResponseWriter, r *http.Request) {
	if err := s.settings.DeleteKubeconfig(); err != nil {
		writeError(w, http.StatusInternalServerError, err)

		return
	}

	s.afterChange(w, r)
}

func (s *Server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	s.afterChange(w, r)
}
