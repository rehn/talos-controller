package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"google.golang.org/protobuf/types/known/emptypb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Role values.
const (
	RoleControlPlane = "controlplane"
	RoleWorker       = "worker"
	RoleUnknown      = "unknown"
)

// Node is a merged Kubernetes + Talos view of a machine.
type Node struct {
	Name           string  `json:"name"`
	IP             string  `json:"ip"`
	Role           string  `json:"role"`
	Ready          bool    `json:"ready"`
	Schedulable    bool    `json:"schedulable"`
	KubeletVersion string  `json:"kubeletVersion"`
	OSImage        string  `json:"osImage"`
	TalosVersion   string  `json:"talosVersion"`
	Arch           string  `json:"arch"`
	Reachable      bool    `json:"reachable"`
	Error          string  `json:"error,omitempty"`
	CPUs           int     `json:"cpus"`
	MemTotal       uint64  `json:"memTotal"`
	MemAvailable   uint64  `json:"memAvailable"`
	Load1          float64 `json:"load1"`
	Load5          float64 `json:"load5"`
	BootTime       int64   `json:"bootTime"`
	InKubernetes   bool    `json:"inKubernetes"`
}

// ListNodes returns all known nodes enriched with live Talos data.
func (c *Cluster) ListNodes(ctx context.Context) ([]Node, error) {
	nodes, kubeErr := c.kubeNodes(ctx)

	if len(nodes) == 0 {
		for _, n := range c.configuredNodes() {
			h := hostOf(n)
			nodes = append(nodes, Node{Name: h, IP: h, Role: RoleUnknown})
		}
	}

	if len(nodes) == 0 {
		if kubeErr != nil {
			return nil, kubeErr
		}

		return nil, errors.New("no nodes found (kubernetes unavailable and talosconfig has no nodes)")
	}

	var wg sync.WaitGroup

	for i := range nodes {
		wg.Add(1)

		go func(n *Node) {
			defer wg.Done()
			c.enrich(ctx, n)
		}(&nodes[i])
	}

	wg.Wait()

	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Role != nodes[j].Role {
			return nodes[i].Role == RoleControlPlane
		}

		return nodes[i].Name < nodes[j].Name
	})

	return nodes, nil
}

// FindNode resolves a node by name or IP.
func (c *Cluster) FindNode(ctx context.Context, nameOrIP string) (*Node, error) {
	nodes, err := c.kubeNodes(ctx)
	if err == nil {
		for _, n := range nodes {
			if n.Name == nameOrIP || n.IP == nameOrIP {
				c.enrich(ctx, &n)

				return &n, nil
			}
		}
	}

	// Fall back to treating it as a Talos address.
	n := Node{Name: nameOrIP, IP: nameOrIP, Role: RoleUnknown}
	c.enrich(ctx, &n)

	if !n.Reachable {
		return nil, fmt.Errorf("node %q not found: %s", nameOrIP, n.Error)
	}

	return &n, nil
}

func (c *Cluster) kubeNodes(ctx context.Context) ([]Node, error) {
	kc, err := c.Kube(ctx)
	if err != nil {
		return nil, err
	}

	list, err := kc.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing kubernetes nodes: %w", err)
	}

	out := make([]Node, 0, len(list.Items))

	for _, kn := range list.Items {
		n := Node{
			Name:           kn.Name,
			Role:           RoleWorker,
			Schedulable:    !kn.Spec.Unschedulable,
			KubeletVersion: kn.Status.NodeInfo.KubeletVersion,
			OSImage:        kn.Status.NodeInfo.OSImage,
			Arch:           kn.Status.NodeInfo.Architecture,
			InKubernetes:   true,
		}

		if _, ok := kn.Labels["node-role.kubernetes.io/control-plane"]; ok {
			n.Role = RoleControlPlane
		}

		for _, a := range kn.Status.Addresses {
			if a.Type == corev1.NodeInternalIP && n.IP == "" {
				n.IP = a.Address
			}
		}

		for _, cond := range kn.Status.Conditions {
			if cond.Type == corev1.NodeReady {
				n.Ready = cond.Status == corev1.ConditionTrue
			}
		}

		out = append(out, n)
	}

	return out, nil
}

