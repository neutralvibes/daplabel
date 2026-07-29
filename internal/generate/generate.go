// Package generate implements the `generate` command
// (SPECIFICATION.md §7.2): extracting inline service labels from
// Compose files and writing them to external label files.
//
// Unlike `add`/`remove` (one service, one Compose file, named
// explicitly), `generate` operates the way `survey` does — across every
// Compose file discovery finds under one or more paths, optionally
// recursively — since its job is bulk migration of whatever inline
// labels already exist, not a targeted edit to one thing the user
// names.
//
// Per service:
//   - if it has no existing label_file references, extracted labels go
//     into a newly created <service>.labels (§2.5's naming convention),
//     which then gets referenced;
//   - if it already has one or more, they're appended to the first,
//     exactly as `add` does (DECISIONS.md Decision 38's default path) —
//     generate and add share the same target-selection rule, since both
//     are "get a label into a service's external label file" at heart.
//
// Two services can reference (or end up creating) the very same target
// label file; when that happens their extracted labels are merged into
// one label_file write rather than each service overwriting the
// other's — see the byPath grouping in processFile.
package generate

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/neutralvibes/daplabel/internal/atomicwrite"
	"github.com/neutralvibes/daplabel/internal/debuglog"
	"github.com/neutralvibes/daplabel/internal/discovery"
	"github.com/neutralvibes/daplabel/internal/labelfile"
	"github.com/neutralvibes/daplabel/internal/labelstate"
	"github.com/neutralvibes/daplabel/internal/lockfile"
	"github.com/neutralvibes/daplabel/internal/orchestrate"
	"github.com/neutralvibes/daplabel/internal/writeops"
	"github.com/neutralvibes/daplabel/internal/yamlbackend"
)

// Options holds generate's own flags, on top of the global options
// every write-path command shares.
type Options struct {
	Force          bool   // --force: overwrite an existing key already present in the target label file
	OnConflict     string // --on-conflict: first|last|skip (SPECIFICATION.md §8.2.1)
	DryRun         bool
	Yes            bool
	DebugLog       *debuglog.Logger // --debug-log: opt-in timestamped debug trace (nil = no-op)
	ConfirmTimeout time.Duration    // timeout for the confirmation prompt while the lock is held
}

// Run extracts inline labels for every service discovery finds under
// paths (recursive controls whether immediate subdirectories are
// scanned too, matching survey's own --recursive). Exit codes follow
// SPECIFICATION.md §10.1, matching survey's conventions for discovery
// errors.
func Run(stdout, stderr io.Writer, stdin io.Reader, paths []string, recursive bool, opts Options) (code int, err error) {
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

	hadErrors, oerr := orchestrate.ForEachTarget(stderr, paths, recursive, opts.Yes, func(target orchestrate.Target, effectiveYes bool) (bool, error) {
		fileOpts := opts
		fileOpts.Yes = effectiveYes
		return processFile(stdout, stderr, stdin, target.ComposeFile, fileOpts)
	})
	if oerr != nil {
		return 4, oerr
	}
	if hadErrors {
		return 1, fmt.Errorf("one or more Compose files had errors during generate")
	}
	return 0, nil
}

// serviceWork is one service's candidate extraction, computed before any
// file is touched.
type serviceWork struct {
	service    string
	targetRef  string
	targetPath string
	refAdded   bool // true if this service has no existing label_file reference yet
	labels     []labelfile.Label
}

