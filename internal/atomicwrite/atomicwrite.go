// Package atomicwrite implements DECISIONS.md Decision 20's write
// strategy, shared by every command that modifies files (`add`,
// `remove`, `generate`, `template apply`/`create`): write new content to
// a temporary file in the same directory as its target, validate a
// Compose file's staged content via the sole authority for Compose
// validity (internal/composevalidate, Decision 15/20) before anything is
// committed, back up any existing target (SPECIFICATION.md §8.4/§19),
// then commit every file in the set via atomic rename.
//
// A Set is scoped to one application (Decision 20's "scoped per
// application"): SPECIFICATION.md §3.5 requires that an operation on one
// application either completes fully or leaves it unchanged, so every
// file belonging to one `add`/`remove` invocation against one Compose
// file is staged and committed together.
//
// Commit-phase guarantee (Decision 45): if a rename or remove in the
// final commit phase fails after prior operations in that phase have
// already succeeded, best-effort rollback restores each already-committed
// file from its .pre-label backup. If every file is restored
// successfully, Commit returns a clean error describing the original
// failure — no partial state persists on disk. If rollback itself cannot
// complete cleanly, a distinct RollbackFailedError is returned naming
// the paths left in an inconsistent state and pointing at their surviving
// .pre-label backups for manual recovery.
package atomicwrite

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/neutralvibes/daplabel/internal/debuglog"
	"github.com/neutralvibes/daplabel/internal/fspreserve"
)

// entry is one file this Set will write, once committed.
type entry struct {
	path    string
	content []byte
	tmpPath string
}

// logger is the debug logger used by Set.Commit; it is package-level
// so callers do not need to thread it through every helper.
var logger *debuglog.Logger

// SetDebugLogger configures the debug logger used by atomicwrite. It is
// optional; if never called, logging is a no-op.
func SetDebugLogger(l *debuglog.Logger) {
	logger = l
}

// osRename is a seam for testing — replaced in TestCommit_dirtyRollback
// to inject backup deletion between rename and rollback. Defaults to
// os.Rename.
var osRename = os.Rename

// osRemove is a seam for testing — replaced in tests to inject removal
// failures without relying on filesystem permissions (which are
// privilege-dependent). Defaults to os.Remove.
var osRemove = os.Remove

// removal is one file this Set will delete, once committed — used by
// `remove` when removing the last key from a label file leaves it empty
// (SPECIFICATION.md §8.6's default "no changes ... unless explicitly
// requested" reads, for remove, as cleaning up an emptied-out file
// rather than leaving useless empty scaffolding behind).
type removal struct {
	path string
}

// Set accumulates the files one application-scoped operation intends to
// write or delete. Nothing touches disk until Commit is called.
type Set struct {
	entries  []*entry
	removals []removal
	// composeOwnerPath is the compose file whose owner should be used
	// when chowning files created or modified by this Set. Empty means
	// no ownership preservation is requested.
	composeOwnerPath string
}

// Add stages path to be overwritten with content once Commit succeeds.
// path may or may not already exist; a pre-existing file is backed up
// (SPECIFICATION.md §8.4) before being replaced.
func (s *Set) Add(path string, content []byte) {
	s.entries = append(s.entries, &entry{path: path, content: content})
}

// Remove stages path to be deleted once Commit succeeds, backing it up
// first (SPECIFICATION.md §8.4) exactly like an overwrite — a deletion
// is recoverable the same way a modification is. path not existing at
// commit time is not an error (nothing to do).
func (s *Set) Remove(path string) {
	s.removals = append(s.removals, removal{path: path})
}

// PreserveOwner records that files created or modified by this Set
// should be owned by the same user and group as composePath. On Linux
// this only takes effect when daplabel is running as root. On Windows
// this is a no-op.
func (s *Set) PreserveOwner(composePath string) {
	s.composeOwnerPath = composePath
}

// actionKind distinguishes a rename (overwrite) from a removal in the
// commit-phase action tracker used for rollback (Decision 45).
type actionKind int

const (
	actionRename actionKind = iota
	actionRemove
)

// committedAction records one successful rename or remove in the final
// commit phase, so rollback can undo it in reverse order.
type committedAction struct {
	kind      actionKind
	path      string
	hadBackup bool // false if the file didn't exist before (new file)
}

// RollbackFailedError is returned by Commit when a commit-phase failure
// triggers rollback, and the rollback itself cannot complete cleanly.
// This is a distinct error type so callers and tests can branch on it
// reliably (not just match text). The paths listed in CorruptedPaths are
// left in an inconsistent state; their .pre-label backups survive for
// manual recovery.
type RollbackFailedError struct {
	OriginalErr    error
	RollbackErrors []error
	CorruptedPaths []string
}

