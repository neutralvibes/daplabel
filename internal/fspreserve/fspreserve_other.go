// Package fspreserve provides platform-aware helpers for preserving file
// ownership when daplabel is run with elevated privileges.
//
// This fallback implementation covers any platform other than Linux and
// Windows and Mac. Ownership preservation is not implemented on these platforms,
// so all operations are safe no-ops.
//go:build !linux && !windows && !darwin

package fspreserve

// IsRoot always returns false on unsupported platforms.
func IsRoot() bool {
	return false
}

// Owner returns sentinel values indicating ownership preservation is not
// implemented on this platform. It never returns an error.
func Owner(path string) (uid, gid int, err error) {
	return -1, -1, nil
}

// Chown is a no-op on unsupported platforms.
func Chown(path string, uid, gid int) error {
	return nil
}