func processFile(stdout, stderr io.Writer, stdin io.Reader, composeFile string, opts Options) (bool, error) {
	services, err := yamlbackend.GetServices(composeFile)
	if err != nil {
		return false, fmt.Errorf("parsing %s: %w", composeFile, err)
	}

	var plan []serviceWork
	for _, service := range services {
		labels, lerr := yamlbackend.GetLabels(composeFile, service)
		if lerr != nil {
			return false, fmt.Errorf("reading labels for %s: %w", service, lerr)
		}
		if len(labels) == 0 {
			continue
		}

		refs, rerr := yamlbackend.GetLabelFileRefs(composeFile, service)
		if rerr != nil {
			return false, fmt.Errorf("reading label_file refs for %s: %w", service, rerr)
		}

		var targetRef, targetPath string
		refAdded := false
		if len(refs) == 0 {
			targetRef = service + ".labels"
			targetPath = filepath.Join(filepath.Dir(composeFile), targetRef)
			refAdded = true
			opts.DebugLog.Logf("generate %s/%s: no existing label_file reference; will create %s", composeFile, service, targetRef)
		} else {
			targetRef = refs[0]
			targetPath = discovery.ResolveLabelFileRef(composeFile, targetRef)
		}

		// §8.2.1: if the service has multiple label_file references,
		// detect and resolve cross-file key conflicts. The merged base
		// determines what keys "already exist" for the purpose of the
		// force/skip check on inline labels being migrated.
		mergedExisting, conflicts, lerr := labelstate.MergedMap(composeFile, service, opts.OnConflict)
		if lerr != nil {
			return false, lerr
		}
		for _, c := range conflicts {
			var filePaths []string
			for _, o := range c.Occurrences {
				filePaths = append(filePaths, o.Path)
			}
			fmt.Fprintf(stderr, "Warning: %s: key %q has conflicting values across label files %v; resolved per --on-conflict=%s\n",
				service, c.Key, filePaths, opts.OnConflict)
		}

		// Filter inline labels against the merged existing state (if
		// multi-ref) or let WriteLabels handle existence checks (single-ref).
		var toMigrate []labelfile.Label
		for _, l := range labels {
			if mergedExisting != nil {
				if _, exists := mergedExisting[l.Key]; exists && !opts.Force {
					fmt.Fprintf(stderr, "Warning: %s: label %q already exists across label files; not overwriting (use --force); left inline\n",
						service, l.Key)
					opts.DebugLog.Logf("generate %s/%s: key %q already exists across label files; left inline (force=%v)", composeFile, service, l.Key, opts.Force)
					continue
				}
			}
			toMigrate = append(toMigrate, labelfile.Label{Key: l.Key, Value: l.Value})
		}
		if len(toMigrate) == 0 {
			continue
		}
		plan = append(plan, serviceWork{
			service: service, targetRef: targetRef, targetPath: targetPath,
			refAdded: refAdded, labels: toMigrate,
		})
	}

	if len(plan) == 0 {
		fmt.Fprintf(stdout, "%s: nothing to generate (no services have inline labels)\n", composeFile)
		return false, nil
	}

	// Group by target path so two services sharing one label_file get
	// one merged WriteLabels call, not two independent ones each
	// reading the same stale on-disk content and clobbering the other.
	var pathOrder []string
	byPath := map[string][]labelfile.Label{}
	for _, sw := range plan {
		if _, ok := byPath[sw.targetPath]; !ok {
			pathOrder = append(pathOrder, sw.targetPath)
		}
		byPath[sw.targetPath] = append(byPath[sw.targetPath], sw.labels...)
	}

	// Pre-compute which keys WriteLabels will skip (because they already
	// exist in the target file). The actual label file content is computed
	// after the YAML removal step, so failed removals don't leak their
	// labels into a file that will still be referenced inline.
	skippedByPath := map[string]map[string]bool{}
	for _, path := range pathOrder {
		_, skipped, werr := labelfile.WriteLabels(path, byPath[path], opts.Force)
		if werr != nil {
			return false, werr
		}
		skipSet := make(map[string]bool, len(skipped))
		for _, k := range skipped {
			skipSet[k] = true
		}
		skippedByPath[path] = skipSet
	}

	root, lerr := yamlbackend.LoadRootForEdit(composeFile)
	if lerr != nil {
		return false, lerr
	}

	touchedPaths := map[string]bool{}
	succeededByPath := map[string][]labelfile.Label{}
	var summary strings.Builder
	fmt.Fprintf(&summary, "%s:\n", composeFile)
	anyChanged := false

	for _, sw := range plan {
		skip := skippedByPath[sw.targetPath]
		var removeKeys []string
		var keptLabels []labelfile.Label
		for _, l := range sw.labels {
			if skip[l.Key] {
				fmt.Fprintf(stderr, "Warning: %s: label %q already exists in %s; not overwriting (use --force); left inline\n",
					sw.service, l.Key, sw.targetPath)
				continue
			}
			removeKeys = append(removeKeys, l.Key)
			keptLabels = append(keptLabels, l)
		}
		if len(removeKeys) == 0 {
			continue
		}

		removed, rerr := yamlbackend.RemoveInlineLabelKeys(root, sw.service, removeKeys)
		if rerr != nil {
			fmt.Fprintf(stderr, "Warning: %s: %v; left inline\n", sw.service, rerr)
			continue
		}
		if len(removed) == 0 {
			continue
		}
		if sw.refAdded {
			if _, serr := yamlbackend.SetLabelFileRef(root, sw.service, sw.targetRef); serr != nil {
				return false, serr
			}
		}

		succeededByPath[sw.targetPath] = append(succeededByPath[sw.targetPath], keptLabels...)
		touchedPaths[sw.targetPath] = true
		anyChanged = true
		note := ""
		if sw.refAdded {
			note = " (new label_file reference)"
		}
		fmt.Fprintf(&summary, "  %s: %d label(s) -> %s%s\n", sw.service, len(removed), sw.targetPath, note)
		opts.DebugLog.Logf("generate %s/%s: migrated %d label(s) to %s%s", composeFile, sw.service, len(removed), sw.targetPath, note)
	}

	if !anyChanged {
		fmt.Fprintf(stdout, "%s: nothing to generate (every extractable label already exists at its target; use --force to overwrite)\n", composeFile)
		return false, nil
	}

	var set atomicwrite.Set
	for _, path := range pathOrder {
		if !touchedPaths[path] {
			continue
		}
		content, _, werr := labelfile.WriteLabels(path, succeededByPath[path], opts.Force)
		if werr != nil {
			return false, werr
		}
		set.Add(path, content)
	}
	set.PreserveOwner(composeFile)
	composeBytes, merr := yamlbackend.Marshal(root)
	if merr != nil {
		return false, fmt.Errorf("encoding %s: %w", composeFile, merr)
	}
	set.Add(composeFile, composeBytes)

	_, allSelected, cerr := writeops.Commit(stdout, stderr, stdin, &set, composeFile, true, opts.DryRun, opts.Yes, strings.TrimRight(summary.String(), "\n"), opts.ConfirmTimeout)
	return allSelected, cerr
}
