// Command daplabel is the entrypoint for the daplabel CLI.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/neutralvibes/daplabel/internal/atomicwrite"
	"github.com/neutralvibes/daplabel/internal/cliapp"
	"github.com/neutralvibes/daplabel/internal/lockfile"
)

// These are populated at build time via -ldflags, e.g.:
//
//	go build -ldflags "-X main.version=v1.0.0 -X main.commit=$(git rev-parse HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// GoReleaser injects these automatically as part of its standard build-info
// support (DECISIONS.md Decision 27's consequences). Left as "dev"/"none"/
// "unknown" for local `go build`/`go run` so --version is never blank.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	repo    = "https://github.com/neutralvibes/daplabel"
)

func main() {
	root := cliapp.NewRootCmd(cliapp.BuildInfo{Version: version, Commit: commit, Date: date, Repo: repo})
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code := 1 // SPECIFICATION.md §10.1: general error, the default
		// Check the most specific error type first: cobra ExitError
		// carries an explicit exit code from the command layer.
		var exitErr *cliapp.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.Code
		}
		// A dirty rollback (Decision 45) is a file system error —
		// exit code 4 per SPECIFICATION.md §10.1. Check after ExitError
		// in case the command wrapped it.
		var rfe *atomicwrite.RollbackFailedError
		if errors.As(err, &rfe) {
			code = 4
		}
		// Lock contention is exit code 6 (SPECIFICATION.md §10.1).
		// Check after the more specific error types above.
		var lce *lockfile.LockContentionError
		if errors.As(err, &lce) {
			code = 6
		}
		os.Exit(code)
	}
}
