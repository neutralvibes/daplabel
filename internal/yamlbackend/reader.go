// Package yamlbackend implements the YAML backend abstraction
// (DECISIONS.md Decision 12/28): reading and writing Compose service
// labels and label_file references.
//
// Read functions are implemented using gopkg.in/yaml.v3's Node API rather
// than the yqlib expression-evaluation layer originally specified in
// Decision 28 — flagged explicitly here since it revises an existing
// decision rather than silently substituting for it. daplabel only ever
// needs a small, fixed set of read/write operations, never arbitrary
// user-supplied expressions; yaml.v3's Node tree gives direct,
// order-preserving access to exactly that, with a much smaller and more
// certain API surface than yqlib's. Write support (needed for
// comment-preserving in-place edits, Decision 20/28) is not yet
// implemented — see docs/IMPLEMENTATION_NOTES.md.
package yamlbackend

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Label is an ordered key/value pair, matching the document order labels
// appear in the Compose file's `labels:` mapping.
type Label struct {
	Key   string
	Value string
}

func loadRoot(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if len(doc.Content) == 0 {
		// An empty, or comment-only, file is well-formed YAML with
		// nothing in it — a normal, legitimate state (e.g. a placeholder
		// override file with nothing active in it yet), not malformed
		// input. mapGet(nil, ...) returns nil safely, so every caller
		// (GetServices, GetLabels via getServiceNode, GetLabelFileRefs)
		// already treats a nil root as "nothing found here" without
		// needing its own special case.
		return nil, nil
	}
	return doc.Content[0], nil
}

// mapGet returns the value node for key in mapping node m, or nil if m
// isn't a mapping or doesn't contain key.
func mapGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// GetServices returns the names of every service defined in the Compose
// file at path. A missing or empty `services:` section is a legitimate
// empty result, not an error — only a file that fails to parse at all is.
//
// Names are returned in sorted alphabetical order. This matches the
// documented behaviour (believed to mirror jq/yq's `keys` operator, which
// sorts, as distinct from `keys_unsorted` — see
// docs/IMPLEMENTATION_NOTES.md for the caveat that this should be
// verified against real yq output rather than assumed with certainty).
func GetServices(path string) ([]string, error) {
	root, err := loadRoot(path)
	if err != nil {
		return nil, err
	}
	services := mapGet(root, "services")
	if services == nil || services.Kind != yaml.MappingNode {
		return nil, nil
	}
	names := make([]string, 0, len(services.Content)/2)
	for i := 0; i+1 < len(services.Content); i += 2 {
		names = append(names, services.Content[i].Value)
	}
	sort.Strings(names)
	return names, nil
}

func getServiceNode(path, service string) (*yaml.Node, error) {
	root, err := loadRoot(path)
	if err != nil {
		return nil, err
	}
	services := mapGet(root, "services")
	if services == nil {
		return nil, nil
	}
	return mapGet(services, service), nil
}

// GetLabels returns the inline `labels:` entries for service in the
// Compose file at path, in document order. A service with no labels at
// all, or that doesn't exist in this file, is a legitimate empty result,
// not an error.
//
// Compose permits `labels:` as either a mapping (`labels: {key: value}`)
// or a list of "KEY=VALUE" strings (`labels: ["KEY=VALUE", ...]`) — both
// are handled, each preserving its own document order. A list entry with
// no "=" is treated as a key with an empty value, matching how
// internal/labelfile reads external label file lines the same way.
func GetLabels(path, service string) ([]Label, error) {
	svc, err := getServiceNode(path, service)
	if err != nil {
		return nil, err
	}
	if svc == nil {
		return nil, nil
	}
	labels := mapGet(svc, "labels")
	if labels == nil {
		return nil, nil
	}

	switch labels.Kind {
	case yaml.MappingNode:
		out := make([]Label, 0, len(labels.Content)/2)
		for i := 0; i+1 < len(labels.Content); i += 2 {
			out = append(out, Label{Key: labels.Content[i].Value, Value: labels.Content[i+1].Value})
		}
		return out, nil

	case yaml.SequenceNode:
		out := make([]Label, 0, len(labels.Content))
		for _, n := range labels.Content {
			key, value, _ := strings.Cut(n.Value, "=")
			out = append(out, Label{Key: key, Value: value})
		}
		return out, nil

	default:
		// `labels: null` (or any other scalar) — Compose doesn't define
		// this, but the sensible reading is "no labels", not an error.
		if labels.Kind == yaml.ScalarNode && labels.Tag == "!!null" {
			return nil, nil
		}
		return nil, fmt.Errorf("service %q in %s: labels is neither a mapping nor a list", service, path)
	}
}

// GetLabelFileRefs returns the label_file entries for service in the
// Compose file at path, in document order. A service with no label_file
// entries at all is a legitimate empty result, not an error.
func GetLabelFileRefs(path, service string) ([]string, error) {
	svc, err := getServiceNode(path, service)
	if err != nil {
		return nil, err
	}
	if svc == nil {
		return nil, nil
	}
	refs := mapGet(svc, "label_file")
	if refs == nil {
		return nil, nil
	}
	if refs.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("service %q in %s: label_file is not a sequence", service, path)
	}
	out := make([]string, 0, len(refs.Content))
	for _, n := range refs.Content {
		out = append(out, n.Value)
	}
	return out, nil
}
