// Isolates the system-wide daplabel lock (internal/lockfile) behind a
// fresh, process-local TMPDIR for the entire test binary, so this
// package's tests — which exercise real lockfile.Acquire() calls via
// templatecmd.Run — never contend with any other concurrently-running
// `go test` package binary (or a real daplabel invocation elsewhere on
// the machine) for the single real /tmp/daplabel-lock/daplabel.lock
// file. `go test ./...` runs different packages' test binaries as
// separate OS processes, by default in parallel (-p defaults to
// GOMAXPROCS) — any two of add/generate/remove/templatecmd/cliapp's
// test binaries running at once previously raced for that one lock
// file, causing sporadic "another operation is already in progress"
// failures (and knock-on failures where an early-validation test
// expected exit code 2/4 but got 6 instead, because Acquire failed
// before validation ever ran). internal/lockfile's own tests already
// solved this for themselves via an equivalent TMPDIR override
// (lockfile_test.go's tempLockRoot) — this applies the same fix to the
// packages that actually call through to the shared lock in their own
// tests, which previously had no such isolation at all.
package templatecmd

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "daplabel-test-tmpdir-*")
	if err != nil {
		panic("TestMain: creating isolated TMPDIR: " + err.Error())
	}
	if err := os.Setenv("TMPDIR", tmp); err != nil {
		panic("TestMain: setting TMPDIR: " + err.Error())
	}

	code := m.Run()
	if err := os.RemoveAll(tmp); err != nil {
		panic("TestMain: removing isolated TMPDIR: " + err.Error())
	}
	os.Exit(code)
}
