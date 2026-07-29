# Project specification

## Purpose

This document defines the functional requirements for `daplabel`.

It specifies the externally observable behaviour of the application and serves as the authoritative reference for implementation.

Where implementation differs from this specification, this specification shall take precedence until formally revised.

---

## Contents

1. Scope
2. Terminology
3. General requirements
4. Command-line interface
5. Configuration
6. Compose project discovery
7. Commands
8. Processing rules
9. User interaction
10. Error handling
11. Dependencies
12. Future considerations

---

## 1. Scope

`daplabel` is a command-line utility for managing Docker Compose service labels.

It shall provide functionality to:

* discover Docker Compose projects;
* inspect service labels;
* extract labels into external label files;
* maintain label files;
* apply labels from templates;
* add and remove labels from services;
* survey one or more Compose projects for label usage.

The tool shall operate on one or more Docker Compose applications while preserving existing project structure wherever practical.

---

## 2. Terminology

### 2.1 Application

A directory containing a Docker Compose project.

---

### 2.2 Compose file

A YAML file used by Docker Compose to define services.

Recognised filenames are defined in section 6.

---

### 2.3 Service

A named service defined within a Compose file.

---

### 2.4 Label

A Docker Compose service label in `key=value` format.

---

### 2.5 Label file

A plain text file containing one label per line.

Each non-empty line shall represent a single `key=value` label.

When `daplabel` creates a new label file for a service (via `generate` or
`add`), the file shall be named `<service>.labels` and placed in the same
directory as the Compose file it belongs to (e.g. a service named `web`
yields `web.labels`). This applies only to files `daplabel` itself creates;
a service's existing `label_file` entries, however they are named, shall
always be respected and written back to as-is (see §8.2, §8.4).

---

### 2.6 label_file

A Compose service attribute referencing an external label file.

Multiple `label_file` entries may be present for a single service.

---

### 2.7 Template

A reusable label file that may include substitution variables.

Templates are plain text files stored in a defined template directory.

---

### 2.8 Parent directory

A directory containing one or more application directories.

When recursive mode is used, only immediate child directories shall be processed.

No deep traversal shall occur unless explicitly specified in a future version.

---

### 2.9 Survey

A read-only operation that inspects Compose projects and reports label-related information.

Survey operations shall not modify any files.

---

## 3. General requirements

### 3.1 Safety

`daplabel` shall not modify any files unless explicitly instructed by the user.

Any operation that modifies files shall require confirmation unless non-interactive mode is enabled.

---

### 3.2 Predictability

Given identical inputs, configuration, and file state, `daplabel` shall produce consistent results.

---

### 3.3 Transparency

Before modifying any files, `daplabel` shall display sufficient information for the user to understand the intended changes.

---

### 3.4 Preservation

Where practical, existing formatting, ordering, and comments shall be preserved.

Where preservation is not possible, resulting files shall remain valid and readable.

---

### 3.5 Atomicity

Operations on a single application shall either:

* complete successfully, or
* leave the application unchanged.

Temporary files or backups shall be used where necessary to ensure atomic behaviour.

---

### 3.6 User intent

Where ambiguity exists, `daplabel` shall:

* request clarification, or
* abort the operation.

It shall not infer intent silently.

---

## 4. Command-line interface

### 4.1 General syntax

`daplabel` shall implement a subcommand-based interface.

```text
daplabel [GLOBAL_OPTIONS] COMMAND [COMMAND_OPTIONS] [ARGS]
```

Global options shall be evaluated before command execution.

---

### 4.1.1 Path arguments

Where a command accepts a PATH or PATHS argument, a comma-separated list of paths may be supplied in a single argument (e.g. `./proj1,./proj2`). All paths in the expanded list are processed equivalently to passing them as separate arguments. Whitespace around commas is removed.

Commands that accept a single positional PATH (`template apply`) iterate over every expanded path. Commands that accept multiple PATH arguments (`survey`, `generate`, `clean`, `add`, `remove`) expand commas in each argument independently.

