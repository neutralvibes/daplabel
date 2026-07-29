// Package debuglog provides an opt-in debug-logging facility for
// write-path commands (add, remove, generate, template apply). When
// --debug-log FILE is given, every meaningful internal decision point
// during that run is appended as a timestamped line to that file,
// independent of --quiet/--verbose's effect on normal stdout/stderr.
//
// When no file is configured (the default), every method is a no-op so
// callers don't need to guard every log call with a nil check.
package debuglog

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Logger writes timestamped debug lines to a file. A nil *Logger (or one
// created by New with an empty path) is a valid no-op logger — every
// method returns immediately without doing anything.
type Logger struct {
	mu  sync.Mutex
	w   io.Writer
	buf []byte
}

// New returns a Logger that appends to path, or a no-op Logger if path is
// empty. The file is opened for append (created if it doesn't exist) on
// the first write, not here, so callers that never log anything don't
// create an empty file.
func New(path string) *Logger {
	if path == "" {
		return nil
	}
	return &Logger{buf: []byte(path)}
}

// Logf formats a line, prepends an RFC3339 timestamp, and appends it to
// the log file. If the file hasn't been opened yet, it's opened here
// (once). If the file can't be opened or written to, the error is
// silently discarded — debug logging is best-effort and must never cause
// a command to fail.
func (l *Logger) Logf(format string, args ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.w == nil {
		f, err := os.OpenFile(string(l.buf), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		l.w = f
		l.buf = nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(l.w, "%s ", now)
	fmt.Fprintf(l.w, format, args...)
	fmt.Fprintln(l.w)
}

// Close flushes and closes the underlying file, if one was opened. It is
// safe to call on a nil Logger.
func (l *Logger) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w != nil {
		if c, ok := l.w.(io.Closer); ok {
			_ = c.Close()
		}
		l.w = nil
	}
}
