// Package writeops holds the confirm-then-commit flow shared by every
// write-path command (`add`, `generate`, and eventually `remove`/
// `template apply`/`template create`): display a summary (§3.3
// Transparency, §9.4 dry-run), stop there for --dry-run, ask for
// confirmation (§4.4/§9.1) unless --yes, validate a changed Compose
// file via the sole authority (internal/composevalidate, Decision
// 15/20/30) before anything commits, then write everything via
// internal/atomicwrite.
package writeops

import (
	"fmt"
	"io"
	"time"

	"github.com/neutralvibes/daplabel/internal/atomicwrite"
	"github.com/neutralvibes/daplabel/internal/composevalidate"
	"github.com/neutralvibes/daplabel/internal/prompt"
)

// Commit prints summary, then:
//   - stops immediately if dryRun, making no changes;
//   - otherwise asks Y/N/Q/A for confirmation unless yes is true;
//   - then commits set, validating composeFile's staged content via
//     `docker compose config` first if composeChanged (composeFile must
//     be one of set's staged paths in that case).
//
// The second return value (allSelected) is true when the user answered
// 'A' (All) to the confirmation prompt, signalling the caller to skip
// further prompts for the remainder of the current batch.
//
// confirmTimeout is applied to the confirmation prompt so that an
// unattended terminal cannot hold the system-wide lock indefinitely.
func Commit(stdout, stderr io.Writer, stdin io.Reader, set *atomicwrite.Set, composeFile string, composeChanged, dryRun, yes bool, summary string, confirmTimeout time.Duration) (int, bool, error) {
	fmt.Fprintln(stdout, summary)

	if dryRun {
		fmt.Fprintln(stdout, "(dry run: no files were changed)")
		return 0, false, nil
	}

	// A zero timeout means the caller didn't set one (e.g. tests, or a
	// configuration that didn't parse). Fall back to the 5-minute default
	// so the prompt doesn't expire immediately.
	if confirmTimeout == 0 {
		confirmTimeout = 5 * time.Minute
	}

	allSelected := false
	if !yes {
		switch prompt.ConfirmTimeout(stdout, stdin, "Proceed with this change?", confirmTimeout) {
		case prompt.No, prompt.TimedOut:
			// TimedOut is handled the same as No/decline: nothing was
			// written, and the caller will release the lock. The
			// timeout message has already been printed by ConfirmTimeout.
			fmt.Fprintln(stdout, "Skipped.")
			return 0, false, nil
		case prompt.Quit:
			fmt.Fprintln(stdout, "Aborted.")
			return 0, false, nil
		case prompt.All:
			allSelected = true
		case prompt.Yes:
			// proceed
		}
	}

	var validatePath string
	var validate func(string) error
	if composeChanged {
		validatePath = composeFile
		validate = composevalidate.ComposeConfig
	}
	if err := set.Commit(validatePath, validate); err != nil {
		return 5, false, fmt.Errorf("committing changes: %w", err)
	}
	fmt.Fprintln(stdout, "Done.")
	return 0, allSelected, nil
}