---

### 4.2 Global options

The following global options shall be supported:

| Option          | Description                           |
| --------------- | ------------------------------------- |
| `--help`        | Display help and exit.                |
| `--version`     | Display version information and exit. |
| `--config FILE` | Specify configuration file path.      |
| `--dry-run`     | Show actions without modifying files. |
| `-y`, `--yes`   | Assume "yes" for all confirmations.   |
| `--verbose`     | Enable detailed output.               |
| `-q`, `--quiet` | Suppress non-essential output.        |
| `--on-conflict=<first\|last\|skip>` | Resolve label_file key conflicts non-interactively (default `skip`); see §8.2.1. |

---

### 4.3 Command behaviour

All commands shall:

* validate input before execution;
* operate independently per application;
* continue processing subsequent applications where errors are recoverable;
* terminate on unrecoverable errors.

---

### 4.4 Confirmation behaviour

Commands that modify files shall require confirmation unless `--yes` is specified.

Prompts shall clearly describe the action to be performed.

---

### 4.5 Non-interactive mode

When `-y` or `--yes` is specified:

* all confirmation prompts shall be suppressed;
* actions shall proceed automatically;
* warnings and errors shall still be displayed.

`--dry-run` shall override any modification behaviour. No files shall be changed when `--dry-run` is active.

---

### 4.6 Help output

The --help output for the main command and for every subcommand shall include:

A usage synopsis – showing the exact command syntax and argument order.

A description of all options and arguments – concise but complete.

At least two concrete, realistic examples – demonstrating common workflows and edge cases (e.g., single project vs recursive mode, template usage).

The examples must use realistic label keys, service names, and paths so that users can adapt them immediately.

---

## 5. Configuration

### 5.1 Configuration model

Configuration shall consist of simple `KEY=value` assignments.

Only recognised keys shall be processed.

Unrecognised keys shall be ignored.

---

### 5.2 Configuration precedence

Configuration values shall be applied in the following order (highest first):

1. Command-line options
2. Environment variables
3. User configuration file
4. System configuration file
5. Defaults

Higher precedence values shall override lower ones.

---

### 5.3 Configuration discovery

Unless explicitly specified, configuration shall be discovered in:

* `$XDG_CONFIG_HOME/daplabel/config`
* `~/.config/daplabel/config`

The system configuration file (§5.2, precedence tier 4) shall be discovered at `/etc/daplabel/config`.

If executed via `sudo`, configuration shall default to the invoking user where possible.

---

### 5.4 Configuration variables

Supported configuration keys:

| Key                     | Description                          |
| ----------------------- | ------------------------------------ |
| `DAPLABEL_PARENT_DIR`   | Default root directory for scanning. |
| `DAPLABEL_TEMPLATE_DIR` | User template directory.             |
| `DAPLABEL_EDITOR`       | Preferred editor.                    |
| `DAPLABEL_LIST_SAFE`    | Enable safe listing behaviour.       |
| `DAPLABEL_DEFAULT_SURVEY_FORMAT` | Default survey output format (`tree`, `plain`, `table`, or `json`). If unset or invalid, defaults to `plain`. |
| `DAPLABEL_CONFIRM_TIMEOUT` | Idle timeout for the confirmation prompt while the system-wide lock is held (e.g. `5m`, `300s`). Defaults to `5m`. If the value is not a valid duration, the default is used and a warning is printed. |

---

## 6. Compose project discovery

### 6.1 Recognised Compose files

The following filenames shall be recognised:

```text
compose.yml
compose.yaml
docker-compose.yml
docker-compose.yaml
```

---

### 6.2 Discovery precedence

Docker's own filename resolution order is, for reference:

1. `compose.yaml`
2. `compose.yml`
3. `docker-compose.yaml`
4. `docker-compose.yml`

`daplabel` does not use this order to select among multiple present files. If more than one recognised Compose filename exists in a directory, the directory shall be skipped regardless of which filenames are present — see §6.4. This order is documented only so that discovery logic recognises the same filenames Docker Compose recognises; it is not a tie-breaking rule within `daplabel`.

