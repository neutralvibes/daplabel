package yamlbackend

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadRootForEdit reads path and returns its root mapping node for
// in-place mutation by this file's Set* functions, followed by Marshal
// to re-serialise it. It is loadRoot (the same function GetServices/
// GetLabels/GetLabelFileRefs use) exported for write-path callers:
// mutating the very Node tree that was parsed — rather than rebuilding a
// document from scratch — is what lets comments on every untouched node
// survive a round trip (SPECIFICATION.md §8.5, DECISIONS.md Decision 20).
//
// A nil, nil return means path parsed as a well-formed but empty
// document (loadRoot's existing "empty is not an error" handling); write
// callers that need an existing services: section to add to should
// treat that as their own error (there is nothing to add a label to),
// not as a signal to fabricate one from nothing.
func LoadRootForEdit(path string) (*yaml.Node, error) {
	return loadRoot(path)
}

// Marshal re-serialises root — expected to be a mapping node of the
// shape loadRoot/LoadRootForEdit returns, after any of this file's Set*
// mutations — back to Compose-file YAML bytes, at a 2-space indent
// matching typical Compose file style. Nodes this package never touched
// keep their original style, comments, and key order; only content a
// Set* function actually added or changed is newly generated and so has
// no original comment to preserve (§8.5: "where preservation is not
// feasible, structural integrity shall take priority").
func Marshal(root *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if root != nil {
		if err := enc.Encode(root); err != nil {
			_ = enc.Close()
			return nil, fmt.Errorf("encoding YAML: %w", err)
		}
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encoding YAML: %w", err)
	}
	return buf.Bytes(), nil
}

// getServiceNodeFromRoot is getServiceNode's already-loaded-root
// counterpart: write-path callers load root once via LoadRootForEdit and
// perform several mutations against it before a single Marshal, rather
// than round-tripping through disk per mutation.
func getServiceNodeFromRoot(root *yaml.Node, service string) *yaml.Node {
	services := mapGet(root, "services")
	if services == nil {
		return nil
	}
	return mapGet(services, service)
}

// valueStyle returns the yaml.Style to use for a label value's scalar
// node: explicitly double-quoted by default, matching Decision 33's
// "label values are always strings, so quoting is the accurate
// representation" — or plain/unquoted when quote is false
// (--values-no-quote).
func valueStyle(quote bool) yaml.Style {
	if quote {
		return yaml.DoubleQuotedStyle
	}
	return 0
}

// SetLabelFileRef ensures service (which must already exist in root)
// references ref via a label_file entry, creating the label_file:
// sequence if the service doesn't have one yet, or appending to an
// existing one. If ref is already present, this is a no-op — added is
// false — since SPECIFICATION.md §7.3/§8.2 describe appending new
// references, not duplicating an existing one.
func SetLabelFileRef(root *yaml.Node, service, ref string) (added bool, err error) {
	svc := getServiceNodeFromRoot(root, service)
	if svc == nil {
		return false, fmt.Errorf("service %q not found", service)
	}
	if svc.Kind != yaml.MappingNode {
		return false, fmt.Errorf("service %q is not a mapping", service)
	}

	refs := mapGet(svc, "label_file")
	if refs == nil {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "label_file"}
		seqNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		svc.Content = append(svc.Content, keyNode, seqNode)
		refs = seqNode
	}
	if refs.Kind != yaml.SequenceNode {
		return false, fmt.Errorf("service %q: label_file is not a sequence", service)
	}

	for _, n := range refs.Content {
		if n.Value == ref {
			return false, nil
		}
	}
	refs.Content = append(refs.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ref})
	return true, nil
}

// RemoveLabelFileRef removes ref from service's label_file sequence, if
// present — used by `remove` when deleting the last key from a
// label_file also empties the file itself and it's being cleaned up
// entirely (SPECIFICATION.md §8.6's default reading for remove; see
// DECISIONS.md Decision 40). If removing ref empties the label_file
// sequence, the label_file: key itself is removed too, the same way
// RemoveInlineLabelKeys tidies up an emptied labels: mapping.
//
// removed is false, with no error, if ref wasn't present — matching
// SetLabelFileRef's already-present-is-a-no-op symmetry, since a caller
// asking to remove a reference that's already gone has nothing left to
// do, not something to fail on.
func RemoveLabelFileRef(root *yaml.Node, service, ref string) (removed bool, err error) {
	svc := getServiceNodeFromRoot(root, service)
	if svc == nil {
		return false, fmt.Errorf("service %q not found", service)
	}
	if svc.Kind != yaml.MappingNode {
		return false, fmt.Errorf("service %q is not a mapping", service)
	}

	refs := mapGet(svc, "label_file")
	if refs == nil {
		return false, nil
	}
	if refs.Kind != yaml.SequenceNode {
		return false, fmt.Errorf("service %q: label_file is not a sequence", service)
	}

	idx := -1
	for i, n := range refs.Content {
		if n.Value == ref {
			idx = i
			break
		}
	}
	if idx == -1 {
		return false, nil
	}
	refs.Content = append(refs.Content[:idx], refs.Content[idx+1:]...)

	if len(refs.Content) == 0 {
		for i := 0; i+1 < len(svc.Content); i += 2 {
			if svc.Content[i].Value == "label_file" {
				svc.Content = append(svc.Content[:i], svc.Content[i+2:]...)
				break
			}
		}
	}

	return true, nil
}

