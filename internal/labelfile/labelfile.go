// Package labelfile implements external label file reading. Conflict
// detection across multiple label_file references for a service is also
// implemented here, since it's a property of label_file semantics
// generally (SPECIFICATION.md §8.2.1), not specific to any one command.
//
// This package deliberately only detects and reports conflicts — it does
// not resolve them. Resolution (the --on-conflict flag, interactive
// prompts) is a write-path concern for `add`/`remove`, not something a
// read path like `survey` should invoke; see docs/DECISIONS.md Decision
// 35 for why conflating the two was a real bug in the Bash version.
//
// Writing (WriteLabels, below) computes new file content for `add`
// (and, eventually, `remove`/`generate`) to commit via
// internal/atomicwrite, with backup and atomic commit per Decision 20.
package labelfile

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Label is an ordered key/value pair as read from a label file line
// (`KEY=VALUE`).
type Label struct {
	Key   string
	Value string
}

// Read returns the ordered KEY=VALUE entries in the label file at path.
// If the file does not exist, exists is false and labels/err are both
// nil/zero — a missing label file is meaningful data for a caller to
// report (SPECIFICATION.md §8.7), not itself an error condition here.
func Read(path string) (labels []Label, exists bool, err error) {
	f, openErr := os.Open(path)
	if openErr != nil {
		if os.IsNotExist(openErr) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("opening %s: %w", path, openErr)
	}
	// A close error on a file only ever opened for reading carries no
	// actionable information (nothing was written that could be lost) and
	// nothing else in this function would change behaviour based on it —
	// explicitly ignored rather than left as a bare, unexplained call.
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		key, value, _ := strings.Cut(line, "=")
		labels = append(labels, Label{Key: key, Value: value})
	}
	if err := scanner.Err(); err != nil {
		return nil, true, fmt.Errorf("reading %s: %w", path, err)
	}
	return labels, true, nil
}

// WriteLabels computes the new content for the label file at path, given
// labels that should be added or (with force) updated. Existing lines —
// including any that aren't well-formed KEY=VALUE labels — are preserved
// exactly as they are and in their original order; a key already present
// is left completely untouched unless force is true, in which case its
// existing line is replaced in place, not moved to the end
// (SPECIFICATION.md §7.3's "existing labels shall not be overwritten
// unless explicitly requested"). A key not already present is appended
// as a new line.
//
// This function performs no I/O beyond reading path's current content (a
// missing file is treated as empty, not an error, since add's "no
// existing label_file" case computes brand new content this way too) —
// it returns the computed bytes for the caller to commit atomically
// (internal/atomicwrite), matching Decision 20's write-to-temp-then-
// commit strategy rather than writing directly here.
//
// skipped names the keys in add that were left untouched because they
// already existed and force was false — the caller is expected to warn
// about these, the same way survey/table report other left-alone state.
func WriteLabels(path string, add []Label, force bool) (content []byte, skipped []string, err error) {
	var lines []string
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		trimmed := strings.TrimRight(string(data), "\n")
		if trimmed != "" {
			lines = strings.Split(trimmed, "\n")
		}
	case os.IsNotExist(readErr):
		// No existing content — starting fresh is the correct, ordinary
		// case for a service's first label_file (§21), not an error.
	default:
		return nil, nil, fmt.Errorf("reading %s: %w", path, readErr)
	}

	existingLine := make(map[string]int, len(lines))
	for i, l := range lines {
		if l == "" {
			continue
		}
		key, _, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		existingLine[key] = i
	}

	for _, lbl := range add {
		if idx, ok := existingLine[lbl.Key]; ok {
			if !force {
				skipped = append(skipped, lbl.Key)
				continue
			}
			lines[idx] = lbl.Key + "=" + lbl.Value
			continue
		}
		lines = append(lines, lbl.Key+"="+lbl.Value)
		existingLine[lbl.Key] = len(lines) - 1
	}

	out := strings.Join(lines, "\n")
	if out != "" {
		out += "\n"
	}
	return []byte(out), skipped, nil
}

