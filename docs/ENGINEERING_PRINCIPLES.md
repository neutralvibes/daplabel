# Engineering principles

## Purpose

This document defines the engineering principles and development standards that govern this project.

Its purpose is to promote consistency, maintainability, reliability and production-quality software. These principles should be applied throughout the project's lifecycle and, where appropriate, reused by future projects.

## 1. Engineering principles

### 1.1 Solve real problems

Every feature shall address a genuine user need.

Features shall be introduced because they solve recurring problems, improve usability or increase reliability.

Novelty alone is not sufficient justification.

### 1.2 Simplicity

Prefer the simplest solution that completely satisfies the requirement.

Avoid unnecessary abstraction, unnecessary complexity and cleverness.

### 1.3 Convention over invention

Use established operating system, shell and command-line conventions wherever practical.

Avoid introducing new concepts where existing conventions are widely understood.

### 1.4 Transparency

Operations shall be predictable.

Users should understand what the software proposes to change and what it has changed.

### 1.5 Safety

Potentially destructive operations shall:

* validate inputs;
* support preview where practical;
* create backups where appropriate;
* fail safely.

### 1.6 Respect user intent

The software shall respect explicit user choices.

Where ambiguity exists, it shall request clarification or decline to proceed.

It shall not silently guess.

### 1.7 Maintainability

Engineering decisions shall consider long-term maintenance.

Avoid unnecessary complexity that increases future maintenance effort.

### 1.8 Quality over speed

Correctness, readability and reliability shall take precedence over rapid implementation.

### 1.9 Incremental improvement

Develop software in well-defined stages.

Each stage shall produce a stable, reviewable and testable result before progressing.

## 2. Software design

### 2.1 Single responsibility

Functions should perform one logical task.

### 2.2 DRY

Avoid duplicated logic.

Code reuse shall not reduce readability or maintainability.

### 2.3 Readability

Readable code is preferred over concise code.

Names should clearly describe purpose.

### 2.4 Modularity

Separate concerns into focused functions.

Keep related functionality together.

### 2.5 Extensibility

Design for future growth without implementing speculative features.

## 3. Go development

*Section 3 originally specified Bash development conventions. Following
the migration to Go (DECISIONS.md, Decision 27), this section is replaced
in full. It is not a translation of the old rules line-for-line; several
no longer apply (e.g. quoting expansions has no Go analogue), and some
Go-native concerns (module dependency management, concurrency) have no
Bash-era counterpart to replace.*

### 3.1 Target and toolchain

Target the Go version pinned in `go.mod`; keep it current with upstream
Go's supported-release policy rather than pinning indefinitely to an
older toolchain.

Use the standard library in preference to a third-party package whenever
it fully satisfies the requirement (Engineering Principle 1.2,
Simplicity; Principle 7, Dependencies).

### 3.2 Formatting and static analysis

All code shall pass, without requiring modification:

* `gofmt` (or `goimports`, which is a superset covering import grouping);
* `go vet`;
* `golangci-lint`, run with a repository-committed configuration so the
  rule set is explicit and reviewable rather than left to each
  contributor's local defaults.

These are the direct Go-toolchain equivalents of the retired `shfmt`
and `ShellCheck` gates: mechanical, run in CI, and blocking on failure —
not advisory.

#### Suppressions

* a `//nolint` (or linter-specific) suppression comment shall always
  state which check it suppresses and why, mirroring the old
  ShellCheck rule that every suppression include a reason;
* prefer suppressing the narrowest possible scope (the offending line)
  over a file- or package-wide suppression;
* a suppression that exists only to silence noise on a large or complex
  function is a signal to reconsider the function's size (§3.4), not a
  reason to keep the suppression indefinitely.

### 3.3 Project layout and naming

Follow standard Go project conventions: an entrypoint under `cmd/`,
non-exported implementation packages under `internal/`, and any
genuinely reusable package under `pkg/` only if it is intended for use
outside this module.

Use standard Go naming (`MixedCaps`/`mixedCaps`, no underscores, no
Hungarian-style prefixes such as the Bash-era `LBL_` / `lbl_` convention).
Exported identifiers require a doc comment beginning with the
identifier's name, per standard `godoc` convention; this satisfies
Engineering Principle 5 (documentation stays synchronised with
implementation) by keeping reference documentation next to the code it
describes.

### 3.4 Functions and interfaces

Functions and methods shall:

* perform one task (Engineering Principle 2.1);
* validate parameters where the caller cannot be trusted to have done so
  already;
* return an `error` as the last return value whenever the operation can
  fail — never a bare status code, and never a panic for an expected,
  recoverable failure (panics are reserved for programmer errors that
  indicate a bug, e.g. an invariant violation, not for expected runtime
  conditions like a missing file);
