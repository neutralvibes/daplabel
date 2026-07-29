// Package fspreserve provides platform-aware helpers for preserving file
// ownership when daplabel is run with elevated privileges.
//
// On Windows, file ownership is not handled by daplabel: POSIX-style
// root detection and chown do not apply, so all operations here are
// explicit no-ops.
package fspreserve

// IsRoot always returns false on Windows: daplabel does not attempt to
// detect or act on elevated privileges on this platform.
func IsRoot() bool {
	return false
}

// Owner returns sentinel values indicating ownership preservation is not
// applicable on Windows. It never returns an error.
func Owner(path string) (uid, gid int, err error) {
	return -1, -1, nil
}

// Chown is a no-op on Windows.
func Chown(path string, uid, gid int) error {
	return nil
}