func (e *RollbackFailedError) Error() string {
	msg := fmt.Sprintf("ROLLBACK FAILED — manual recovery required: %v", e.OriginalErr)
	for _, p := range e.CorruptedPaths {
		msg += fmt.Sprintf("\n  %s is in an inconsistent state (restore from %s.pre-label)", p, p)
	}
	for _, re := range e.RollbackErrors {
		msg += fmt.Sprintf("\n  rollback error: %v", re)
	}
	return msg
}

func (e *RollbackFailedError) Unwrap() error { return e.OriginalErr }

// Commit writes every staged file and deletes every staged removal. If
// validateComposePath is non-empty, it must match the path of one of
// this Set's entries; validate is called against an isolated scratch
// copy of validateComposePath's directory, built to mirror this
// operation's post-commit state (Decision 20: "Compose file validity is
// confirmed ... against the temporary content before anything is
// committed") — every file currently in that directory, overlaid with
// this Set's pending writes and with pending removals deleted. This
// matters because docker compose config resolves label_file: entries
// relative to the Compose file's own directory using their real,
// written names; validating the compose temp file in isolation (its
// real siblings only present under random .daplabel-tmp-* names, or a
// brand new label_file not committed yet) was rejected as broken by
// docker compose config itself — a real bug caught by testing against a
// real Docker Compose install, not a hypothetical one. The scratch copy
// gives the validator an accurate picture without touching the real
// directory before validation passes.
//
// validate receives the scratch copy's compose file path (a real path
// on disk, so a validator that shells out to an external tool —
// internal/composevalidate — has something to point at) rather than the
// content directly.
//
// On any failure — writing a temp file, preparing the validation copy,
// validation itself, or backing up an existing target — every temp file
// already written is removed, the scratch directory is removed, and no
// original file is touched or deleted. Once validation (if requested)
// has passed, backups, renames, and deletions proceed file by file;
// SPECIFICATION.md §3.5's per-application atomicity is therefore
// guaranteed up to the point the first rename/removal begins, matching
// Decision 20's mechanism exactly.
//
// Commit-phase guarantee (Decision 45): if a rename or remove in the
// final commit phase fails after prior operations in that phase have
// already succeeded, best-effort rollback restores each already-committed
// file from its .pre-label backup. If every file is restored
// successfully, Commit returns a clean error describing the original
// failure — no partial state persists on disk. If rollback itself cannot
// complete cleanly, a distinct RollbackFailedError is returned naming
// the paths left in an inconsistent state and pointing at their surviving
// .pre-label backups for manual recovery.
func (s *Set) Commit(validateComposePath string, validate func(scratchPath string) error) (err error) {
	if len(s.entries) == 0 && len(s.removals) == 0 {
		return nil
	}

	var written []*entry
	// Clean up every temp file written so far on any early return —
	// "no original file is touched" only holds if a failed validation or
	// write doesn't leave stray .daplabel-tmp-* files behind either.
	defer func() {
		if err == nil {
			return
		}
		for _, e := range written {
			_ = os.Remove(e.tmpPath)
		}
	}()

	for _, e := range s.entries {
		tmp, werr := writeTemp(e.path, e.content)
		if werr != nil {
			return fmt.Errorf("staging %s: %w", e.path, werr)
		}
		e.tmpPath = tmp
		written = append(written, e)
	}

	if validateComposePath != "" && validate != nil {
		if _, found := findEntry(s.entries, validateComposePath); !found {
			return fmt.Errorf("internal error: validateComposePath %s is not part of this write set", validateComposePath)
		}
		scratchComposePath, cleanup, perr := s.prepareValidationCopy(validateComposePath)
		if perr != nil {
			return perr
		}
		defer cleanup()
		if verr := validate(scratchComposePath); verr != nil {
			return verr
		}
	}

	ownerUID, ownerGID, preserveOwner := s.owner()

	for _, e := range s.entries {
		created, err := backup(e.path)
		if err != nil {
			return fmt.Errorf("backing up %s: %w", e.path, err)
		}
		// A brand-new file (no prior content to back up) has no
		// .pre-label to chown — only attempt it when backup() actually
		// wrote one, otherwise this fails with ENOENT under sudo/root
		// for every first-time-created label file.
		if preserveOwner && created {
			if err := fspreserve.Chown(e.path+".pre-label", ownerUID, ownerGID); err != nil {
				return fmt.Errorf("chown backup of %s: %w", e.path, err)
			}
		}
		logger.Logf("atomicwrite: backed up %s", e.path)
	}
	for _, r := range s.removals {
		created, err := backup(r.path)
		if err != nil {
			return fmt.Errorf("backing up %s: %w", r.path, err)
		}
		if preserveOwner && created {
			if err := fspreserve.Chown(r.path+".pre-label", ownerUID, ownerGID); err != nil {
				return fmt.Errorf("chown backup of %s: %w", r.path, err)
			}
		}
		logger.Logf("atomicwrite: backed up %s for removal", r.path)
	}

	if preserveOwner {
		for _, e := range s.entries {
			if err := fspreserve.Chown(e.tmpPath, ownerUID, ownerGID); err != nil {
				return fmt.Errorf("chown %s: %w", e.tmpPath, err)
			}
		}
	}

	// --- Final commit phase: rename entries, then remove removals ---
	// This is the only phase where an original file is touched. The
	// action tracker spans both loops so a failure in the removal loop
	// also rolls back already-renamed entries from the earlier loop.
	//
	// osRename is used instead of os.Rename directly so tests can inject
	// failures via the seam (see atomicwrite_test.go).
	var actions []committedAction

	for _, e := range s.entries {
		if err := osRename(e.tmpPath, e.path); err != nil {
			rollbackErrs := s.rollback(actions, preserveOwner, ownerUID, ownerGID)
			if len(rollbackErrs) > 0 {
				return &RollbackFailedError{
					OriginalErr:    fmt.Errorf("committing %s: %w", e.path, err),
					RollbackErrors: rollbackErrs,
					CorruptedPaths: collectPaths(actions),
				}
			}
			return fmt.Errorf("committing %s: %w (all prior changes rolled back)", e.path, err)
		}
		actions = append(actions, committedAction{
			kind:      actionRename,
			path:      e.path,
			hadBackup: fileExists(e.path + ".pre-label"),
		})
		logger.Logf("atomicwrite: committed %s", e.path)
	}
	for _, r := range s.removals {
		if err := osRemove(r.path); err != nil && !os.IsNotExist(err) {
			rollbackErrs := s.rollback(actions, preserveOwner, ownerUID, ownerGID)
			if len(rollbackErrs) > 0 {
				return &RollbackFailedError{
					OriginalErr:    fmt.Errorf("removing %s: %w", r.path, err),
					RollbackErrors: rollbackErrs,
					CorruptedPaths: collectPaths(actions),
				}
			}
			return fmt.Errorf("removing %s: %w (all prior changes rolled back)", r.path, err)
		}
		actions = append(actions, committedAction{
			kind:      actionRemove,
			path:      r.path,
			hadBackup: fileExists(r.path + ".pre-label"),
		})
	}

	return nil
}

