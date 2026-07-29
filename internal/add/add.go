// Package add implements the `add` command (SPECIFICATION.md §7.3):
// adding one or more labels to a service, via direct input or a
// template.
//
// The default write target is an external label file, per §7.3: a
// service with no existing label_file references gets a newly created
// one (named per §2.5/Decision 21); a service that already has one or
// more gets the new keys appended to the first. DECISIONS.md Decision
// 38 covers the one place this reading needed an explicit resolution:
// Decision 33 documents an inline-write mode too (`--values-no-quote`
// only has meaning there), which this package exposes as an opt-in
// --inline flag rather than the default, since §7.3's label_file
// behaviour is the specific, unambiguous, and thoroughly documented
// default path.
//
// This package is deliberately independent of cliapp/cobra (matching
// internal/survey's shape) so its logic is directly unit-testable.
package add

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
	"github.com/neutralvibes/daplabel/internal/template"
	"github.com/neutralvibes/daplabel/internal/writeops"
	"github.com/neutralvibes/daplabel/internal/yamlbackend"
)

// Options holds add's own flags, layered on top of the global options
// every command shares (cliapp.Options: --dry-run, --yes, etc. — passed
// through into DryRun/Yes here so this package doesn't need to know
// about cliapp at all).
type Options struct {
	Force          bool   // --force: overwrite an existing key's value
	OnEmptyCreate  bool   // --on-empty-create, SPECIFICATION.md §8.6
	OnNoneCreate   bool   // --on-none-create, SPECIFICATION.md §8.7
	OnConflict     string // --on-conflict: first|last|skip (SPECIFICATION.md §8.2.1)
	ValuesNoQuote  bool   // --values-no-quote, Decision 33 (only meaningful with Inline)
	Inline         bool   // --inline, Decision 38: write into labels: directly instead of a label_file
	Template       string
	TemplateDir    string // config.Config.TemplateDir.Value
	DryRun         bool
	Yes            bool
	DebugLog       *debuglog.Logger // --debug-log: opt-in timestamped debug trace (nil = no-op)
	ConfirmTimeout time.Duration    // timeout for the confirmation prompt while the lock is held
	// SuppressExistingKeyWarnings is set by batch callers that already
	// emitted pre-prompt conflict warnings, so add.Run doesn't repeat
	// them after confirmation.
	SuppressExistingKeyWarnings bool
}

// LabelArg is one `KEY[=VALUE]` command-line argument, already split.
type LabelArg struct {
	Key   string
	Value string
}

// ParseLabelArg splits a `KEY=VALUE` (or bare `KEY`, meaning an empty
// value) command-line argument.
func ParseLabelArg(s string) LabelArg {
	key, value, _ := strings.Cut(s, "=")
	return LabelArg{Key: key, Value: value}
}

// Run adds labelArgs (and/or opts.Template's labels) to service in the
// Compose file at composeFile. stdin is read only for the Y/N/Q/A
// confirmation prompt (§9.1) when opts.Yes and opts.DryRun are both
// false. Exit codes follow SPECIFICATION.md §10.1.
func Run(stdout, stderr io.Writer, stdin io.Reader, composeFile, service string, labelArgs []LabelArg, opts Options) (code int, err error) {
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

	labels, rerr := resolveLabels(composeFile, service, labelArgs, opts)
	if rerr != nil {
		return 2, rerr
	}
	if len(labels) == 0 {
		return 2, fmt.Errorf("no labels to add: supply LABEL[=VALUE] arguments and/or --template")
	}

	if opts.Inline {
		return runInline(stdout, stderr, stdin, composeFile, service, labels, opts)
	}
	return runLabelFile(stdout, stderr, stdin, composeFile, service, labels, opts)
}

