// Package settings persists the cluster connection settings that can be
// changed from the UI: the talosconfig, the selected context, endpoint/node
// overrides and an optional kubeconfig.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"k8s.io/client-go/tools/clientcmd"
)

// Source of a config file.
const (
	SourceNone = "none"
	SourceUI   = "ui"   // uploaded through the UI, stored in DATA_DIR
	SourceFile = "file" // TALOSCONFIG / KUBECONFIG environment path
)

// Prefs are the user's connection choices.
type Prefs struct {
	Context   string   `json:"context"`
	Endpoints []string `json:"endpoints"`
	Nodes     []string `json:"nodes"`
}

// ContextInfo describes one talosconfig context (no key material).
type ContextInfo struct {
	Name      string   `json:"name"`
	Cluster   string   `json:"cluster"`
	Endpoints []string `json:"endpoints"`
	Nodes     []string `json:"nodes"`
	Roles     string   `json:"roles,omitempty"`
}

// Store reads and writes settings under a data directory.
type Store struct {
	dir          string
	envTalosPath string
	envKubePath  string

	mu sync.Mutex
}

// New creates a Store. envTalos/envKube are optional fallback file paths.
func New(dir, envTalos, envKube string) *Store {
	_ = os.MkdirAll(dir, 0o700)

	return &Store{dir: dir, envTalosPath: envTalos, envKubePath: envKube}
}

func (s *Store) uiTalosPath() string { return filepath.Join(s.dir, "talosconfig") }
func (s *Store) uiKubePath() string  { return filepath.Join(s.dir, "kubeconfig") }
func (s *Store) prefsPath() string   { return filepath.Join(s.dir, "settings.json") }

// usable reports whether path is a regular, non-empty file. Docker mounts
// /dev/null (or creates an empty directory) when nothing is configured.
func usable(path string) bool {
	if path == "" {
		return false
	}

	fi, err := os.Stat(path)

	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// TalosconfigPath returns the effective talosconfig path and its source.
func (s *Store) TalosconfigPath() (string, string) {
	if usable(s.uiTalosPath()) {
		return s.uiTalosPath(), SourceUI
	}

	if usable(s.envTalosPath) {
		return s.envTalosPath, SourceFile
	}

	return "", SourceNone
}

// KubeconfigPath returns the effective kubeconfig path ("" = fetch via Talos) and its source.
func (s *Store) KubeconfigPath() (string, string) {
	if usable(s.uiKubePath()) {
		return s.uiKubePath(), SourceUI
	}

	if usable(s.envKubePath) {
		return s.envKubePath, SourceFile
	}

	return "", SourceNone
}

// LoadTalosconfig parses the effective talosconfig. Returns (nil, nil) if none.
func (s *Store) LoadTalosconfig() (*clientconfig.Config, error) {
	path, _ := s.TalosconfigPath()
	if path == "" {
		return nil, nil
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading talosconfig: %w", err)
	}

	return ParseTalosconfig(string(b))
}

// ParseTalosconfig validates talosconfig content.
func ParseTalosconfig(raw string) (*clientconfig.Config, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("talosconfig is empty")
	}

	cfg, err := clientconfig.FromString(raw)
	if err != nil {
		return nil, fmt.Errorf("not a valid talosconfig: %w", err)
	}

	if len(cfg.Contexts) == 0 {
		return nil, errors.New("talosconfig has no contexts")
	}

	for name, c := range cfg.Contexts {
		if c.CA == "" || c.Crt == "" || c.Key == "" {
			return nil, fmt.Errorf("context %q is missing ca/crt/key", name)
		}
	}

	return cfg, nil
}

// Contexts lists the contexts in a config.
func Contexts(cfg *clientconfig.Config) []ContextInfo {
	out := []ContextInfo{}
	if cfg == nil {
		return out
	}

	for name, c := range cfg.Contexts {
		out = append(out, ContextInfo{
			Name:      name,
			Cluster:   c.Cluster,
			Endpoints: nonNil(c.Endpoints),
			Nodes:     nonNil(c.Nodes),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}

// SaveTalosconfig validates and stores talosconfig content uploaded in the UI.
func (s *Store) SaveTalosconfig(raw string) (*clientconfig.Config, error) {
	cfg, err := ParseTalosconfig(raw)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := writeFile(s.uiTalosPath(), []byte(raw)); err != nil {
		return nil, err
	}

	// Reset the context choice if it no longer exists.
	p := s.loadPrefsLocked()
	if _, ok := cfg.Contexts[p.Context]; !ok {
		p.Context = ""
		_ = s.savePrefsLocked(p)
	}

	return cfg, nil
}

// DeleteTalosconfig removes the UI-uploaded talosconfig (falls back to the file, if any).
func (s *Store) DeleteTalosconfig() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := os.Remove(s.uiTalosPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	return err
}

// SaveKubeconfig validates and stores a kubeconfig.
func (s *Store) SaveKubeconfig(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("kubeconfig is empty")
	}

	if _, err := clientcmd.RESTConfigFromKubeConfig([]byte(raw)); err != nil {
		return fmt.Errorf("not a valid kubeconfig: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return writeFile(s.uiKubePath(), []byte(raw))
}

// DeleteKubeconfig removes the UI-uploaded kubeconfig.
func (s *Store) DeleteKubeconfig() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := os.Remove(s.uiKubePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	return err
}

// Prefs returns the saved connection preferences.
func (s *Store) Prefs() Prefs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loadPrefsLocked()
}

// SavePrefs validates and stores connection preferences.
func (s *Store) SavePrefs(p Prefs) (Prefs, error) {
	var err error

	if p.Endpoints, err = cleanHosts(p.Endpoints); err != nil {
		return p, fmt.Errorf("endpoints: %w", err)
	}

	if p.Nodes, err = cleanHosts(p.Nodes); err != nil {
		return p, fmt.Errorf("nodes: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return p, s.savePrefsLocked(p)
}

func (s *Store) loadPrefsLocked() Prefs {
	var p Prefs

	b, err := os.ReadFile(s.prefsPath())
	if err == nil {
		_ = json.Unmarshal(b, &p)
	}

	p.Endpoints = nonNil(p.Endpoints)
	p.Nodes = nonNil(p.Nodes)

	return p
}

func (s *Store) savePrefsLocked(p Prefs) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}

	return writeFile(s.prefsPath(), b)
}

// cleanHosts trims, de-duplicates and validates host[:port] entries.
func cleanHosts(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}

	for _, h := range in {
		for _, part := range strings.FieldsFunc(h, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}

			host := part
			if h, _, err := net.SplitHostPort(part); err == nil {
				host = h
			}

			if host == "" || strings.ContainsAny(part, "/?#@ ") {
				return nil, fmt.Errorf("invalid host %q (use an IP or hostname, optionally with :port)", part)
			}

			seen[part] = true
			out = append(out, part)
		}
	}

	return out, nil
}

func writeFile(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("saving %s: %w (is DATA_DIR writable?)", filepath.Base(path), err)
	}

	return os.Rename(tmp, path)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}
