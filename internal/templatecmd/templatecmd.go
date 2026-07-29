// Package templatecmd implements the `template` command's `list`,
// `create`, `edit`, and `remove` subcommands (SPECIFICATION.md §7.6).
// `template apply` isn't here — it's exactly `add --template NAME SERVICE
// [PATH]` with no direct labels, so cliapp calls internal/add directly
// for it rather than duplicating that logic behind a second entry point.
//
// A template is exactly a label file — plain KEY=VALUE lines — stored
// in a different directory (internal/template's package doc covers the
// substitution-variable behaviour that's the only actual difference).
// Create therefore reuses internal/labelfile.WriteLabels and
// internal/atomicwrite/internal/writeops the same way `add` does,
// except there's no Compose file involved at all, so no `docker compose`
// config validation is ever needed for a template write.
package templatecmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/neutralvibes/daplabel/internal/atomicwrite"
	"github.com/neutralvibes/daplabel/internal/labelfile"
	"github.com/neutralvibes/daplabel/internal/lockfile"
	"github.com/neutralvibes/daplabel/internal/prompt"
	"github.com/neutralvibes/daplabel/internal/writeops"
)

// List returns the sorted names of templates available in dir. A dir
// that doesn't exist yet (no templates created there so far) is a
// legitimate empty result, not an error — matching this project's usual
// treatment of "nothing there yet."
func List(dir string) ([]string, error) {
	if dir == "" {
		return nil, fmt.Errorf("no template directory configured (DAPLABEL_TEMPLATE_DIR)")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading template directory %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// Create writes labels as KEY=VALUE lines into a template named name in
// dir (SPECIFICATION.md §7.6's "creating new templates from existing
// label sets"), creating dir itself if it doesn't exist yet — unlike a
// project's label files (scoped per-project, never invented a directory
// for), the template directory is one location the user has already
// configured or defaulted to, so creating it here is low-risk and
// expected the first time `create` is ever run.
//
// Calling create again for a name that already exists merges rather
// than replacing wholesale: an existing key is left untouched unless
// force is true (mirroring add's own "existing labels shall not be
// overwritten unless explicitly requested"), and a new key is appended.
// SPECIFICATION.md doesn't say which behaviour create should have here;
// this was chosen for consistency with every other write path in this
// tool rather than being a special case (DECISIONS.md Decision 41).
//
// If labels is empty, an empty template file is created; if the template
// already exists it is left untouched (there is nothing to merge).
func Create(stdout, stderr io.Writer, stdin io.Reader, dir, name string, labels []labelfile.Label, force, dryRun, yes bool, confirmTimeout time.Duration) (code int, err error) {
	release, lerr := lockfile.Acquire()
	if lerr != nil {
		return 6, lerr
	}
	defer release()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		release()
		os.Exit(130)
	}()
	defer signal.Stop(sigCh)

	if dir == "" {
		return 2, fmt.Errorf("no template directory configured (DAPLABEL_TEMPLATE_DIR)")
	}
	if name == "" {
		return 2, fmt.Errorf("template name must not be empty")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 1, fmt.Errorf("creating template directory %s: %w", dir, err)
	}

	path := filepath.Join(dir, name)

	// Empty create: ensure the file exists, but leave existing content
	// untouched. There is nothing to force-overwrite.
	if len(labels) == 0 {
		if _, statErr := os.Stat(path); statErr == nil {
			fmt.Fprintf(stdout, "Template %q already exists; nothing to add.\n", name)
			return 0, nil
		}

		var set atomicwrite.Set
		set.Add(path, []byte{})
		summary := fmt.Sprintf("Template %q (%s):\n  (empty)", name, path)
		commitCode, _, cerr := writeops.Commit(stdout, stderr, stdin, &set, "", false, dryRun, yes, summary, confirmTimeout)
		if commitCode != 0 {
			return commitCode, cerr
		}
		return 0, nil
	}

	content, skipped, werr := labelfile.WriteLabels(path, labels, force)
	if werr != nil {
		return 1, werr
	}
	skipSet := make(map[string]bool, len(skipped))
	for _, k := range skipped {
		skipSet[k] = true
		fmt.Fprintf(stderr, "Warning: label %q already exists in template %q; not overwriting (use --force)\n", k, name)
	}

	if len(skipped) == len(labels) {
		fmt.Fprintln(stdout, "No labels to add; template unchanged.")
		return 0, nil
	}

	var set atomicwrite.Set
	set.Add(path, content)

	var summary strings.Builder
	fmt.Fprintf(&summary, "Template %q (%s):\n", name, path)
	for _, l := range labels {
		if skipSet[l.Key] {
			continue
		}
		fmt.Fprintf(&summary, "  + %s=%s\n", l.Key, l.Value)
	}

	// No Compose file is involved in a template write, so composeChanged
	// is always false — Commit never calls docker compose config here.
	commitCode, _, cerr := writeops.Commit(stdout, stderr, stdin, &set, "", false, dryRun, yes, strings.TrimRight(summary.String(), "\n"), confirmTimeout)
	if commitCode != 0 {
		return commitCode, cerr
	}
	return 0, nil
}

// Edit opens name in dir with editor. The template must already exist;
// this command does not create a template. If editor is empty it defaults
// to "nano".
//
// The editor process inherits this process's stdin/stdout/stderr so it
// has a real terminal to work with. No backup is created: the edit is a
// user-driven editor session rather than an automated modification.
func Edit(dir, name, editor string) error {
	if os.Getenv("SUDO_USER") != "" {
		return fmt.Errorf(
			"SECURITY-RISK: template edit is not available under sudo, since it would run " +
				"$EDITOR as root using the invoking user's own configuration; " +
				"edit the template directly as yourself, or use `sudoedit`")
	}
	if dir == "" {
		return fmt.Errorf("no template directory configured (DAPLABEL_TEMPLATE_DIR)")
	}
	if name == "" {
		return fmt.Errorf("template name must not be empty")
	}

	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("template %q not found in %s", name, dir)
		}
		return fmt.Errorf("checking template %s: %w", path, err)
	}

	if editor == "" {
		editor = "nano"
	}

	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running editor %q: %w", editor, err)
	}
	return nil
}

