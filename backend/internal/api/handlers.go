package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lars/talos-controller/internal/cluster"
	"github.com/lars/talos-controller/internal/jobs"
	"github.com/lars/talos-controller/internal/releases"
)

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	c := s.cl(w)
	if c == nil {
		return
	}

	force := r.URL.Query().Get("refresh") == "1"

	s.ovMu.Lock()
	defer s.ovMu.Unlock()

	if !force && s.ovData != nil && time.Since(s.ovAt) < 10*time.Second {
		writeJSON(w, http.StatusOK, s.ovData)

		return
	}

	ctx, cancel := reqCtx(r, 45*time.Second)
	defer cancel()

	ov, err := c.Overview(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)

		return
	}

	s.ovData, s.ovAt = ov, time.Now()
	writeJSON(w, http.StatusOK, ov)
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
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

	writeJSON(w, http.StatusOK, nodes)
}

type nodeDetail struct {
	Node        *cluster.Node              `json:"node"`
	Services    []cluster.Service          `json:"services"`
	Mounts      []cluster.Mount            `json:"mounts"`
	Config      *cluster.MachineConfigInfo `json:"config,omitempty"`
	ServicesErr string                     `json:"servicesError,omitempty"`
	ConfigErr   string                     `json:"configError,omitempty"`
}

func (s *Server) handleNode(w http.ResponseWriter, r *http.Request) {
	c := s.cl(w)
	if c == nil {
		return
	}

	ctx, cancel := reqCtx(r, 30*time.Second)
	defer cancel()

	n, err := c.FindNode(ctx, r.PathValue("node"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)

		return
	}

	d := nodeDetail{Node: n, Services: []cluster.Service{}, Mounts: []cluster.Mount{}}

	if !n.Reachable {
		writeJSON(w, http.StatusOK, d)

		return
	}

	var wg sync.WaitGroup

	wg.Add(3)

	go func() {
		defer wg.Done()

		svcs, err := c.Services(ctx, n.IP)
		if err != nil {
			d.ServicesErr = err.Error()
		} else {
			d.Services = svcs
		}
	}()

	go func() {
		defer wg.Done()

		if m, err := c.Mounts(ctx, n.IP); err == nil {
			d.Mounts = m
		}
	}()

	go func() {
		defer wg.Done()

		cfg, err := c.MachineConfig(ctx, n.IP, false)
		if err != nil {
			d.ConfigErr = err.Error()
		} else {
			d.Config = cfg
		}
	}()

	wg.Wait()
	writeJSON(w, http.StatusOK, d)
}

// resolveIP maps a node name to its Talos address.
func resolveIP(ctx context.Context, c *cluster.Cluster, nameOrIP string) (string, error) {
	n, err := c.FindNode(ctx, nameOrIP)
	if err != nil {
		return "", err
	}

	return n.IP, nil
}

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	c := s.cl(w)
	if c == nil {
		return
	}

	ctx, cancel := reqCtx(r, 20*time.Second)
	defer cancel()

	ip, err := resolveIP(ctx, c, r.PathValue("node"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)

		return
	}

	svcs, err := c.Services(ctx, ip)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)

		return
	}

	writeJSON(w, http.StatusOK, svcs)
}

func (s *Server) handleServiceRestart(w http.ResponseWriter, r *http.Request) {
	c := s.cl(w)
	if c == nil {
		return
	}

	ctx, cancel := reqCtx(r, 45*time.Second)
	defer cancel()

	ip, err := resolveIP(ctx, c, r.PathValue("node"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)

		return
	}

	id := r.PathValue("id")
	if err := c.RestartService(ctx, ip, id); err != nil {
		writeError(w, http.StatusBadGateway, err)

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "restarting " + id})
}

func linesParam(r *http.Request, def int) int {
	n, err := strconv.Atoi(r.URL.Query().Get("lines"))
	if err != nil || n <= 0 {
		return def
	}

	if n > 5000 {
		return 5000
	}

	return n
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	c := s.cl(w)
	if c == nil {
		return
	}

	ctx, cancel := reqCtx(r, 30*time.Second)
	defer cancel()

	ip, err := resolveIP(ctx, c, r.PathValue("node"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)

		return
	}

	out, err := c.Logs(ctx, ip, r.PathValue("service"), int32(linesParam(r, 300)))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"log": out})
}