// DetectConflicts resolves labels (template + labelArgs) and emits a
// warning to stderr for each incoming key that already exists for
// service. It performs no writes and does not prompt; batch callers use
// it so the user sees conflict warnings before the confirmation prompt.
func DetectConflicts(stderr io.Writer, composeFile, service string, labelArgs []LabelArg, opts Options) error {
	services, gerr := yamlbackend.GetServices(composeFile)
	if gerr != nil {
		return fmt.Errorf("parsing %s: %w", composeFile, gerr)
	}
	if !contains(services, service) {
		return fmt.Errorf("service %q not found in %s", service, composeFile)
	}

	labels, err := resolveLabels(composeFile, service, labelArgs, opts)
	if err != nil {
		return err
	}
	if len(labels) == 0 {
		return nil
	}

	existingKeys := make(map[string]bool)
	if opts.Inline {
		inlineLabels, ierr := yamlbackend.GetLabels(composeFile, service)
		if ierr != nil {
			return fmt.Errorf("reading inline labels for %s: %w", service, ierr)
		}
		for _, l := range inlineLabels {
			existingKeys[l.Key] = true
		}
	} else {
		merged, _, err := labelstate.MergedMap(composeFile, service, opts.OnConflict)
		if err != nil {
			return err
		}
		for k := range merged {
			existingKeys[k] = true
		}
		if len(existingKeys) == 0 {
			refs, rerr := yamlbackend.GetLabelFileRefs(composeFile, service)
			if rerr != nil {
				return fmt.Errorf("parsing %s: %w", composeFile, rerr)
			}
			if len(refs) > 0 {
				p := discovery.ResolveLabelFileRef(composeFile, refs[0])
				readLabels, exists, rerr := labelfile.Read(p)
				if rerr == nil && exists {
					for _, l := range readLabels {
						existingKeys[l.Key] = true
					}
				}
			} else {
				// No label_file refs yet: inline labels would be
				// migrated into the new label file, so they count as
				// existing for conflict-detection purposes.
				inlineLabels, ierr := yamlbackend.GetLabels(composeFile, service)
				if ierr != nil {
					return fmt.Errorf("reading inline labels for %s: %w", service, ierr)
				}
				for _, l := range inlineLabels {
					existingKeys[l.Key] = true
				}
			}
		}
	}

	for _, l := range labels {
		if existingKeys[l.Key] {
			fmt.Fprintf(stderr, "Warning: %s: label %q already exists; will be overwritten if you proceed\n", service, l.Key)
		}
	}
	return nil
}

// resolveLabels merges opts.Template's (expanded) labels with labelArgs,
// in that order — an explicit command-line KEY=VALUE overrides the same
// key coming from a template, since it's the more specific, more
// immediate expression of intent (SPECIFICATION.md §3.6).
func resolveLabels(composeFile, service string, labelArgs []LabelArg, opts Options) ([]LabelArg, error) {

	var out []LabelArg
	index := map[string]int{}
	set := func(key, value string) {
		if i, ok := index[key]; ok {
			out[i].Value = value
			return
		}
		index[key] = len(out)
		out = append(out, LabelArg{Key: key, Value: value})
	}

	if opts.Template != "" {
		if opts.TemplateDir == "" {
			return nil, fmt.Errorf("--template requires a configured template directory (DAPLABEL_TEMPLATE_DIR)")
		}
		tmplLabels, err := template.Load(opts.TemplateDir, opts.Template)
		if err != nil {
			return nil, err
		}
		appDir := filepath.Dir(composeFile)
		vars := template.Vars{ServiceName: service, AppDir: appDir, AppName: filepath.Base(appDir)}
		for _, l := range tmplLabels {
			set(l.Key, template.Expand(l.Value, vars))
		}
	}

	for _, l := range labelArgs {
		set(l.Key, l.Value)
	}

	return out, nil
}

