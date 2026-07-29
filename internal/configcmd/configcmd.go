// Package configcmd implements the `config` command's rendering
// (SPECIFICATION.md §7.7): displaying resolved configuration values,
// validating configuration sources, and helping debug resolution.
// internal/config.Load remains the single source of truth for what the
// actual resolved values are — this package only formats what Load
// (and internal/config.Sources) already computed. It never modifies
// configuration, per §7.7's explicit "shall not modify configuration
// unless explicitly extended in future versions."
package configcmd

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/neutralvibes/daplabel/internal/config"
)

// validSurveyFormats is the set of format strings survey.Run accepts.
// It is checked by configcmd.Render to annotate invalid
// DAPLABEL_DEFAULT_SURVEY_FORMAT values in `daplabel config` output.
var validSurveyFormats = map[string]bool{
	"tree":  true,
	"plain": true,
	"table": true,
	"json":  true,
}

// namedValue pairs a configuration key's name with its resolved Value,
// so Render can walk them in a fixed, documented order
// (SPECIFICATION.md §5.4's table order) rather than Go struct field
// order happening to match by coincidence.
type namedValue struct {
	name  string
	value config.Value
}

// Render writes cfg's resolved values and sources' file-discovery
// detail to w, as plain text — no markdown syntax, aligned with
// text/tabwriter, the same approach `survey`'s table format uses
// (DECISIONS.md Decision 37's amendment) and for the same reason: this
// output is meant to be read directly in a terminal, not rendered
// somewhere else first.
func Render(w io.Writer, cfg *config.Config, sources []config.FileSource) {
	values := []namedValue{
		{"DAPLABEL_PARENT_DIR", cfg.ParentDir},
		{"DAPLABEL_TEMPLATE_DIR", cfg.TemplateDir},
		{"DAPLABEL_EDITOR", cfg.Editor},
		{"DAPLABEL_LIST_SAFE", cfg.ListSafe},
		{"DAPLABEL_DEFAULT_SURVEY_FORMAT", cfg.DefaultSurveyFormat},
		{"DAPLABEL_CONFIRM_TIMEOUT", cfg.ConfirmTimeout},
	}

	fmt.Fprintln(w, "Configuration:")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, nv := range values {
		display := nv.value.Value
		if display == "" {
			display = "(empty)"
		}
		if nv.name == "DAPLABEL_DEFAULT_SURVEY_FORMAT" && display != "(empty)" && !validSurveyFormats[display] {
			display += "\t# ERROR see survey --help for valid formats"
		}
		fmt.Fprintf(tw, "  %s\t=\t%s\t(%s)\n", nv.name, display, nv.value.Source)
	}
	// See internal/survey/render_table.go's identical comment: an
	// unactionable, rare write failure, deliberately ignored the same
	// way fmt.Fprint's own errcheck exclusion treats it.
	_ = tw.Flush()

	if len(sources) == 0 {
		return
	}
	fmt.Fprintln(w, "\nConfiguration sources checked:")
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, s := range sources {
		status := "not found"
		switch {
		case s.Used:
			status = "found, used"
		case s.Exists:
			status = "found, not used (no recognised keys, or a higher-precedence file already won)"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", tierLabel(s.Tier), s.Path, status)
	}
	_ = tw.Flush() // deliberately ignored — see the comment on the first Flush above
}

func tierLabel(t config.Source) string {
	switch t {
	case config.SourceSystemFile:
		return "system file"
	case config.SourceUserFile:
		return "user file"
	default:
		return t.String()
	}
}
