package survey

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/neutralvibes/daplabel/internal/discovery"
)

// Options are survey's own flags (SPECIFICATION.md §7.5.1, plus
// filtering, DECISIONS.md — see the entry recording this addition).
type Options struct {
	Recursive bool
	Format    string // "tree" (default), "plain", "table", or "json"
	Filter    []KeyValueFilter
	Missing   []string
	NoSummary bool // suppress the Summary block for tree/plain/table
	NoWrap    bool // disable column wrapping for table format
}

// breakdown accumulates the footer summary printed after every project has
// been reported. Counts are taken from each project's base file only
// (rpt.Files[0]) — override files are already shown in full detail in the
// tree/plain output above; this footer is a quick eyeball aid across many
// projects, not a strict ledger, and counting override-introduced services
// separately would need a decision about whether they're "the same
// service" or not (see docs/IMPLEMENTATION_NOTES.md).
type breakdown struct {
	projects           int
	services           int
	servicesNoLabels   int
	projectsWithErrors int
	skipped            int // count of discovery warnings (skipped dirs, etc.)

	// Plain-format global label counts (across all files, base + override).
	totalLabels  int
	inlineLabels int
	fileLabels   int
	labelFiles   map[string]bool // distinct resolved label-file paths
}

func (b *breakdown) add(rpt ProjectReport) {
	b.projects++
	if rpt.HadError {
		b.projectsWithErrors++
	}
	if len(rpt.Files) == 0 {
		return
	}
	for _, s := range rpt.Files[0].Services {
		b.services++
		if len(s.InlineLabels) == 0 && len(s.LabelFileRefs) == 0 {
			b.servicesNoLabels++
		}
	}
}