// runLabelFile implements §7.3's default, label_file-based add path.
func runLabelFile(stdout, stderr io.Writer, stdin io.Reader, composeFile, service string, labels []LabelArg, opts Options) (int, error) {
	refs, err := yamlbackend.GetLabelFileRefs(composeFile, service)
	if err != nil {
		return 5, fmt.Errorf("parsing %s: %w", composeFile, err)
	}

	var targetRef, targetPath string
	composeChanged := false
	// seedPath is a temp file holding migrated inline labels, used as
	// the read source for WriteLabels so the real target path is not
	// modified before the atomic commit. Empty string = no migration.
	var seedPath string
	// migratedInlineKeys tracks which inline label keys were migrated
	// into the new label file, so they can be removed from the Compose
	// file's inline labels: block at commit time.
	var migratedInlineKeys []string

	if len(refs) == 0 {
		// §2.5/Decision 21's naming convention for a file daplabel itself creates.
		targetRef = service + ".labels"
		targetPath = filepath.Join(filepath.Dir(composeFile), targetRef)
		composeChanged = true
		opts.DebugLog.Logf("add %s/%s: no existing label_file reference; will create %s", composeFile, service, targetRef)

		// Migrate existing inline labels into the new label file
		// before applying the user's requested labels. This ensures
		// incoming labels are checked against migrated inline values
		// by WriteLabels below, preventing silent duplication
		// (SPECIFICATION.md §7.3 "existing labels shall not be
		// overwritten unless explicitly requested"; §3.6 "where
		// ambiguity exists, request clarification or abort").
		inlineLabels, ierr := yamlbackend.GetLabels(composeFile, service)
		if ierr != nil {
			return 5, fmt.Errorf("reading inline labels for %s: %w", service, ierr)
		}
		if len(inlineLabels) > 0 {
			for _, l := range inlineLabels {
				migratedInlineKeys = append(migratedInlineKeys, l.Key)
			}

			// Build seed content: existing file content (a stray
			// file at targetPath, if any) plus inline labels not
			// already present in it. A stray file's keys take
			// precedence over the same key from inline labels.
			existingKeys := make(map[string]bool)
			var seedLines []string
			if data, readErr := os.ReadFile(targetPath); readErr == nil {
				trimmed := strings.TrimRight(string(data), "\n")
				if trimmed != "" {
					seedLines = strings.Split(trimmed, "\n")
					for _, sl := range seedLines {
						if sl == "" {
							continue
						}
						if key, _, ok := strings.Cut(sl, "="); ok {
							existingKeys[key] = true
						}
					}
				}
			}
			for _, l := range inlineLabels {
				if !existingKeys[l.Key] {
					seedLines = append(seedLines, l.Key+"="+l.Value)
				}
			}

			// Write seed to a temp file so WriteLabels can read
			// it as "existing content" without modifying the real
			// target path before the atomic commit (Decision 20).
			tmpFile, terr := os.CreateTemp(filepath.Dir(targetPath), ".daplabel-seed-*")
			if terr != nil {
				return 4, fmt.Errorf("creating seed temp file: %w", terr)
			}
			seedPath = tmpFile.Name()
			defer func() {
				if err := os.Remove(seedPath); err != nil {
					opts.DebugLog.Logf("warning: removing seed temp file %s: %v", seedPath, err)
				}
			}()

			seedContent := strings.Join(seedLines, "\n")
			if seedContent != "" {
				seedContent += "\n"
			}
			if _, werr := tmpFile.WriteString(seedContent); werr != nil {
				_ = tmpFile.Close()
				return 4, fmt.Errorf("writing seed: %w", werr)
			}
			if cerr := tmpFile.Close(); cerr != nil {
				return 4, fmt.Errorf("closing seed temp file: %w", cerr)
			}

			opts.DebugLog.Logf("add %s/%s: migrated %d inline label(s) into %s", composeFile, service, len(inlineLabels), targetRef)
		}
	} else {
		// §7.3: "newly added keys shall be appended to the first such
		// reference rather than creating an additional file."
		targetRef = refs[0]
		targetPath = discovery.ResolveLabelFileRef(composeFile, targetRef)
		if _, statErr := os.Stat(targetPath); statErr != nil {
			if !os.IsNotExist(statErr) {
				return 4, fmt.Errorf("checking %s: %w", targetPath, statErr)
			}
			if !opts.OnNoneCreate {
				// §8.7: no file created by default, a warning is
				// emitted, and — since this is the only place add()
				// would have written to — no changes are made at all.
				fmt.Fprintf(stderr, "Warning: %s: label_file %q does not exist; not creating it (use --on-none-create); no labels were added\n", service, targetRef)
				opts.DebugLog.Logf("add %s/%s: referenced label_file %q missing and --on-none-create not set; no changes", composeFile, service, targetRef)
				return 0, nil
			}
			opts.DebugLog.Logf("add %s/%s: referenced label_file %q missing but --on-none-create set; will create it", composeFile, service, targetRef)
			// opts.OnNoneCreate: fall through — WriteLabels below treats
			// a missing file as empty and creates it fresh.
		}
	}

	// §8.2.1: if the service has multiple label_file references, detect
	// and resolve cross-file key conflicts before applying incoming labels.
	// The merged base determines what keys "already exist" for the purpose
	// of the force/skip check — a key present in any referenced file counts
	// as existing, not just keys in the target file.
	mergedExisting, conflicts, lerr := labelstate.MergedMap(composeFile, service, opts.OnConflict)
	if lerr != nil {
		return 1, lerr
	}
	for _, c := range conflicts {
		var filePaths []string
		for _, o := range c.Occurrences {
			filePaths = append(filePaths, o.Path)
		}
		fmt.Fprintf(stderr, "Warning: %s: key %q has conflicting values across label files %v; resolved per --on-conflict=%s\n",
			service, c.Key, filePaths, opts.OnConflict)
	}

	// When a seed file exists (inline labels were migrated), read from
	// it so WriteLabels sees the migrated keys as "existing content"
	// and correctly applies skip/force logic against them.
	readPath := targetPath
	if seedPath != "" {
		readPath = seedPath
	}

	// effectiveForce is what actually governs this operation. In prompt
	// mode (not --yes), the confirmation prompt itself is the user's
	// chance to decline, so proceeding implies --force for any keys that
	// already exist. In non-interactive --yes mode, --force must be
	// passed explicitly.
	effectiveForce := opts.Force || !opts.Yes

	// buildContent applies the requested force semantics and returns the
	// content that would be written plus the list of keys that were
	// skipped under that force setting.
	buildContent := func(force bool) ([]byte, []string, error) {
		var tw []labelfile.Label
		var preSkipped []string
		if mergedExisting != nil {
			for _, l := range labels {
				if _, exists := mergedExisting[l.Key]; exists && !force {
					preSkipped = append(preSkipped, l.Key)
					continue
				}
				tw = append(tw, labelfile.Label{Key: l.Key, Value: l.Value})
			}
		} else {
			tw = make([]labelfile.Label, len(labels))
			for i, l := range labels {
				tw[i] = labelfile.Label{Key: l.Key, Value: l.Value}
			}
		}
		content, writeSkipped, err := labelfile.WriteLabels(readPath, tw, force)
		if err != nil {
			return nil, nil, err
		}
		allSkipped := make([]string, 0, len(preSkipped)+len(writeSkipped))
		allSkipped = append(allSkipped, preSkipped...)
		allSkipped = append(allSkipped, writeSkipped...)
		return content, allSkipped, nil
	}

	// Detection pass: find keys that would be skipped under the user's
	// explicit --force setting, so we can warn about them.
	_, detectSkipped, derr := buildContent(opts.Force)
	if derr != nil {
		return 1, derr
	}
	if !opts.SuppressExistingKeyWarnings {
		for _, k := range detectSkipped {
			if opts.Yes {
				fmt.Fprintf(stderr, "Warning: %s: label %q already exists in %s; not overwriting (use --force)\n", service, k, targetPath)
			} else {
				fmt.Fprintf(stderr, "Warning: %s: label %q already exists in %s; will be overwritten if you proceed\n", service, k, targetPath)
			}
			opts.DebugLog.Logf("add %s/%s: key %q already exists in %s; skipped under force=%v (effectiveForce=%v)", composeFile, service, k, targetPath, opts.Force, effectiveForce)
		}
	}

	// When force is active (explicit or implied by prompt confirmation)
	// the detection pass finds nothing to skip, so the warning loop above
	// is silent. Emit declarative informational messages for transparency
	// (SPECIFICATION.md §3.3) so the user sees which existing keys were
	// overwritten. These are not "use --force" warnings — the overwrite
	// has already been authorised.
	if effectiveForce && !opts.SuppressExistingKeyWarnings {
		existingKeys := make(map[string]bool, len(mergedExisting))
		for k := range mergedExisting {
			existingKeys[k] = true
		}
		if len(existingKeys) == 0 {
			// MergedMap returns nil for services with a single label_file
			// reference, so read the target file directly for the existence
			// check.
			readLabels, exists, rerr := labelfile.Read(readPath)
			if rerr == nil && exists {
				for _, l := range readLabels {
					existingKeys[l.Key] = true
				}
			}
		}
		for _, l := range labels {
			if existingKeys[l.Key] {
				fmt.Fprintf(stderr, "Warning: %s: label %q already exists in %s; overwriting\n", service, l.Key, targetPath)
			}
		}
	}

	// Actual write pass using the effective force setting.
	content, allSkipped, werr := buildContent(effectiveForce)
	if werr != nil {
		return 1, werr
	}

	if len(allSkipped) == len(labels) {
		// Every requested key already existed and force was off.
		// If inline labels were migrated into the seed, the label file
		// still needs to be created (with the migrated content) and
		// the inline labels removed from the Compose file — the
		// migration itself is a real change even though no incoming
		// label was accepted.
		switch {
		case len(migratedInlineKeys) > 0:
			// Fall through: the label file will be created with
			// migrated content, and inline labels will be removed.
		case composeChanged && !opts.OnEmptyCreate:
			// §8.6: no label file created by default when nothing
			// would change and no migration is happening.
			fmt.Fprintln(stdout, "No labels to add; nothing created (use --on-empty-create to create an empty label file anyway).")
			return 0, nil
		case !composeChanged:
			fmt.Fprintln(stdout, "No labels to add; nothing changed.")
			return 0, nil
		}
	}

	opts.DebugLog.Logf("add %s/%s: writing %d label(s) to %s", composeFile, service, len(labels)-len(allSkipped), targetPath)
	var set atomicwrite.Set
	set.Add(targetPath, content)
	set.PreserveOwner(composeFile)

	if composeChanged {
		root, lerr := yamlbackend.LoadRootForEdit(composeFile)
		if lerr != nil {
			return 5, lerr
		}
		// Remove ALL migrated inline labels from the Compose file so
		// they don't exist in both places. Every migrated key was
		// written into the label file (either as the original value,
		// or overwritten by the incoming value with --force), so the
		// inline copy must always be removed — even keys that were
		// "skipped" by WriteLabels (meaning the incoming value wasn't
		// applied, but the migrated value is still in the label file).
		if len(migratedInlineKeys) > 0 {
			if _, rerr := yamlbackend.RemoveInlineLabelKeys(root, service, migratedInlineKeys); rerr != nil {
				return 1, fmt.Errorf("removing migrated inline labels for %s: %w", service, rerr)
			}
			opts.DebugLog.Logf("add %s/%s: removed %d migrated inline label(s) from Compose file", composeFile, service, len(migratedInlineKeys))
		}
		if _, serr := yamlbackend.SetLabelFileRef(root, service, targetRef); serr != nil {
			return 1, serr
		}
		out, merr := yamlbackend.Marshal(root)
		if merr != nil {
			return 1, fmt.Errorf("encoding %s: %w", composeFile, merr)
		}
		set.Add(composeFile, out)
	}

	summary := summarizeLabelFile(service, targetPath, targetRef, labels, allSkipped, composeChanged)
	code, _, commitErr := writeops.Commit(stdout, stderr, stdin, &set, composeFile, composeChanged, opts.DryRun, opts.Yes, summary, opts.ConfirmTimeout)
	return code, commitErr
}

