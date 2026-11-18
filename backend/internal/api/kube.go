package api

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/lars/talos-controller/internal/cluster"
)

var errBadName = errors.New("invalid namespace or name")

var k8sName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

// validNS accepts an empty namespace (all) or a DNS-label-like name.
func validNS(ns string) bool { return ns == "" || (len(ns) <= 253 && k8sName.MatchString(ns)) }

// kubeList wires a namespace-scoped list endpoint.
func kubeList[T any](s *Server, fn func(c *cluster.Cluster, r *http.Request, ns string) (T, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := s.cl(w)
		if c == nil {
			return
		}

		ns := r.URL.Query().Get("namespace")
		if !validNS(ns) {
			writeError(w, http.StatusBadRequest, errBadName)

			return
		}

		ctx, cancel := reqCtx(r, 30*time.Second)
		defer cancel()

		v, err := fn(c, r.WithContext(ctx), ns)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)

			return
		}

		writeJSON(w, http.StatusOK, v)
	}
}

func (s *Server) handleKubeNamespaces(w http.ResponseWriter, r *http.Request) {
	kubeList(s, func(c *cluster.Cluster, r *http.Request, _ string) ([]string, error) {
		return c.KubeNamespaces(r.Context())
	})(w, r)
}

func (s *Server) handleKubeDeployments(w http.ResponseWriter, r *http.Request) {
	kubeList(s, func(c *cluster.Cluster, r *http.Request, ns string) (any, error) {
		return c.KubeDeployments(r.Context(), ns)
	})(w, r)
}

func (s *Server) handleKubePods(w http.ResponseWriter, r *http.Request) {
	kubeList(s, func(c *cluster.Cluster, r *http.Request, ns string) (any, error) {
		return c.KubePods(r.Context(), ns)
	})(w, r)
}

func (s *Server) handleKubeServices(w http.ResponseWriter, r *http.Request) {
	kubeList(s, func(c *cluster.Cluster, r *http.Request, ns string) (any, error) {
		return c.KubeServices(r.Context(), ns)
	})(w, r)
}

func (s *Server) handleKubeHTTPRoutes(w http.ResponseWriter, r *http.Request) {
	kubeList(s, func(c *cluster.Cluster, r *http.Request, ns string) (any, error) {
		return c.KubeHTTPRoutes(r.Context(), ns)
	})(w, r)
}

func (s *Server) handleKubeConfigMaps(w http.ResponseWriter, r *http.Request) {
	kubeList(s, func(c *cluster.Cluster, r *http.Request, ns string) (any, error) {
		return c.KubeConfigMaps(r.Context(), ns)
	})(w, r)
}

func (s *Server) handleKubeSecrets(w http.ResponseWriter, r *http.Request) {
	kubeList(s, func(c *cluster.Cluster, r *http.Request, ns string) (any, error) {
		return c.KubeSecrets(r.Context(), ns)
	})(w, r)
}

func (s *Server) handleKubeConfigMap(w http.ResponseWriter, r *http.Request) {
	c := s.cl(w)
	if c == nil {
		return
	}

	ns, name := r.PathValue("ns"), r.PathValue("name")
	if !k8sName.MatchString(ns) || !k8sName.MatchString(name) {
		writeError(w, http.StatusBadRequest, errBadName)

		return
	}

	ctx, cancel := reqCtx(r, 15*time.Second)
	defer cancel()

	cm, err := c.KubeConfigMap(ctx, ns, name)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)

		return
	}

	writeJSON(w, http.StatusOK, cm)
}

func (s *Server) handleKubePodLogs(w http.ResponseWriter, r *http.Request) {
	c := s.cl(w)
	if c == nil {
		return
	}

	ns, name := r.PathValue("ns"), r.PathValue("name")
	q := r.URL.Query()
	container := q.Get("container")

	if !k8sName.MatchString(ns) || !k8sName.MatchString(name) || (container != "" && !k8sName.MatchString(container)) {
		writeError(w, http.StatusBadRequest, errBadName)

		return
	}

	tail, _ := strconv.ParseInt(q.Get("lines"), 10, 64)
	if tail <= 0 {
		tail = 200
	}

	ctx, cancel := reqCtx(r, 30*time.Second)
	defer cancel()

	out, err := c.KubePodLogs(ctx, ns, name, container, tail, q.Get("previous") == "true")
	if err != nil {
		writeError(w, http.StatusBadGateway, err)

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"logs": out})
}
