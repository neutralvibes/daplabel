package survey

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

const (
	tableWidthProject = 20
	tableWidthFile    = 20
	tableWidthService = 20
	tableWidthLabel   = 45
	tableWidthSource  = 6
)

// tableRow is one row of the plaintext table. Project/File/Service are
// left empty when they repeat the previous row's value — matching a
// spreadsheet-style report where a repeated group value is shown once,
// not on every row. Labels are stored already merged as "KEY=VALUE".
type tableRow struct {
	project string // shown only on the first row of a new project
	file    string // shown only on the first row of a new file within the project
	service string // shown only on the first row of a new service within the file
	source  string // "yaml" (inline), "file" (label_file), or blank
	label   string // merged "KEY=VALUE" string
}

// buildTableRows flattens a set of already-filtered ProjectReports into
// table rows, in project → file → service → label order. The source is
// normalised to the constrained table vocabulary: "yaml" for inline
// labels and "file" for anything coming from a label_file reference
// (including conflicts).
func buildTableRows(reports []ProjectReport) []tableRow {
	var rows []tableRow

	for _, rpt := range reports {
		projectShown := false

		for _, f := range rpt.Files {
			fileShown := false
			fileLabel := filepath.Base(f.Path)

			emitFileRow := func() {
				row := tableRow{}
				if !projectShown {
					row.project = rpt.ProjectName
					projectShown = true
				}
				if !fileShown {
					row.file = fileLabel
					fileShown = true
				}
				rows = append(rows, row)
			}

			if f.Err != nil {
				emitFileRow()
				continue
			}
			if len(f.Services) == 0 {
				emitFileRow()
				continue
			}

			for _, s := range f.Services {
				serviceShown := false

				emit := func(source, label string) {
					row := tableRow{source: source, label: label}
					if !projectShown {
						row.project = rpt.ProjectName
						projectShown = true
					}
					if !fileShown {
						row.file = fileLabel
						fileShown = true
					}
					if !serviceShown {
						row.service = s.Name
						serviceShown = true
					}
					rows = append(rows, row)
				}

				if s.Err != nil {
					emit("", "")
					continue
				}

				if len(s.InlineLabels) == 0 && len(s.LabelFileRefs) == 0 {
					emit("", "")
					continue
				}

				for _, l := range s.InlineLabels {
					emit("yaml", l.Key+"="+l.Value)
				}

				for _, lfr := range s.LabelFileRefs {
					switch {
					case lfr.ReadErr != nil, !lfr.Exists, len(lfr.Labels) == 0:
						emit("file", "")
					default:
						for _, l := range lfr.Labels {
							emit("file", l.Key+"="+l.Value)
						}
					}
				}

				for _, c := range s.LabelFileConflicts {
					parts := make([]string, len(c.Occurrences))
					for i, o := range c.Occurrences {
						parts[i] = fmt.Sprintf("%s=%s (%s)", c.Key, o.Value, o.Path)
					}
					emit("file", strings.Join(parts, "; "))
				}
			}
		}
	}

	return rows
}

// wrapService breaks a service name into lines no wider than max using the
// delimiter priority required by the spec: dot, hyphen, underscore, then
// hard character break.
func wrapService(name string, max int) []string {
	if len(name) <= max {
		return []string{name}
	}

	var lines []string
	remaining := name
	for len(remaining) > max {
		chunk := remaining[:max]
		breakIdx := -1
		for i := max - 1; i >= 0; i-- {
			if chunk[i] == '.' {
				breakIdx = i + 1
				break
			}
		}
		if breakIdx < 0 {
			for i := max - 1; i >= 0; i-- {
				if chunk[i] == '-' {
					breakIdx = i + 1
					break
				}
			}
		}
		if breakIdx < 0 {
			for i := max - 1; i >= 0; i-- {
				if chunk[i] == '_' {
					breakIdx = i + 1
					break
				}
			}
		}
		if breakIdx < 0 {
			breakIdx = max
		}
		lines = append(lines, remaining[:breakIdx])
		remaining = remaining[breakIdx:]
	}
	if remaining != "" {
		lines = append(lines, remaining)
	}
	return lines
}

