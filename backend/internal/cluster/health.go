package cluster

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Check status values.
const (
	StatusOK   = "ok"
	StatusWarn = "warn"
	StatusFail = "fail"
)

// Check is one health check result.
type Check struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Status string   `json:"status"`
	Detail string   `json:"detail"`
	Items  []string `json:"items,omitempty"`
}

// Overview is the dashboard payload.
type Overview struct {
	ClusterName       string     `json:"clusterName"`
	KubernetesVersion string     `json:"kubernetesVersion"`
	TalosVersions     []string   `json:"talosVersions"`
	Nodes             []Node     `json:"nodes"`
	Etcd              EtcdInfo   `json:"etcd"`
	Checks            []Check    `json:"checks"`
	Status            string     `json:"status"`
	Workloads         *Workloads `json:"workloads,omitempty"`
	GeneratedAt       time.Time  `json:"generatedAt"`
}

// Overview runs all health checks and gathers dashboard data.
func (c *Cluster) Overview(ctx context.Context) (*Overview, error) {
	nodes, err := c.ListNodes(ctx)
	if err != nil {
		return nil, err
	}

	ov := &Overview{ClusterName: c.Name(), Nodes: nodes, GeneratedAt: time.Now()}

	var cps []Node

	for _, n := range nodes {
		if n.Role == RoleControlPlane && n.Reachable {
			cps = append(cps, n)
		}
	}

	var (
		wg        sync.WaitGroup
		services  = map[string][]Service{}
		mounts    = map[string][]Mount{}
		svcErrs   = map[string]string{}
		mu        sync.Mutex
		workloads *Workloads
		wlErr     error
	)

	wg.Add(3)

	go func() {
		defer wg.Done()
		ov.Etcd = c.Etcd(ctx, cps)
	}()

	go func() {
		defer wg.Done()
		workloads, wlErr = c.Workloads(ctx)
	}()

	go func() {
		defer wg.Done()

		if v, err := c.KubernetesVersion(ctx); err == nil {
			ov.KubernetesVersion = v
		}
	}()

	for _, n := range nodes {
		if !n.Reachable {
			continue
		}

		wg.Add(1)

		go func(n Node) {
			defer wg.Done()

			svcs, err := c.Services(ctx, n.IP)
			m, _ := c.Mounts(ctx, n.IP)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				svcErrs[n.Name] = shortErr(err)
			}

			services[n.Name] = svcs
			mounts[n.Name] = m
		}(n)
	}

	wg.Wait()

	ov.Workloads = workloads

	versions := map[string]struct{}{}
	for _, n := range nodes {
		if n.TalosVersion != "" {
			versions[n.TalosVersion] = struct{}{}
		}
	}

	for v := range versions {
		ov.TalosVersions = append(ov.TalosVersions, v)
	}

	sort.Strings(ov.TalosVersions)

	ov.Checks = []Check{
		checkTalosReachable(nodes),
		checkKubeReady(nodes),
		checkVersionSkew(nodes),
		checkEtcd(ov.Etcd, len(cps), nodes),
		checkServices(services, svcErrs),
		checkMemory(nodes),
		checkDisk(mounts),
		checkWorkloads(workloads, wlErr),
	}

	ov.Status = StatusOK

	for _, ch := range ov.Checks {
		if ch.Status == StatusFail {
			ov.Status = StatusFail

			break
		}

		if ch.Status == StatusWarn {
			ov.Status = StatusWarn
		}
	}

	return ov, nil
}

func checkTalosReachable(nodes []Node) Check {
	ch := Check{ID: "talos-api", Name: "Talos API reachable", Status: StatusOK}

	for _, n := range nodes {
		if !n.Reachable {
			ch.Items = append(ch.Items, fmt.Sprintf("%s (%s): %s", n.Name, n.IP, n.Error))
		}
	}

	if len(ch.Items) > 0 {
		ch.Status = StatusFail
		ch.Detail = fmt.Sprintf("%d of %d nodes unreachable", len(ch.Items), len(nodes))
	} else {
		ch.Detail = fmt.Sprintf("all %d nodes respond", len(nodes))
	}

	return ch
}