// rollback walks actions in reverse order and restores each file to its
// pre-commit state. For a file that had a backup, it copies the .pre-label
// content back onto the original path. For a file that was new (no backup),
// it removes the file. For a no-op removal (file never existed), it does
// nothing — there is nothing to undo. Returns a slice of errors; a non-nil
// slice means rollback was incomplete and the caller should return a
// RollbackFailedError.
func (s *Set) rollback(actions []committedAction, preserveOwner bool, ownerUID, ownerGID int) []error {
	var errs []error
	for i := len(actions) - 1; i >= 0; i-- {
		a := actions[i]
		if !a.hadBackup {
			switch a.kind {
			case actionRemove:
				// No-op removal: the file never existed on disk, so
				// there is nothing to undo. Do not call os.Remove.
				continue
			case actionRename:
				// File was new (no pre-existing original). Undo by
				// deleting.
				if err := os.Remove(a.path); err != nil {
					errs = append(errs, fmt.Errorf("rollback remove (new file) %s: %w", a.path, err))
				}
				continue
			}
			continue
		}
		// Restore from .pre-label backup via file copy, not rename —
		// the backup file itself must survive rollback (Decision 45).
		data, rerr := os.ReadFile(a.path + ".pre-label")
		if rerr != nil {
			errs = append(errs, fmt.Errorf("rollback read backup for %s: %w", a.path, rerr))
			continue
		}
		if werr := os.WriteFile(a.path, data, 0o644); werr != nil {
			errs = append(errs, fmt.Errorf("rollback write for %s: %w", a.path, werr))
			continue
		}
		if preserveOwner {
			if cerr := fspreserve.Chown(a.path, ownerUID, ownerGID); cerr != nil {
				// Content restored but ownership wrong — still an
				// inconsistent state (Decision 45).
				errs = append(errs, fmt.Errorf("rollback chown for %s: %w", a.path, cerr))
			}
		}
	}
	return errs
}

// collectPaths returns the paths from a slice of committed actions.
func collectPaths(actions []committedAction) []string {
	paths := make([]string, len(actions))
	for i, a := range actions {
		paths[i] = a.path
	}
	return paths
}

// fileExists returns true if path exists on disk.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// owner returns the UID/GID to use when preserving file ownership, and
// a bool indicating whether preservation is requested. If the compose
// owner path is empty, the platform is not Linux, or the process is not
// root, it returns (-1, -1, false).
func (s *Set) owner() (uid, gid int, ok bool) {
	if s.composeOwnerPath == "" || !fspreserve.IsRoot() {
		return -1, -1, false
	}
	uid, gid, err := fspreserve.Owner(s.composeOwnerPath)
	if err != nil {
		return -1, -1, false
	}
	return uid, gid, true
}