* keep behind an interface only what genuinely needs to vary or be
  substituted in tests (e.g. the YAML backend, DECISIONS.md Decision 12
  / Decision 28) — an interface with exactly one production
  implementation and no test double is unnecessary abstraction
  (Engineering Principle 1.2).

### 3.5 Error handling

Every returned `error` shall be checked at the call site; an ignored
error is a defect, and `golangci-lint`'s `errcheck` (or equivalent) shall
enforce this mechanically rather than relying on review.

One narrow, explicit exception: errors returned by `fmt.Fprint`/
`Fprintf`/`Fprintln` when writing CLI output are not checked (configured
in `.golangci.yml`, not left as an unexplained gap). A write failure to
stdout/stderr is both rare and not actionable by the code doing the
writing; checking it everywhere trades real readability for negligible
robustness, which is exactly the kind of cost Simplicity (§1.2) exists to
weigh against. This exception is scoped to those three functions only —
it does not extend to file I/O, parsing, or subprocess errors, which are
always checked.

Wrap errors with context on the way up (`fmt.Errorf("doing X for
service %q: %w", name, err)`) so a failure deep in the YAML backend
arrives at the user-facing error message with enough context to act on,
without requiring `--verbose` to explain what and where.

Use `errors.Is` / `errors.As` to test for specific underlying failures
(e.g. "file does not exist" vs. "file exists but is not valid YAML")
rather than string-matching an error message.

Do not continue after an unrecoverable failure (Engineering Principle
3.5 carries over unchanged); return the wrapped error up to the caller
that is positioned to decide whether it is fatal for the whole run or
scoped to a single application (Specification §7.1).

### 3.6 File system operations

#### Temporary files

Temporary files are used in the atomic-write path. They are written into the same directory as the target file, so the rename is on the same filesystem and remains atomic. All temporary files created by the atomic-write mechanism use a single, recognisable naming prefix (`.daplabel-tmp-`), following Decision 31, so that any file left behind by an interrupted run is unambiguously identifiable as safe to delete.

When running as `root` on Linux, temporary files and backups are `chown`ed to the owner of the compose file being modified, so that the resulting files are not left owned by `root`. On Windows this behaviour is a no-op. See DECISIONS.md Decision 41.

#### Backup files

Backups carry a `.pre-label` suffix in the same directory as the original, per SPECIFICATION.md §8.4.

### 3.7 Concurrency

Goroutines and channels are permitted only where they solve a genuine
problem (Engineering Principle 1.1) — for example, processing multiple
discovered applications concurrently, if and when that is shown to be
worth the added complexity. Introducing concurrency for its own sake, or
before a real need is demonstrated, conflicts with Engineering Principle
1.2 (Simplicity).

Where used, a `context.Context` shall be threaded through for
cancellation, and every goroutine's exit path shall be accounted for
(no goroutine leaks on early return or error).

### 3.8 Testing

Table-driven tests are the default shape for functions with multiple
input/output cases, run via `go test`. Use `go test -race` in CI for any
package that uses goroutines or shared state (§3.7).

Regression tests should accompany defect fixes (Engineering Principle 6),
matching the same intent as the retired Bash-era `.bats` regression
suite, expressed as ordinary Go tests instead.

### 3.9 Dependencies

Dependencies are recorded and version-pinned in `go.mod`/`go.sum`.
Each dependency shall be justified in the same terms as Engineering
Principle 7 (Dependencies) generally — mature, widely used, and not
providing marginal benefit for its cost. A dependency's `go.sum` entry
makes its exact version independently verifiable, which is itself a
production-safety property Bash's "whatever's on `$PATH`" model did not
provide (see DECISIONS.md Decision 27, Decision 28).

## 4. User interface

### 4.1 Command-line interface

Follow established Unix command-line conventions.

Use subcommands where functionality naturally groups.

### 4.2 Output

Output shall be:

* concise;
* informative;
* consistent.

Verbose output shall be optional.

### 4.3 Prompts

Interactive prompts shall:

* be clear;
* explain consequences where appropriate;
* provide sensible default behaviour.

## 5. Documentation

Documentation shall remain synchronised with implementation.

Examples shall reflect actual behaviour.

Requirements belong in the project specification rather than source code comments.

## 6. Testing

Changes should be verified before release.

Regression tests should accompany defect fixes where practical.

## 7. Dependencies

Dependencies shall be justified.

Prefer mature, widely available tools.

Avoid introducing dependencies that provide little practical benefit.

## 8. Maintenance

Features shall remain relevant throughout the lifetime of the project.

Avoid accumulating unnecessary complexity.

Engineering decisions should reduce long-term effort for both users and maintainers.

---

End of Document
