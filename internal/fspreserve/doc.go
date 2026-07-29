// Package fspreserve provides platform-aware helpers for preserving file
// ownership when daplabel is run with elevated privileges.
//
// On Linux and Darwin (macOS), when daplabel is running as root, files it
// creates or modifies are chown'd back to the owner of the related compose
// file. On Windows and other platforms, these operations are no-ops.
package fspreserve