// prepareValidationCopy builds a scratch directory mirroring the
// post-commit state of validateComposePath's own directory: every file
// currently there, overlaid with this Set's pending writes (for entries
// in that same directory), with pending removals (same directory)
// deleted. It returns the scratch copy's compose file path, ready to
// validate, and a cleanup func the caller must run once done with it.
func (s *Set) prepareValidationCopy(validateComposePath string) (scratchComposePath string, cleanup func(), err error) {
	dir := filepath.Dir(validateComposePath)

	scratchDir, err := os.MkdirTemp("", "daplabel-validate-*")
	if err != nil {
		return "", nil, fmt.Errorf("preparing validation directory: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(scratchDir) }

	// Start from every file already in the real directory — this is
	// what makes a pre-existing, untouched sibling label_file (one this
	// operation isn't writing at all) still resolve correctly.
	dirEntries, rerr := os.ReadDir(dir)
	if rerr != nil && !os.IsNotExist(rerr) {
		cleanup()
		return "", nil, fmt.Errorf("preparing validation directory: %w", rerr)
	}
	for _, de := range dirEntries {
		if de.IsDir() {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(dir, de.Name()))
		if rerr != nil {
			// Best-effort: a sibling that vanished or became unreadable
			// between ReadDir and ReadFile isn't this Set's problem to
			// solve, and skipping it just means the scratch copy is
			// missing something the real directory doesn't reliably
			// have either.
			continue
		}
		if werr := os.WriteFile(filepath.Join(scratchDir, de.Name()), data, 0o644); werr != nil {
			cleanup()
			return "", nil, fmt.Errorf("preparing validation directory: %w", werr)
		}
	}

	// Overlay this Set's own pending writes — new files, and updates to
	// existing ones — under their real basenames.
	for _, e := range s.entries {
		if filepath.Dir(e.path) != dir {
			continue
		}
		if werr := os.WriteFile(filepath.Join(scratchDir, filepath.Base(e.path)), e.content, 0o644); werr != nil {
			cleanup()
			return "", nil, fmt.Errorf("preparing validation directory: %w", werr)
		}
	}

	// And remove anything this Set is about to delete, so an emptied,
	// about-to-be-cleaned-up label file doesn't spuriously still "exist"
	// during validation.
	for _, r := range s.removals {
		if filepath.Dir(r.path) != dir {
			continue
		}
		_ = os.Remove(filepath.Join(scratchDir, filepath.Base(r.path)))
	}

	scratchComposePath = filepath.Join(scratchDir, filepath.Base(validateComposePath))
	if _, statErr := os.Stat(scratchComposePath); statErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("internal error: %s missing from its own validation copy", validateComposePath)
	}
	return scratchComposePath, cleanup, nil
}

func findEntry(entries []*entry, path string) (*entry, bool) {
	for _, e := range entries {
		if e.path == path {
			return e, true
		}
	}
	return nil, false
}

// writeTemp writes content to a new temporary file in the same directory
// as path (same filesystem, so the later rename is atomic), returning
// the temporary file's path.
func writeTemp(path string, content []byte) (string, error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".daplabel-tmp-*")
	if err != nil {
		return "", err
	}
	tmpPath := f.Name()
	if _, werr := f.Write(content); werr != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return "", werr
	}
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(tmpPath)
		return "", cerr
	}
	return tmpPath, nil
}

// backup copies path to path+".pre-label" (SPECIFICATION.md
// §8.4/DECISIONS.md Decision 19) if path currently exists. A target that
// doesn't exist yet (a freshly created label file, a service's first
// label_file) has nothing to back up — not an error.
//
// Note: backup writes the backup with mode 0o644 and does not preserve
// the original file's mode bits. This is a pre-existing gap (noted in
// Decision 45) — the rollback logic in Commit uses the same 0o644 mode
// when restoring from backup, which is consistent with backup's own
// behaviour.
// backup copies path to path+".pre-label" if path currently exists,
// reporting whether a backup was actually created. A target that
// doesn't exist yet (a freshly created label file, a service's first
// label_file) has nothing to back up — created is false and err is nil,
// not an error. Callers must use created to decide whether the backup
// path is safe to touch afterward (e.g. chowning it) — see Commit's
// entry/removal loops, where chowning a backup that was never created
// (bug: previously attempted unconditionally whenever preserveOwner was
// set, failing with ENOENT for any brand-new file under sudo/root) is
// exactly the mistake this return value exists to prevent.
func backup(path string) (created bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := os.WriteFile(path+".pre-label", data, 0o644); err != nil {
		return false, err
	}
	return true, nil
}
