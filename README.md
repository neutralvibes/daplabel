# daplabel

[![CI](https://github.com/neutralvibes/daplabel/actions/workflows/test.yml/badge.svg)](https://github.com/neutralvibes/daplabel/actions/workflows/test.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/neutralvibes/daplabel)](https://goreportcard.com/report/github.com/neutralvibes/daplabel)
[![Release](https://img.shields.io/github/v/release/neutralvibes/daplabel)](https://github.com/neutralvibes/daplabel/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE.md)

A command-line tool for managing Docker Compose service labels safely and consistently across one or many projects.

Managing labels across a growing collection of Compose projects can get messy. Some labels live inline under `labels:`, others live in external `label_file:` files, and conventions that made sense for one project may not be obvious six months later.

`daplabel` gives you one consistent way to inspect, add, remove, extract, and manage those labels — without having to edit every Compose file by hand.

It is designed to be **safe by default**:

- Changes are validated with `docker compose config` before they are written.
- Writes are confirmed before they touch disk.
- `--dry-run` lets you preview changes without modifying anything.
- Multi-file changes are committed atomically, with automatic rollback if a write fails partway through.
- Conflicting label definitions can be handled explicitly instead of silently guessing.

Whether you're managing one Compose project or a directory full of them, `daplabel` helps keep your labels organised and predictable.

---

## Why daplabel?

Docker Compose labels are useful for everything from monitoring and backups to automation and service discovery. But once you have more than a few services, managing them consistently becomes tedious.

For example, you might start with labels directly in a Compose file:

```yaml
services:
  web:
    image: example/web
    labels:
      - com.example.environment=production
      - com.example.team=platform
```

As your configuration grows, you may prefer to keep labels separate:

```yaml
services:
  web:
    image: example/web
    label_file:
      - web.labels
```

Now you have to keep track of:

- Which labels are inline?
- Which labels are in external files?
- Which services have `label_file:` references?
- Are the same keys defined in more than one place?
- Which projects are missing a particular label?
- How do you apply the same set of labels consistently across many projects?

`daplabel` is built to make those tasks straightforward.

---

## Features

- **Survey** — inspect services, labels, and label files across one or many projects in tree, plain, table, or JSON format.
- **Filter** — find services by label key/value or identify services missing a particular label.
- **Generate** — extract inline `labels:` from Compose files into external label files and add the required `label_file:` references.
- **Add** — add labels directly, from templates, or using a combination of both.
- **Remove** — remove label keys wherever they are defined, whether inline or in external label files.
- **Templates** — create reusable label sets with `$SERVICE_NAME`, `$APP_DIR`, and `$APP_NAME` substitution variables.
- **Multi-project mode** — operate across multiple paths or immediate subdirectories with `--recursive`.
- **Safe writes** — confirm changes before writing, preview with `--dry-run`, or use `--yes` for non-interactive operation.
- **Validation** — validate Compose configuration before committing changes.
- **Atomic writes** — multi-file changes are committed as a unit, with automatic rollback when possible.
- **Conflict resolution** — explicitly choose how cross-file key conflicts are handled with `--on-conflict=first|last|skip`.

---

## Prerequisites

- **Go 1.26+** — required when building from source.
- **`docker compose`** — the only runtime dependency. It is used to validate Compose configuration before write operations.

---

## Installation

### Automatic Installation

Install the latest pre-compiled binary for Linux or macOS automatically using the setup utility:

```bash
curl -fsSL https://githubusercontent.com | sh
```

### From source

```sh
go install github.com/neutralvibes/daplabel/cmd/daplabel@latest
```

### Pre-built binaries

Pre-built releases are available for:

- Linux AMD64
- Linux ARM64
- Linux ARMv7
- macOS Intel (AMD64)
- macOS Apple Silicon (ARM64)
- Windows AMD64

Download the appropriate archive from the [releases page](https://github.com/neutralvibes/daplabel/releases), extract it, and place the `daplabel` binary somewhere on your `PATH`.

For example, on Linux:

```sh
tar xzf daplabel_linux_amd64.tar.gz
sudo mv daplabel /usr/local/bin/
```

### Uninstalling

`daplabel` does not install a daemon or maintain system-wide state. It only modifies the paths you explicitly operate on.

Remove the binary from wherever you installed it and, if you no longer need it, remove your configuration:

```sh
sudo rm /usr/local/bin/daplabel
rm -rf ~/.config/daplabel
```

If you set `DAPLABEL_TEMPLATE_DIR` to a location outside your configuration directory, remove that separately.

If you installed using `go install`, the binary is normally located in `$(go env GOPATH)/bin` or `$GOBIN`.

---

## Quick Start

### Inspect your projects

Single project

```sh
daplabel survey /opt/dapps/frigate/
```

Output:

```text
Base Folder: /opt/dapps/frigate/

frigate
compose.yml
   frigate
     [inline]
       someother.label=somevalue
     [file: frigate.labels]
       diun.enable=true
       diun.metadata.app=frigate
Summary: 1 project, 1 service; Labels: 3 total, 1 inline, 2 in 1 files
```

See the labels used across all projects in under a common parent directory:

```sh
daplabel survey --recursive  /opt/dapps
```

### Add a label

Add a label to a service:

```sh
daplabel add web com.example.tier=frontend
```

By default, `daplabel` writes labels to an external label file and adds or updates the corresponding `label_file:` reference.

### Extract inline labels

Move inline labels into external label files:

```sh
# Single project
daplabel generate /path/to/project

# Recursive
#
# parent/
#   app1_dir/
#       compose.yml
#   app2_dir/
#       compose.yml
daplabel generate -r /path/to/parent  # Generates for app1_dir and app2_dir
```

### Apply a template

Apply a reusable label template:

```sh
daplabel add --template prod web ./myproject
```

### Preview a change

See what would happen without writing anything:

```sh
daplabel add --dry-run web com.example.tier=frontend
```

---

## Commands

### survey

Inspect services, labels, and label files without modifying anything.

```text
daplabel survey [OPTIONS] [PATHS...]
```

| Option | Description |
| --- | --- |
| `-r`, `--recursive` | Scan immediate subdirectories |
| `--format-tree` | Tree output format (default) |
| `--format-plain` | Flat, non-tree output format |
| `--format-table` | Plaintext table output format |
| `--format-json` | JSON output format |
| `--filter KEY[=VALUE]` | Show services matching `KEY` or `KEY=VALUE` (repeatable) |
| `--missing KEY` | Show services missing `KEY` (repeatable) |
| `--no-summary` | Suppress the summary block |
| `--no-wrap` | Disable column wrapping for `--format-table` |

Examples:

```sh
daplabel survey .
daplabel survey --format-table --recursive ~/compose-projects/
daplabel survey --format-json --recursive ~/compose-projects/ | jq .
daplabel survey --filter diun.enable=true --recursive ~/compose-projects/
daplabel survey --missing diun.enable --recursive ~/compose-projects/
```

---

### generate

Extract inline service labels into external label files.

```text
daplabel generate [OPTIONS] [PATHS...]
```

| Option | Description |
| --- | --- |
| `-r`, `--recursive` | Scan immediate subdirectories |
| `-f`, `--force` | Overwrite existing keys in the target label file |

If a service has no existing `label_file:` reference, `daplabel` creates a new file named `<service>.labels` alongside the Compose file.

If a service already references one or more label files, inline labels are appended to the first referenced file.

Examples:

```sh
daplabel generate .
daplabel generate --recursive ~/compose-projects/
```

---

### add

Add one or more labels to a service.

```text
daplabel add [OPTIONS] [SERVICE] [LABEL[=VALUE]...] [PATH...]
```

| Option | Description |
| --- | --- |
| `-r`, `--recursive` | Scan immediate subdirectories |
| `-s`, `--service NAME` | Target a service by name in multi-project mode |
| `-a`, `--service-all` | Apply to every service in every project |
| `-t`, `--template NAME` | Apply labels from a template |
| `-f`, `--force` | Overwrite an existing key's value |
| `-i`, `--inline` | Write into the Compose file instead of a label file |
| `--values-no-quote` | Use unquoted YAML values (only with `--inline`) |
| `-e`, `--on-empty-create` | Create an empty label file if no labels would be written |
| `-n`, `--on-none-create` | Create a missing referenced label file instead of skipping |

By default, labels are written to an external label file referenced by `label_file:`.

If a service has no existing reference, `daplabel` creates `<service>.labels` and adds the reference automatically.

Use `--inline` to write directly into the service's `labels:` block.

Examples:

```sh
daplabel add web com.example.tier=frontend

daplabel add web \
  com.example.tier=frontend \
  com.example.env=prod

daplabel add web com.example.managed

daplabel add web com.example.tier=frontend ./myproject

daplabel add --template prod web ./project

daplabel add -t prod web \
  com.example.env=staging ./project

daplabel add --inline web com.example.tier=prod

daplabel add --inline --values-no-quote \
  web com.example.port=8080

daplabel add --force web com.example.tier=prod

daplabel add --dry-run web com.example.tier=frontend

daplabel add --yes web com.example.tier=frontend
```

In multi-project mode, use `--service NAME` to target a named service across projects or `--service-all` to target every service.

---

### remove

Remove one or more label keys from a service.

```text
daplabel remove [OPTIONS] SERVICE KEY... [PATH]
```

| Option | Description |
| --- | --- |
| `-e`, `--on-empty-create` | Keep an emptied label file and its reference |
| `-n`, `--on-none-create` | Accepted for symmetry with `add`; no effect on `remove` |
| `--values-no-quote` | Accepted for symmetry with `add`; no effect on `remove` |

Keys are removed wherever they are defined — inline, in label files, or both.

If removing a key empties a label file, the file and its `label_file:` reference are removed by default.

Examples:

```sh
daplabel remove web com.example.label

daplabel remove web \
  com.example.env \
  com.example.tier

daplabel remove --on-empty-create \
  web com.example.label ./project
```

---

### template

Manage reusable label templates.

#### template list

Show available templates in the configured template directory.

```sh
daplabel template list
daplabel --config ./ci.conf template list
```

#### template create

Create a new template from label pairs. Values may contain `$SERVICE_NAME`, `$APP_DIR`, and `$APP_NAME` — they are not expanded until the template is applied.

Running create again for an existing name merges into it; existing keys are left untouched unless `--force` is given.

```sh
daplabel template create staging ENV=staging LOG_LEVEL=debug
daplabel template create prod com.example.owner='$SERVICE_NAME'
daplabel template create -f staging LOG_LEVEL=info
```

#### template apply

Apply every label in a template to a service, expanding substitution variables. This is equivalent to `add --template NAME SERVICE [PATH]` with no direct labels.

```sh
daplabel template apply prod web
daplabel template apply prod web ./project
daplabel template apply -i staging web
```

---

### config

Display resolved configuration values and where each one came from.

Never modifies configuration.

```sh
daplabel config
daplabel --config ./ci.conf config
```

---

## Configuration

Configuration uses simple `KEY=value` assignments.

Values are resolved in this order, from highest to lowest precedence:

1. Command-line options
2. Environment variables
3. User configuration: `$XDG_CONFIG_HOME/daplabel/config` or `~/.config/daplabel/config`
4. System configuration: `/etc/daplabel/config`
5. Built-in defaults

| Key | Description |
| --- | --- |
| `DAPLABEL_PARENT_DIR` | Default root directory for scanning |
| `DAPLABEL_TEMPLATE_DIR` | User template directory |
| `DAPLABEL_EDITOR` | Preferred editor |

Use:

```sh
daplabel config
```

to inspect every resolved value and its source.

---

## Global Options

These options apply to every command:

| Option | Description |
| --- | --- |
| `--config FILE` | Specify a configuration file |
| `--dry-run` | Show actions without modifying files |
| `-y`, `--yes` | Assume "yes" for all confirmations |
| `--verbose` | Enable detailed output |
| `-q`, `--quiet` | Suppress non-essential output |
| `--on-conflict=first\|last\|skip` | Resolve `label_file` key conflicts |
| `--debug-log FILE` | Append timestamped debug lines to a file |

---

## Safety and Error Handling

`daplabel` is designed to fail safely rather than leave Compose projects partially modified.

Before a write is committed:

1. The proposed changes are prepared.
2. The resulting Compose configuration is validated with `docker compose config`.
3. Confirmation is requested unless `--yes` is used.
4. Changes are committed atomically across the affected files.
5. If a multi-file write fails partway through, previously applied changes are rolled back automatically where possible.

Use `--dry-run` to preview a change without writing anything.

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success |
| `1` | General error |
| `2` | Invalid arguments |
| `3` | Configuration error |
| `4` | File system error |
| `5` | Compose parsing error |
| `6` | Lock contention |

A file system error also covers the rare case where a multi-file commit fails and automatic rollback cannot fully recover.

If you see a message beginning with `ROLLBACK FAILED`, do not immediately retry the same command. Check the paths and `.pre-label` backups identified in the error message first.

---

## Troubleshooting / FAQ

**"`docker compose` not found" / validation fails immediately**

`docker compose` is the one hard runtime dependency — it is used to validate every write before it is committed, even for a single-service `add`.

Confirm that `docker compose version` works from the same shell where you run `daplabel`:

```sh
docker compose version
```

**My label went into a `.labels` file instead of the Compose file**

That's the default behaviour, not a bug.

`add` writes to an external label file, creating one and adding a `label_file:` reference if the service doesn't have one yet, unless you pass `--inline`.

**A key I tried to remove says "not found" but I know it's there**

`remove` reports a key that isn't found anywhere as a warning, not an error.

Run `daplabel survey` first to confirm the service, path, and location of the key.

**What does `--on-conflict=skip` actually skip?**

It only matters when the same key is defined in more than one place that would apply to a service, such as inline and in a label file, or in more than one referenced label file.

`skip` (the default) leaves the conflicting write out entirely rather than guessing which value should win. Use `--on-conflict=first` or `--on-conflict=last` to pick a side explicitly, or resolve the duplication by hand.

**Running under `sudo`**

Configuration is resolved from the invoking user's home directory (not root's) when run via `sudo`, so your usual `DAPLABEL_*` settings still apply.

`template edit` is intentionally disabled when running as root. The command launches your configured `$EDITOR`, and executing an arbitrary editor with elevated privileges creates an unnecessary security risk. Edit the template as your normal user, or use `sudoedit` if elevated file access is genuinely required.

**`--recursive` didn't find a project nested two levels deep**

That's expected: `--recursive` scans immediate subdirectories only, not an arbitrary depth.

For example, `daplabel survey --recursive ~/compose/` finds `~/compose/app1/compose.yml`, but not `~/compose/group/app1/compose.yml`.

Point `--recursive` directly at the parent of each project tier, or pass multiple paths.

**A directory has both `compose.yml` and `docker-compose.yml` (or similar) — which one does daplabel use?**

Neither, automatically.

If more than one recognised Compose filename is present in a directory, that directory is skipped entirely and a warning is reported rather than silently guessing which file you meant.

Rename or remove the one you don't want resolved.

**Can I run multiple `daplabel` commands against the same project at the same time?**

No. `daplabel` acquires a system-wide advisory lock before every write operation (`add`, `remove`, `generate`, `template create`).

If another `daplabel` process already holds the lock, the second invocation exits immediately with code `6` and shows the PID, user, timestamp, and command of the process holding the lock.

Serialize writes to the same project rather than parallelizing them.

**"another operation is already in progress" — how do I clear a stale lock?**

If a `daplabel` process crashed or was killed (for example, Ctrl-C during a confirmation prompt), it may have left a stale lock file behind.

Run:

```sh
daplabel --force-unlock
```

to remove it and exit without performing any operation.

The lock file is at `$TMPDIR/daplabel-lock/daplabel.lock` (or `/tmp/daplabel-lock/daplabel.lock` if `$TMPDIR` is unset). You can also delete it directly.

**`daplabel` doesn't protect against concurrent runs under systemd `PrivateTmp=yes`**

If you run `daplabel` from a hardened systemd unit with `PrivateTmp=yes`, that unit gets its own namespaced `/tmp`, invisible to and from any other process's `/tmp`.

The system-wide lock provides no protection between a systemd-run invocation and any other invocation in that specific configuration. This is a known limitation of the lock design.

**Something went wrong mid-write — is my Compose file corrupted?**

No. Writes only ever touch temp files until validation passes, and a failure partway through a multi-file commit triggers automatic rollback of everything already applied.

See [Safety and Error Handling](#safety-and-error-handling) for the one case where rollback itself can't fully recover and manual intervention is needed.

---

## Release builds

Cross-compiled release binaries are produced by [GoReleaser](https://goreleaser.com/):

- Linux AMD64
- Linux ARM64
- Linux ARMv7
- macOS Intel (AMD64)
- macOS Apple Silicon (ARM64)
- Windows AMD64

---

## Documentation

For more detail on how daplabel is specified and built:

| File | Description |
| --- | --- |
| [`docs/SPECIFICATION.md`](docs/SPECIFICATION.md) | What the project is specified to do |
| [`docs/ENGINEERING_PRINCIPLES.md`](docs/ENGINEERING_PRINCIPLES.md) | How the project is built |

---

## License

[MIT](LICENSE.md)
