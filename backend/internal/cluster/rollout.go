package cluster

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
)

// Selector picks the nodes a rollout targets: either a role
// ("controlplane", "worker", "all") or explicit node names.
type Selector struct {
	Role  string   `json:"role"`
	Nodes []string `json:"nodes"`
}

// SelectTargets resolves a selector against the known nodes. Unreachable
// nodes are returned in skipped. Targets are ordered control planes first,
// then by name.
func SelectTargets(all []Node, sel Selector) (targets []Node, skipped []string, err error) {
	switch {
	case sel.Role != "" && len(sel.Nodes) > 0:
		return nil, nil, errors.New("choose either a role or specific nodes, not both")
	case sel.Role == "" && len(sel.Nodes) == 0:
		return nil, nil, errors.New("no target nodes selected")
	}

	var picked []Node

	if sel.Role != "" {
		switch sel.Role {
		case "all", RoleControlPlane, RoleWorker:
		default:
			return nil, nil, fmt.Errorf("unknown role %q (use controlplane, worker or all)", sel.Role)
		}

		for _, n := range all {
			if sel.Role == "all" || n.Role == sel.Role {
				picked = append(picked, n)
			}
		}
	} else {
		byName := map[string]Node{}
		for _, n := range all {
			byName[n.Name] = n
		}

		for _, name := range sel.Nodes {
			n, ok := byName[name]
			if !ok {
				return nil, nil, fmt.Errorf("unknown node %q", name)
			}

			picked = append(picked, n)
		}
	}

	skipped = []string{}

	for _, n := range picked {
		if n.Reachable {
			targets = append(targets, n)
		} else {
			skipped = append(skipped, n.Name)
		}
	}

	if len(targets) == 0 {
		return nil, skipped, errors.New("none of the selected nodes are reachable")
	}

	sort.SliceStable(targets, func(i, j int) bool {
		ci, cj := targets[i].Role == RoleControlPlane, targets[j].Role == RoleControlPlane
		if ci != cj {
			return ci
		}

		return targets[i].Name < targets[j].Name
	})

	sort.Strings(skipped)

	return targets, skipped, nil
}

// ValidateRolloutMode rejects apply modes that don't make sense one node at a time.
func ValidateRolloutMode(m machineapi.ApplyConfigurationRequest_Mode) error {
	if m == machineapi.ApplyConfigurationRequest_TRY {
		return errors.New("'try' mode rolls back automatically and can't be used for a rolling apply")
	}

	return nil
}

// PatchOps is what a rolling patch needs from the cluster (faked in tests).
type PatchOps interface {
	ApplyPatch(ctx context.Context, ip, patch string, mode machineapi.ApplyConfigurationRequest_Mode, dryRun bool) PatchResult
	BootTime(ctx context.Context, ip string) (int64, error)
	WaitHealthy(ctx context.Context, n Node, log Logger) error
}

// RolloutOptions tunes a rolling patch.
type RolloutOptions struct {
	ContinueOnError bool
	PollInterval    time.Duration
	RebootTimeout   time.Duration
	Settle          time.Duration
}

// DefaultRolloutOptions are used for real clusters.
var DefaultRolloutOptions = RolloutOptions{
	PollInterval:  10 * time.Second,
	RebootTimeout: 20 * time.Minute,
	Settle:        15 * time.Second,
}

// RollingPatch dry-runs the patch on every target first; if all succeed it
// applies it one node at a time, waiting for reboots and node health between
// nodes.
func RollingPatch(ctx context.Context, ops PatchOps, targets []Node, patch string, mode machineapi.ApplyConfigurationRequest_Mode, opts RolloutOptions, log Logger) error {
	if err := ValidateRolloutMode(mode); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	log("dry run on %d node(s)", len(targets))

	var dryErrs []string

	for _, n := range targets {
		res := ops.ApplyPatch(ctx, n.IP, patch, mode, true)
		if !res.OK {
			dryErrs = append(dryErrs, fmt.Sprintf("%s: %s", n.Name, res.Error))
		}
	}

	if len(dryErrs) > 0 {
		for _, e := range dryErrs {
			log("✗ dry run failed on %s", e)
		}

		return fmt.Errorf("dry run failed, nothing was applied: %s", strings.Join(dryErrs, "; "))
	}

	log("dry run OK on all nodes")

	failed := 0

	for i, n := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}

		log("━━ [%d/%d] %s (%s, %s)", i+1, len(targets), n.Name, n.IP, n.Role)

		if err := patchOne(ctx, ops, n, patch, mode, opts, log); err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}

			failed++

			log("✗ %s: %v", n.Name, err)

			if !opts.ContinueOnError {
				return fmt.Errorf("%s failed, stopping rollout: %w", n.Name, err)
			}

			continue
		}

		log("✓ %s done", n.Name)
	}

	if failed > 0 {
		return fmt.Errorf("%d node(s) failed", failed)
	}

	return nil
}

func patchOne(ctx context.Context, ops PatchOps, n Node, patch string, mode machineapi.ApplyConfigurationRequest_Mode, opts RolloutOptions, log Logger) error {
	boot0, err := ops.BootTime(ctx, n.IP)
	if err != nil {
		return fmt.Errorf("reading boot time: %w", err)
	}

	res := ops.ApplyPatch(ctx, n.IP, patch, mode, false)
	if !res.OK {
		return errors.New(res.Error)
	}

	for _, w := range res.Warnings {
		log("  warning: %s", w)
	}

	log("applied (mode %s)", res.Mode)

	if res.Mode == machineapi.ApplyConfigurationRequest_REBOOT.String() {
		log("node is rebooting to apply the change, waiting for it to come back")

		if err := waitFor(ctx, opts.RebootTimeout, opts.PollInterval, func() (bool, string) {
			bt, err := ops.BootTime(ctx, n.IP)
			if err != nil {
				return false, "node offline"
			}

			if bt > boot0+30 {
				return true, ""
			}

			return false, "waiting for reboot"
		}, log); err != nil {
			return fmt.Errorf("waiting for reboot: %w", err)
		}

		log("node rebooted")
	}

	if err := ops.WaitHealthy(ctx, n, log); err != nil {
		return err
	}

	if opts.Settle > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(opts.Settle):
		}
	}

	return nil
}
