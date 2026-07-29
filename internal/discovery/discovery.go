// Package discovery implements Compose project discovery: recognised
// filenames, ambiguity handling, one-level recursive scanning, override
// sibling detection, and label_file reference path resolution
// (SPECIFICATION.md §6). This is an original Go design informed by, not a
// port of, src/lib/discovery.sh (DECISIONS.md Decision 34) — the logic
// flow is intentionally close to the Bash version's where it was correct
// (it was; discovery.sh had no divergence issues found during review).
package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// RecognisedFilenames are the base Compose filenames daplabel will treat
// a directory as containing an application for (SPECIFICATION.md §6.1).
// Order here has no significance — see §6.2: daplabel does not use
// Docker's own resolution order to pick among multiple present files: if
// more than one is present, the directory is skipped (§6.4).
var RecognisedFilenames = []string{
	"compose.yml",
	"compose.yaml",
	"docker-compose.yml",
	"docker-compose.yaml",
}

// IsRecognisedFilename reports whether name is one of RecognisedFilenames.
func IsRecognisedFilename(name string) bool {
	for _, f := range RecognisedFilenames {
		if name == f {
			return true
		}
	}
	return false
}

// Warning is a non-fatal discovery-time issue (an ambiguous directory, a
// path that doesn't exist, an explicitly-named file that isn't a
// recognised Compose filename). Callers decide how to surface these —
// discovery itself never treats them as fatal errors, matching §6.4's
// "skipped ... a warning shall be reported" (not an abort).
type Warning struct {
	Path    string
	Message string
	// Files is populated only for the "multiple Compose files" case
	// (DiscoverComposeFiles), listing the conflicting filenames found
	// in the directory. Callers that render a tree view can use this
	// to show the conflicting files as nested children of the skipped
	// directory node.
	Files []string
}

func (w Warning) String() string { return fmt.Sprintf("%s: %s", w.Path, w.Message) }

// DiscoverComposeFiles finds the single recognised Compose file directly
// inside dir (SPECIFICATION.md §6.3). If none is present, found is false
// and there is no warning — an ordinary, non-noteworthy case. If more
// than one is present, the directory is skipped per §6.4 and a Warning is
// returned describing the ambiguity.
func DiscoverComposeFiles(dir string) (file string, found bool, warn *Warning) {
	var matches []string
	for _, name := range RecognisedFilenames {
		candidate := filepath.Join(dir, name)
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			matches = append(matches, candidate)
		}
	}

	switch len(matches) {
	case 0:
		return "", false, nil
	case 1:
		return matches[0], true, nil
	default:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = filepath.Base(m)
		}
		return "", false, &Warning{
			Path:    dir,
			Message: "skipped (contains multiple Compose files)",
			Files:   names,
		}
	}
}

// overrideName returns the automatically-loaded override sibling's
// filename for a given base filename, matching whichever naming style
// (compose.* vs docker-compose.*, .yml vs .yaml) the base file uses
// (DECISIONS.md Decision 23). ok is false for any filename that isn't one
// of RecognisedFilenames.
func overrideName(baseFilename string) (name string, ok bool) {
	switch baseFilename {
	case "docker-compose.yml":
		return "docker-compose.override.yml", true
	case "docker-compose.yaml":
		return "docker-compose.override.yaml", true
	case "compose.yml":
		return "compose.override.yml", true
	case "compose.yaml":
		return "compose.override.yaml", true
	default:
		return "", false
	}
}

// FindOverrideFile returns the path to baseFile's automatically-loaded
// override sibling, if one exists alongside it (DECISIONS.md Decision
// 23). An override file is deliberately not itself a member of
// RecognisedFilenames / DiscoverComposeFiles's results — it is only ever
// a sibling of a base file, never a valid target on its own.
func FindOverrideFile(baseFile string) (path string, found bool) {
	dir := filepath.Dir(baseFile)
	name, ok := overrideName(filepath.Base(baseFile))
	if !ok {
		return "", false
	}
	candidate := filepath.Join(dir, name)
	if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
		return candidate, true
	}
	return "", false
}

// DiscoverApplications scans the immediate child directories of parent
// only — no nested recursion (SPECIFICATION.md §6.5) — returning the
// resolved Compose file path for each child directory that is a valid,
// unambiguous application.
func DiscoverApplications(parent string) (files []string, warnings []Warning, err error) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, nil, fmt.Errorf("reading directory %s: %w", parent, err)
	}
	// Sorted for deterministic output — os.ReadDir already sorts by
	// filename, but this is made explicit rather than relied upon
	// implicitly (Engineering Principles §3.3, "predictable" per
	// SPECIFICATION.md §3.2).
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		childDir := filepath.Join(parent, name)
		file, found, warn := DiscoverComposeFiles(childDir)
		if warn != nil {
			warnings = append(warnings, *warn)
			continue
		}
		if found {
			files = append(files, file)
		}
	}
	return files, warnings, nil
}

// ResolveTargets resolves a list of user-supplied paths (files or
// directories) into concrete Compose file targets, per SPECIFICATION.md
// §6.3–§6.6. A path that is itself a recognised Compose file is used
// directly (explicit file mode, §6.6, disabling discovery for that
// path). A directory is scanned per §6.3/§6.5 depending on recursive.
// Anything else (a non-Compose file, a path that doesn't exist) produces
// a Warning rather than aborting the whole run.
func ResolveTargets(recursive bool, paths []string) (targets []string, warnings []Warning, err error) {
	for _, p := range paths {
		fi, statErr := os.Stat(p)
		switch {
		case statErr != nil:
			warnings = append(warnings, Warning{Path: p, Message: "path not found"})

		case !fi.IsDir():
			if IsRecognisedFilename(filepath.Base(p)) {
				targets = append(targets, p)
			} else {
				warnings = append(warnings, Warning{
					Path:    p,
					Message: "not a recognised Compose filename; ignoring",
				})
			}

		case recursive:
			found, apps, dErr := DiscoverApplications(p)
			if dErr != nil {
				return nil, nil, dErr
			}
			targets = append(targets, found...)
			warnings = append(warnings, apps...)

		default:
			file, ok, warn := DiscoverComposeFiles(p)
			switch {
			case warn != nil:
				warnings = append(warnings, *warn)
			case ok:
				targets = append(targets, file)
			default:
				warnings = append(warnings, Warning{Path: p, Message: "no valid Compose file found; skipping"})
			}
		}
	}
	return targets, warnings, nil
}

// ResolveLabelFileRef resolves a label_file reference against the
// directory of the Compose file it belongs to — exactly as Docker
// Compose itself resolves it — rather than against daplabel's own
// current working directory. This matters specifically for --recursive,
// where daplabel's cwd is the parent directory, never the individual
// project directory (mirrors src/lib/discovery.sh's
// lbl_resolve_label_file_ref exactly; this one was already correct).
func ResolveLabelFileRef(composeFile, ref string) string {
	if filepath.IsAbs(ref) {
		return ref
	}
	return filepath.Join(filepath.Dir(composeFile), ref)
}