// enrich fills Talos-side fields. Errors are recorded on the node.
func (c *Cluster) enrich(ctx context.Context, n *Node) {
	if n.IP == "" {
		n.Error = "no internal IP"

		return
	}

	cctx, cancel := nodeCtx(ctx, n.IP, 6*time.Second)
	defer cancel()

	ver, err := c.talos.Version(cctx)
	if err != nil {
		n.Error = shortErr(err)

		return
	}

	n.Reachable = true

	if len(ver.Messages) > 0 && ver.Messages[0].Version != nil {
		n.TalosVersion = ver.Messages[0].Version.Tag
		if n.Arch == "" {
			n.Arch = ver.Messages[0].Version.Arch
		}
	}

	if mem, err := c.talos.Memory(cctx); err == nil && len(mem.Messages) > 0 && mem.Messages[0].Meminfo != nil {
		n.MemTotal = mem.Messages[0].Meminfo.Memtotal * 1024
		n.MemAvailable = mem.Messages[0].Meminfo.Memavailable * 1024
	}

	if la, err := c.talos.MachineClient.LoadAvg(cctx, &emptypb.Empty{}); err == nil && len(la.Messages) > 0 {
		n.Load1 = la.Messages[0].Load1
		n.Load5 = la.Messages[0].Load5
	}

	if st, err := c.talos.MachineClient.SystemStat(cctx, &emptypb.Empty{}); err == nil && len(st.Messages) > 0 {
		n.BootTime = int64(st.Messages[0].BootTime)
		n.CPUs = len(st.Messages[0].Cpu)
	}

	if n.Role == RoleUnknown {
		if mt, err := c.machineType(cctx); err == nil {
			n.Role = mt
		}
	}
}

func (c *Cluster) machineType(ctx context.Context) (string, error) {
	mc, err := safe.StateGetByID[*config.MachineConfig](ctx, c.talos.COSI, config.ActiveID)
	if err != nil {
		return "", err
	}

	if mc.Config().Machine().Type().IsControlPlane() {
		return RoleControlPlane, nil
	}

	return RoleWorker, nil
}

// Service is a Talos system service.
type Service struct {
	ID          string    `json:"id"`
	State       string    `json:"state"`
	Healthy     bool      `json:"healthy"`
	HealthKnown bool      `json:"healthKnown"`
	Message     string    `json:"message"`
	LastChange  time.Time `json:"lastChange"`
	LastEvent   string    `json:"lastEvent"`
}

// Services lists Talos services on a node.
func (c *Cluster) Services(ctx context.Context, node string) ([]Service, error) {
	cctx, cancel := nodeCtx(ctx, node, 8*time.Second)
	defer cancel()

	resp, err := c.talos.ServiceList(cctx)
	if err != nil {
		return nil, err
	}

	var out []Service

	for _, m := range resp.Messages {
		for _, s := range m.Services {
			svc := Service{ID: s.Id, State: s.State}

			if s.Health != nil {
				svc.HealthKnown = !s.Health.Unknown
				svc.Healthy = s.Health.Healthy
				svc.Message = s.Health.LastMessage

				if s.Health.LastChange != nil {
					svc.LastChange = s.Health.LastChange.AsTime()
				}
			}

			if s.Events != nil && len(s.Events.Events) > 0 {
				svc.LastEvent = s.Events.Events[len(s.Events.Events)-1].Msg
			}

			out = append(out, svc)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	return out, nil
}

// RestartService restarts a Talos service on a node.
func (c *Cluster) RestartService(ctx context.Context, node, id string) error {
	cctx, cancel := nodeCtx(ctx, node, 30*time.Second)
	defer cancel()

	_, err := c.talos.ServiceRestart(cctx, id)

	return err
}

// Logs returns the last n lines of a Talos service log.
func (c *Cluster) Logs(ctx context.Context, node, service string, lines int32) (string, error) {
	cctx, cancel := nodeCtx(ctx, node, 15*time.Second)
	defer cancel()

	stream, err := c.talos.Logs(cctx, "system", common.ContainerDriver_CONTAINERD, service, false, lines)
	if err != nil {
		return "", err
	}

	return drain(stream.Recv)
}

// Dmesg returns the kernel log tail of a node.
func (c *Cluster) Dmesg(ctx context.Context, node string, lines int) (string, error) {
	cctx, cancel := nodeCtx(ctx, node, 15*time.Second)
	defer cancel()

	stream, err := c.talos.Dmesg(cctx, false, false)
	if err != nil {
		return "", err
	}

	out, err := drain(stream.Recv)
	if err != nil {
		return "", err
	}

	all := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}

	return strings.Join(all, "\n"), nil
}

func drain(recv func() (*common.Data, error)) (string, error) {
	var sb strings.Builder

	for {
		d, err := recv()
		if err != nil {
			if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "EOF") {
				break
			}

			if sb.Len() > 0 {
				break
			}

			return "", err
		}

		if d.Metadata != nil && d.Metadata.Error != "" {
			return "", errors.New(d.Metadata.Error)
		}

		sb.Write(d.Bytes)

		if sb.Len() > 4<<20 {
			break
		}
	}

	return sb.String(), nil
}

