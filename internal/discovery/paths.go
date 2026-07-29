// Package discovery implements Compose project discovery.
package discovery

import "strings"

// ExpandCommaSeparated splits each string in args on ',' and returns the
// flattened result, trimming whitespace and skipping empty entries (e.g.
// trailing comma). Strings without a comma pass through unchanged.
//
// This is a pure string-splitting operation — validation (directory
// existence, etc.) is already handled by ResolveTargets's normal flow,
// so combining splitting with validation here would duplicate that logic
// and violate DRY (Engineering Principles §2.2).
func ExpandCommaSeparated(args []string) []string {
	var out []string
	for _, a := range args {
		if !strings.Contains(a, ",") {
			out = append(out, a)
			continue
		}
		for _, part := range strings.Split(a, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
}
