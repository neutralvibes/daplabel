package lockfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// lockDirName and lockFileName use a hyphen, not an underscore, matching
// this codebase's existing convention for self-explanatory artifact names
// (.pre-label, .daplabel-tmp-*).
const (
	lockDirName  = "daplabel-lock"
	lockFileName = "daplabel.lock"
)

// LockContentionError is returned by Acquire when another process already
// holds the system-wide lock. Callers (and main.go) can use errors.As to
// map this to exit code 6 (SPECIFICATION.md §10.1).
type LockContentionError struct {
	LockPath string
	Contents string
}

func (e *LockContentionError) Error() string {
	return fmt.Sprintf("daplabel: another operation is already in progress\n\n%s\n\nIf this is stale (the process crashed or was terminated), remove the lock with --force-unlock, or delete %s directly.",
		indentContents(e.Contents), e.LockPath)
}

func indentContents(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, line := range lines {
		lines[i] = "  " + line
	}
	return strings.Join(lines, "\n")
}

// lockPath returns the resolved path to the system-wide lock file.
func lockPath() string {
	return filepath.Join(os.TempDir(), lockDirName, lockFileName)
}

// lockDir returns the resolved path to the directory containing the lock file.
func lockDir() string {
	return filepath.Join(os.TempDir(), lockDirName)
}

// Acquire creates the system-wide daplabel lock. On success it returns a
// release function that must be called exactly once to remove the lock,
// and a nil error. On contention, it returns a non-nil error wrapping
// *LockContentionError whose message includes the full contents of the
// existing lock file.
func Acquire() (release func(), err error) {
	path := lockPath()
	dir := lockDir()

	// Create the lock directory world-writable so any user can remove a
	// stale lock file. MkdirAll's mode argument is masked by the process
	// umask, so an explicit os.Chmod is required afterward.
	if mkErr := os.MkdirAll(dir, 0o777); mkErr != nil {
		return nil, fmt.Errorf("creating lock directory %s: %w", dir, mkErr)
	}
	if chErr := os.Chmod(dir, 0o777); chErr != nil {
		return nil, fmt.Errorf("setting lock directory permissions on %s: %w", dir, chErr)
	}

	f, openErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if openErr != nil {
		if errors.Is(openErr, os.ErrExist) {
			contents, readErr := os.ReadFile(path)
			if readErr != nil {
				contents = []byte("(could not read lock file contents: " + readErr.Error() + ")")
			}
			return nil, &LockContentionError{
				LockPath: path,
				Contents: string(contents),
			}
		}
		return nil, fmt.Errorf("creating lock file %s: %w", path, openErr)
	}

	contents := fmt.Sprintf("pid: %d\nuser: %s\ncreated: %s\ncommand: %s\n",
		os.Getpid(),
		currentUsername(),
		time.Now().UTC().Format(time.RFC3339),
		strings.Join(os.Args, " "),
	)
	if _, wErr := f.WriteString(contents); wErr != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("writing lock file %s: %w", path, wErr)
	}
	if cErr := f.Close(); cErr != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("closing lock file %s: %w", path, cErr)
	}

	release = func() {
		_ = os.Remove(path)
	}
	return release, nil
}

// ForceUnlock unconditionally removes the lock file, regardless of who
// created it or whether it's held by a running process. Used by the
// --force-unlock flag. Returns nil if the lock file didn't exist.
func ForceUnlock() error {
	path := lockPath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing lock file %s: %w", path, err)
	}
	return nil
}

// currentUsername returns the name of the user running this process.
// os/user.Current() is deliberately avoided because it can fail in
// CGO_ENABLED=0 macOS release builds (see internal/config/config.go's
// invokingUserHomeDir and .goreleaser.yaml's darwin target). Environment
// variables behave identically cross-platform with no build tags.
func currentUsername() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	return "unknown"
}
