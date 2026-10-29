// Package cluster wraps the Talos and Kubernetes APIs behind a small,
// UI-oriented surface.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Cluster is the entry point for all cluster interactions.
type Cluster struct {
	talos *client.Client

	kubeconfigPath string

	kubeMu  sync.Mutex
	kube    kubernetes.Interface
	kubeAt  time.Time
	kubeErr error
}

// Options configures a connection.
type Options struct {
	// Config is the parsed talosconfig (required).
	Config *clientconfig.Config
	// Context selects a talosconfig context ("" = the file's current context).
	Context string
	// Endpoints overrides the context's endpoints if non-empty.
	Endpoints []string
	// Nodes overrides the context's nodes if non-empty.
	Nodes []string
	// KubeconfigPath is optional; "" means fetch it through the Talos API.
	KubeconfigPath string
}

// New creates a Talos client for the selected context. The Kubernetes client
// is created lazily: from KubeconfigPath if set, otherwise fetched through the
// Talos API.
func New(ctx context.Context, o Options) (*Cluster, error) {
	if o.Config == nil {
		return nil, errors.New("no talosconfig")
	}

	name := o.Context
	if name == "" {
		name = o.Config.Context
	}

	src, ok := o.Config.Contexts[name]
	if !ok || src == nil {
		if name == "" {
			return nil, errors.New("talosconfig has no current context; select one in Settings")
		}

		return nil, fmt.Errorf("context %q not found in talosconfig", name)
	}

	cctx := *src
	if len(o.Endpoints) > 0 {
		cctx.Endpoints = o.Endpoints
	}

	if len(o.Nodes) > 0 {
		cctx.Nodes = o.Nodes
	}

	if len(cctx.Endpoints) == 0 {
		return nil, fmt.Errorf("context %q has no endpoints; add your control plane IP(s) in Settings", name)
	}

	if cctx.Cluster == "" {
		cctx.Cluster = name
	}

	c, err := client.New(ctx, client.WithConfigContext(&cctx))
	if err != nil {
		return nil, fmt.Errorf("creating talos client: %w", err)
	}

	return &Cluster{talos: c, kubeconfigPath: o.KubeconfigPath}, nil
}

// EndpointCheck is the result of probing one endpoint.
type EndpointCheck struct {
	Endpoint string `json:"endpoint"`
	OK       bool   `json:"ok"`
	Version  string `json:"version,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	Error    string `json:"error,omitempty"`
}

// CheckEndpoints asks every endpoint for its Talos version.
func (c *Cluster) CheckEndpoints(ctx context.Context) []EndpointCheck {
	eps := c.Endpoints()
	out := make([]EndpointCheck, len(eps))

	var wg sync.WaitGroup

	for i, ep := range eps {
		wg.Add(1)

		go func(i int, ep string) {
			defer wg.Done()

			res := EndpointCheck{Endpoint: ep}

			cctx, cancel := nodeCtx(ctx, hostOf(ep), 8*time.Second)
			defer cancel()

			v, err := c.talos.Version(cctx)

			switch {
			case err != nil:
				res.Error = shortErr(err)
			case len(v.Messages) == 0 || v.Messages[0].Version == nil:
				res.Error = "empty response"
			default:
				res.OK = true
				res.Version = v.Messages[0].Version.Tag

				if md := v.Messages[0].Metadata; md != nil {
					res.Hostname = md.Hostname
				}
			}

			out[i] = res
		}(i, ep)
	}

	wg.Wait()

	return out
}

// Close releases the underlying connections.
func (c *Cluster) Close() error {
	return c.talos.Close()
}

// Name returns the configured cluster/context name.
func (c *Cluster) Name() string {
	if ctx := c.talos.GetConfigContext(); ctx != nil && ctx.Cluster != "" {
		return ctx.Cluster
	}

	if n := c.talos.GetClusterName(); n != "" {
		return n
	}

	return "talos"
}

// Endpoints returns the Talos API endpoints from the talosconfig.
func (c *Cluster) Endpoints() []string {
	return c.talos.GetEndpoints()
}

// configuredNodes returns the default nodes from the talosconfig context.
func (c *Cluster) configuredNodes() []string {
	if ctx := c.talos.GetConfigContext(); ctx != nil {
		if len(ctx.Nodes) > 0 {
			return ctx.Nodes
		}

		return ctx.Endpoints
	}

	return nil
}

// Kube returns a Kubernetes client, creating it on first use.
func (c *Cluster) Kube(ctx context.Context) (kubernetes.Interface, error) {
	c.kubeMu.Lock()
	defer c.kubeMu.Unlock()

	if c.kube != nil {
		return c.kube, nil
	}

	// Avoid hammering the API when it keeps failing.
	if c.kubeErr != nil && time.Since(c.kubeAt) < 15*time.Second {
		return nil, c.kubeErr
	}

	c.kubeAt = time.Now()

	var (
		restCfg *rest.Config
		err     error
	)

	// Use the kubeconfig file if it is a real, non-empty file (docker compose
	// mounts /dev/null when none is configured).
	if fi, statErr := os.Stat(c.kubeconfigPath); statErr == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
		restCfg, err = clientcmd.BuildConfigFromFlags("", c.kubeconfigPath)
	} else {
		restCfg, err = c.kubeconfigFromTalos(ctx)
	}

	if err != nil {
		slog.Warn("kubernetes client unavailable", "err", err)

		c.kubeErr = fmt.Errorf("kubernetes unavailable: %w", err)

		return nil, c.kubeErr
	}

	c.kubeErr = nil

	restCfg.Timeout = 15 * time.Second

	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, err
	}

	c.kube = cs

	return cs, nil
}

func (c *Cluster) kubeconfigFromTalos(ctx context.Context) (*rest.Config, error) {
	var lastErr error

	for _, ep := range c.Endpoints() {
		node := hostOf(ep)

		cctx, cancel := context.WithTimeout(client.WithNode(ctx, node), 15*time.Second)
		raw, err := c.talos.Kubeconfig(cctx)

		cancel()

		if err != nil {
			lastErr = err

			continue
		}

		slog.Info("fetched kubeconfig via Talos API", "node", node)

		return clientcmd.RESTConfigFromKubeConfig(raw)
	}

	if lastErr == nil {
		lastErr = errors.New("no talos endpoints configured")
	}

	return nil, fmt.Errorf("fetching kubeconfig from talos: %w", lastErr)
}

// nodeCtx targets a single Talos node with a timeout.
func nodeCtx(ctx context.Context, node string, d time.Duration) (context.Context, context.CancelFunc) {
	cctx, cancel := context.WithTimeout(ctx, d)

	return client.WithNode(cctx, node), cancel
}

func hostOf(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		endpoint = u.Host
	}

	if h, _, err := net.SplitHostPort(endpoint); err == nil {
		return h
	}

	return endpoint
}
