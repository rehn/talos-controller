// Package drift compares machine configs of nodes that share a role and
// reports settings that differ ("configuration drift").
//
// Configs are flattened into JSON-pointer-like paths ("/machine/sysctls/x")
// mapped to the JSON encoding of the leaf value. Documents other than the
// v1alpha1 config are prefixed with "/#Kind" or "/#Kind:name".
package drift

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"
	sigsyaml "sigs.k8s.io/yaml"
)

// Missing marks a path that a node does not have.
const Missing = "<missing>"

// Values maps a path to the JSON encoding of its leaf value.
type Values map[string]string

// Node is one node's flattened config.
type Node struct {
	Name   string
	Role   string
	Config Values
}

// Diff is a path whose value is not the same on all nodes of a role.
type Diff struct {
	Path string `json:"path"`
	// Values per node name (Missing if the node doesn't set it).
	Values map[string]string `json:"values"`
	// Majority is the most common value ("" on a tie).
	Majority string `json:"majority"`
	// Outliers are the nodes whose value differs from the majority (all nodes on a tie).
	Outliers []string `json:"outliers"`
	// Patchable is true when a merge patch can set the outliers to the majority value.
	Patchable bool `json:"patchable"`
	// Patch is a strategic merge patch that applies the majority value (when Patchable).
	Patch string `json:"patch,omitempty"`
}

// RoleReport is the drift report for one role.
type RoleReport struct {
	Role      string   `json:"role"`
	Nodes     []string `json:"nodes"`
	Identical int      `json:"identical"`
	Diffs     []Diff   `json:"diffs"`
}

// DefaultIgnore lists settings that are expected to differ per node.
// A pattern matches the path itself and everything below it; "*" matches
// within one path segment.
var DefaultIgnore = []string{
	"/machine/network/hostname",
	"/machine/network/interfaces",
	"/machine/network/extraHostEntries",
	"/machine/install/disk",
	"/machine/install/diskSelector",
	"/machine/certSANs",
	"/machine/kubelet/nodeIP",
	"/#HostnameConfig",
	"/#LinkConfig*",
	"/#LinkAliasConfig*",
	"/#BondConfig*",
	"/#VLANConfig*",
	"/#BridgeConfig*",
	"/#DHCPv4Config*",
	"/#DHCPv6Config*",
	"/#Layer2VIPConfig*",
	"/#StaticHostConfig*",
	"/#UserVolumeConfig*",
	"/#RawVolumeConfig*",
	"/#ExistingVolumeConfig*",
	"/#VolumeConfig*",
}

// Flatten parses a (multi-document) machine config.
func Flatten(data []byte) (Values, error) {
	out := Values{}
	dec := yaml.NewDecoder(bytes.NewReader(data))

	for {
		var doc any

		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("parsing config: %w", err)
		}

		if doc == nil {
			continue
		}

		prefix := ""

		if m, ok := doc.(map[string]any); ok {
			if kind, ok := m["kind"].(string); ok && kind != "" {
				prefix = "/" + escape("#"+kind)
				if name, ok := m["name"].(string); ok && name != "" {
					prefix = "/" + escape("#"+kind+":"+name)
				}

				clean := make(map[string]any, len(m))
				for k, v := range m {
					if k != "kind" && k != "apiVersion" && k != "name" {
						clean[k] = v
					}
				}

				doc = clean
			}
		}

		if err := flatten(prefix, doc, out); err != nil {
			return nil, err
		}
	}

	return out, nil
}

func flatten(prefix string, v any, out Values) error {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			out[prefix] = "{}"

			return nil
		}

		for k, child := range t {
			if err := flatten(prefix+"/"+escape(k), child, out); err != nil {
				return err
			}
		}

		return nil
	case []any:
		if allScalar(t) {
			return leaf(prefix, t, out)
		}

		for i, child := range t {
			if err := flatten(prefix+"/"+strconv.Itoa(i), child, out); err != nil {
				return err
			}
		}

		return nil
	default:
		return leaf(prefix, t, out)
	}
}

func allScalar(l []any) bool {
	for _, v := range l {
		switch v.(type) {
		case map[string]any, []any:
			return false
		}
	}

	return true
}

func leaf(p string, v any, out Values) error {
	if p == "" {
		p = "/"
	}

	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", p, err)
	}

	out[p] = string(b)

	return nil
}