// RemoveInlineLabelKeys removes the named keys from service's inline
// labels: block (used by `generate`, SPECIFICATION.md §7.2's "replace
// inline labels with label_file references" — only the keys actually
// migrated to a label file should disappear from labels:, not
// necessarily all of them, since some may have been left in place after
// a naming collision at the destination). If removing every key empties
// the block, the labels: entry itself is removed too, rather than
// leaving a pointless `labels: {}` or `labels: []` behind.
//
// Both mapping-form (`labels: {key: value}`) and list-form
// (`labels: ["KEY=VALUE", ...]`) labels are supported; each form is read
// the same way GetLabels does and keys to remove are matched identically
// regardless of form. A service with no labels: at all returns
// removed=nil, err=nil — not an error, since generate calls this only
// after already reading some labels via GetLabels, but a caller that got
// here another way shouldn't be surprised by an error for "there was
// nothing to remove."
func RemoveInlineLabelKeys(root *yaml.Node, service string, keys []string) (removed []string, err error) {
	svc := getServiceNodeFromRoot(root, service)
	if svc == nil {
		return nil, fmt.Errorf("service %q not found", service)
	}
	if svc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("service %q is not a mapping", service)
	}

	labels := mapGet(svc, "labels")
	if labels == nil {
		return nil, nil
	}

	remove := make(map[string]bool, len(keys))
	for _, k := range keys {
		remove[k] = true
	}

	switch labels.Kind {
	case yaml.MappingNode:
		removed = removeMappingKeys(labels, remove)
	case yaml.SequenceNode:
		removed = removeSequenceKeys(labels, remove)
	default:
		return nil, fmt.Errorf(
			"service %q: labels is neither a mapping nor a list; daplabel cannot edit it in place", service)
	}

	if len(labels.Content) == 0 {
		for i := 0; i+1 < len(svc.Content); i += 2 {
			if svc.Content[i].Value == "labels" {
				svc.Content = append(svc.Content[:i], svc.Content[i+2:]...)
				break
			}
		}
	}

	return removed, nil
}

// removeMappingKeys drops every key/value pair from labels (a MappingNode)
// whose key is in removeSet, keeping the rest in their original order.
func removeMappingKeys(labels *yaml.Node, removeSet map[string]bool) (removed []string) {
	kept := labels.Content[:0]
	for i := 0; i+1 < len(labels.Content); i += 2 {
		k, v := labels.Content[i], labels.Content[i+1]
		if removeSet[k.Value] {
			removed = append(removed, k.Value)
			continue
		}
		kept = append(kept, k, v)
	}
	labels.Content = kept
	return removed
}

// removeSequenceKeys drops every entry from labels (a SequenceNode) whose
// key (split from the entry text the same way GetLabels reads list-form
// labels) is in removeSet, keeping the rest in their original order and
// text.
func removeSequenceKeys(labels *yaml.Node, removeSet map[string]bool) (removed []string) {
	kept := labels.Content[:0]
	for _, n := range labels.Content {
		key, _, _ := strings.Cut(n.Value, "=")
		if removeSet[key] {
			removed = append(removed, key)
			continue
		}
		kept = append(kept, n)
	}
	labels.Content = kept
	return removed
}

// SetInlineLabel sets service's inline labels: key to value in root
// (which must already contain service), creating the labels: mapping if
// the service doesn't have one yet.
//
// If key already exists and force is false, the existing value is left
// completely untouched — added and changed are both false — per
// SPECIFICATION.md §7.3's "existing labels shall not be overwritten
// unless explicitly requested." If force is true, the existing value is
// replaced in place (same key position, not moved to the end).
//
// quote selects Decision 33's default-quoted-string behaviour
// (--values-no-quote passes false).
//
// Both mapping-form (`labels: {key: value}`) and list-form
// (`labels: ["KEY=VALUE", ...]`) labels are supported; each form is
// updated in its own shape. Untouched entries keep their original YAML
// text and style; only added or replaced entries are newly generated.
func SetInlineLabel(root *yaml.Node, service, key, value string, quote, force bool) (added, changed bool, err error) {
	svc := getServiceNodeFromRoot(root, service)
	if svc == nil {
		return false, false, fmt.Errorf("service %q not found", service)
	}
	if svc.Kind != yaml.MappingNode {
		return false, false, fmt.Errorf("service %q is not a mapping", service)
	}

	labels := mapGet(svc, "labels")
	if labels == nil {
		// A service with no existing labels block gets a mapping by
		// default; list-form handling is for preserving an already-
		// present list, not for creating new lists.
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "labels"}
		mapNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		svc.Content = append(svc.Content, keyNode, mapNode)
		labels = mapNode
	}

	style := valueStyle(quote)
	switch labels.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(labels.Content); i += 2 {
			if labels.Content[i].Value == key {
				if !force {
					return false, false, nil
				}
				labels.Content[i+1].SetString(value)
				labels.Content[i+1].Style = style
				return false, true, nil
			}
		}

		valNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, Style: style}
		labels.Content = append(labels.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			valNode,
		)
		return true, true, nil

	case yaml.SequenceNode:
		entry := key + "=" + value
		for _, n := range labels.Content {
			existingKey, _, _ := strings.Cut(n.Value, "=")
			if existingKey == key {
				if !force {
					return false, false, nil
				}
				n.SetString(entry)
				n.Style = style
				return false, true, nil
			}
		}
		labels.Content = append(labels.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: entry,
			Style: style,
		})
		return true, true, nil

	default:
		return false, false, fmt.Errorf(
			"service %q: labels is neither a mapping nor a list; daplabel cannot edit it in place", service)
	}
}
