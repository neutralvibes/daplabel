// Package lockfile implements a system-wide advisory lock that covers the
// full read-modify-write lifecycle of every daplabel command that writes
// files (add, remove, generate, template create).
//
// The lock is a single, whole-invocation mutex stored under os.TempDir().
// Per-file locking was considered and rejected because label_file
// references can escape their project directory (absolute paths or paths
// containing ../), so a file can be shared across projects that discovery
// treats as separate. A system-wide lock closes that gap by construction
// without path canonicalisation, deduplication, or lock-ordering schemes.
// The accepted trade-off is that unrelated concurrent operations are
// serialised against each other; this is judged acceptable for this tool's
// audience (home-lab/self-hosted, single or a few trusted operators).
//
// The lock is acquired with os.OpenFile(..., O_CREATE|O_EXCL), which is
// atomic exclusive-create on every target platform. No flock, LockFileEx,
// or cgo is required. The package deliberately does not check whether the
// PID recorded in a stale lock file is still alive: PIDs are reused after
// wraparound, and a PID recorded inside a container is meaningless on the
// host or in another container. Recovery from a stale lock is the
// operator's explicit responsibility via --force-unlock.
//
// See docs/DECISIONS.md Decision 46 for the full rationale and accepted
// limitations.
package lockfile
