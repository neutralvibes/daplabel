// Package orchestrate provides shared multi-target command scaffolding:
// resolving user-supplied paths to concrete Compose files, printing
// discovery warnings, and iterating over targets with the "All" (batch
// confirmation) propagation pattern from SPECIFICATION.md §9.1/§9.2.
package orchestrate

import (
	"fmt"
	"io"

	"github.com/neutralvibes/daplabel/internal/discovery"
)

// Target is one concrete Compose file discovered from user-supplied paths.
type Target struct {
	ComposeFile string
}

// ForEachTarget resolves paths to concrete Compose files, prints discovery
// warnings to stderr, and calls fn for each target. The effectiveYes value
// passed to the first invocation is initialYes; if any invocation returns
// allSelected=true, subsequent invocations receive effectiveYes=true.
// Errors from fn are accumulated and printed as warnings; the function
// returns hadErrors=true if any target failed.
//
// If no targets are found, it returns an error suitable for SPECIFICATION.md
// §10.1's "no Compose files found" case (the caller decides the exit code).
func ForEachTarget(
	stderr io.Writer,
	paths []string,
	recursive bool,
	initialYes bool,
	fn func(target Target, effectiveYes bool) (allSelected bool, err error),
) (hadErrors bool, err error) {
	targets, warnings, derr := discovery.ResolveTargets(recursive, paths)
	if derr != nil {
		return false, derr
	}
	for _, w := range warnings {
		fmt.Fprintln(stderr, w.String())
	}
	if len(targets) == 0 {
		return false, fmt.Errorf("no valid Compose files found")
	}

	effectiveYes := initialYes
	for _, composeFile := range targets {
		selected, ferr := fn(Target{ComposeFile: composeFile}, effectiveYes)
		if selected {
			effectiveYes = true
		}
		if ferr != nil {
			fmt.Fprintf(stderr, "Warning: %s: %v\n", composeFile, ferr)
			hadErrors = true
		}
	}
	return hadErrors, nil
}