func checkKubeReady(nodes []Node) Check {
	ch := Check{ID: "k8s-nodes", Name: "Kubernetes nodes Ready", Status: StatusOK}
	inKube := 0

	for _, n := range nodes {
		if !n.InKubernetes {
			continue
		}

		inKube++

		if !n.Ready {
			ch.Items = append(ch.Items, n.Name+" is NotReady")
		}

		if !n.Schedulable {
			ch.Items = append(ch.Items, n.Name+" is cordoned")
		}
	}

	switch {
	case inKube == 0:
		ch.Status = StatusWarn
		ch.Detail = "Kubernetes API unavailable"
	case len(ch.Items) > 0:
		ch.Status = StatusFail
		ch.Detail = strings.Join(ch.Items, ", ")
	default:
		ch.Detail = fmt.Sprintf("%d/%d Ready", inKube, inKube)
	}

	return ch
}

func checkVersionSkew(nodes []Node) Check {
	ch := Check{ID: "version-skew", Name: "Consistent versions", Status: StatusOK}
	talos := map[string][]string{}
	kubelet := map[string][]string{}

	for _, n := range nodes {
		if n.TalosVersion != "" {
			talos[n.TalosVersion] = append(talos[n.TalosVersion], n.Name)
		}

		if n.KubeletVersion != "" {
			kubelet[n.KubeletVersion] = append(kubelet[n.KubeletVersion], n.Name)
		}
	}

	if len(talos) > 1 {
		ch.Status = StatusWarn

		for v, ns := range talos {
			ch.Items = append(ch.Items, fmt.Sprintf("Talos %s: %s", v, strings.Join(ns, ", ")))
		}
	}

	if len(kubelet) > 1 {
		ch.Status = StatusWarn

		for v, ns := range kubelet {
			ch.Items = append(ch.Items, fmt.Sprintf("kubelet %s: %s", v, strings.Join(ns, ", ")))
		}
	}

	sort.Strings(ch.Items)

	if ch.Status == StatusOK {
		ch.Detail = "all nodes run the same Talos and kubelet versions"
	} else {
		ch.Detail = "mixed versions (expected mid-upgrade)"
	}

	return ch
}

func checkEtcd(info EtcdInfo, reachableCPs int, nodes []Node) Check {
	ch := Check{ID: "etcd", Name: "etcd quorum", Status: StatusOK}

	totalCPs := 0
	for _, n := range nodes {
		if n.Role == RoleControlPlane {
			totalCPs++
		}
	}

	if len(info.Members) == 0 {
		ch.Status = StatusFail
		ch.Detail = "could not read etcd members"

		if info.Error != "" {
			ch.Detail += ": " + info.Error
		}

		return ch
	}

	healthy := 0

	for _, m := range info.Members {
		if m.Healthy {
			healthy++
		} else {
			msg := m.Hostname + ": "
			if m.Error != "" {
				msg += m.Error
			} else {
				msg += strings.Join(m.Errors, "; ")
			}

			ch.Items = append(ch.Items, msg)
		}

		if m.IsLearner {
			ch.Items = append(ch.Items, m.Hostname+" is a learner")
		}
	}

	quorum := len(info.Members)/2 + 1

	for _, a := range info.Alarms {
		ch.Items = append(ch.Items, "alarm: "+a)
	}

	switch {
	case healthy < quorum:
		ch.Status = StatusFail
	case healthy < len(info.Members) || len(info.Alarms) > 0 || info.Leader == "":
		ch.Status = StatusWarn
	case totalCPs > 0 && len(info.Members) != totalCPs:
		ch.Status = StatusWarn
		ch.Items = append(ch.Items, fmt.Sprintf("%d members but %d control plane nodes", len(info.Members), totalCPs))
	}

	_ = reachableCPs
	ch.Detail = fmt.Sprintf("%d/%d members healthy", healthy, len(info.Members))

	if info.Leader != "" {
		ch.Detail += ", leader " + info.Leader
	} else {
		ch.Detail += ", no leader"
	}

	return ch
}

