// Package remove implements the `remove` command (SPECIFICATION.md
// §7.4): removing one or more labels from a service.
//
// Unlike `add`, remove doesn't choose where to write something new — it
// has to find each requested key wherever it currently lives (inline,
// or in any of the service's label_file references — possibly more than
// one place at once, an inconsistent state add/generate wouldn't
// normally create, but not one remove should leave half-fixed either)
// and take it out from there. A key not found anywhere is reported, not
// treated as an error: SPECIFICATION.md doesn't require removing a label
// that was never there to fail.
//
// --on-empty-create governs what happens to a label_file emptied out by
// this removal (§8.6): by default it's deleted along with its
// label_file: reference, rather than leaving useless empty scaffolding
// behind; with the flag, the (now empty) file and its reference are both
// kept. This only applies to label_file — an emptied inline labels:
// mapping is always removed outright (DECISIONS.md Decision 40): there's
// no file/reference lifecycle for an inline block to optionally
// preserve, only a `labels: {}` with no purpose.
//
// --on-none-create and --values-no-quote are both accepted, for
// consistency with `add`'s flag set (and Decision 33's own "add/remove"
// phrasing), but have no distinct effect here: remove never creates a
// label_file that doesn't already exist (there's nothing to "create" a
// missing referenced file for — a key can't be removed from content that
// was never there either way), and remove never writes a new quoted
// value (only ever deletes existing key/value pairs, leaving every
// surviving entry's original style untouched).
package remove

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/neutralvibes/daplabel/internal/atomicwrite"
	"github.com/neutralvibes/daplabel/internal/debuglog"
	"github.com/neutralvibes/daplabel/internal/discovery"
	"github.com/neutralvibes/daplabel/internal/labelfile"
	"github.com/neutralvibes/daplabel/internal/lockfile"
	"github.com/neutralvibes/daplabel/internal/writeops"
	"github.com/neutralvibes/daplabel/internal/yamlbackend"
)

// Options holds remove's own flags, on top of the global options every
// write-path command shares.
type Options struct {
	OnEmptyCreate  bool // --on-empty-create, SPECIFICATION.md §8.6
	OnNoneCreate   bool // --on-none-create — accepted, no effect (see package doc)
	ValuesNoQuote  bool // --values-no-quote — accepted, no effect (see package doc)
	DryRun         bool
	Yes            bool
	DebugLog       *debuglog.Logger // --debug-log: opt-in timestamped debug trace (nil = no-op)
	ConfirmTimeout time.Duration    // timeout for the confirmation prompt while the lock is held
}

// refInfo is one of a service's label_file references, read once up
// front, before anything is decided or written.
type refInfo struct {
	ref      string
	path     string
	exists   bool
	keysHere map[string]bool
}