func (s *Server) handleDmesg(w http.ResponseWriter, r *http.Request) {
	c := s.cl(w)
	if c == nil {
		return
	}

	ctx, cancel := reqCtx(r, 30*time.Second)
	defer cancel()

	ip, err := resolveIP(ctx, c, r.PathValue("node"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)

		return
	}

	out, err := c.Dmesg(ctx, ip, linesParam(r, 300))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"log": out})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	c := s.cl(w)
	if c == nil {
		return
	}

	ctx, cancel := reqCtx(r, 20*time.Second)
	defer cancel()

	ip, err := resolveIP(ctx, c, r.PathValue("node"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)

		return
	}

	cfg, err := c.MachineConfig(ctx, ip, true)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)

		return
	}

	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleReboot(w http.ResponseWriter, r *http.Request) {
	c := s.cl(w)
	if c == nil {
		return
	}

	var body struct {
		Confirm string `json:"confirm"`
	}

	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	ctx, cancel := reqCtx(r, 20*time.Second)
	defer cancel()

	n, err := c.FindNode(ctx, r.PathValue("node"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)

		return
	}

	if body.Confirm != n.Name {
		writeError(w, http.StatusBadRequest, errors.New("type the node name to confirm"))

		return
	}

	node := *n

	job, err := s.jobs.Start("reboot", "Reboot "+node.Name, false, func(ctx context.Context, log func(string, ...any)) error {
		boot0, _ := c.BootTime(ctx, node.IP)

		log("rebooting %s (%s)", node.Name, node.IP)

		if err := c.Reboot(ctx, node.IP); err != nil {
			return err
		}

		log("reboot requested, waiting for node to come back")

		deadline := time.Now().Add(20 * time.Minute)
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Second):
			}

			bt, err := c.BootTime(ctx, node.IP)
			if err == nil && bt > boot0+30 {
				log("node is back (Talos API responding)")

				return nil
			}
		}

		return errors.New("node did not come back within 20 minutes")
	})
	if err != nil {
		writeError(w, http.StatusConflict, err)

		return
	}

	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleEtcd(w http.ResponseWriter, r *http.Request) {
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

	var cps []cluster.Node

	for _, n := range nodes {
		if n.Role == cluster.RoleControlPlane && n.Reachable {
			cps = append(cps, n)
		}
	}

	writeJSON(w, http.StatusOK, c.Etcd(ctx, cps))
}

func (s *Server) handleWorkloads(w http.ResponseWriter, r *http.Request) {
	c := s.cl(w)
	if c == nil {
		return
	}

	ctx, cancel := reqCtx(r, 30*time.Second)
	defer cancel()

	wl, err := c.Workloads(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)

		return
	}

	writeJSON(w, http.StatusOK, wl)
}

func (s *Server) handlePatch(w http.ResponseWriter, r *http.Request) {
	c := s.cl(w)
	if c == nil {
		return
	}

	var body struct {
		Nodes  []string `json:"nodes"`
		Patch  string   `json:"patch"`
		Mode   string   `json:"mode"`
		DryRun bool     `json:"dryRun"`
	}

	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	if len(body.Nodes) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("select at least one node"))

		return
	}

	if err := cluster.ValidatePatch(body.Patch); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	mode, err := cluster.ParseApplyMode(body.Mode)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	ctx, cancel := reqCtx(r, 90*time.Second)
	defer cancel()

	results := make([]cluster.PatchResult, len(body.Nodes))

	var wg sync.WaitGroup

	for i, name := range body.Nodes {
		wg.Add(1)

		go func(i int, name string) {
			defer wg.Done()

			ip, err := resolveIP(ctx, c, name)
			if err != nil {
				results[i] = cluster.PatchResult{Node: name, Error: err.Error()}

				return
			}

			res := c.ApplyPatch(ctx, ip, body.Patch, mode, body.DryRun)
			res.Node = name
			results[i] = res
		}(i, name)
	}

	wg.Wait()
	writeJSON(w, http.StatusOK, results)
}

