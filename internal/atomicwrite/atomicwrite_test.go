package atomicwrite

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/neutralvibes/daplabel/internal/fspreserve"
)

func TestCommit_writesNewFiles(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yml")
	labelPath := filepath.Join(dir, "web.labels")

	var s Set
	s.Add(composePath, []byte("services:\n  web: {}\n"))
	s.Add(labelPath, []byte("com.example.a=1\n"))

	if err := s.Commit("", nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("reading committed compose file: %v", err)
	}
	if string(got) != "services:\n  web: {}\n" {
		t.Errorf("compose file content = %q", got)
	}

	got, err = os.ReadFile(labelPath)
	if err != nil {
		t.Fatalf("reading committed label file: %v", err)
	}
	if string(got) != "com.example.a=1\n" {
		t.Errorf("label file content = %q", got)
	}

	// Neither file existed before, so there is nothing to back up.
	if _, err := os.Stat(composePath + ".pre-label"); !os.IsNotExist(err) {
		t.Errorf("expected no backup for a file that didn't previously exist, err=%v", err)
	}
}

func TestCommit_backsUpExistingFileBeforeOverwriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web.labels")
	if err := os.WriteFile(path, []byte("com.example.old=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var s Set
	s.Add(path, []byte("com.example.old=1\ncom.example.new=2\n"))
	if err := s.Commit("", nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	backupContent, err := os.ReadFile(path + ".pre-label")
	if err != nil {
		t.Fatalf("reading backup: %v", err)
	}
	if string(backupContent) != "com.example.old=1\n" {
		t.Errorf("backup content = %q, want original pre-write content", backupContent)
	}

	newContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading committed file: %v", err)
	}
	if string(newContent) != "com.example.old=1\ncom.example.new=2\n" {
		t.Errorf("committed content = %q", newContent)
	}
}

func TestCommit_validationFailureLeavesOriginalsUntouched(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yml")
	labelPath := filepath.Join(dir, "web.labels")

	if err := os.WriteFile(composePath, []byte("original compose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(labelPath, []byte("original labels\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var s Set
	s.Add(composePath, []byte("new compose\n"))
	s.Add(labelPath, []byte("new labels\n"))

	validateErr := errors.New("docker compose config rejected this file")
	err := s.Commit(composePath, func(tempPath string) error {
		// The validator gets a real, already-written temp file to
		// inspect, not just the in-memory content.
		content, rerr := os.ReadFile(tempPath)
		if rerr != nil {
			t.Fatalf("validator's temp path could not be read: %v", rerr)
		}
		if string(content) != "new compose\n" {
			t.Errorf("validator saw %q, want staged content", content)
		}
		return validateErr
	})
	if !errors.Is(err, validateErr) {
		t.Fatalf("Commit error = %v, want %v", err, validateErr)
	}

	// Neither original file should have changed at all.
	gotCompose, _ := os.ReadFile(composePath)
	if string(gotCompose) != "original compose\n" {
		t.Errorf("compose file was modified despite failed validation: %q", gotCompose)
	}
	gotLabels, _ := os.ReadFile(labelPath)
	if string(gotLabels) != "original labels\n" {
		t.Errorf("label file was modified despite failed validation: %q", gotLabels)
	}

	// No backup should have been created either — nothing was
	// authorised to change.
	if _, err := os.Stat(composePath + ".pre-label"); !os.IsNotExist(err) {
		t.Errorf("expected no backup after a failed validation, err=%v", err)
	}

	// No stray temp files left behind.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "compose.yml" && e.Name() != "web.labels" {
			t.Errorf("unexpected leftover file after failed commit: %s", e.Name())
		}
	}
}

func TestCommit_validationSuccessCommitsEverything(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yml")

	var s Set
	s.Add(composePath, []byte("validated compose\n"))

	validateCalls := 0
	err := s.Commit(composePath, func(tempPath string) error {
		validateCalls++
		return nil
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if validateCalls != 1 {
		t.Errorf("validate called %d times, want 1", validateCalls)
	}

	got, _ := os.ReadFile(composePath)
	if string(got) != "validated compose\n" {
		t.Errorf("compose file content = %q", got)
	}
}

func TestCommit_removesStagedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web.labels")
	if err := os.WriteFile(path, []byte("com.example.a=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var s Set
	s.Remove(path)
	if err := s.Commit("", nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to be deleted, err=%v", path, err)
	}
	backup, err := os.ReadFile(path + ".pre-label")
	if err != nil {
		t.Fatalf("reading backup: %v", err)
	}
	if string(backup) != "com.example.a=1\n" {
		t.Errorf("backup content = %q", backup)
	}
}

func TestCommit_removingNonexistentFileIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.labels")

	var s Set
	s.Remove(path)
	if err := s.Commit("", nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func TestCommit_writesAndRemovalsTogether(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yml")
	staleLabelFile := filepath.Join(dir, "stale.labels")
	if err := os.WriteFile(composePath, []byte("original compose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staleLabelFile, []byte("com.example.a=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var s Set
	s.Add(composePath, []byte("updated compose\n"))
	s.Remove(staleLabelFile)
	if err := s.Commit("", nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, _ := os.ReadFile(composePath)
	if string(got) != "updated compose\n" {
		t.Errorf("compose content = %q", got)
	}
	if _, err := os.Stat(staleLabelFile); !os.IsNotExist(err) {
		t.Error("expected the stale label file to be removed")
	}
}

func TestCommit_validationFailureLeavesRemovalsUntouchedToo(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yml")
	labelFile := filepath.Join(dir, "web.labels")
	if err := os.WriteFile(composePath, []byte("original compose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(labelFile, []byte("com.example.a=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var s Set
	s.Add(composePath, []byte("new compose\n"))
	s.Remove(labelFile)

	err := s.Commit(composePath, func(tempPath string) error {
		return errors.New("rejected")
	})
	if err == nil {
		t.Fatal("expected an error from the failed validation")
	}

	if _, statErr := os.Stat(labelFile); statErr != nil {
		t.Errorf("expected the label file to still exist after a failed validation, got: %v", statErr)
	}
	got, _ := os.ReadFile(composePath)
	if string(got) != "original compose\n" {
		t.Errorf("compose content = %q, want untouched", got)
	}
}

func TestCommit_emptySetIsANoOp(t *testing.T) {
	var s Set
	if err := s.Commit("", nil); err != nil {
		t.Fatalf("Commit on empty set: %v", err)
	}
}

// --- Rollback tests (Decision 45) ---

// TestCommit_partialFailureRollsBackEntries verifies that when the second
// entry's rename fails, the first entry is rolled back to its original
// state (or removed if it was a new file). Uses a non-empty directory at
// the second target path to force os.Rename to fail — this is
// privilege-independent (works as root or non-root).
func TestCommit_partialFailureRollsBackEntries(t *testing.T) {
	dir := t.TempDir()

	// First entry: a new file (no pre-existing content).
	firstPath := filepath.Join(dir, "first.labels")

	// Second entry: target is a non-empty directory, so os.Rename will fail.
	secondPath := filepath.Join(dir, "second.labels")
	if err := os.MkdirAll(secondPath, 0o755); err != nil {
		t.Fatal(err)
	}
	// Put a file inside so the directory is non-empty.
	if err := os.WriteFile(filepath.Join(secondPath, "placeholder"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	var s Set
	s.Add(firstPath, []byte("com.example.first=1\n"))
	s.Add(secondPath, []byte("com.example.second=1\n"))

	err := s.Commit("", nil)
	if err == nil {
		t.Fatal("expected Commit to fail")
	}

	// Verify it's a clean rollback error (not RollbackFailedError).
	var rfe *RollbackFailedError
	if errors.As(err, &rfe) {
		t.Fatalf("expected clean rollback error, got RollbackFailedError: %v", err)
	}

	// First file should not exist (it was new, rolled back via os.Remove).
	if _, statErr := os.Stat(firstPath); !os.IsNotExist(statErr) {
		t.Errorf("first file should have been removed during rollback, stat err=%v", statErr)
	}

	// Second file should still be a directory (the rename never happened).
	fi, statErr := os.Stat(secondPath)
	if statErr != nil {
		t.Fatalf("second path should still exist: %v", statErr)
	}
	if !fi.IsDir() {
		t.Error("second path should still be a directory")
	}

	// No stray .daplabel-tmp-* files left behind.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() == "placeholder" {
			continue // the file we put inside the directory
		}
		if e.Name() == "second.labels" {
			continue // the directory
		}
		t.Errorf("unexpected leftover file: %s", e.Name())
	}
}

// TestCommit_renameSucceedsThenRemovalFailsRollsBackBoth verifies that
// the action tracker spans both loops: when a removal fails after an
// entry rename has already succeeded, the entry is rolled back too.
// Uses a non-empty directory as the removal target to force os.Remove
// to fail (privilege-independent).
func TestCommit_renameSucceedsThenRemovalFailsRollsBackBoth(t *testing.T) {
	dir := t.TempDir()

	// Entry: a file with pre-existing content.
	entryPath := filepath.Join(dir, "web.labels")
	originalContent := []byte("com.example.old=1\n")
	if err := os.WriteFile(entryPath, originalContent, 0o644); err != nil {
		t.Fatal(err)
	}

	// Removal target: a non-empty directory, so os.Remove will fail.
	removalPath := filepath.Join(dir, "stale.labels")
	if err := os.MkdirAll(removalPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(removalPath, "placeholder"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	var s Set
	s.Add(entryPath, []byte("com.example.new=1\n"))
	s.Remove(removalPath)

	err := s.Commit("", nil)
	if err == nil {
		t.Fatal("expected Commit to fail")
	}

	// Verify it's a clean rollback error.
	var rfe *RollbackFailedError
	if errors.As(err, &rfe) {
		t.Fatalf("expected clean rollback error, got RollbackFailedError: %v", err)
	}

	// Entry should be rolled back to original content.
	got, _ := os.ReadFile(entryPath)
	if string(got) != string(originalContent) {
		t.Errorf("entry content = %q, want original %q", got, originalContent)
	}

	// Removal target should still be a directory.
	fi, statErr := os.Stat(removalPath)
	if statErr != nil {
		t.Fatalf("removal path should still exist: %v", statErr)
	}
	if !fi.IsDir() {
		t.Error("removal path should still be a directory")
	}
}

// TestCommit_dirtyRollback verifies that when rollback itself fails
// (because a .pre-label backup is missing), a RollbackFailedError is
// returned with the affected paths named. The first entry's backup is
// deleted after the rename succeeds but before the second rename fails,
// so the rollback attempt cannot restore it.
func TestCommit_dirtyRollback(t *testing.T) {
	dir := t.TempDir()

	// First entry: a file with pre-existing content (so a backup is created).
	firstPath := filepath.Join(dir, "first.labels")
	originalContent := []byte("com.example.first=1\n")
	if err := os.WriteFile(firstPath, originalContent, 0o644); err != nil {
		t.Fatal(err)
	}

	// Second entry: a regular file path that does not exist yet.
	secondPath := filepath.Join(dir, "second.labels")

	var s Set
	s.Add(firstPath, []byte("com.example.first=new\n"))
	s.Add(secondPath, []byte("com.example.second=1\n"))

	// Use a seam on osRename to:
	//   - Succeed (via origRename) for the first rename.
	//   - Delete the first file's backup, then fail with a crafted error
	//     for the second rename — simulating a commit-phase failure after
	//     the backup has been lost.
	origRename := osRename
	defer func() { osRename = origRename }()

	renameCount := 0
	osRename = func(oldpath, newpath string) error {
		renameCount++
		if renameCount == 1 {
			return origRename(oldpath, newpath)
		}
		// Delete the first file's backup so rollback cannot restore it.
		_ = os.Remove(firstPath + ".pre-label")
		return os.ErrInvalid // arbitrary non-nil error that's not IsExist/IsNotExist
	}

	err := s.Commit("", nil)
	if err == nil {
		t.Fatal("expected Commit to fail")
	}

	// Verify it's a RollbackFailedError.
	var rfe *RollbackFailedError
	if !errors.As(err, &rfe) {
		t.Fatalf("expected RollbackFailedError, got %T: %v", err, err)
	}

	// The first file should be in its post-rename state (not rolled back).
	got, _ := os.ReadFile(firstPath)
	if string(got) == string(originalContent) {
		t.Error("first file was rolled back despite missing backup")
	}
	if string(got) != "com.example.first=new\n" {
		t.Errorf("first file content = %q, want post-rename content", got)
	}

	// The first file's backup should be gone.
	if _, statErr := os.Stat(firstPath + ".pre-label"); !os.IsNotExist(statErr) {
		t.Errorf("expected first backup to be missing, stat err=%v", statErr)
	}

	// The error should name the corrupted path.
	found := false
	for _, p := range rfe.CorruptedPaths {
		if p == firstPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("CorruptedPaths should include %s, got %v", firstPath, rfe.CorruptedPaths)
	}

	// The second path should not exist (the temp file was cleaned up).
	if _, statErr := os.Stat(secondPath); !os.IsNotExist(statErr) {
		t.Errorf("second path should not exist (rename never succeeded), stat err=%v", statErr)
	}
}

// TestCommit_noopRemovalIsNotRolledBackAsError verifies that a no-op
// removal (path never existed) is not spuriously rolled back with an
// os.Remove call when a later action fails. Without the kind-based
// branch in rollback(), the no-op removal's !hadBackup would trigger
// os.Remove on a never-existent path, producing a spurious ENOENT
// error and a false RollbackFailedError.
//
// Loop order in Commit: entries rename first, then removals remove.
// This test stages: one entry (new file, no backup), one no-op removal
// (non-existent path), and one failing removal (non-empty directory).
// The no-op removal is tracked in actions before the failure, so
// rollback must handle it correctly.
func TestCommit_noopRemovalIsNotRolledBackAsError(t *testing.T) {
	dir := t.TempDir()

	// Entry: a new file (no pre-existing content, so hadBackup=false).
	entryPath := filepath.Join(dir, "web.labels")

	// No-op removal: a path that does not exist on disk.
	noopPath := filepath.Join(dir, "never-existed.labels")

	// Failing removal: a non-empty directory, so os.Remove will fail.
	failPath := filepath.Join(dir, "fail-removal.labels")
	if err := os.MkdirAll(failPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(failPath, "placeholder"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	var s Set
	s.Add(entryPath, []byte("com.example.new=1\n"))
	s.Remove(noopPath)
	s.Remove(failPath)

	err := s.Commit("", nil)
	if err == nil {
		t.Fatal("expected Commit to fail")
	}

	// Verify it's a plain error, not RollbackFailedError.
	var rfe *RollbackFailedError
	if errors.As(err, &rfe) {
		t.Fatalf("expected clean rollback error, got RollbackFailedError: %v", err)
	}

	// Entry should not exist (new file, rolled back via os.Remove).
	if _, statErr := os.Stat(entryPath); !os.IsNotExist(statErr) {
		t.Errorf("entry should have been removed during rollback, stat err=%v", statErr)
	}

	// No-op removal path should still not exist.
	if _, statErr := os.Stat(noopPath); !os.IsNotExist(statErr) {
		t.Errorf("no-op removal path should not exist, stat err=%v", statErr)
	}

	// No .pre-label should exist for the no-op removal path.
	if _, statErr := os.Stat(noopPath + ".pre-label"); !os.IsNotExist(statErr) {
		t.Errorf("no .pre-label should exist for never-existent path, stat err=%v", statErr)
	}
}

// TestCommit_preserveOwnerOnBrandNewFile is a regression test for a bug
// where Commit unconditionally chowned path+".pre-label" whenever
// PreserveOwner was set, even for an entry that had nothing to back up
// (a brand-new file, e.g. a service's first label_file — the tool's
// default behaviour). backup() correctly skips creating a .pre-label
// for a file that didn't previously exist, but the chown call ran
// anyway and failed with ENOENT. This only surfaces when running as
// root (PreserveOwner is a no-op otherwise), which is exactly the
// documented sudo/root use case this feature exists for — see
// DECISIONS.md's ownership-preservation decision. Skips under a
// non-root test runner, since that's the only condition under which
// PreserveOwner actually does anything.
func TestCommit_preserveOwnerOnBrandNewFile(t *testing.T) {
	if !fspreserve.IsRoot() {
		t.Skip("PreserveOwner only takes effect when running as root; skipping under non-root test runner")
	}

	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(composePath, []byte("services:\n  web: {}\n"), 0o644); err != nil {
		t.Fatalf("seeding compose file: %v", err)
	}
	newLabelPath := filepath.Join(dir, "web.labels")

	var s Set
	s.Add(newLabelPath, []byte("com.example.a=1\n"))
	s.PreserveOwner(composePath)

	if err := s.Commit("", nil); err != nil {
		t.Fatalf("Commit with PreserveOwner on a brand-new file: %v", err)
	}

	if _, statErr := os.Stat(newLabelPath + ".pre-label"); !os.IsNotExist(statErr) {
		t.Errorf("no .pre-label should exist for a file that never previously existed, stat err=%v", statErr)
	}
}
