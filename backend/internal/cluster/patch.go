package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"google.golang.org/protobuf/types/known/durationpb"
)

// PatchResult is the outcome of applying (or dry-running) a patch on a node.
type PatchResult struct {
	Node     string   `json:"node"`
	OK       bool     `json:"ok"`
	DryRun   bool     `json:"dryRun"`
	Mode     string   `json:"mode"`
	Details  string   `json:"details"`
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// ParseApplyMode converts a UI mode string into the API enum.
func ParseApplyMode(s string) (machineapi.ApplyConfigurationRequest_Mode, error) {
	switch strings.ToLower(s) {
	case "", "auto":
		return machineapi.ApplyConfigurationRequest_AUTO, nil
	case "no-reboot", "no_reboot":
		return machineapi.ApplyConfigurationRequest_NO_REBOOT, nil
	case "reboot":
		return machineapi.ApplyConfigurationRequest_REBOOT, nil
	case "staged":
		return machineapi.ApplyConfigurationRequest_STAGED, nil
	case "try":
		return machineapi.ApplyConfigurationRequest_TRY, nil
	}

	return 0, fmt.Errorf("unknown apply mode %q", s)
}

// ValidatePatch checks that a patch parses.
func ValidatePatch(patch string) error {
	if strings.TrimSpace(patch) == "" {
		return fmt.Errorf("patch is empty")
	}

	if strings.HasPrefix(strings.TrimSpace(patch), "@") {
		return fmt.Errorf("file references (@file) are not supported, paste the patch content")
	}

	_, err := configpatcher.LoadPatch([]byte(patch))

	return err
}

// ApplyPatch patches the node's active machine config and applies it.
func (c *Cluster) ApplyPatch(ctx context.Context, node, patch string, mode machineapi.ApplyConfigurationRequest_Mode, dryRun bool) PatchResult {
	res := PatchResult{Node: node, DryRun: dryRun, Mode: mode.String()}

	cctx, cancel := nodeCtx(ctx, node, 45*time.Second)
	defer cancel()

	mc, err := safe.StateGetByID[*config.MachineConfig](cctx, c.talos.COSI, config.ActiveID)
	if err != nil {
		res.Error = "reading machine config: " + shortErr(err)

		return res
	}

	current, err := mc.Provider().EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		res.Error = "encoding machine config: " + err.Error()

		return res
	}

	p, err := configpatcher.LoadPatch([]byte(patch))
	if err != nil {
		res.Error = "invalid patch: " + err.Error()

		return res
	}

	out, err := configpatcher.Apply(configpatcher.WithBytes(current), []configpatcher.Patch{p})
	if err != nil {
		res.Error = "applying patch: " + err.Error()

		return res
	}

	patched, err := out.Bytes()
	if err != nil {
		res.Error = "encoding patched config: " + err.Error()

		return res
	}

	req := &machineapi.ApplyConfigurationRequest{Data: patched, Mode: mode, DryRun: dryRun}
	if mode == machineapi.ApplyConfigurationRequest_TRY {
		req.TryModeTimeout = durationpb.New(time.Minute)
	}

	resp, err := c.talos.ApplyConfiguration(cctx, req)
	if err != nil {
		res.Error = shortErr(err)

		return res
	}

	res.OK = true

	if len(resp.Messages) > 0 {
		res.Details = resp.Messages[0].ModeDetails
		res.Warnings = resp.Messages[0].Warnings
		res.Mode = resp.Messages[0].Mode.String()
	}

	return res
}