---

### 6.3 Application discovery

A directory shall be treated as an application if it contains at least one recognised Compose file.

---

### 6.4 Ambiguity handling

If multiple valid Compose files exist within a single directory:

* the directory shall be skipped;
* a warning shall be reported.

---

### 6.5 Recursive discovery

When recursive mode is enabled:

* only immediate child directories of the parent directory shall be scanned;
* no nested recursion shall occur;
* non-matching directories shall be skipped.

---

### 6.6 Explicit Compose file mode

If a Compose file is explicitly provided:

* automatic discovery shall be disabled;
* only the specified file shall be processed;
* validation shall occur before processing.

---

### 6.7 Validation

Before processing, Compose files shall be validated for:

* existence;
* readability;
* presence of a `services` section.

Failure shall result in skipping the application with an error message.

---

## 7. Commands

### 7.1 General

All commands shall operate on one or more discovered applications unless explicitly constrained.

Each command shall operate independently per application.

Failure in one application shall not prevent processing of subsequent applications unless the error is fatal.

---

### 7.2 generate

The `generate` command shall extract inline service labels from Compose files and write them to external label files.

It shall:

* detect service definitions;
* extract `labels`;
* create or update label files (see §2.5 for the naming convention used for newly created files);
* replace inline labels with `label_file` references.

If label files already exist, behaviour shall be determined by user confirmation or `--yes`.

---

### 7.3 add

The `add` command shall add one or more labels to a service.

It shall support:

* direct label input;
* template-based label application;
* multi-project operation via `--recursive`/`-r`.

Existing labels shall not be overwritten unless explicitly requested.

If a service has no existing `label_file` references, a newly created
label file shall follow the naming convention in §2.5. If a service already
has one or more `label_file` references, newly added keys shall be appended
to the first such reference rather than creating an additional file.

In multi-project mode:

* `--service NAME` applies labels to the named service in every project;
* `--service-all` (`-a`) applies labels to every service in every project;
* without either flag, direct labels are applied to every service in a
  project that has multiple services, while `--template` requires an
  explicit `--service` or `--service-all` selection.

---

### 7.4 remove

The `remove` command shall remove one or more labels from a service.

If removal results in an empty label set, the resulting state shall be defined by `--on-empty-create` behaviour (see section 8.6).

---

### 7.5 survey

The `survey` command shall:

* scan applications;
* report services and labels;
* indicate presence or absence of label files;
* perform no modifications.

Output shall be human-readable and optionally machine-parsable.

#### 7.5.1 Output format

Two output formats shall be supported:

* **`tree`** — a hierarchical, indented rendering of each
  scanned project, showing the project directory, its Compose file(s),
  each service, and that service's labels (inline and/or `label_file`),
  using box-drawing connectors to show nesting.
* **`plain`** (default when no config key is set) — a flat,
  non-hierarchical listing of the same information.

The default format shall be determined by the
`DAPLABEL_DEFAULT_SURVEY_FORMAT` configuration key (§5.4). If that key
is not set or its value is not a recognised format, the default shall be
`plain`. An explicit `--format-tree`, `--format-plain`, `--format-table`,
or `--format-json` flag shall always override the configured default.
An unrecognised `--format` value shall be treated as an error; it shall
not silently fall back to a default.

Both formats shall present identical underlying information — the choice
of format shall affect presentation only, never what is reported.

#### 7.5.2 Override file reporting

Where a Compose file has an automatically-loaded override sibling
(`docker-compose.override.yml` / `compose.override.yaml`, per §6.1), both
output formats shall:

* report the override file's inline labels separately from the base
  file's, never merged into a single combined view;