// Remove deletes name from dir. It prompts for confirmation unless yes
// is true, and shows the planned action without deleting when dryRun is
// true.
func Remove(stdout, stderr io.Writer, stdin io.Reader, dir, name string, dryRun, yes bool) (code int, err error) {
	if dir == "" {
		return 2, fmt.Errorf("no template directory configured (DAPLABEL_TEMPLATE_DIR)")
	}
	if name == "" {
		return 2, fmt.Errorf("template name must not be empty")
	}

	path := filepath.Join(dir, name)
	if _, statErr := os.Stat(path); statErr != nil {
		if os.IsNotExist(statErr) {
			return 4, fmt.Errorf("template %q not found in %s", name, dir)
		}
		return 4, fmt.Errorf("checking template %s: %w", path, statErr)
	}

	summary := fmt.Sprintf("Remove template %q (%s)", name, path)
	fmt.Fprintln(stdout, summary)

	if dryRun {
		fmt.Fprintln(stdout, "(dry run: no files were changed)")
		return 0, nil
	}

	if !yes {
		switch prompt.Confirm(stdout, stdin, "Remove this template?") {
		case prompt.No:
			fmt.Fprintln(stdout, "Skipped.")
			return 0, nil
		case prompt.Quit:
			fmt.Fprintln(stdout, "Aborted.")
			return 0, nil
		case prompt.Yes, prompt.All:
			// proceed
		}
	}

	if err := os.Remove(path); err != nil {
		return 4, fmt.Errorf("removing %s: %w", path, err)
	}
	fmt.Fprintln(stdout, "Done.")
	return 0, nil
}