// runInline implements Decision 38's opt-in --inline mode: writing
// directly into the service's labels: mapping instead of a label_file.
func runInline(stdout, stderr io.Writer, stdin io.Reader, composeFile, service string, labels []LabelArg, opts Options) (int, error) {
	root, lerr := yamlbackend.LoadRootForEdit(composeFile)
	if lerr != nil {
		return 5, lerr
	}

	quote := !opts.ValuesNoQuote

	// In prompt mode, proceeding implies --force for existing inline keys.
	effectiveForce := opts.Force || !opts.Yes

	// Detect existing inline keys for warning purposes; SetInlineLabel with
	// effectiveForce=true will overwrite them, so we check first.
	existingInline, ierr := yamlbackend.GetLabels(composeFile, service)
	if ierr != nil {
		return 5, fmt.Errorf("reading inline labels for %s: %w", service, ierr)
	}
	existingInlineKeys := make(map[string]bool, len(existingInline))
	for _, l := range existingInline {
		existingInlineKeys[l.Key] = true
	}

	var skipped []string
	changedAny := false
	for _, l := range labels {
		if existingInlineKeys[l.Key] && !opts.Yes {
			skipped = append(skipped, l.Key)
		}
		added, changed, serr := yamlbackend.SetInlineLabel(root, service, l.Key, l.Value, quote, effectiveForce)
		if serr != nil {
			return 1, serr
		}
		if added || changed {
			changedAny = true
		} else {
			skipped = append(skipped, l.Key)
		}
	}
	for _, k := range skipped {
		if opts.Yes {
			fmt.Fprintf(stderr, "Warning: %s: inline label %q already exists; not overwriting (use --force)\n", service, k)
		} else {
			fmt.Fprintf(stderr, "Warning: %s: inline label %q already exists; will be overwritten if you proceed\n", service, k)
		}
		opts.DebugLog.Logf("add %s/%s: inline key %q already exists; skipped under force=%v (effectiveForce=%v)", composeFile, service, k, opts.Force, effectiveForce)
	}
	if !changedAny {
		fmt.Fprintln(stdout, "No labels to add; nothing changed.")
		return 0, nil
	}

	out, merr := yamlbackend.Marshal(root)
	if merr != nil {
		return 1, fmt.Errorf("encoding %s: %w", composeFile, merr)
	}

	var set atomicwrite.Set
	set.Add(composeFile, out)
	set.PreserveOwner(composeFile)

	skipSet := toSet(skipped)
	var b strings.Builder
	fmt.Fprintf(&b, "Service %q:\n", service)
	for _, l := range labels {
		if skipSet[l.Key] {
			continue
		}
		fmt.Fprintf(&b, "  + %s=%s (inline) -> %s\n", l.Key, l.Value, composeFile)
	}
	summary := strings.TrimRight(b.String(), "\n")

	code, _, commitErr := writeops.Commit(stdout, stderr, stdin, &set, composeFile, true, opts.DryRun, opts.Yes, summary, opts.ConfirmTimeout)
	return code, commitErr
}

func summarizeLabelFile(service, targetPath, targetRef string, labels []LabelArg, skipped []string, composeChanged bool) string {
	skipSet := toSet(skipped)
	var b strings.Builder
	fmt.Fprintf(&b, "Service %q:\n", service)
	for _, l := range labels {
		if skipSet[l.Key] {
			continue
		}
		fmt.Fprintf(&b, "  + %s=%s -> %s\n", l.Key, l.Value, targetPath)
	}
	if composeChanged {
		fmt.Fprintf(&b, "  + label_file: %s (new reference)\n", targetRef)
	}
	return strings.TrimRight(b.String(), "\n")
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