// Mount is a filesystem usage entry.
type Mount struct {
	Filesystem string `json:"filesystem"`
	MountedOn  string `json:"mountedOn"`
	Size       uint64 `json:"size"`
	Available  uint64 `json:"available"`
}

// Mounts returns filesystem usage on a node.
func (c *Cluster) Mounts(ctx context.Context, node string) ([]Mount, error) {
	cctx, cancel := nodeCtx(ctx, node, 8*time.Second)
	defer cancel()

	resp, err := c.talos.Mounts(cctx)
	if err != nil {
		return nil, err
	}

	var out []Mount

	for _, m := range resp.Messages {
		for _, s := range m.Stats {
			if s.Size == 0 {
				continue
			}

			out = append(out, Mount{Filesystem: s.Filesystem, MountedOn: s.MountedOn, Size: s.Size, Available: s.Available})
		}
	}

	return out, nil
}

// Reboot reboots a node.
func (c *Cluster) Reboot(ctx context.Context, node string) error {
	cctx, cancel := nodeCtx(ctx, node, 30*time.Second)
	defer cancel()

	return c.talos.Reboot(cctx)
}

// BootTime returns the node boot time (unix seconds).
func (c *Cluster) BootTime(ctx context.Context, node string) (int64, error) {
	cctx, cancel := nodeCtx(ctx, node, 6*time.Second)
	defer cancel()

	st, err := c.talos.MachineClient.SystemStat(cctx, &emptypb.Empty{})
	if err != nil {
		return 0, err
	}

	if len(st.Messages) == 0 {
		return 0, errors.New("empty response")
	}

	return int64(st.Messages[0].BootTime), nil
}

// TalosVersion returns the running Talos version tag of a node.
func (c *Cluster) TalosVersion(ctx context.Context, node string) (string, error) {
	cctx, cancel := nodeCtx(ctx, node, 6*time.Second)
	defer cancel()

	ver, err := c.talos.Version(cctx)
	if err != nil {
		return "", err
	}

	if len(ver.Messages) == 0 || ver.Messages[0].Version == nil {
		return "", errors.New("empty version response")
	}

	return ver.Messages[0].Version.Tag, nil
}

// MachineConfigInfo is a summary of a node's machine configuration.
type MachineConfigInfo struct {
	InstallImage string `json:"installImage"`
	Type         string `json:"type"`
	YAML         string `json:"yaml,omitempty"`
}

// MachineConfig returns the node's active config. Secrets are redacted.
func (c *Cluster) MachineConfig(ctx context.Context, node string, withYAML bool) (*MachineConfigInfo, error) {
	cctx, cancel := nodeCtx(ctx, node, 10*time.Second)
	defer cancel()

	mc, err := safe.StateGetByID[*config.MachineConfig](cctx, c.talos.COSI, config.ActiveID)
	if err != nil {
		return nil, err
	}

	p := mc.Provider()
	info := &MachineConfigInfo{Type: p.Machine().Type().String()}

	if p.Machine().Install() != nil {
		info.InstallImage = p.Machine().Install().Image()
	}

	if withYAML {
		b, err := p.RedactSecrets("<redacted>").EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
		if err != nil {
			return nil, err
		}

		info.YAML = string(b)
	}

	return info, nil
}

// upgradeRaw calls the Talos upgrade API on one node.
func (c *Cluster) upgradeRaw(ctx context.Context, node, image string, stage, force bool) error {
	cctx, cancel := nodeCtx(ctx, node, 2*time.Minute)
	defer cancel()

	//nolint:staticcheck // the legacy Upgrade API performs install+reboot and works on all Talos versions.
	_, err := c.talos.UpgradeWithOptions(cctx,
		client.WithUpgradeImage(image),
		client.WithUpgradeStage(stage),
		client.WithUpgradeForce(force),
		client.WithUpgradeRebootMode(machineapi.UpgradeRequest_DEFAULT),
	)

	return err
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, "desc = "); i >= 0 {
		s = s[i+len("desc = "):]
	}

	s = strings.Trim(s, "\"")

	if len(s) > 200 {
		s = s[:200] + "…"
	}

	return s
}
