package survey

import (
	"encoding/json"
	"io"

	"github.com/neutralvibes/daplabel/internal/labelfile"
	"github.com/neutralvibes/daplabel/internal/yamlbackend"
)

// Every slice field below is built with make([]T, 0, ...), never a bare
// nil slice, and every function returns that same guarantee up the
// chain. This matters concretely: encoding/json marshals a nil slice as
// `null`, but a non-nil, zero-length slice as `[]` — the two are
// interchangeable in Go but NOT in the JSON this produces, and a
// consumer parsing this output shouldn't have to handle both shapes for
// "no items". A file that failed to parse, or a project with zero
// matching services, still gets `"services": []` / a `[]` top-level
// array — never `null` — with the failure surfaced through an explicit
// "error" field instead.

type jsonLabel struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type jsonOccurrence struct {
	Path  string `json:"path"`
	Value string `json:"value"`
}

type jsonConflict struct {
	Key         string           `json:"key"`
	Occurrences []jsonOccurrence `json:"occurrences"`
}

type jsonLabelFileRef struct {
	Ref          string      `json:"ref"`
	ResolvedPath string      `json:"resolved_path"`
	Exists       bool        `json:"exists"`
	Error        string      `json:"error,omitempty"`
	Labels       []jsonLabel `json:"labels"`
}

type jsonService struct {
	Name                 string             `json:"name"`
	InlineLabels         []jsonLabel        `json:"inline_labels"`
	OverrideConflictKeys []string           `json:"override_conflict_keys"`
	LabelFiles           []jsonLabelFileRef `json:"label_files"`
	LabelFileConflicts   []jsonConflict     `json:"label_file_conflicts"`
	Error                string             `json:"error,omitempty"`
}

type jsonFile struct {
	Path     string        `json:"path"`
	Override bool          `json:"override"`
	Error    string        `json:"error,omitempty"`
	Services []jsonService `json:"services"`
}

type jsonProject struct {
	Project string     `json:"project"`
	Files   []jsonFile `json:"files"`
}

func jsonLabelsFromYAML(in []yamlbackend.Label) []jsonLabel {
	out := make([]jsonLabel, 0, len(in))
	for _, l := range in {
		out = append(out, jsonLabel{Key: l.Key, Value: l.Value})
	}
	return out
}

func jsonLabelsFromLabelfile(in []labelfile.Label) []jsonLabel {
	out := make([]jsonLabel, 0, len(in))
	for _, l := range in {
		out = append(out, jsonLabel{Key: l.Key, Value: l.Value})
	}
	return out
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func buildJSONProjects(reports []ProjectReport) []jsonProject {
	out := make([]jsonProject, 0, len(reports))
	for _, rpt := range reports {
		jp := jsonProject{Project: rpt.ProjectName, Files: make([]jsonFile, 0, len(rpt.Files))}

		for fi, f := range rpt.Files {
			jf := jsonFile{
				Path:     f.Path,
				Override: fi > 0, // Files[0] is always the base file; Files[1], if present, is the override
				Error:    errString(f.Err),
				Services: make([]jsonService, 0, len(f.Services)),
			}

			for _, s := range f.Services {
				js := jsonService{
					Name:                 s.Name,
					InlineLabels:         jsonLabelsFromYAML(s.InlineLabels),
					OverrideConflictKeys: make([]string, 0, len(s.OverrideConflictKeys)),
					LabelFiles:           make([]jsonLabelFileRef, 0, len(s.LabelFileRefs)),
					LabelFileConflicts:   make([]jsonConflict, 0, len(s.LabelFileConflicts)),
					Error:                errString(s.Err),
				}
				js.OverrideConflictKeys = append(js.OverrideConflictKeys, s.OverrideConflictKeys...)
				for _, lfr := range s.LabelFileRefs {
					js.LabelFiles = append(js.LabelFiles, jsonLabelFileRef{
						Ref:          lfr.Ref,
						ResolvedPath: lfr.ResolvedPath,
						Exists:       lfr.Exists,
						Error:        errString(lfr.ReadErr),
						Labels:       jsonLabelsFromLabelfile(lfr.Labels),
					})
				}
				for _, c := range s.LabelFileConflicts {
					occs := make([]jsonOccurrence, 0, len(c.Occurrences))
					for _, o := range c.Occurrences {
						occs = append(occs, jsonOccurrence{Path: o.Path, Value: o.Value})
					}
					js.LabelFileConflicts = append(js.LabelFileConflicts, jsonConflict{Key: c.Key, Occurrences: occs})
				}
				jf.Services = append(jf.Services, js)
			}
			jp.Files = append(jp.Files, jf)
		}
		out = append(out, jp)
	}
	return out
}

// renderJSON writes reports as a JSON array of projects. Nothing else is
// printed alongside it (no footer breakdown, unlike tree/plain/table) —
// output meant for a JSON parser to consume must be valid JSON and
// nothing else.
func renderJSON(w io.Writer, reports []ProjectReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(buildJSONProjects(reports))
}
