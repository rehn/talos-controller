package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"

	"github.com/lars/talos-controller/internal/cluster"
	"github.com/lars/talos-controller/internal/drift"
	"github.com/lars/talos-controller/internal/jobs"
)

type rolloutRequest struct {
	Role            string   `json:"role"`
	Nodes           []string `json:"nodes"`
	Patch           string   `json:"patch"`
	Mode            string   `json:"mode"`
	ContinueOnError bool     `json:"continueOnError"`
}

type parsedRollout struct {
	selector cluster.Selector
	patch    string
	mode     machineapi.ApplyConfigurationRequest_Mode
	opts     cluster.RolloutOptions
}

// parseRolloutRequest validates everything that can be checked without the cluster.
func parseRolloutRequest(r rolloutRequest) (parsedRollout, error) {
	var out parsedRollout

	switch {
	case r.Role == "" && len(r.Nodes) == 0:
		return out, errors.New("select a role or at least one node")
	case r.Role != "" && len(r.Nodes) > 0:
		return out, errors.New("choose either a role or specific nodes, not both")
	}

	if err := cluster.ValidatePatch(r.Patch); err != nil {
		return out, err
	}

	mode, err := cluster.ParseApplyMode(r.Mode)
	if err != nil {
		return out, err
	}

	if err := cluster.ValidateRolloutMode(mode); err != nil {
		return out, err
	}

	out.selector = cluster.Selector{Role: r.Role, Nodes: r.Nodes}
	out.patch = r.Patch
	out.mode = mode
	out.opts = cluster.DefaultRolloutOptions
	out.opts.ContinueOnError = r.ContinueOnError

	return out, nil
}

// handleRollout applies a patch to a role or a set of nodes, one node at a time, as a job.
func (s *Server) handleRollout(w http.ResponseWriter, r *http.Request) {
	var body rolloutRequest

	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	req, err := parseRolloutRequest(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	c := s.cl(w)
	if c == nil {
		return
	}

	ctx, cancel := reqCtx(r, 30*time.Second)
	defer cancel()

	nodes, err := c.ListNodes(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)

		return
	}

	targets, skipped, err := cluster.SelectTargets(nodes, req.selector)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	what := req.selector.Role
	switch what {
	case "":
		what = fmt.Sprintf("%d node(s)", len(targets))
	case "all":
		what = "all nodes"
	default:
		what = "all " + what + "s"
	}

	title := fmt.Sprintf("Config patch → %s (%s)", what, strings.ToLower(req.mode.String()))

	job, err := s.jobs.Start("config-rollout", title, true, func(ctx context.Context, log func(string, ...any)) error {
		names := make([]string, 0, len(targets))
		for _, n := range targets {
			names = append(names, n.Name)
		}

		log("targets (in order): %s", strings.Join(names, ", "))

		if len(skipped) > 0 {
			log("skipped (unreachable): %s", strings.Join(skipped, ", "))
		}

		log("patch:\n%s", strings.TrimRight(req.patch, "\n"))

		return cluster.RollingPatch(ctx, c, targets, req.patch, req.mode, req.opts, log)
	})
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, jobs.ErrBusy) {
			code = http.StatusConflict
		}

		writeError(w, code, err)

		return
	}

	writeJSON(w, http.StatusAccepted, job)
}

type driftResponse struct {
	Roles   []drift.RoleReport `json:"roles"`
	Errors  map[string]string  `json:"errors"`
	Ignored []string           `json:"ignored"`
}

// handleDrift compares the live (redacted) configs of nodes within each role.
func (s *Server) handleDrift(w http.ResponseWriter, r *http.Request) {
	c := s.cl(w)
	if c == nil {
		return
	}

	ctx, cancel := reqCtx(r, 45*time.Second)
	defer cancel()

	nodes, err := c.ListNodes(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)

		return
	}

	resp := driftResponse{Errors: map[string]string{}, Ignored: drift.DefaultIgnore}
	if r.URL.Query().Get("all") == "1" {
		resp.Ignored = []string{}
	}

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		dns []drift.Node
	)

	for _, n := range nodes {
		if !n.Reachable {
			resp.Errors[n.Name] = "unreachable"

			continue
		}

		wg.Add(1)

		go func(n cluster.Node) {
			defer wg.Done()

			cfg, err := c.MachineConfig(ctx, n.IP, true)

			var vals drift.Values
			if err == nil {
				vals, err = drift.Flatten([]byte(cfg.YAML))
			}

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				resp.Errors[n.Name] = err.Error()

				return
			}

			dns = append(dns, drift.Node{Name: n.Name, Role: n.Role, Config: vals})
		}(n)
	}

	wg.Wait()

	resp.Roles = drift.Compare(dns, resp.Ignored)
	writeJSON(w, http.StatusOK, resp)
}
