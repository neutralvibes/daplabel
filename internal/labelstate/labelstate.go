// Package labelstate aggregates a service's label state across all its
// sources: inline labels in a Compose file and external label_file
// references. It is the natural home for logic that needs to understand
// "what labels already exist for this service" before deciding what to
// write, including cross-file conflict resolution (SPECIFICATION.md §8.2.1)
// and — in future — override-file awareness (docs/DECISIONS.md Decision 23).
package labelstate

import (
	"github.com/neutralvibes/daplabel/internal/discovery"
	"github.com/neutralvibes/daplabel/internal/labelfile"
	"github.com/neutralvibes/daplabel/internal/yamlbackend"
)

// ResolveConflicts reads all label_file references for service in
// composeFile, merges them per SPECIFICATION.md §8.2.1 using onConflict,
// and returns the merged labels plus every detected conflict. It is a
// shared helper for write-path commands so that conflict detection and
// resolution live in one place.
//
// If the service has zero or one label_file reference, there are no
// cross-file conflicts to resolve and both merged and conflicts are nil.
//
// The returned merged slice preserves first-occurrence order across the
// input files. conflicts contains every key that was detected as a
// conflict (differing values across files), regardless of how it was
// resolved — callers use this for warning output.
func ResolveConflicts(composeFile, service string, onConflict string) (merged []labelfile.Label, conflicts []labelfile.ConflictEntry, err error) {
	refs, err := yamlbackend.GetLabelFileRefs(composeFile, service)
	if err != nil {
		return nil, nil, err
	}
	if len(refs) <= 1 {
		return nil, nil, nil
	}

	files := make([]labelfile.FileLabels, len(refs))
	for i, ref := range refs {
		p := discovery.ResolveLabelFileRef(composeFile, ref)
		lbls, exists, rerr := labelfile.Read(p)
		if rerr != nil {
			return nil, nil, rerr
		}
		files[i] = labelfile.FileLabels{Path: p, Exists: exists, Labels: lbls}
	}
	merged, conflicts = labelfile.MergeLabelFiles(files, onConflict)
	return merged, conflicts, nil
}

// MergedMap returns the result of ResolveConflicts as a map for quick
// key-existence checks. It is a convenience wrapper for callers that only
// need to know "does this key already exist across label files?"
func MergedMap(composeFile, service string, onConflict string) (map[string]string, []labelfile.ConflictEntry, error) {
	merged, conflicts, err := ResolveConflicts(composeFile, service, onConflict)
	if err != nil {
		return nil, conflicts, err
	}
	out := make(map[string]string, len(merged))
	for _, l := range merged {
		out[l.Key] = l.Value
	}
	return out, conflicts, nil
}
