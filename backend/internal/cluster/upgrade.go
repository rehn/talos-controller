package cluster

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// UpgradeOptions controls a rolling Talos upgrade.
type UpgradeOptions struct {
	Stage           bool `json:"stage"`
	Force           bool `json:"force"`
	SkipPreflight   bool `json:"skipPreflight"`
	ContinueOnError bool `json:"continueOnError"`
}

// Logger receives progress lines.
type Logger func(format string, args ...any)

// ImageTag returns the tag part of an image reference ("" for digests / no tag).
func ImageTag(image string) string {
	if strings.Contains(image, "@") {
		return ""
	}

	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")

	if colon > slash {
		return image[colon+1:]
	}

	return ""
}

// ReplaceTag swaps the tag on an image reference.
func ReplaceTag(image, tag string) string {
	if i := strings.Index(image, "@"); i >= 0 {
		image = image[:i]
	}

	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")

	if colon > slash {
		image = image[:colon]
	}

	return image + ":" + tag
}

// UpgradeStep is one node in a rolling upgrade plan.
type UpgradeStep struct {
	Node  Node
	Image string
}

// RollingUpgrade upgrades the given nodes one at a time, waiting for each to
// come back healthy before continuing.
func (c *Cluster) RollingUpgrade(ctx context.Context, plan []UpgradeStep, opts UpgradeOptions, log Logger) error {
	failed := 0

	for i, step := range plan {
		if err := ctx.Err(); err != nil {
			return err
		}

		n := step.Node

		log("━━ [%d/%d] %s (%s, %s) → %s", i+1, len(plan), n.Name, n.IP, n.Role, step.Image)

		if step.Image == "" {
			return errors.New("no image for " + n.Name)
		}

		err := c.upgradeOne(ctx, n, step.Image, opts, ImageTag(step.Image), log)
		if err != nil {
			failed++

			log("✗ %s: %v", n.Name, err)

			if !opts.ContinueOnError {
				return fmt.Errorf("upgrade of %s failed, stopping rollout: %w", n.Name, err)
			}

			continue
		}

		log("✓ %s upgraded", n.Name)
	}

	if failed > 0 {
		return fmt.Errorf("%d node(s) failed to upgrade", failed)
	}

	return nil
}

func (c *Cluster) upgradeOne(ctx context.Context, n Node, image string, opts UpgradeOptions, target string, log Logger) error {
	current, err := c.TalosVersion(ctx, n.IP)
	if err != nil {
		return fmt.Errorf("node unreachable before upgrade: %w", err)
	}

	if target != "" && current == target && !opts.Force {
		log("already on %s, skipping", target)

		return nil
	}

	if !opts.SkipPreflight {
		log("preflight: checking cluster health")

		if err := c.preflight(ctx, n); err != nil {
			return fmt.Errorf("preflight failed (enable 'skip preflight' to override): %w", err)
		}

		log("preflight OK")
	}

	boot0, err := c.BootTime(ctx, n.IP)
	if err != nil {
		return fmt.Errorf("reading boot time: %w", err)
	}

	log("current version %s, upgrading to %s", current, image)

	if err := c.upgradeRaw(ctx, n.IP, image, opts.Stage, opts.Force); err != nil {
		return fmt.Errorf("upgrade API call: %w", err)
	}

	log("upgrade accepted, waiting for the node to reboot (this can take several minutes)")

	if err := waitFor(ctx, 30*time.Minute, 10*time.Second, func() (bool, string) {
		bt, err := c.BootTime(ctx, n.IP)
		if err != nil {
			return false, "node offline"
		}

		if bt > boot0+30 {
			return true, ""
		}

		return false, "still running the old boot"
	}, log); err != nil {
		return fmt.Errorf("waiting for reboot: %w", err)
	}

	log("node rebooted, verifying version")

	var running string

	if err := waitFor(ctx, 5*time.Minute, 5*time.Second, func() (bool, string) {
		v, err := c.TalosVersion(ctx, n.IP)
		if err != nil {
			return false, "Talos API not ready"
		}

		running = v

		return true, ""
	}, log); err != nil {
		return err
	}

	if target != "" && running != target {
		return fmt.Errorf("node came back on %s, expected %s (upgrade may have rolled back)", running, target)
	}

	log("running %s", running)

	if err := c.WaitHealthy(ctx, n, log); err != nil {
		return err
	}

	log("letting the node settle for 30s")

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
	}

	return nil
}