func checkServices(services map[string][]Service, errs map[string]string) Check {
	ch := Check{ID: "services", Name: "Talos services healthy", Status: StatusOK}
	total := 0

	for node, e := range errs {
		ch.Items = append(ch.Items, node+": "+e)
	}

	for node, svcs := range services {
		for _, s := range svcs {
			total++

			bad := false

			switch s.State {
			case "Running", "Finished", "Skipped":
			default:
				bad = true
			}

			if s.HealthKnown && !s.Healthy {
				bad = true
			}

			if bad {
				msg := fmt.Sprintf("%s/%s: %s", node, s.ID, s.State)
				if s.Message != "" {
					msg += " (" + s.Message + ")"
				}

				ch.Items = append(ch.Items, msg)
			}
		}
	}

	sort.Strings(ch.Items)

	if len(ch.Items) > 0 {
		ch.Status = StatusFail
		ch.Detail = fmt.Sprintf("%d issue(s)", len(ch.Items))
	} else {
		ch.Detail = fmt.Sprintf("%d services OK", total)
	}

	return ch
}

func checkMemory(nodes []Node) Check {
	ch := Check{ID: "memory", Name: "Memory pressure", Status: StatusOK, Detail: "all nodes have >10% memory available"}

	for _, n := range nodes {
		if n.MemTotal == 0 {
			continue
		}

		pct := float64(n.MemAvailable) / float64(n.MemTotal) * 100
		if pct < 10 {
			ch.Items = append(ch.Items, fmt.Sprintf("%s: %.0f%% available", n.Name, pct))
		}
	}

	if len(ch.Items) > 0 {
		ch.Status = StatusWarn
		ch.Detail = fmt.Sprintf("%d node(s) low on memory", len(ch.Items))
	}

	return ch
}

func checkDisk(mounts map[string][]Mount) Check {
	ch := Check{ID: "disk", Name: "Disk usage", Status: StatusOK, Detail: "all filesystems below 85%"}

	for node, ms := range mounts {
		for _, m := range ms {
			if !strings.HasPrefix(m.MountedOn, "/var") && m.MountedOn != "/system/state" {
				continue
			}

			used := float64(m.Size-m.Available) / float64(m.Size) * 100
			if used >= 85 {
				ch.Items = append(ch.Items, fmt.Sprintf("%s %s: %.0f%% used", node, m.MountedOn, used))

				if used >= 95 {
					ch.Status = StatusFail
				} else if ch.Status == StatusOK {
					ch.Status = StatusWarn
				}
			}
		}
	}

	sort.Strings(ch.Items)

	if len(ch.Items) > 0 {
		ch.Detail = fmt.Sprintf("%d filesystem(s) filling up", len(ch.Items))
	}

	return ch
}

func checkWorkloads(w *Workloads, err error) Check {
	ch := Check{ID: "workloads", Name: "Workloads", Status: StatusOK}

	if err != nil {
		ch.Status = StatusWarn
		ch.Detail = "Kubernetes API unavailable: " + shortErr(err)

		return ch
	}

	for _, p := range w.Problems {
		ch.Items = append(ch.Items, fmt.Sprintf("pod %s/%s: %s", p.Namespace, p.Name, p.Reason))
	}

	unhealthy := 0

	for _, c := range w.Controllers {
		if !c.Healthy {
			unhealthy++

			ch.Items = append(ch.Items, fmt.Sprintf("%s %s/%s: %d/%d ready", c.Kind, c.Namespace, c.Name, c.Ready, c.Desired))
		}
	}

	if len(w.Problems) > 0 || unhealthy > 0 {
		ch.Status = StatusWarn

		for _, p := range w.Problems {
			if p.Namespace == "kube-system" {
				ch.Status = StatusFail
			}
		}
	}

	ch.Detail = fmt.Sprintf("%d/%d pods healthy, %d unhealthy controller(s)", w.PodsTotal-len(w.Problems), w.PodsTotal, unhealthy)

	return ch
}
