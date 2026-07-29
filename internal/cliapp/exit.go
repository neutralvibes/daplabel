package cliapp

// ExitError carries a specific process exit code (SPECIFICATION.md §10.1)
// alongside the error to display. Command RunE functions return this
// instead of a plain error whenever the spec requires something other
// than the generic "1" (e.g. survey's "2" for a bad --format value, "4"
// for no Compose files found). main() unwraps it via errors.As.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }
