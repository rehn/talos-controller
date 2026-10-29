package releases

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.14.5", "v1.14.4", 1},
		{"v1.9.0", "v1.10.0", -1},
		{"v1.15.0-alpha.1", "v1.15.0", -1},
		{"v1.15.0-beta.0", "v1.15.0-alpha.2", 1},
		{"v1.14.5", "1.14.5", 0},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%s,%s)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestK8sInNotes(t *testing.T) {
	m := k8sInNotes.FindStringSubmatch("### Component Updates\n\n* Linux: 6.12.40\n* Kubernetes: 1.35.1\n")
	if m == nil || m[1] != "1.35.1" {
		t.Fatalf("got %v", m)
	}
}
