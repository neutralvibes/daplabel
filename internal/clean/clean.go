// Package clean implements the `clean` command: removing .pre-label backup
// files created by other write-path commands (SPECIFICATION.md §8.4).
//
// It is a deletion-only operation: it never reads or modifies Docker Compose
// files, label files, or YAML structure. It only removes backup files in
// directories that contain recognised Compose files.
package clean

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neutralvibes/daplabel/internal/discovery"
	"github.com/neutralvibes/daplabel/internal/prompt"
)

// Options holds clean's flags, on top of the global options every
// write-path command shares.
type Options struct {
	DryRun bool // --dry-run: show files without removing them
	Yes    bool // -y/--yes: skip the confirmation prompt
}

// Run removes .pre-label backup files from every project directory found
// under paths (recursive controls whether immediate subdirectories are
// scanned too, matching survey's and generate's --recursive). It walks
// directories one at a time, prompting per directory, and propagates an
// "All" response to skip prompts for the remaining directories. Exit codes
// follow SPECIFICATION.md §10.1.
func Run(stdout, stderr io.Writer, stdin io.Reader, paths []string, recursive bool, opts Options) (code int, err error) {
	if len(paths) == 0 {
		paths = []string{"."}
	}

	targets, warnings, derr := discovery.ResolveTargets(recursive, paths)
	if derr != nil {
		return 4, derr
	}
	for _, w := range warnings {
		fmt.Fprintln(stderr, w.String())
	}
	if len(targets) == 0 {
		return 4, fmt.Errorf("no valid Compose files found")
	}

	effectiveYes := opts.Yes
	totalFound := 0
	hadErrors := false

	for _, composeFile := range targets {
		dir := filepath.Dir(composeFile)
		files, rerr := findPreLabelFiles(dir)
		if rerr != nil {
			fmt.Fprintf(stderr, "Warning: %s: %v\n", dir, rerr)
			hadErrors = true
			continue
		}
		if len(files) == 0 {
			continue
		}
		totalFound += len(files)

		fmt.Fprintf(stdout, "\n%s:\n", dir)
		for _, f := range files {
			fmt.Fprintf(stdout, "  %s\n", f)
		}

		if opts.DryRun {
			fmt.Fprintln(stdout, "(dry run: no files were removed)")
			continue
		}

		proceed := effectiveYes
		if !effectiveYes {
			msg := fmt.Sprintf("Remove %d .pre-label backup file(s) from %s?", len(files), dir)
			switch prompt.Confirm(stdout, stdin, msg) {
			case prompt.No:
				fmt.Fprintln(stdout, "Skipped.")
				continue
			case prompt.Quit:
				fmt.Fprintln(stdout, "Aborted.")
				return 0, nil
			case prompt.All:
				effectiveYes = true
				proceed = true
			case prompt.Yes:
				proceed = true
			}
		}

		if !proceed {
			continue
		}

		removed := 0
		for _, f := range files {
			path := filepath.Join(dir, f)
			if rerr := os.Remove(path); rerr != nil {
				fmt.Fprintf(stderr, "Warning: %s: %v\n", path, rerr)
				hadErrors = true
				continue
			}
			removed++
		}
		fmt.Fprintf(stdout, "Removed %d .pre-label backup file(s) from %s.\n", removed, dir)
	}

	if totalFound == 0 {
		fmt.Fprintln(stdout, "No .pre-label backup files found.")
		return 0, nil
	}

	if hadErrors {
		return 1, fmt.Errorf("one or more backup files could not be removed")
	}
	return 0, nil
}

// findPreLabelFiles returns every regular file in dir whose name ends with
// the .pre-label backup suffix, sorted by name.
func findPreLabelFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".pre-label") {
			files = append(files, name)
		}
	}

	sort.Strings(files)
	return files, nil
}