// addPlain counts labels across all files (base + override) for the
// plain-format summary.
func (b *breakdown) addPlain(rpt ProjectReport) {
	if b.labelFiles == nil {
		b.labelFiles = make(map[string]bool)
	}
	for _, f := range rpt.Files {
		for _, s := range f.Services {
			b.inlineLabels += len(s.InlineLabels)
			for _, lfr := range s.LabelFileRefs {
				b.fileLabels += len(lfr.Labels)
				if lfr.Exists && lfr.ReadErr == nil {
					b.labelFiles[lfr.ResolvedPath] = true
				}
			}
		}
	}
	b.totalLabels = b.inlineLabels + b.fileLabels
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func (b breakdown) String() string {
	msg := fmt.Sprintf("%s, %s", plural(b.projects, "project"), plural(b.services, "service"))
	if b.servicesNoLabels > 0 {
		msg += fmt.Sprintf(" (%d without any labels)", b.servicesNoLabels)
	}
	if b.projectsWithErrors > 0 {
		msg += fmt.Sprintf("; %s had errors", plural(b.projectsWithErrors, "project"))
	}
	return msg
}

// plainSummary returns the plain-format summary line:
//
//	Summary: 25 projects, 36 services; Labels: 100 total, 34 inline, 23 in 5 files, 12 services no labels
func (b breakdown) plainSummary() string {
	msg := fmt.Sprintf("Summary: %s, %s",
		plural(b.projects, "project"), plural(b.services, "service"))
	msg += fmt.Sprintf("; Labels: %d total, %d inline, %d in %d files",
		b.totalLabels, b.inlineLabels, b.fileLabels, len(b.labelFiles))
	if b.servicesNoLabels > 0 {
		msg += fmt.Sprintf(", %d services no labels", b.servicesNoLabels)
	}
	return msg
}

// Run performs a full survey over paths and writes the result to out,
// with any recoverable warnings (SPECIFICATION.md §6.4's ambiguous
// directories, unresolvable paths) written to warnOut as they occur.
//
// If opts.Filter or opts.Missing is set, only services matching every
// criterion are shown (see matchesFilters); a project with no matching
// service is omitted entirely and not counted in the footer breakdown.
//
// For "tree" format, each user-supplied path argument is rendered as its
// own tree: a root line (the path itself) for --recursive scans, or no
// root line for a single-folder non-recursive scan. Discovery warnings
// (skipped ambiguous directories, etc.) are interleaved with projects
// under the root, sorted alphabetically by name.
//
// "plain" streams directly to out, one project at a time.
//
// "table" and "json" render once, across every matching project together,
// since both are naturally a single combined structure rather than a
// per-project one; "table" still gets the same footer, but "json" does
// not — output meant for a JSON parser to consume must be exactly that
// and nothing else, so no summary text is mixed into it.
//
// The returned exitCode follows SPECIFICATION.md §10.1: 2 for an
// unrecognised --format value, 4 if no Compose files were found at all,
// 1 if one or more discovered projects had an error partway through (the
// specific errors are shown inline in out, at the point they occurred —
// other projects are still fully reported, they are not aborted), 0
// otherwise. err, when non-nil, is a summary suitable for printing once
// by the caller; Run does not print its own top-level error line.
func Run(out io.Writer, warnOut io.Writer, paths []string, opts Options) (exitCode int, err error) {
	switch opts.Format {
	case "tree", "plain", "table", "json":
		// all implemented
	default:
		return 2, fmt.Errorf("unrecognised --format value %q (expected %q, %q, %q, or %q)",
			opts.Format, "tree", "plain", "table", "json")
	}

	targets, warnings, dErr := discovery.ResolveTargets(opts.Recursive, paths)
	if dErr != nil {
		return 1, dErr
	}
	// Warnings are no longer printed to warnOut here — for tree format
	// they are rendered inline in the tree; for other formats they are
	// still printed as before.
	if opts.Format != "tree" {
		for _, w := range warnings {
			fmt.Fprintf(warnOut, "warning: %s\n", w)
		}
	}

	if len(targets) == 0 && len(warnings) == 0 {
		return 4, fmt.Errorf("no Compose files found")
	}

	var bd breakdown
	bd.skipped = len(warnings)
	var accumulated []ProjectReport // used by "table"/"json" (and now "tree" too)

	// For tree format, we need to group projects and warnings per
	// user-supplied path argument so each path gets its own root tree.
	// For other formats, we still stream/accumulate as before.
	if opts.Format == "tree" {
		// Group targets and warnings by the user-supplied path they
		// came from. For --recursive, there's one path (the parent
		// directory) and all targets/warnings belong to it. For
		// non-recursive with multiple paths, each path contributes its
		// own targets/warnings.
		type pathGroup struct {
			path     string
			targets  []string
			warnings []discovery.Warning
		}

		// Build a map from the user-supplied path to its group.
		groups := make(map[string]*pathGroup)
		for _, p := range paths {
			groups[p] = &pathGroup{path: p}
		}

		// Assign each target to the path that contains it.
		for _, t := range targets {
			// Find which user-supplied path this target belongs under.
			// For --recursive, all targets are under the single parent
			// path. For non-recursive, a target is either the path
			// itself (explicit file) or directly inside it.
			assigned := false
			for _, p := range paths {
				if t == p || isUnderPath(t, p) {
					groups[p].targets = append(groups[p].targets, t)
					assigned = true
					break
				}
			}
			if !assigned {
				// Fallback: assign to the first path.
				groups[paths[0]].targets = append(groups[paths[0]].targets, t)
			}
		}

		// Assign each warning to the path that contains it.
		for _, w := range warnings {
			assigned := false
			for _, p := range paths {
				if w.Path == p || isUnderPath(w.Path, p) {
					groups[p].warnings = append(groups[p].warnings, w)
					assigned = true
					break
				}
			}
			if !assigned {
				groups[paths[0]].warnings = append(groups[paths[0]].warnings, w)
			}
		}

		first := true
		for _, p := range paths {
			g := groups[p]
			// Gather projects for this path's targets.
			var pathReports []ProjectReport
			for _, t := range g.targets {
				rpt := gatherProject(t)
				rpt, matched := filterProject(rpt, opts.Filter, opts.Missing)
				if !matched {
					continue
				}
				pathReports = append(pathReports, rpt)
				bd.add(rpt)
			}

			if len(pathReports) == 0 && len(g.warnings) == 0 {
				continue
			}

			if !first {
				fmt.Fprintln(out) // blank line between path trees
			}
			first = false

			rootLabel := ""
			if opts.Recursive {
				rootLabel = p
			}
			renderTree(out, rootLabel, pathReports, g.warnings)
		}
	} else {
		for _, target := range targets {
			rpt := gatherProject(target)
			rpt, matched := filterProject(rpt, opts.Filter, opts.Missing)
			if !matched {
				continue // no service in this project satisfied the filter — nothing to report or count
			}

			accumulated = append(accumulated, rpt)
			bd.add(rpt)
			if opts.Format == "plain" {
				bd.addPlain(rpt)
			}
		}
	}

	switch opts.Format {
	case "plain":
		basePath := deriveBasePath(paths)
		renderPlain(out, basePath, accumulated)
		if !opts.NoSummary {
			fmt.Fprintln(out, bd.plainSummary())
		}
	case "table":
		renderTable(out, accumulated, opts.NoWrap)
		if s := summaryBlock(bd, opts.NoSummary); s != "" {
			fmt.Fprintln(out, s)
		}
	case "json":
		if jsonErr := renderJSON(out, accumulated); jsonErr != nil {
			return 1, fmt.Errorf("encoding JSON output: %w", jsonErr)
		}
		// deliberately no footer line — see doc comment above
	default:
		// tree: print the summary block.
		if s := summaryBlock(bd, opts.NoSummary); s != "" {
			fmt.Fprintln(out, s)
		}
	}

	if len(targets) == 0 {
		return 4, fmt.Errorf("no Compose files found")
	}
	if bd.projectsWithErrors > 0 {
		return 1, fmt.Errorf("one or more projects had errors during survey")
	}
	return 0, nil
}

// deriveBasePath returns the "Base Folder:" header value for plain format.
// For a single path argument it uses that path's directory (or the path
// itself if it is a directory). For multiple paths it falls back to the
// first path's directory.
func deriveBasePath(paths []string) string {
	if len(paths) == 0 {
		return "."
	}
	p := paths[0]
	// If the path looks like a file (has an extension), use its directory.
	if filepath.Ext(p) != "" {
		return filepath.Dir(p)
	}
	return p
}

// isUnderPath reports whether child is equal to parent or is a
// descendant of parent in the filesystem.
func isUnderPath(child, parent string) bool {
	if child == parent {
		return true
	}
	// Simple prefix check: child starts with parent + separator.
	return len(child) > len(parent) && child[len(parent)] == '/' && child[:len(parent)] == parent
}