// wrapLabel breaks a merged LABEL=VALUE string into lines no wider than
// max. The preferred break points are punctuation characters (= , ;).
func wrapLabel(label string, max int) []string {
	if len(label) <= max {
		return []string{label}
	}

	var lines []string
	remaining := label
	for len(remaining) > max {
		breakIdx := -1
		for i := max - 1; i >= 0; i-- {
			if remaining[i] == '=' || remaining[i] == ',' || remaining[i] == ';' {
				breakIdx = i + 1
				break
			}
		}
		if breakIdx < 0 {
			breakIdx = max
		}
		lines = append(lines, remaining[:breakIdx])
		remaining = remaining[breakIdx:]
	}
	if remaining != "" {
		lines = append(lines, remaining)
	}
	return lines
}

// truncateString shortens a string to fit within max, appending an
// ellipsis when truncation occurs.
func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

// truncateFile shortens a path to fit within the FILE column, replacing
// the middle with an ellipsis.
func truncateFile(path string, max int) string {
	if len(path) <= max {
		return path
	}
	if max <= 3 {
		return path[:max]
	}
	// Keep the leading directory component and the basename when possible.
	base := filepath.Base(path)
	dir := filepath.Dir(path)
	if len(base) >= max-3 {
		return "..." + base[intMax(max-3-len(base), 0):]
	}
	wantDir := max - len(base) - 3
	if wantDir <= 0 {
		return "..." + base
	}
	return dir[:wantDir] + "..." + base
}

func intMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// renderTable writes reports as a single plaintext table spanning every
// project. Columns are aligned with tabwriter and constrained to the
// maximum widths defined by the tableWidth* constants. When noWrap is
// true, fields are emitted exactly as they are without wrapping.
func renderTable(w io.Writer, reports []ProjectReport, noWrap bool) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	fmt.Fprintln(tw, "PROJECT\tFILE\tSERVICE\tLABEL\tSOURCE")

	rows := buildTableRows(reports)
	for ri, row := range rows {
		projectLines := []string{truncateString(row.project, tableWidthProject)}
		fileLines := []string{truncateFile(row.file, tableWidthFile)}
		serviceLines := []string{row.service}
		labelLines := []string{row.label}

		if !noWrap {
			serviceLines = wrapService(row.service, tableWidthService)
			labelLines = wrapLabel(row.label, tableWidthLabel)
		}

		n := len(projectLines)
		if len(fileLines) > n {
			n = len(fileLines)
		}
		if len(serviceLines) > n {
			n = len(serviceLines)
		}
		if len(labelLines) > n {
			n = len(labelLines)
		}

		for i := 0; i < n; i++ {
			project := ""
			if i < len(projectLines) {
				project = projectLines[i]
			}
			file := ""
			if i < len(fileLines) {
				file = fileLines[i]
			}
			service := ""
			if i < len(serviceLines) {
				service = serviceLines[i]
			}
			source := ""
			if i == 0 {
				source = row.source
			}
			label := ""
			if i < len(labelLines) {
				label = labelLines[i]
			}

			fmt.Fprintf(tw, "%-20s\t%-20s\t%-20s\t%-45s\t%-6s\n",
				project, file, service, label, source)
		}

		// Emit a blank line after each complete project block. A project
		// block ends when the next row belongs to a different project or
		// this is the last row.
		if ri == len(rows)-1 || rows[ri+1].project != "" {
			fmt.Fprintln(tw)
		}
	}

	// tw.Flush()'s only failure mode is the underlying writer's own write
	// failing — the same rare, unactionable case fmt.Fprint's own
	// errcheck exclusion covers (.golangci.yml), just via tabwriter
	// instead of fmt directly. Explicitly ignored, not overlooked.
	_ = tw.Flush()
}
