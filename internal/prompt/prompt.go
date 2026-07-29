// Package prompt implements SPECIFICATION.md §9.1/§9.2's standard
// confirmation prompt (Y/N/Q/A), shared by every command that modifies
// files.
package prompt

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"
)

// Response is a user's answer to a confirmation prompt.
type Response int

const (
	// No skips the current operation (§9.2: "N or n shall skip the
	// current operation").
	No Response = iota
	// Yes proceeds with the current operation only.
	Yes
	// Quit terminates execution immediately (§9.2: "Q or q shall
	// terminate execution immediately").
	Quit
	// All proceeds with this and every remaining operation in the
	// current command execution without further prompts (§9.1/§9.2).
	All
	// TimedOut means no input was received within the timeout period.
	// Used only by ConfirmTimeout while the system-wide lock is held,
	// so an unattended terminal cannot hold that lock indefinitely.
	TimedOut
)

// Confirm writes message (expected to already describe the action and
// its consequences, per §9.1) followed by a "[Y]es | [N]o | [A]ll | [Q]uit" cue
// to out, then reads and parses a single line of input from in.
//
// An empty line (just pressing enter) takes the default, No — §9.1
// requires prompts to "provide a clear default option," and skipping
// rather than proceeding is the safer default for an operation that
// modifies files (SPECIFICATION.md §3.1 "Safety"). Unrecognised input is
// re-prompted rather than silently defaulted, so a mistyped response
// can't be misread as an intentional skip. Reaching end-of-input without
// a recognised response (e.g. stdin closed, non-interactive with no
// --yes) is treated as Quit — aborting is the safe choice when no further
// input is possible, matching §3.6's "it shall not infer intent
// silently."
func Confirm(out io.Writer, in io.Reader, message string) Response {
	return confirm(out, in, message, nil)
}

// ConfirmTimeout behaves like Confirm, but aborts and returns TimedOut
// if no input is received within d. It is used only by the write-path
// confirmation that runs while the system-wide lock (internal/lockfile)
// is held, so an unattended terminal doesn't hold that lock
// indefinitely. On timeout it prints a distinct message so the user can
// tell timeout apart from a manual decline.
func ConfirmTimeout(out io.Writer, in io.Reader, message string, d time.Duration) Response {
	return confirm(out, in, message, &d)
}

func confirm(out io.Writer, in io.Reader, message string, timeout *time.Duration) Response {
	reader := bufio.NewReader(in)
	for {
		fmt.Fprintf(out, "%s\nAction: [Y]es | [N]o | [A]ll | [Q]uit: ", message)

		var line string
		var err error
		if timeout != nil {
			lineCh := make(chan string, 1)
			errCh := make(chan error, 1)
			go func() {
				l, e := reader.ReadString('\n')
				lineCh <- l
				errCh <- e
			}()
			select {
			case <-time.After(*timeout):
				fmt.Fprintf(out, "Confirmation timed out after %s; no changes were made.\n", *timeout)
				return TimedOut
			case line = <-lineCh:
				err = <-errCh
			}
		} else {
			line, err = reader.ReadString('\n')
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" && err != nil {
			return Quit
		}
		switch strings.ToLower(trimmed) {
		case "":
			return No
		case "y":
			return Yes
		case "n":
			return No
		case "q":
			return Quit
		case "a":
			return All
		}
		fmt.Fprintf(out, "Please answer Y(es), N(o), A(ll), or Q(uit).\n")
		if err != nil {
			return Quit
		}
	}
}