// Run removes keys from service in the Compose file at composeFile.
// Exit codes follow SPECIFICATION.md §10.1, matching add's conventions.
func Run(stdout, stderr io.Writer, stdin io.Reader, composeFile, service string, keys []string, opts Options) (code int, err error) {
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

	services, gerr := yamlbackend.GetServices(composeFile)
	if gerr != nil {
		return 5, fmt.Errorf("parsing %s: %w", composeFile, gerr)
	}
	if !contains(services, service) {
		return 2, fmt.Errorf("service %q not found in %s", service, composeFile)
	}
	if len(keys) == 0 {
		return 2, fmt.Errorf("no labels to remove: supply one or more LABEL keys")
	}

	inlineLabels, ierr := yamlbackend.GetLabels(composeFile, service)
	if ierr != nil {
		return 5, fmt.Errorf("parsing %s: %w", composeFile, ierr)
	}
	inlineSet := make(map[string]bool, len(inlineLabels))
	for _, l := range inlineLabels {
		inlineSet[l.Key] = true
	}

	refs, rerr := yamlbackend.GetLabelFileRefs(composeFile, service)
	if rerr != nil {
		return 5, fmt.Errorf("parsing %s: %w", composeFile, rerr)
	}

	var refInfos []refInfo
	for _, ref := range refs {
		path := discovery.ResolveLabelFileRef(composeFile, ref)
		labels, exists, lerr := labelfile.Read(path)
		if lerr != nil {
			return 1, lerr
		}
		if !exists {
			fmt.Fprintf(stderr, "Warning: %s: label_file %q does not exist; skipping it\n", service, ref)
			refInfos = append(refInfos, refInfo{ref: ref, path: path, exists: false})
			opts.DebugLog.Logf("remove %s/%s: referenced label_file %q missing; skipping", composeFile, service, ref)
			continue
		}
		keysHere := make(map[string]bool, len(labels))
		for _, l := range labels {
			keysHere[l.Key] = true
		}
		refInfos = append(refInfos, refInfo{ref: ref, path: path, exists: true, keysHere: keysHere})
	}

	// For each requested key, find every place it currently lives.
	var keysInline []string
	keysForRef := make(map[string][]string, len(refInfos)) // path -> keys to remove there
	var notFound []string
	for _, key := range keys {
		found := false
		if inlineSet[key] {
			keysInline = append(keysInline, key)
			found = true
		}
		for _, ri := range refInfos {
			if ri.exists && ri.keysHere[key] {
				keysForRef[ri.path] = append(keysForRef[ri.path], key)
				found = true
			}
		}
		if !found {
			notFound = append(notFound, key)
		}
	}
	for _, k := range notFound {
		fmt.Fprintf(stderr, "Warning: %s: label %q not found (inline or in any label_file); nothing to remove\n", service, k)
		opts.DebugLog.Logf("remove %s/%s: key %q not found in any source", composeFile, service, k)
	}

	if len(keysInline) == 0 && len(keysForRef) == 0 {
		fmt.Fprintln(stdout, "Nothing to remove.")
		return 0, nil
	}

	root, lerr := yamlbackend.LoadRootForEdit(composeFile)
	if lerr != nil {
		return 5, lerr
	}

	var set atomicwrite.Set
	set.PreserveOwner(composeFile)
	var summary strings.Builder
	fmt.Fprintf(&summary, "Service %q:\n", service)
	composeChanged := false

	if len(keysInline) > 0 {
		removedInline, cerr := yamlbackend.RemoveInlineLabelKeys(root, service, keysInline)
		if cerr != nil {
			fmt.Fprintf(stderr, "Warning: %s: %v; left inline\n", service, cerr)
		}
		for _, k := range removedInline {
			fmt.Fprintf(&summary, "  - %s (inline)\n", k)
			composeChanged = true
		}
	}

	for _, ri := range refInfos {
		toRemove, ok := keysForRef[ri.path]
		if !ok {
			continue
		}
		content, removed, werr := labelfile.RemoveLabels(ri.path, toRemove)
		if werr != nil {
			return 1, werr
		}
		for _, k := range removed {
			fmt.Fprintf(&summary, "  - %s -> %s\n", k, ri.path)
		}
		if len(removed) == 0 {
			continue
		}

		if strings.TrimSpace(string(content)) == "" {
			if opts.OnEmptyCreate {
				set.Add(ri.path, content)
			} else {
				set.Remove(ri.path)
				if _, serr := yamlbackend.RemoveLabelFileRef(root, service, ri.ref); serr != nil {
					return 1, serr
				}
				composeChanged = true
				fmt.Fprintf(&summary, "  (emptied %s; removing the file and its label_file reference — use --on-empty-create to keep it)\n", ri.path)
				opts.DebugLog.Logf("remove %s/%s: emptied %s; removing file and label_file reference", composeFile, service, ri.path)
			}
		} else {
			set.Add(ri.path, content)
		}
	}

	if composeChanged {
		composeBytes, merr := yamlbackend.Marshal(root)
		if merr != nil {
			return 1, fmt.Errorf("encoding %s: %w", composeFile, merr)
		}
		set.Add(composeFile, composeBytes)
	}

	commitCode, _, cerr := writeops.Commit(stdout, stderr, stdin, &set, composeFile, composeChanged, opts.DryRun, opts.Yes, strings.TrimRight(summary.String(), "\n"), opts.ConfirmTimeout)
	if commitCode != 0 {
		return commitCode, cerr
	}
	return 0, nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
