package survey

import (
	"fmt"
	"io"
	"path/filepath"
)

// renderPlain writes the compact, indented presentation of all
// accumulated reports under a single "Base Folder:" header.
//
// Layout (per project):
//
//	<projectName>
//	  <compose-file-basename>
//	    <service-name>
//	      [inline]
//	        key=value
//	      [file: ref]
//	        key=value
//	      (no labels)
func renderPlain(w io.Writer, basePath string, reports []ProjectReport) {
	fmt.Fprintf(w, "Base Folder: %s\n\n", basePath)

	for _, rpt := range reports {
		fmt.Fprintln(w, rpt.ProjectName)

		for _, f := range rpt.Files {
			fmt.Fprintf(w, "  %s\n", filepath.Base(f.Path))

			if f.Err != nil {
				fmt.Fprintf(w, "    ERROR: failed to read services (%v)\n", f.Err)
				continue
			}
			if len(f.Services) == 0 {
				fmt.Fprintln(w, "    "+textNoServicesFound)
				continue
			}

			for _, s := range f.Services {
				fmt.Fprintf(w, "    %s\n", s.Name)

				if s.Err != nil {
					fmt.Fprintf(w, "      ERROR: %v\n", s.Err)
					continue
				}

				if len(s.InlineLabels) == 0 && len(s.LabelFileRefs) == 0 {
					fmt.Fprintln(w, "      "+textNoLabels)
					continue
				}

				if len(s.InlineLabels) > 0 {
					fmt.Fprintln(w, "      "+textInlineTag)
					for _, l := range s.InlineLabels {
						fmt.Fprintf(w, "        %s=%s\n", l.Key, l.Value)
					}
				}

				for _, lfr := range s.LabelFileRefs {
					fmt.Fprintf(w, "      [file: %s]\n", lfr.Ref)
					switch {
					case lfr.ReadErr != nil:
						fmt.Fprintf(w, "        [ERROR: %v]\n", lfr.ReadErr)
					case !lfr.Exists:
						fmt.Fprintf(w, "        [%s]\n", textMissing)
					default:
						for _, l := range lfr.Labels {
							fmt.Fprintf(w, "        %s=%s\n", l.Key, l.Value)
						}
					}
				}
			}
		}
		fmt.Fprintln(w)
	}
}