type upgradeNode struct {
	Name           string `json:"name"`
	IP             string `json:"ip"`
	Role           string `json:"role"`
	Reachable      bool   `json:"reachable"`
	TalosVersion   string `json:"talosVersion"`
	KubeletVersion string `json:"kubeletVersion"`
	InstallImage   string `json:"installImage"`
	Arch           string `json:"arch"`
}

type upgradeInfo struct {
	Nodes              []upgradeNode      `json:"nodes"`
	KubernetesVersion  string             `json:"kubernetesVersion"`
	LatestTalos        *releases.Release  `json:"latestTalos"`
	RecommendedTalos   *releases.Release  `json:"recommendedTalos"`
	LatestKubernetes   *releases.Release  `json:"latestKubernetes"`
	TalosReleases      []releases.Release `json:"talosReleases"`
	KubernetesReleases []releases.Release `json:"kubernetesReleases"`
	ReleasesError      string             `json:"releasesError,omitempty"`
	ReleasesFetchedAt  time.Time          `json:"releasesFetchedAt"`
	Notes              []string           `json:"notes"`
	ControlPlaneIP     string             `json:"controlPlaneIp"`
}

func (s *Server) handleUpgrades(w http.ResponseWriter, r *http.Request) {
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

	info := upgradeInfo{Nodes: []upgradeNode{}, Notes: []string{}}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	for _, n := range nodes {
		un := upgradeNode{Name: n.Name, IP: n.IP, Role: n.Role, Reachable: n.Reachable, TalosVersion: n.TalosVersion, KubeletVersion: n.KubeletVersion, Arch: n.Arch}

		if n.Role == cluster.RoleControlPlane && n.Reachable && info.ControlPlaneIP == "" {
			info.ControlPlaneIP = n.IP
		}

		idx := len(info.Nodes)
		info.Nodes = append(info.Nodes, un)

		if !n.Reachable {
			continue
		}

		wg.Add(1)

		go func(idx int, ip string) {
			defer wg.Done()

			cfg, err := c.MachineConfig(ctx, ip, false)
			if err != nil {
				return
			}

			mu.Lock()
			info.Nodes[idx].InstallImage = cfg.InstallImage
			mu.Unlock()
		}(idx, n.IP)
	}

	var rel *releases.Info

	wg.Add(2)

	go func() {
		defer wg.Done()

		rel = s.releases.Get(ctx, r.URL.Query().Get("refresh") == "1")
	}()

	go func() {
		defer wg.Done()

		if v, err := c.KubernetesVersion(ctx); err == nil {
			info.KubernetesVersion = v
		}
	}()

	wg.Wait()

	info.ReleasesError = rel.Error
	info.ReleasesFetchedAt = rel.FetchedAt
	info.LatestTalos = releases.Latest(rel.Talos)
	info.LatestKubernetes = releases.Latest(rel.Kubernetes)
	info.TalosReleases = limitReleases(rel.Talos, 15)
	info.KubernetesReleases = limitReleases(rel.Kubernetes, 15)

	oldest := ""
	for _, n := range info.Nodes {
		if n.TalosVersion != "" && (oldest == "" || releases.Compare(n.TalosVersion, oldest) < 0) {
			oldest = n.TalosVersion
		}
	}

	info.RecommendedTalos = recommendNext(oldest, rel.Talos)

	if info.LatestTalos != nil && info.RecommendedTalos != nil && info.RecommendedTalos.Version != info.LatestTalos.Version {
		info.Notes = append(info.Notes, fmt.Sprintf(
			"Talos should be upgraded one minor release at a time. Recommended next step: %s, then continue to %s.",
			info.RecommendedTalos.Version, info.LatestTalos.Version))
	}

	info.Notes = append(info.Notes,
		"Upgrade Talos first, then Kubernetes. Check the Talos support matrix before moving to a new Kubernetes minor.",
		"Each node keeps its own installer image (schematic/extensions); only the tag is changed.")

	writeJSON(w, http.StatusOK, info)
}

func limitReleases(rs []releases.Release, n int) []releases.Release {
	out := []releases.Release{}

	for _, r := range rs {
		if len(out) >= n {
			break
		}

		out = append(out, r)
	}

	return out
}

