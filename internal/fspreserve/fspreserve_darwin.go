// Package fspreserve provides platform-aware helpers for preserving file
// ownership when daplabel is run with elevated privileges.
//
// Darwin (macOS) implementation is identical to Linux — the same syscall
// interfaces (os.Geteuid, os.Stat + syscall.Stat_t, os.Chown) are available
// on both platforms.
//go:build darwin

package fspreserve

import (
	"fmt"
	"os"
	"syscall"
)

// IsRoot reports whether the current process is running as the root user.
func IsRoot() bool {
	return os.Geteuid() == 0
}

// Owner returns the UID and GID that own path. If path does not exist or
// its ownership cannot be determined, an error is returned.
func Owner(path string) (uid, gid int, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return -1, -1, fmt.Errorf("stat %s: %w", path, err)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return -1, -1, fmt.Errorf("could not determine ownership of %s", path)
	}

	return int(stat.Uid), int(stat.Gid), nil
}

// Chown changes path's owner to the given UID and GID.
func Chown(path string, uid, gid int) error {
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	return nil
}
