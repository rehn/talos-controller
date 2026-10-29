package api

import (
	"testing"

	"github.com/lars/talos-controller/internal/releases"
)

func TestRecommendNext(t *testing.T) {
	rs := []releases.Release{
		{Version: "v1.15.0-alpha.1", Prerelease: true},
		{Version: "v1.14.5"}, {Version: "v1.14.4"},
		{Version: "v1.13.12"}, {Version: "v1.13.11"},
		{Version: "v1.12.9"},
	}
	cases := map[string]string{
		"v1.12.3": "v1.13.12", // one minor at a time
		"v1.13.4": "v1.14.5",
		"v1.14.2": "v1.14.5",
		"":        "v1.14.5",
	}
	for cur, want := range cases {
		if got := recommendNext(cur, rs); got == nil || got.Version != want {
			t.Errorf("recommendNext(%q) = %v, want %s", cur, got, want)
		}
	}
}
