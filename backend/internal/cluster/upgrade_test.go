package cluster

import "testing"

func TestImageTag(t *testing.T) {
	cases := map[string]string{
		"ghcr.io/siderolabs/installer:v1.14.5":         "v1.14.5",
		"factory.talos.dev/installer/abc123:v1.13.4":   "v1.13.4",
		"registry.local:5000/talos/installer:v1.12.0":  "v1.12.0",
		"registry.local:5000/talos/installer":          "",
		"ghcr.io/siderolabs/installer@sha256:deadbeef": "",
	}
	for in, want := range cases {
		if got := ImageTag(in); got != want {
			t.Errorf("ImageTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReplaceTag(t *testing.T) {
	cases := map[string]string{
		"factory.talos.dev/installer/abc123:v1.13.4":   "factory.talos.dev/installer/abc123:v1.14.5",
		"registry.local:5000/talos/installer":          "registry.local:5000/talos/installer:v1.14.5",
		"ghcr.io/siderolabs/installer@sha256:deadbeef": "ghcr.io/siderolabs/installer:v1.14.5",
	}
	for in, want := range cases {
		if got := ReplaceTag(in, "v1.14.5"); got != want {
			t.Errorf("ReplaceTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidatePatch(t *testing.T) {
	if err := ValidatePatch("machine:\n  time:\n    servers: [pool.ntp.org]\n"); err != nil {
		t.Fatalf("valid strategic patch rejected: %v", err)
	}
	if err := ValidatePatch("- op: add\n  path: /machine/network/hostname\n  value: foo\n"); err != nil {
		t.Fatalf("valid json6902 patch rejected: %v", err)
	}
	if err := ValidatePatch("@/etc/passwd"); err == nil {
		t.Fatal("file reference accepted")
	}
	if err := ValidatePatch("   "); err == nil {
		t.Fatal("empty patch accepted")
	}
}

func TestParseApplyMode(t *testing.T) {
	for _, m := range []string{"", "auto", "no-reboot", "staged", "try", "reboot"} {
		if _, err := ParseApplyMode(m); err != nil {
			t.Errorf("mode %q: %v", m, err)
		}
	}
	if _, err := ParseApplyMode("yolo"); err == nil {
		t.Error("unknown mode accepted")
	}
}
