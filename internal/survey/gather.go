// Package survey implements the `survey` command's read-only inspection
// (SPECIFICATION.md §7.5): scanning applications, reporting services and
// labels, and indicating label file presence/absence — performing no
// modifications.
//
// Both output formats are built from one shared ProjectReport per
// project, gathered once in gatherProject, so §7.5.1's requirement that
// "both formats shall present identical information" is structural rather
// than something each renderer has to separately remember to honour. This
// is an original Go design (DECISIONS.md Decision 34); see Decision 35
// for why it deliberately does not reproduce the Bash `plain` format's
// conflict-resolving merged view.
package survey

import (
	"path/filepath"

	"github.com/neutralvibes/daplabel/internal/discovery"
	"github.com/neutralvibes/daplabel/internal/labelfile"
	"github.com/neutralvibes/daplabel/internal/yamlbackend"
)

// LabelFileRefReport is one label_file reference belonging to a service,
// as written in the Compose file, together with what was found when
// resolving and reading it.
type LabelFileRefReport struct {
	Ref          string // exactly as written in the Compose file
	ResolvedPath string

	// Exists is false when the file simply doesn't exist
	// (SPECIFICATION.md §8.7's "missing label file" case) — the
	// ordinary, expected-to-happen-sometimes case.
	Exists bool
	Labels []labelfile.Label

	// ReadErr is set when the file exists but could not be read (e.g. a
	// permissions error) — a distinct, more concerning condition than
	// "missing", and reported as such rather than collapsed into it.
	ReadErr error
}

// ServiceReport is everything survey reports about one service within
// one Compose file (a base file, or its override sibling).
type ServiceReport struct {
	Name string

	InlineLabels []yamlbackend.Label

	// OverrideConflictKeys lists inline label keys this service defines
	// that are ALSO defined for the same service in the base file. Only
	// ever populated for a ServiceReport belonging to the override
	// FileReport (SPECIFICATION.md §7.5.2; DECISIONS.md Decision 23 Rule
	// 4). Compose applies the override's value at runtime; survey only
	// makes that visible, it does not compute a merged value.
	OverrideConflictKeys []string

	LabelFileRefs []LabelFileRefReport

	// LabelFileConflicts lists keys defined in more than one of this
	// service's label_file references. Reported, never resolved — see
	// package doc comment and DECISIONS.md Decision 35.
	LabelFileConflicts []labelfile.ConflictEntry

	// Err is set if reading this service's data failed outright (e.g. a
	// YAML parse error specific to this service's shape). InlineLabels
	// etc. above may still be partially populated if the failure occurred
	// partway through.
	Err error
}

// FileReport is everything survey reports about one Compose file: either
// a project's base file, or its automatically-loaded override sibling.
type FileReport struct {
	Path     string
	Services []ServiceReport

	// Err is set if the services list itself could not be read (e.g. the
	// file fails to parse at all) — distinct from a single service's Err.
	Err error
}

// ProjectReport is everything survey reports about one discovered
// application: its base Compose file, and its override file if one
// exists (SPECIFICATION.md §7.5.2).
type ProjectReport struct {
	ProjectName string // basename of the project directory
	Files       []FileReport
	HadError    bool
}

// gatherProject reads everything survey needs to report for the
// application whose base Compose file is at composeFile, exactly once,
// regardless of which output format will render it.
func gatherProject(composeFile string) ProjectReport {
	rpt := ProjectReport{ProjectName: filepath.Base(filepath.Dir(composeFile))}

	files := []string{composeFile}
	if override, ok := discovery.FindOverrideFile(composeFile); ok {
		files = append(files, override)
	}

	for i, f := range files {
		fr := FileReport{Path: f}

		services, err := yamlbackend.GetServices(f)
		if err != nil {
			fr.Err = err
			rpt.HadError = true
			rpt.Files = append(rpt.Files, fr)
			continue
		}

		for _, name := range services {
			sr := gatherService(composeFile, f, name, i > 0)
			if sr.Err != nil {
				rpt.HadError = true
			}
			fr.Services = append(fr.Services, sr)
		}
		rpt.Files = append(rpt.Files, fr)
	}

	return rpt
}

// gatherService reads one service's data from currentFile. isOverride
// indicates currentFile is the override sibling of baseFile, in which
// case its inline labels are also compared against baseFile's to compute
// OverrideConflictKeys.
func gatherService(baseFile, currentFile, name string, isOverride bool) ServiceReport {
	sr := ServiceReport{Name: name}

	inline, err := yamlbackend.GetLabels(currentFile, name)
	if err != nil {
		sr.Err = err
		return sr
	}
	sr.InlineLabels = inline

	if isOverride {
		if baseInline, bErr := yamlbackend.GetLabels(baseFile, name); bErr == nil {
			baseKeys := make(map[string]bool, len(baseInline))
			for _, l := range baseInline {
				baseKeys[l.Key] = true
			}
			for _, l := range sr.InlineLabels {
				if baseKeys[l.Key] {
					sr.OverrideConflictKeys = append(sr.OverrideConflictKeys, l.Key)
				}
			}
		}
	}

	refs, err := yamlbackend.GetLabelFileRefs(currentFile, name)
	if err != nil {
		sr.Err = err
		return sr
	}

	var forConflictCheck []labelfile.FileLabels
	for _, ref := range refs {
		resolved := discovery.ResolveLabelFileRef(currentFile, ref)
		labels, exists, readErr := labelfile.Read(resolved)
		lfr := LabelFileRefReport{Ref: ref, ResolvedPath: resolved, Exists: exists, Labels: labels, ReadErr: readErr}
		sr.LabelFileRefs = append(sr.LabelFileRefs, lfr)
		// A file that errored while being read contributes nothing to
		// conflict detection — its contents, if any, are not reliable.
		if readErr == nil {
			forConflictCheck = append(forConflictCheck, labelfile.FileLabels{Path: resolved, Exists: exists, Labels: labels})
		}
	}
	sr.LabelFileConflicts = labelfile.DetectConflicts(forConflictCheck)

	return sr
}
