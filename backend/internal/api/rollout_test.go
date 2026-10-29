package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"

	"github.com/lars/talos-controller/internal/jobs"
	"github.com/lars/talos-controller/internal/releases"
	"github.com/lars/talos-controller/internal/settings"
)

const validPatch = "machine:\n  sysctls:\n    vm.max_map_count: \"262144\"\n"

func TestParseRolloutRequest(t *testing.T) {
	req, err := parseRolloutRequest(rolloutRequest{Role: "worker", Patch: validPatch, Mode: "auto"})
	if err != nil {
		t.Fatal(err)
	}

	if req.selector.Role != "worker" || req.mode != machineapi.ApplyConfigurationRequest_AUTO {
		t.Fatalf("got %+v", req)
	}

	req, err = parseRolloutRequest(rolloutRequest{Nodes: []string{"w1"}, Patch: validPatch, Mode: "staged", ContinueOnError: true})
	if err != nil || req.selector.Nodes[0] != "w1" || req.mode != machineapi.ApplyConfigurationRequest_STAGED || !req.opts.ContinueOnError {
		t.Fatalf("got %+v, %v", req, err)
	}

	for name, bad := range map[string]rolloutRequest{
		"no target": {Patch: validPatch},
		"try mode":  {Role: "worker", Patch: validPatch, Mode: "try"},
		"bad mode":  {Role: "worker", Patch: validPatch, Mode: "yolo"},
		"empty":     {Role: "worker", Patch: "  "},
		"file ref":  {Role: "worker", Patch: "@/etc/passwd"},
		"bad yaml":  {Role: "worker", Patch: "machine: ["},
		"both":      {Role: "worker", Nodes: []string{"w1"}, Patch: validPatch},
	} {
		if _, err := parseRolloutRequest(bad); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func unconfiguredServer(t *testing.T) *Server {
	t.Helper()

	dir := t.TempDir()

	return New(context.Background(), settings.New(dir, "", ""), nil, jobs.NewManager(""), releases.New(""), fstest.MapFS{})
}

func TestRolloutEndpointValidatesBeforeConnecting(t *testing.T) {
	s := unconfiguredServer(t)

	rec := httptest.NewRecorder()
	s.handleRollout(rec, httptest.NewRequest(http.MethodPost, "/api/config/rollout",
		strings.NewReader(`{"role":"worker","patch":"x: 1","mode":"try"}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
}

func TestRolloutAndDriftNeedConfiguration(t *testing.T) {
	s := unconfiguredServer(t)

	rec := httptest.NewRecorder()
	s.handleRollout(rec, httptest.NewRequest(http.MethodPost, "/api/config/rollout",
		strings.NewReader(`{"role":"worker","patch":"machine:\n  sysctls:\n    a: \"1\"\n","mode":"auto"}`)))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("rollout status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	s.handleDrift(rec, httptest.NewRequest(http.MethodGet, "/api/drift", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("drift status = %d", rec.Code)
	}

	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)

	if body["code"] != "not_configured" {
		t.Fatalf("body = %v", body)
	}
}
