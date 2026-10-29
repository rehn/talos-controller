package settings

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = `context: home
contexts:
  home:
    endpoints: []
    ca: Y2E=
    crt: Y3J0
    key: a2V5
  lab:
    endpoints: [10.0.0.1]
    nodes: [10.0.0.2]
    ca: Y2E=
    crt: Y3J0
    key: a2V5
`

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, filepath.Join(dir, "missing"), "")

	if p, src := s.TalosconfigPath(); p != "" || src != SourceNone {
		t.Fatalf("expected none, got %q %q", p, src)
	}

	if _, err := s.SaveTalosconfig("not yaml: ["); err == nil {
		t.Fatal("invalid config accepted")
	}

	cfg, err := s.SaveTalosconfig(sample)
	if err != nil {
		t.Fatal(err)
	}

	if ctxs := Contexts(cfg); len(ctxs) != 2 || ctxs[0].Name != "home" || ctxs[1].Endpoints[0] != "10.0.0.1" {
		t.Fatalf("contexts: %+v", ctxs)
	}

	if _, src := s.TalosconfigPath(); src != SourceUI {
		t.Fatalf("source = %s", src)
	}

	fi, _ := os.Stat(filepath.Join(dir, "talosconfig"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("talosconfig mode %v", fi.Mode().Perm())
	}

	p, err := s.SavePrefs(Prefs{Context: "home", Endpoints: []string{"192.168.1.10, 192.168.1.11:50000", "192.168.1.10"}})
	if err != nil {
		t.Fatal(err)
	}

	if len(p.Endpoints) != 2 || s.Prefs().Context != "home" {
		t.Fatalf("prefs: %+v", s.Prefs())
	}

	if _, err := s.SavePrefs(Prefs{Endpoints: []string{"https://x/y"}}); err == nil {
		t.Fatal("bad endpoint accepted")
	}

	if err := s.DeleteTalosconfig(); err != nil {
		t.Fatal(err)
	}

	if _, src := s.TalosconfigPath(); src != SourceNone {
		t.Fatal("delete did not remove config")
	}
}

func TestEnvFileFallbackIgnoresDirsAndEmpty(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, "talosconfig-dir")
	_ = os.Mkdir(d, 0o700)

	if _, src := New(dir, d, "").TalosconfigPath(); src != SourceNone {
		t.Fatal("directory treated as talosconfig")
	}

	f := filepath.Join(dir, "tc")
	_ = os.WriteFile(f, []byte(sample), 0o600)

	if p, src := New(filepath.Join(dir, "data"), f, "").TalosconfigPath(); src != SourceFile || p != f {
		t.Fatalf("got %q %q", p, src)
	}
}