func (c *Cluster) preflight(ctx context.Context, target Node) error {
	nodes, err := c.kubeNodes(ctx)
	if err == nil {
		var notReady []string

		for _, n := range nodes {
			if !n.Ready {
				notReady = append(notReady, n.Name)
			}
		}

		if len(notReady) > 0 {
			return fmt.Errorf("nodes not Ready: %s", strings.Join(notReady, ", "))
		}
	}

	if target.Role == RoleControlPlane {
		var cps []Node

		for _, n := range nodes {
			if n.Role == RoleControlPlane {
				cps = append(cps, n)
			}
		}

		if len(cps) == 0 {
			cps = []Node{target}
		}

		info := c.Etcd(ctx, cps)

		if len(info.Members) == 0 {
			return fmt.Errorf("cannot read etcd members: %s", info.Error)
		}

		for _, m := range info.Members {
			if !m.Healthy {
				return fmt.Errorf("etcd member %s unhealthy", m.Hostname)
			}
		}

		if len(info.Alarms) > 0 {
			return fmt.Errorf("etcd alarms active: %s", strings.Join(info.Alarms, ", "))
		}
	}

	return nil
}

func (c *Cluster) kubeNodeReady(ctx context.Context, name string) (bool, error) {
	nodes, err := c.kubeNodes(ctx)
	if err != nil {
		return false, err
	}

	for _, n := range nodes {
		if n.Name == name {
			return n.Ready, nil
		}
	}

	return false, fmt.Errorf("node %s not registered", name)
}

func waitFor(ctx context.Context, timeout, interval time.Duration, cond func() (bool, string), log Logger) error {
	deadline := time.Now().Add(timeout)
	lastMsg := ""

	for {
		ok, msg := cond()
		if ok {
			return nil
		}

		if msg != lastMsg {
			log("  … %s", msg)
			lastMsg = msg
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s (%s)", timeout, msg)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// WaitHealthy waits until the node is Ready in Kubernetes and, for control
// planes, its etcd member reports no errors.
func (c *Cluster) WaitHealthy(ctx context.Context, n Node, log Logger) error {
	if n.InKubernetes {
		log("waiting for Kubernetes node Ready")

		if err := waitFor(ctx, 10*time.Minute, 10*time.Second, func() (bool, string) {
			ready, err := c.kubeNodeReady(ctx, n.Name)
			if err != nil {
				return false, err.Error()
			}

			return ready, "NotReady"
		}, log); err != nil {
			return fmt.Errorf("waiting for node Ready: %w", err)
		}
	}

	if n.Role == RoleControlPlane {
		log("waiting for etcd member to be healthy")

		if err := waitFor(ctx, 5*time.Minute, 10*time.Second, func() (bool, string) {
			cctx, cancel := nodeCtx(ctx, n.IP, 6*time.Second)
			defer cancel()

			st, err := c.talos.EtcdStatus(cctx)
			if err != nil || len(st.Messages) == 0 || st.Messages[0].MemberStatus == nil {
				return false, "etcd not responding"
			}

			if errs := st.Messages[0].MemberStatus.Errors; len(errs) > 0 {
				return false, strings.Join(errs, "; ")
			}

			return true, ""
		}, log); err != nil {
			return fmt.Errorf("waiting for etcd: %w", err)
		}
	}

	return nil
}
