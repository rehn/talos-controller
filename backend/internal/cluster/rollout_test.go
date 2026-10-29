package cluster

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
)

var testNodes = []Node{
	{Name: "w2", IP: "10.0.0.22", Role: RoleWorker, Reachable: true},
	{Name: "cp1", IP: "10.0.0.1", Role: RoleControlPlane, Reachable: true},
	{Name: "w1", IP: "10.0.0.21", Role: RoleWorker, Reachable: true},
	{Name: "w3", IP: "10.0.0.23", Role: RoleWorker, Reachable: false},
}

func names(ns []Node) []string {
	out := []string{}
	for _, n := range ns {
		out = append(out, n.Name)
	}

	return out
}

func TestSelectTargetsByRole(t *testing.T) {
	got, skipped, err := SelectTargets(testNodes, Selector{Role: "worker"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(names(got), []string{"w1", "w2"}) {
		t.Errorf("targets = %v", names(got))
	}

	if !reflect.DeepEqual(skipped, []string{"w3"}) {
		t.Errorf("skipped = %v", skipped)
	}
}

func TestSelectTargetsAllOrdersControlPlanesFirst(t *testing.T) {
	got, _, err := SelectTargets(testNodes, Selector{Role: "all"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(names(got), []string{"cp1", "w1", "w2"}) {
		t.Errorf("targets = %v", names(got))
	}
}

func TestSelectTargetsByName(t *testing.T) {
	got, _, err := SelectTargets(testNodes, Selector{Nodes: []string{"w2", "cp1"}})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(names(got), []string{"cp1", "w2"}) {
		t.Errorf("targets = %v", names(got))
	}
}

func TestSelectTargetsErrors(t *testing.T) {
	for name, sel := range map[string]Selector{
		"empty":          {},
		"unknown role":   {Role: "gpu"},
		"unknown node":   {Nodes: []string{"nope"}},
		"both":           {Role: "worker", Nodes: []string{"w1"}},
		"none reachable": {Nodes: []string{"w3"}},
	} {
		if _, _, err := SelectTargets(testNodes, sel); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestValidateRolloutMode(t *testing.T) {
	if err := ValidateRolloutMode(machineapi.ApplyConfigurationRequest_TRY); err == nil {
		t.Error("try mode must be rejected for rollouts")
	}

	for _, m := range []machineapi.ApplyConfigurationRequest_Mode{
		machineapi.ApplyConfigurationRequest_AUTO,
		machineapi.ApplyConfigurationRequest_NO_REBOOT,
		machineapi.ApplyConfigurationRequest_STAGED,
		machineapi.ApplyConfigurationRequest_REBOOT,
	} {
		if err := ValidateRolloutMode(m); err != nil {
			t.Errorf("%s: %v", m, err)
		}
	}
}

// fakeOps records calls and simulates reboots.
type fakeOps struct {
	mu       sync.Mutex
	calls    []string
	resMode  map[string]string // node IP -> mode returned on real apply
	dryFail  map[string]bool
	applyErr map[string]bool
	boot     map[string]int64
	// rebootAfter: number of BootTime polls after an apply before boot time changes (-1 = never)
	rebootAfter int
	polls       map[string]int
	healthErr   map[string]bool
}

func newFake() *fakeOps {
	return &fakeOps{
		resMode:     map[string]string{},
		dryFail:     map[string]bool{},
		applyErr:    map[string]bool{},
		boot:        map[string]int64{},
		polls:       map[string]int{},
		healthErr:   map[string]bool{},
		rebootAfter: 2,
	}
}

func (f *fakeOps) record(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
}

func (f *fakeOps) ApplyPatch(_ context.Context, ip, _ string, _ machineapi.ApplyConfigurationRequest_Mode, dryRun bool) PatchResult {
	if dryRun {
		f.record("dry:" + ip)

		if f.dryFail[ip] {
			return PatchResult{Node: ip, Error: "invalid config"}
		}

		return PatchResult{Node: ip, OK: true, DryRun: true}
	}

	f.record("apply:" + ip)

	if f.applyErr[ip] {
		return PatchResult{Node: ip, Error: "boom"}
	}

	mode := f.resMode[ip]
	if mode == "" {
		mode = "NO_REBOOT"
	}

	f.mu.Lock()
	f.polls[ip] = 0
	f.mu.Unlock()

	return PatchResult{Node: ip, OK: true, Mode: mode}
}

func (f *fakeOps) BootTime(_ context.Context, ip string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.boot[ip] == 0 {
		f.boot[ip] = 1000
	}

	if f.resMode[ip] == "REBOOT" && f.rebootAfter >= 0 {
		f.polls[ip]++
		if f.polls[ip] > f.rebootAfter {
			return f.boot[ip] + 600, nil
		}
	}

	return f.boot[ip], nil
}

func (f *fakeOps) WaitHealthy(_ context.Context, n Node, _ Logger) error {
	f.record("health:" + n.IP)

	if f.healthErr[n.IP] {
		return errors.New("NotReady")
	}

	return nil
}

var fastOpts = RolloutOptions{PollInterval: time.Millisecond, RebootTimeout: 200 * time.Millisecond, Settle: 0}

func nopLog(string, ...any) {}

func targets(t *testing.T) []Node {
	t.Helper()

	ts, _, err := SelectTargets(testNodes, Selector{Role: "all"})
	if err != nil {
		t.Fatal(err)
	}

	return ts
}

func TestRollingPatchDryRunsEverythingFirst(t *testing.T) {
	f := newFake()

	err := RollingPatch(context.Background(), f, targets(t), "p", machineapi.ApplyConfigurationRequest_AUTO, fastOpts, nopLog)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"dry:10.0.0.1", "dry:10.0.0.21", "dry:10.0.0.22",
		"apply:10.0.0.1", "health:10.0.0.1",
		"apply:10.0.0.21", "health:10.0.0.21",
		"apply:10.0.0.22", "health:10.0.0.22",
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("calls:\n%v\nwant:\n%v", f.calls, want)
	}
}

func TestRollingPatchAbortsWhenAnyDryRunFails(t *testing.T) {
	f := newFake()
	f.dryFail["10.0.0.22"] = true

	err := RollingPatch(context.Background(), f, targets(t), "p", machineapi.ApplyConfigurationRequest_AUTO, fastOpts, nopLog)
	if err == nil || !strings.Contains(err.Error(), "w2") {
		t.Fatalf("expected dry-run error naming w2, got %v", err)
	}

	for _, c := range f.calls {
		if strings.HasPrefix(c, "apply:") {
			t.Fatalf("nothing should be applied after a failed dry run: %v", f.calls)
		}
	}
}

func TestRollingPatchWaitsForReboot(t *testing.T) {
	f := newFake()
	f.resMode["10.0.0.1"] = "REBOOT"

	var logs []string
	logf := func(format string, a ...any) { logs = append(logs, fmt.Sprintf(format, a...)) }

	err := RollingPatch(context.Background(), f, targets(t)[:1], "p", machineapi.ApplyConfigurationRequest_AUTO, fastOpts, logf)
	if err != nil {
		t.Fatal(err)
	}

	if f.polls["10.0.0.1"] <= f.rebootAfter {
		t.Fatalf("expected to poll boot time until the reboot was seen, polls=%d", f.polls["10.0.0.1"])
	}

	if !strings.Contains(strings.Join(logs, "\n"), "reboot") {
		t.Errorf("expected a log line about the reboot, got %v", logs)
	}

	if f.calls[len(f.calls)-1] != "health:10.0.0.1" {
		t.Errorf("health check should follow the reboot: %v", f.calls)
	}
}

func TestRollingPatchRebootTimeout(t *testing.T) {
	f := newFake()
	f.resMode["10.0.0.1"] = "REBOOT"
	f.rebootAfter = -1

	err := RollingPatch(context.Background(), f, targets(t), "p", machineapi.ApplyConfigurationRequest_AUTO, fastOpts, nopLog)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout, got %v", err)
	}

	for _, c := range f.calls {
		if c == "apply:10.0.0.21" {
			t.Fatal("rollout must stop after a failed node")
		}
	}
}

func TestRollingPatchStopsOnApplyError(t *testing.T) {
	f := newFake()
	f.applyErr["10.0.0.21"] = true

	err := RollingPatch(context.Background(), f, targets(t), "p", machineapi.ApplyConfigurationRequest_AUTO, fastOpts, nopLog)
	if err == nil {
		t.Fatal("expected error")
	}

	for _, c := range f.calls {
		if c == "apply:10.0.0.22" {
			t.Fatalf("w2 should not be touched after w1 failed: %v", f.calls)
		}
	}
}

func TestRollingPatchContinueOnError(t *testing.T) {
	f := newFake()
	f.applyErr["10.0.0.21"] = true
	f.healthErr["10.0.0.1"] = true

	opts := fastOpts
	opts.ContinueOnError = true

	err := RollingPatch(context.Background(), f, targets(t), "p", machineapi.ApplyConfigurationRequest_AUTO, opts, nopLog)
	if err == nil || !strings.Contains(err.Error(), "2 node(s) failed") {
		t.Fatalf("expected aggregated error, got %v", err)
	}

	applied := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, "apply:") {
			applied++
		}
	}

	if applied != 3 {
		t.Fatalf("all 3 nodes should be attempted, calls=%v", f.calls)
	}
}

func TestRollingPatchCancelled(t *testing.T) {
	f := newFake()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := RollingPatch(ctx, f, targets(t), "p", machineapi.ApplyConfigurationRequest_AUTO, fastOpts, nopLog)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	for _, c := range f.calls {
		if strings.HasPrefix(c, "apply:") {
			t.Fatalf("nothing should be applied when cancelled: %v", f.calls)
		}
	}
}

// The real cluster client must satisfy PatchOps.
var _ PatchOps = (*Cluster)(nil)
