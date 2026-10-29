package drift

import (
	"reflect"
	"strings"
	"testing"
)

func mustFlatten(t *testing.T, y string) Values {
	t.Helper()

	v, err := Flatten([]byte(y))
	if err != nil {
		t.Fatalf("Flatten: %v", err)
	}

	return v
}

func TestFlattenV1Alpha1(t *testing.T) {
	v := mustFlatten(t, `
version: v1alpha1
machine:
  type: worker
  sysctls:
    vm.max_map_count: "262144"
  network:
    nameservers: [1.1.1.1, 9.9.9.9]
    interfaces:
      - interface: eth0
        dhcp: true
  nodeLabels:
    topology.kubernetes.io/zone: home
  disks: []
  kubelet: {}
`)

	want := Values{
		"/version":                                         `"v1alpha1"`,
		"/machine/type":                                    `"worker"`,
		"/machine/sysctls/vm.max_map_count":                `"262144"`,
		"/machine/network/nameservers":                     `["1.1.1.1","9.9.9.9"]`,
		"/machine/network/interfaces/0/interface":          `"eth0"`,
		"/machine/network/interfaces/0/dhcp":               `true`,
		"/machine/nodeLabels/topology.kubernetes.io~1zone": `"home"`,
		"/machine/disks":                                   `[]`,
		"/machine/kubelet":                                 `{}`,
	}

	if !reflect.DeepEqual(v, want) {
		t.Fatalf("got\n%v\nwant\n%v", v, want)
	}
}

func TestFlattenMultiDocument(t *testing.T) {
	v := mustFlatten(t, `
version: v1alpha1
machine:
  type: worker
---
apiVersion: v1alpha1
kind: HostnameConfig
hostname: worker-1
---
apiVersion: v1alpha1
kind: LinkConfig
name: eth0
mtu: 9000
`)

	for path, want := range map[string]string{
		"/machine/type":             `"worker"`,
		"/#HostnameConfig/hostname": `"worker-1"`,
		"/#LinkConfig:eth0/mtu":     `9000`,
	} {
		if v[path] != want {
			t.Errorf("%s = %q, want %q (all: %v)", path, v[path], want, v)
		}
	}

	for path := range v {
		if strings.HasSuffix(path, "/kind") || strings.HasSuffix(path, "/apiVersion") || strings.HasSuffix(path, "/name") {
			t.Errorf("document metadata should not be flattened: %s", path)
		}
	}
}