// escape/unescape implement JSON pointer escaping for one segment.
func escape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

func unescape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
}

func segments(p string) []string {
	return strings.Split(strings.TrimPrefix(p, "/"), "/")
}

// Ignored reports whether path matches one of the patterns (or lies below it).
func Ignored(p string, patterns []string) bool {
	ps := segments(p)

	for _, pat := range patterns {
		qs := segments(pat)
		if len(qs) > len(ps) {
			continue
		}

		match := true

		for i, q := range qs {
			ok, err := path.Match(q, ps[i])
			if err != nil || !ok {
				match = false

				break
			}
		}

		if match {
			return true
		}
	}

	return false
}

// Compare groups nodes by role and reports differing paths within each role.
func Compare(nodes []Node, ignore []string) []RoleReport {
	byRole := map[string][]Node{}
	for _, n := range nodes {
		byRole[n.Role] = append(byRole[n.Role], n)
	}

	roles := make([]string, 0, len(byRole))
	for r := range byRole {
		roles = append(roles, r)
	}

	sort.Slice(roles, func(i, j int) bool {
		// control planes first, then alphabetical
		if (roles[i] == "controlplane") != (roles[j] == "controlplane") {
			return roles[i] == "controlplane"
		}

		return roles[i] < roles[j]
	})

	reports := make([]RoleReport, 0, len(roles))

	for _, role := range roles {
		group := byRole[role]
		sort.Slice(group, func(i, j int) bool { return group[i].Name < group[j].Name })

		rep := RoleReport{Role: role, Nodes: []string{}, Diffs: []Diff{}}

		paths := map[string]struct{}{}

		for _, n := range group {
			rep.Nodes = append(rep.Nodes, n.Name)

			for p := range n.Config {
				if !Ignored(p, ignore) {
					paths[p] = struct{}{}
				}
			}
		}

		sorted := make([]string, 0, len(paths))
		for p := range paths {
			sorted = append(sorted, p)
		}

		sort.Strings(sorted)

		for _, p := range sorted {
			values := map[string]string{}
			counts := map[string]int{}

			for _, n := range group {
				v, ok := n.Config[p]
				if !ok {
					v = Missing
				}

				values[n.Name] = v
				counts[v]++
			}

			if len(counts) == 1 {
				rep.Identical++

				continue
			}

			rep.Diffs = append(rep.Diffs, newDiff(p, values, counts, rep.Nodes))
		}

		reports = append(reports, rep)
	}

	return reports
}

func newDiff(p string, values map[string]string, counts map[string]int, order []string) Diff {
	d := Diff{Path: p, Values: values, Outliers: []string{}}

	best, bestN, tie := "", 0, false

	for v, n := range counts {
		switch {
		case n > bestN:
			best, bestN, tie = v, n, false
		case n == bestN:
			tie = true
		}
	}

	if !tie {
		d.Majority = best
	}

	for _, name := range order {
		if d.Majority == "" || values[name] != d.Majority {
			d.Outliers = append(d.Outliers, name)
		}
	}

	if d.Majority != "" && d.Majority != Missing {
		patch, err := BuildPatch(p, d.Majority)
		d.Patchable = err == nil
		d.Patch = patch
	}

	return d
}

// BuildPatch creates a strategic merge patch (YAML) that sets path to value
// (a JSON-encoded leaf). Only plain v1alpha1 map paths are supported.
func BuildPatch(p, value string) (string, error) {
	if value == Missing || value == "" {
		return "", errors.New("cannot build a patch that removes a setting")
	}

	segs := segments(p)
	if len(segs) == 0 || segs[0] == "" {
		return "", errors.New("empty path")
	}

	for _, s := range segs {
		if strings.HasPrefix(s, "#") {
			return "", errors.New("patches for multi-document configs are not supported here")
		}

		if _, err := strconv.Atoi(s); err == nil {
			return "", errors.New("paths inside lists cannot be patched safely with a merge patch")
		}
	}

	var leafValue any
	if err := json.Unmarshal([]byte(value), &leafValue); err != nil {
		return "", fmt.Errorf("invalid value: %w", err)
	}

	var doc any = leafValue
	for i := len(segs) - 1; i >= 0; i-- {
		doc = map[string]any{unescape(segs[i]): doc}
	}

	j, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}

	y, err := sigsyaml.JSONToYAML(j)
	if err != nil {
		return "", err
	}

	return string(y), nil
}
