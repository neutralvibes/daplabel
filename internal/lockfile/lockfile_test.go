package lockfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// tempLockRoot sets TMPDIR to a fresh temporary directory for the
// duration of the test so that Acquire/ForceUnlock operations do not
// interfere with any real system lock or with other tests.
func tempLockRoot(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	return tmp
}

func TestAcquireCreatesLockFileWithAllFields(t *testing.T) {
	tempLockRoot(t)

	release, err := Acquire()
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer release()

	path := lockPath()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading lock file: %v", err)
	}

	// Each of the four required fields must be present and non-empty.
	required := []string{"pid:", "user:", "created:", "command:"}
	for _, prefix := range required {
		if !strings.Contains(string(contents), prefix) {
			t.Errorf("lock file missing %q field\ncontents:\n%s", prefix, contents)
		}
	}

	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) != 4 {
		t.Errorf("expected 4 lines, got %d\ncontents:\n%s", len(lines), contents)
	}

	for _, line := range lines {
		if !strings.Contains(line, ":") || strings.TrimSpace(strings.SplitN(line, ":", 2)[1]) == "" {
			t.Errorf("field has empty value: %q", line)
		}
	}
}

func TestAcquireFailsWhenLockExists(t *testing.T) {
	tempLockRoot(t)

	release, err := Acquire()
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	defer release()

	path := lockPath()
	firstContents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading lock file: %v", err)
	}

	_, err = Acquire()
	if err == nil {
		t.Fatal("second Acquire should have failed")
	}
	if !strings.Contains(err.Error(), "another operation is already in progress") {
		t.Errorf("error message missing contention text: %v", err)
	}
	// Compare line by line, ignoring the command line which differs
	// between the test's own os.Args and a literal string compare.
	for _, line := range strings.Split(string(firstContents), "\n") {
		if line == "" {
			continue
		}
		if !strings.Contains(err.Error(), line) {
			t.Errorf("error message missing line %q from lock file contents; got:\n%v", line, err)
		}
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error message should include lock path %q; got:\n%v", path, err)
	}
}

func TestAcquireContentionErrorHasContents(t *testing.T) {
	tempLockRoot(t)

	release, err := Acquire()
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	defer release()

	firstContents, err := os.ReadFile(lockPath())
	if err != nil {
		t.Fatalf("reading lock file: %v", err)
	}

	_, err = Acquire()
	lce, ok := err.(*LockContentionError)
	if !ok {
		t.Fatalf("expected *LockContentionError, got %T", err)
	}
	if lce.LockPath != lockPath() {
		t.Errorf("LockPath = %q, want %q", lce.LockPath, lockPath())
	}
	if lce.Contents != string(firstContents) {
		t.Errorf("Contents mismatch\nwant:\n%s\ngot:\n%s", firstContents, lce.Contents)
	}
}

func TestReleaseRemovesLockFile(t *testing.T) {
	tempLockRoot(t)

	release, err := Acquire()
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	path := lockPath()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}

	release()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("release() did not remove lock file: %v", err)
	}

	// A subsequent Acquire should now succeed.
	release2, err := Acquire()
	if err != nil {
		t.Fatalf("Acquire after release failed: %v", err)
	}
	release2()
}

func TestReleaseSafeWhenFileAlreadyGone(t *testing.T) {
	tempLockRoot(t)

	release, err := Acquire()
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	if err := os.Remove(lockPath()); err != nil {
		t.Fatalf("removing lock file out of band: %v", err)
	}

	// release() must not panic or return an error.
	release()
}

func TestForceUnlockRemovesExistingLock(t *testing.T) {
	tempLockRoot(t)

	release, err := Acquire()
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	// Intentionally not calling release(); ForceUnlock should clean up.
	_ = release

	if err := ForceUnlock(); err != nil {
		t.Fatalf("ForceUnlock failed: %v", err)
	}

	if _, err := os.Stat(lockPath()); !os.IsNotExist(err) {
		t.Fatalf("ForceUnlock did not remove lock file: %v", err)
	}
}

func TestForceUnlockNoOpWhenLockMissing(t *testing.T) {
	tempLockRoot(t)

	if err := ForceUnlock(); err != nil {
		t.Fatalf("ForceUnlock should return nil when lock is absent, got: %v", err)
	}
}

func TestAcquireCreatesWorldWritableDirectory(t *testing.T) {
	tempLockRoot(t)

	// On Unix, set a restrictive umask so that MkdirAll without the
	// explicit os.Chmod would otherwise produce 0755, proving the
	// post-MkdirAll chmod is doing its job.
	if runtime.GOOS != "windows" {
		oldMask := syscall.Umask(0o022)
		defer syscall.Umask(oldMask)
	}

	release, err := Acquire()
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer release()

	dir := lockDir()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat lock directory: %v", err)
	}

	if runtime.GOOS == "windows" {
		// Windows does not use POSIX permission bits; the directory
		// existing is the meaningful cross-platform assertion here.
		if !info.IsDir() {
			t.Fatal("lock path is not a directory")
		}
		return
	}

	mode := info.Mode().Perm()
	if mode != 0o777 {
		t.Errorf("lock directory mode = 0o%o, want 0o777", mode)
	}
}

func TestAcquireIdempotentDirectoryCreation(t *testing.T) {
	tempLockRoot(t)

	// Pre-create the directory with a restrictive mode; Acquire should
	// still leave it world-writable.
	dir := lockDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("pre-creating lock directory: %v", err)
	}

	release, err := Acquire()
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer release()

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat lock directory: %v", err)
	}

	if runtime.GOOS != "windows" {
		mode := info.Mode().Perm()
		if mode != 0o777 {
			t.Errorf("lock directory mode = 0o%o, want 0o777", mode)
		}
	}
}

func TestCurrentUsername(t *testing.T) {
	t.Run("both unset returns unknown", func(t *testing.T) {
		t.Setenv("USER", "")
		t.Setenv("USERNAME", "")
		if got := currentUsername(); got != "unknown" {
			t.Errorf("currentUsername() = %q, want unknown", got)
		}
	})

	t.Run("USER set", func(t *testing.T) {
		t.Setenv("USER", "alice")
		t.Setenv("USERNAME", "")
		if got := currentUsername(); got != "alice" {
			t.Errorf("currentUsername() = %q, want alice", got)
		}
	})

	t.Run("USERNAME set", func(t *testing.T) {
		t.Setenv("USER", "")
		t.Setenv("USERNAME", "bob")
		if got := currentUsername(); got != "bob" {
			t.Errorf("currentUsername() = %q, want bob", got)
		}
	})

	t.Run("USER preferred over USERNAME", func(t *testing.T) {
		t.Setenv("USER", "alice")
		t.Setenv("USERNAME", "bob")
		if got := currentUsername(); got != "alice" {
			t.Errorf("currentUsername() = %q, want alice", got)
		}
	})
}

func TestLockPathUsesTempDir(t *testing.T) {
	tmp := tempLockRoot(t)
	want := filepath.Join(tmp, lockDirName, lockFileName)
	if got := lockPath(); got != want {
		t.Errorf("lockPath() = %q, want %q", got, want)
	}
}