func TestFlattenInvalid(t *testing.T) {
	if _, err := Flatten([]byte("machine: [unclosed")); err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func worker(t *testing.T, name, extra string) Node {
	t.Helper()

	return Node{Name: name, Role: "worker", Config: mustFlatten(t, `
machine:
  type: worker
  network:
    hostname: `+name+`
  sysctls:
    net.core.somaxconn: "1024"
`+extra)}
}

func TestCompareFindsOutlier(t *testing.T) {
	nodes := []Node{
		worker(t, "w1", ""),
		worker(t, "w2", "  kubelet:\n    extraArgs:\n      max-pods: \"250\"\n"),
		worker(t, "w3", ""),
		{Name: "cp1", Role: "controlplane", Config: mustFlatten(t, "machine:\n  type: controlplane\n")},
	}

	reports := Compare(nodes, DefaultIgnore)

	if len(reports) != 2 {
		t.Fatalf("expected 2 role reports, got %d", len(reports))
	}

	// Sorted: controlplane first.
	if reports[0].Role != "controlplane" || reports[1].Role != "worker" {
		t.Fatalf("unexpected order: %s, %s", reports[0].Role, reports[1].Role)
	}

	if len(reports[0].Diffs) != 0 {
		t.Errorf("single-node role should have no diffs, got %v", reports[0].Diffs)
	}

	w := reports[1]
	if !reflect.DeepEqual(w.Nodes, []string{"w1", "w2", "w3"}) {
		t.Errorf("nodes = %v", w.Nodes)
	}

	if len(w.Diffs) != 1 {
		t.Fatalf("expected exactly one diff (hostname is ignored), got %+v", w.Diffs)
	}

	d := w.Diffs[0]
	if d.Path != "/machine/kubelet/extraArgs/max-pods" {
		t.Errorf("path = %s", d.Path)
	}

	if d.Values["w1"] != Missing || d.Values["w2"] != `"250"` || d.Values["w3"] != Missing {
		t.Errorf("values = %v", d.Values)
	}

	if d.Majority != Missing || !reflect.DeepEqual(d.Outliers, []string{"w2"}) {
		t.Errorf("majority = %q outliers = %v", d.Majority, d.Outliers)
	}

	if d.Patchable {
		t.Error("removing a key cannot be expressed as a merge patch, should not be patchable")
	}

	// type + sysctl are identical (hostname ignored).
	if w.Identical != 2 {
		t.Errorf("identical = %d, want 2", w.Identical)
	}
}

func TestComparePatchableWhenMajorityHasValue(t *testing.T) {
	nodes := []Node{
		worker(t, "w1", "  kubelet:\n    extraArgs:\n      max-pods: \"250\"\n"),
		worker(t, "w2", "  kubelet:\n    extraArgs:\n      max-pods: \"250\"\n"),
		worker(t, "w3", ""),
	}

	d := Compare(nodes, DefaultIgnore)[0].Diffs[0]

	if d.Majority != `"250"` || !reflect.DeepEqual(d.Outliers, []string{"w3"}) || !d.Patchable {
		t.Fatalf("got %+v", d)
	}

	if !strings.Contains(d.Patch, "max-pods: \"250\"") {
		t.Fatalf("diff should carry a ready-to-use patch, got %q", d.Patch)
	}
}

func TestCompareTieHasNoMajority(t *testing.T) {
	nodes := []Node{
		worker(t, "w1", "  kubelet:\n    extraArgs:\n      max-pods: \"110\"\n"),
		worker(t, "w2", "  kubelet:\n    extraArgs:\n      max-pods: \"250\"\n"),
	}

	d := Compare(nodes, DefaultIgnore)[0].Diffs[0]

	if d.Majority != "" || d.Patchable || !reflect.DeepEqual(d.Outliers, []string{"w1", "w2"}) {
		t.Fatalf("got %+v", d)
	}
}

func TestCompareWithoutIgnoreShowsHostname(t *testing.T) {
	nodes := []Node{worker(t, "w1", ""), worker(t, "w2", "")}

	d := Compare(nodes, nil)[0].Diffs
	if len(d) != 1 || d[0].Path != "/machine/network/hostname" {
		t.Fatalf("got %+v", d)
	}
}

func TestIgnored(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/machine/network/hostname", true},
		{"/machine/network/interfaces/0/addresses", true},
		{"/machine/network/nameservers", false},
		{"/machine/install/disk", true},
		{"/machine/install/image", false},
		{"/#HostnameConfig/hostname", true},
		{"/#LinkConfig:eth0/addresses", true},
		{"/machine/sysctls/foo", false},
	}

	for _, c := range cases {
		if got := Ignored(c.path, DefaultIgnore); got != c.want {
			t.Errorf("Ignored(%s) = %v, want %v", c.path, got, c.want)
		}
	}

	if !Ignored("/a/b/c", []string{"/a/*/c"}) || Ignored("/a/b/d", []string{"/a/*/c"}) {
		t.Error("wildcard segment matching broken")
	}
}

func TestBuildPatch(t *testing.T) {
	p, err := BuildPatch("/machine/kubelet/extraArgs/max-pods", `"250"`)
	if err != nil {
		t.Fatal(err)
	}

	want := "machine:\n  kubelet:\n    extraArgs:\n      max-pods: \"250\"\n"
	if p != want {
		t.Fatalf("got\n%s\nwant\n%s", p, want)
	}

	p, err = BuildPatch("/machine/nodeLabels/topology.kubernetes.io~1zone", `"home"`)
	if err != nil || !strings.Contains(p, "topology.kubernetes.io/zone: home") {
		t.Fatalf("escaped key not decoded: %q %v", p, err)
	}

	p, err = BuildPatch("/machine/network/nameservers", `["1.1.1.1","9.9.9.9"]`)
	if err != nil || !strings.Contains(p, "- 1.1.1.1") {
		t.Fatalf("list value: %q %v", p, err)
	}

	for _, bad := range []string{"/machine/network/interfaces/0/dhcp", "/#LinkConfig:eth0/mtu"} {
		if _, err := BuildPatch(bad, `true`); err == nil {
			t.Errorf("BuildPatch(%s) should fail", bad)
		}
	}

	if _, err := BuildPatch("/machine/foo", Missing); err == nil {
		t.Error("BuildPatch with Missing value should fail")
	}
}