// recommendNext picks the newest patch of the next minor (or current minor if
// that is the newest), so users don't skip minor releases.
func recommendNext(current string, rs []releases.Release) *releases.Release {
	latest := releases.Latest(rs)
	if current == "" || latest == nil {
		return latest
	}

	curMinor := minorOf(current)
	latestMinor := minorOf(latest.Version)

	if curMinor == latestMinor || curMinor == "" {
		return latest
	}

	// Find the next minor after current.
	var (
		nextMinor string
		best      *releases.Release
	)

	for i := range rs {
		r := &rs[i]
		if r.Prerelease {
			continue
		}

		m := minorOf(r.Version)
		if releases.Compare(m+".0", curMinor+".0") <= 0 {
			continue
		}

		if nextMinor == "" || releases.Compare(m+".0", nextMinor+".0") < 0 {
			nextMinor = m
			best = r
		} else if m == nextMinor && releases.Compare(r.Version, best.Version) > 0 {
			best = r
		}
	}

	if best == nil {
		return latest
	}

	return best
}

func minorOf(v string) string {
	parts := strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3)
	if len(parts) < 2 {
		return ""
	}

	return "v" + parts[0] + "." + parts[1]
}

func (s *Server) handleTalosUpgrade(w http.ResponseWriter, r *http.Request) {
	c := s.cl(w)
	if c == nil {
		return
	}

	var body struct {
		Nodes           []string          `json:"nodes"`
		Version         string            `json:"version"`
		Images          map[string]string `json:"images"`
		Stage           bool              `json:"stage"`
		Force           bool              `json:"force"`
		SkipPreflight   bool              `json:"skipPreflight"`
		ContinueOnError bool              `json:"continueOnError"`
	}

	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	if len(body.Nodes) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("select at least one node"))

		return
	}

	if body.Version == "" && len(body.Images) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("version or images required"))

		return
	}

	ctx, cancel := reqCtx(r, 45*time.Second)
	defer cancel()

	var plan []cluster.UpgradeStep

	for _, name := range body.Nodes {
		n, err := c.FindNode(ctx, name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)

			return
		}

		image := body.Images[name]
		if image == "" {
			current := "ghcr.io/siderolabs/installer"

			if cfg, err := c.MachineConfig(ctx, n.IP, false); err == nil && cfg.InstallImage != "" {
				current = cfg.InstallImage
			}

			image = cluster.ReplaceTag(current, body.Version)
		}

		plan = append(plan, cluster.UpgradeStep{Node: *n, Image: image})
	}

	// Control planes first (one at a time), then workers, preserving the
	// user's order within each group.
	ordered := make([]cluster.UpgradeStep, 0, len(plan))
	for _, st := range plan {
		if st.Node.Role == cluster.RoleControlPlane {
			ordered = append(ordered, st)
		}
	}

	for _, st := range plan {
		if st.Node.Role != cluster.RoleControlPlane {
			ordered = append(ordered, st)
		}
	}

	opts := cluster.UpgradeOptions{Stage: body.Stage, Force: body.Force, SkipPreflight: body.SkipPreflight, ContinueOnError: body.ContinueOnError}

	title := fmt.Sprintf("Talos upgrade → %s (%d nodes)", body.Version, len(ordered))
	if body.Version == "" {
		title = fmt.Sprintf("Talos upgrade (%d nodes)", len(ordered))
	}

	job, err := s.jobs.Start("talos-upgrade", title, true, func(ctx context.Context, log func(string, ...any)) error {
		log("rolling upgrade plan:")

		for i, st := range ordered {
			log("  %d. %s (%s) → %s", i+1, st.Node.Name, st.Node.Role, st.Image)
		}

		return c.RollingUpgrade(ctx, ordered, opts, log)
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

func (s *Server) handleJobs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.jobs.List())
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	j, ok := s.jobs.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("job not found"))

		return
	}

	writeJSON(w, http.StatusOK, j)
}

func (s *Server) handleJobCancel(w http.ResponseWriter, r *http.Request) {
	if !s.jobs.Cancel(r.PathValue("id")) {
		writeError(w, http.StatusConflict, errors.New("job is not running"))

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelling"})
}