// RemoveLabels computes the new content for the label file at path with
// keys removed, mirroring WriteLabels' approach: every other line —
// including malformed or blank ones — is preserved exactly and in
// order. removed names the keys that were actually found and removed; a
// key in keys that wasn't present at all is simply absent from removed,
// not an error (there's nothing to remove is a legitimate outcome, for
// the caller to report if it wants).
//
// A missing file has nothing to remove: content is nil, removed is nil,
// err is nil. Callers that need to distinguish "file doesn't exist" from
// "file exists but the key wasn't in it" should check with os.Stat (or
// Read) themselves first — mirroring how add's on-none-create check
// works.
func RemoveLabels(path string, keys []string) (content []byte, removed []string, err error) {
	var lines []string
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		trimmed := strings.TrimRight(string(data), "\n")
		if trimmed != "" {
			lines = strings.Split(trimmed, "\n")
		}
	case os.IsNotExist(readErr):
		return nil, nil, nil
	default:
		return nil, nil, fmt.Errorf("reading %s: %w", path, readErr)
	}

	remove := make(map[string]bool, len(keys))
	for _, k := range keys {
		remove[k] = true
	}

	kept := lines[:0]
	for _, l := range lines {
		if l != "" {
			if key, _, ok := strings.Cut(l, "="); ok && remove[key] {
				removed = append(removed, key)
				continue
			}
		}
		kept = append(kept, l)
	}

	out := strings.Join(kept, "\n")
	if out != "" {
		out += "\n"
	}
	return []byte(out), removed, nil
}

// FileLabels pairs a label_file reference's resolved path with what was
// read from it (or its absence), for conflict detection across several
// references belonging to one service.
type FileLabels struct {
	Path   string
	Exists bool
	Labels []Label
}

// Occurrence is one file's contribution to a key that appears in more
// than one of a service's label_file references.
type Occurrence struct {
	Path  string
	Value string
}

// ConflictEntry is a key that appears in more than one of a service's
// label_file references, with every file it appears in and the value
// there.
type ConflictEntry struct {
	Key         string
	Occurrences []Occurrence
}

// DetectConflicts returns, for a set of a service's label_file
// references, every key that appears in more than one file — each with
// every file it appears in and the value there, in reference order. Keys
// that appear in exactly one file are not included: they are
// unambiguous, not a conflict to report (SPECIFICATION.md §8.2.1;
// DECISIONS.md Decision 16's "unambiguous target" case). The returned
// slice preserves first-occurrence order across the input files, for
// deterministic, predictable output (Engineering Principles §2.3,
// SPECIFICATION.md §3.2).
func DetectConflicts(files []FileLabels) []ConflictEntry {
	order := make([]string, 0)
	occs := make(map[string][]Occurrence)

	for _, f := range files {
		if !f.Exists {
			continue
		}
		for _, l := range f.Labels {
			if _, seen := occs[l.Key]; !seen {
				order = append(order, l.Key)
			}
			occs[l.Key] = append(occs[l.Key], Occurrence{Path: f.Path, Value: l.Value})
		}
	}

	var out []ConflictEntry
	for _, k := range order {
		if len(occs[k]) > 1 {
			out = append(out, ConflictEntry{Key: k, Occurrences: occs[k]})
		}
	}
	return out
}

// MergeLabelFiles resolves cross-file key conflicts among a service's
// label_file references, producing a single merged label set
// (SPECIFICATION.md §8.2.1).
//
// files must be in label_file reference order (first → last), each
// representing what was read from one reference. A missing file
// (Exists=false) contributes no labels and is silently skipped.
//
// onConflict controls resolution for keys that appear in more than one
// file with differing values:
//
//	"first" — use the value from the first file in which the key appears
//	"last"  — use the value from the last file in which the key appears
//	"skip"  — omit the key entirely from the merged set
//
// Keys that appear in multiple files with the same value are not
// conflicts and are included once in the merged set.
//
// The returned merged slice preserves first-occurrence order across the
// input files. conflicts contains every key that was detected as a
// conflict (differing values across files), regardless of how it was
// resolved — callers use this for warning output.
func MergeLabelFiles(files []FileLabels, onConflict string) (merged []Label, conflicts []ConflictEntry) {
	// Build per-key occurrence list in reference order.
	order := make([]string, 0)
	occs := make(map[string][]Occurrence)

	for _, f := range files {
		if !f.Exists {
			continue
		}
		for _, l := range f.Labels {
			if _, seen := occs[l.Key]; !seen {
				order = append(order, l.Key)
			}
			occs[l.Key] = append(occs[l.Key], Occurrence{Path: f.Path, Value: l.Value})
		}
	}

	for _, k := range order {
		occ := occs[k]
		if len(occ) == 0 {
			continue
		}

		// Check whether all occurrences share the same value.
		allSame := true
		for i := 1; i < len(occ); i++ {
			if occ[i].Value != occ[0].Value {
				allSame = false
				break
			}
		}

		if allSame {
			// Not a conflict — same value everywhere.
			merged = append(merged, Label{Key: k, Value: occ[0].Value})
			continue
		}

		// Real conflict: differing values across files.
		conflicts = append(conflicts, ConflictEntry{Key: k, Occurrences: occ})

		switch onConflict {
		case "first":
			merged = append(merged, Label{Key: k, Value: occ[0].Value})
		case "last":
			merged = append(merged, Label{Key: k, Value: occ[len(occ)-1].Value})
		case "skip", "":
			// Omit the key entirely.
		}
	}
	return merged, conflicts
}
