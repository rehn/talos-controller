// Package releases checks GitHub for new Talos and Kubernetes releases.
package releases

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Release is a published release.
type Release struct {
	Version     string    `json:"version"`
	PublishedAt time.Time `json:"publishedAt"`
	URL         string    `json:"url"`
	Prerelease  bool      `json:"prerelease"`
	// DefaultKubernetes is the Kubernetes version bundled with a Talos release, if found in the notes.
	DefaultKubernetes string `json:"defaultKubernetes,omitempty"`
}

// Info is the cached result.
type Info struct {
	Talos      []Release `json:"talos"`
	Kubernetes []Release `json:"kubernetes"`
	FetchedAt  time.Time `json:"fetchedAt"`
	Error      string    `json:"error,omitempty"`
}

// Checker fetches and caches release info.
type Checker struct {
	token string
	http  *http.Client

	mu    sync.Mutex
	cache *Info
}

// New creates a Checker.
func New(token string) *Checker {
	return &Checker{token: token, http: &http.Client{Timeout: 15 * time.Second}}
}

// Get returns cached release info, refreshing it at most hourly.
func (c *Checker) Get(ctx context.Context, force bool) *Info {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cache != nil && !force && time.Since(c.cache.FetchedAt) < time.Hour && c.cache.Error == "" {
		return c.cache
	}

	if c.cache != nil && c.cache.Error != "" && !force && time.Since(c.cache.FetchedAt) < 5*time.Minute {
		return c.cache
	}

	info := &Info{FetchedAt: time.Now()}

	talos, err := c.fetch(ctx, "siderolabs/talos")
	if err != nil {
		info.Error = err.Error()
	}

	info.Talos = talos

	k8s, err := c.fetch(ctx, "kubernetes/kubernetes")
	if err != nil && info.Error == "" {
		info.Error = err.Error()
	}

	info.Kubernetes = k8s

	if info.Error != "" && c.cache != nil && len(c.cache.Talos) > 0 {
		// Keep serving stale data but surface the error.
		stale := *c.cache
		stale.Error = info.Error
		stale.FetchedAt = info.FetchedAt
		c.cache = &stale

		return c.cache
	}

	c.cache = info

	return info
}

var k8sInNotes = regexp.MustCompile(`(?i)kubernetes:?\s*v?(1\.\d+\.\d+)`)

func (c *Checker) fetch(ctx context.Context, repo string) ([]Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases?per_page=40", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github %s: %w", repo, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github %s: HTTP %d", repo, resp.StatusCode)
	}

	var raw []struct {
		TagName     string    `json:"tag_name"`
		HTMLURL     string    `json:"html_url"`
		Prerelease  bool      `json:"prerelease"`
		Draft       bool      `json:"draft"`
		PublishedAt time.Time `json:"published_at"`
		Body        string    `json:"body"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]Release, 0, len(raw))

	for _, r := range raw {
		if r.Draft {
			continue
		}

		rel := Release{Version: r.TagName, PublishedAt: r.PublishedAt, URL: r.HTMLURL, Prerelease: r.Prerelease || strings.Contains(r.TagName, "-")}

		if repo == "siderolabs/talos" {
			if m := k8sInNotes.FindStringSubmatch(r.Body); m != nil {
				rel.DefaultKubernetes = "v" + m[1]
			}
		}

		out = append(out, rel)
	}

	sort.Slice(out, func(i, j int) bool { return Compare(out[i].Version, out[j].Version) > 0 })

	return out, nil
}

// Latest returns the newest stable release, or nil.
func Latest(rs []Release) *Release {
	for i := range rs {
		if !rs[i].Prerelease {
			return &rs[i]
		}
	}

	return nil
}

// Compare compares two semver-ish versions ("v1.2.3", "v1.2.3-beta.1").
func Compare(a, b string) int {
	pa, pre1 := parse(a)
	pb, pre2 := parse(b)

	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}

			return 1
		}
	}

	switch {
	case pre1 == pre2:
		return 0
	case pre1 == "":
		return 1
	case pre2 == "":
		return -1
	case pre1 < pre2:
		return -1
	default:
		return 1
	}
}

func parse(v string) ([3]int, string) {
	v = strings.TrimPrefix(v, "v")
	core, pre, _ := strings.Cut(v, "-")

	var out [3]int

	for i, p := range strings.SplitN(core, ".", 3) {
		n, _ := strconv.Atoi(p)
		out[i] = n
	}

	return out, pre
}