* explicitly flag any inline label key present in both files, since
  Docker Compose itself resolves such a conflict at container-start
  time (the override's value takes effect) and `survey` shall make that
  visible rather than silently show only one file's value.

`survey` shall not compute or display a single "effective" merged label
value for a key present in both files; only Docker Compose itself is
authoritative about which value is actually used by a running container.

---

### 7.6 template

The `template` command shall manage label templates.

It shall support:

* listing available templates;
* applying templates to services;
* creating new templates from existing label sets.

---

### 7.7 config

The `config` command shall:

* display current configuration values;
* validate configuration sources;
* assist in debugging configuration resolution.

It shall not modify configuration unless explicitly extended in future versions.

---

### 7.8 clean

The `clean` command shall remove `.pre-label` backup files from project
directories.

It shall:

* discover project directories using the same rules as `survey` and
  `generate`;
* remove every file in a discovered project directory whose name ends with
  the `.pre-label` suffix;
* leave all other files in the directory untouched.

The `clean` command shall support `--dry-run`, `-r` / `--recursive`, and
`-y` / `--yes`. When `--dry-run` is enabled, it shall list the `.pre-label`
files that would be removed without deleting them. When neither `--yes` nor
`--dry-run` is specified, it shall prompt the user once per discovered
project directory, naming the number of files in that directory. The
standard responses apply: `Y` proceeds with the current directory, `N`
skips the current directory, `Q` terminates the command, and `A` proceeds
with the current directory and every remaining directory without further
prompts.

---

## 8. Processing rules

### 8.1 Label extraction

When extracting labels:

* each service shall be processed independently;
* labels shall be normalised into `key=value` format;
* duplicate labels shall be resolved deterministically.

---

### 8.2 label_file handling

Services may contain one or more `label_file` entries.

When processing label files:

* each file shall be read sequentially;
* missing files shall result in warnings;
* multiple label files shall be merged logically.

If a service already references a label file, new label files shall be appended rather than replacing existing entries.

#### 8.2.1 Collision handling

If the same key is defined with differing values across two or more label files for a service:

* by default, the key shall be skipped and a warning reported naming the service, key, and the conflicting files — no value shall be silently chosen;
* `--on-conflict=<first|last|skip>` shall resolve conflicts non-interactively (default `skip`), for use with `--yes` or automation;
* in interactive mode, without `--on-conflict` specified, the user shall be prompted per conflict to keep the first value, keep the last value, or skip the key, with an option to apply that choice to all remaining conflicts in the current execution (see §9.2.1).

---

### 8.3 Template expansion

Templates may contain substitution variables.

Supported variables include:

* `$SERVICE_NAME`
* `$APP_DIR`
* `$APP_NAME`

Unrecognised variables shall remain unchanged.

Variable matching shall use exact, whole-token matching against the names above; partial or prefix matches shall not be substituted.

A literal `$` immediately preceding what would otherwise match a variable name shall be escaped by doubling it: `$$SERVICE_NAME` shall produce the literal text `$SERVICE_NAME` rather than being substituted.

---

### 8.4 Backup behaviour

Before modifying any Compose file, or any existing label file:

* a backup shall be created with `.pre-label` suffix;
* backups shall be stored in the same directory as the original file.

This applies independently of, and in addition to, confirmation/`--dry-run` controls: confirmation governs whether a change is authorised, while the backup provides recovery after an authorised change.

---

### 8.5 Comment preservation

Where possible, comments in Compose files shall be preserved during modification.

Where preservation is not feasible, structural integrity shall take priority.

---

### 8.6 Empty label handling

If a service results in no labels:

- no label file shall be created by default;
- no changes shall be made unless explicitly requested;
- when `--on-empty-create` is specified, an empty label file shall be created and referenced.

---

### 8.7 Missing label file creation

If a service references a label file that does not exist:

- no file shall be created by default;
- a warning shall be emitted;
- when `--on-none-create` is specified, the missing label file shall be created.

---

## 9. User interaction

### 9.1 Prompts

When interactive input is required, prompts shall:

* clearly describe the action being confirmed;
* state the consequences of proceeding;
* provide a clear default option;
* accept standard responses (Y/N/Q where applicable).

If `A` is selected:

- all subsequent confirmation prompts for the current command execution shall be suppressed;
- all remaining applicable operations shall proceed automatically.


---

### 9.2 Standard responses

Where confirmation prompts are used:

- `Y` or `y` shall proceed with the current operation.
- `N` or `n` shall skip the current operation.
- `Q` or `q` shall terminate execution immediately.
- `A` or `a` shall proceed with all remaining operations in the current command execution without further prompts.

---

### 9.2.1 Label conflict prompts

Where a label key collides across multiple `label_file` entries (§8.2.1) and no `--on-conflict` value has been given, a distinct prompt shall be shown per conflict, offering:

- keep the first value;
- keep the last value;
- skip the key;
- apply the chosen resolution to all remaining conflicts in the current execution.

This prompt is distinct from the Y/N/Q/A confirmation prompt in §9.1–9.2, though it reuses the same "apply to all remaining" concept for consistency.

---

### 9.3 Output behaviour

Output shall be:

* concise by default;
* informative when `--verbose` is enabled;
* minimal when `--quiet` is enabled.

---

### 9.4 Dry-run mode

When `--dry-run` is enabled:

* no files shall be modified;
* all proposed changes shall be displayed;
* execution shall otherwise proceed as normal.

---

## 10. Error handling

### 10.1 Exit codes

The following exit codes shall be used:

| Code | Meaning               |
| ---- | --------------------- |
| 0    | Success               |
| 1    | General error         |
| 2    | Invalid arguments     |
| 3    | Configuration error   |
| 4    | File system error     |
| 5    | Compose parsing error |
| 6    | Lock contention       |

---

### 10.2 Error behaviour

Errors shall:

* be reported with meaningful context;
* not expose internal implementation details unless `--verbose` is enabled;
* terminate processing only when unrecoverable.

---

### 10.3 Warnings

Warnings shall:

* not terminate execution;
* be displayed unless `--quiet` is enabled;
* clearly indicate the affected application or service.

---

### 10.4 System-wide advisory lock

`daplabel` acquires a system-wide advisory lock before any write operation
(`add`, `remove`, `generate`, `template create`) to prevent concurrent
processes from racing on the same file. The lock is held for the entire
read-modify-write lifecycle, including the interactive confirmation prompt.

The lock file is stored at `$TMPDIR/daplabel-lock/daplabel.lock` (or
`/tmp/daplabel-lock/daplabel.lock` if `$TMPDIR` is unset). If another
process already holds the lock, `daplabel` exits immediately with code 6
and displays the PID, user, timestamp, and command of the process holding
the lock.

A stale lock (left behind by a crashed process) can be removed with the
`--force-unlock` flag, which deletes the lock file and exits without
performing any operation.

The confirmation prompt has a 5-minute idle timeout (configurable via
`DAPLABEL_CONFIRM_TIMEOUT`) to prevent an unattended terminal from holding
the lock indefinitely.

---

## 11. Dependencies

`daplabel` is distributed as a single compiled binary. It shall:

* avoid unnecessary external runtime dependencies;
* rely on `docker compose` as its sole required external runtime
  dependency, used exclusively for Compose file validation (§6.7);
* embed its YAML-processing capability at compile time rather than
  depending on an external YAML tool being present on the host at
  runtime.

There is no optional or degraded-functionality dependency mode:
`docker compose` is mandatory for the validation this specification
requires (§6.7), with no fallback behaviour if it is absent. *(This
corrects a previous inconsistency in this section, which described
dependencies as optional with a degraded fallback; no such fallback has
ever existed or been implemented — see DECISIONS.md Decisions 9 and 11.)*

---

## 12. Future considerations (informative)

The following items are explicitly out of scope for Version 1 but may be considered in future versions:

* remote template repositories;
* template versioning and updates;
* audit mode with rule enforcement;
* integration with CI/CD pipelines;
* JSON or machine-parsable output modes beyond survey;
* plugin architecture;
* multi-repository orchestration.

These items shall not be assumed to exist in Version 1 behaviour.

---

## End of Document
