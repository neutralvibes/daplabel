// Package cliapp wires the daplabel command-line interface together using
// cobra (DECISIONS.md Decision 29). Each subcommand's Use/Short/Long/Example
// fields are first-class, reviewed content: they are what satisfies
// SPECIFICATION.md §4.6's requirement that --help output for every
// subcommand include a usage synopsis, full option documentation, and at
// least two concrete, realistic examples.
package cliapp

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/neutralvibes/daplabel/internal/add"
	"github.com/neutralvibes/daplabel/internal/clean"
	"github.com/neutralvibes/daplabel/internal/config"
	"github.com/neutralvibes/daplabel/internal/configcmd"
	"github.com/neutralvibes/daplabel/internal/debuglog"
	"github.com/neutralvibes/daplabel/internal/discovery"
	"github.com/neutralvibes/daplabel/internal/generate"
	"github.com/neutralvibes/daplabel/internal/labelfile"
	"github.com/neutralvibes/daplabel/internal/lockfile"
	"github.com/neutralvibes/daplabel/internal/prompt"
	"github.com/neutralvibes/daplabel/internal/remove"
	"github.com/neutralvibes/daplabel/internal/survey"
	"github.com/neutralvibes/daplabel/internal/templatecmd"
	"github.com/neutralvibes/daplabel/internal/yamlbackend"
)

// BuildInfo holds version metadata injected at build time via linker flags
// (DECISIONS.md Decision 27's consequences). It is populated by main() from
// the package-level vars GoReleaser sets with -ldflags.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
	Repo    string
}

// Options holds the global flags defined in SPECIFICATION.md §4.2. They are
// parsed once by the root command's PersistentPreRunE and threaded down to
// subcommands via the context, rather than as package-level mutable globals
// (the Go equivalent of the Bash implementation's LBL_* globals in core.sh,
// but scoped and testable rather than process-wide state).
type Options struct {
	ConfigFile  string
	DryRun      bool
	Yes         bool
	Verbose     bool
	Quiet       bool
	OnConflict  string // first|last|skip — default "skip", see SPECIFICATION §8.2.1
	DebugLog    string // --debug-log FILE: append timestamped debug lines to FILE
	ForceUnlock bool   // --force-unlock: remove the system-wide lock and exit
}

// NewRootCmd constructs the root daplabel command. Subcommands are attached
func NewRootCmd(info BuildInfo) *cobra.Command {
	opts := &Options{OnConflict: "skip"}

	root := &cobra.Command{
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if mustBool(cmd, "force-unlock") {
				if err := lockfile.ForceUnlock(); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "System-wide lock removed.")
				os.Exit(0)
			}
			return nil
		},
		Use:     "daplabel",
		Short:   "Manage Docker Compose service labels safely and consistently",
		Version: info.Version,
		Long: `
Commands:
    add            Add labels to a service.
    clean          Remove .pre-label backup files.
    config         Display resolved configuration and value sources.
    generate       Extract inline labels to external label files.
    remove         Remove label keys from a service.
    survey         Inspect services, labels, and label files (read-only).
    template       Manage label templates (list, apply, create, edit, remove).

Safety:
    * All write operations create .pre-label backups before modifying files.
    * --dry-run previews changes without writing.
    * -y / --yes skips confirmation prompts for non-interactive use.

Conflict Resolution:
    --on-conflict=first|last|skip controls how duplicate keys across
    multiple label_file references are resolved (default: skip).`,
		Example: `  daplabel survey .
  daplabel generate --recursive ~/compose-projects/
  daplabel add web com.example.label=value`,
		SilenceUsage:  true,
		SilenceErrors: true, // main.go prints the returned error exactly once; cobra's own default printing would duplicate it
	}
	root.SetVersionTemplate(fmt.Sprintf("daplabel version %s (%s)\n", info.Version, info.Repo))

	// Custom help template: reorders sections to Usage → Brief Description →
	// Flags → Global Flags → More Info → Examples, so users see the most
	// actionable information (flags) before the detailed documentation.
	root.SetHelpTemplate(`Usage:
  {{.UseLine}}

  {{.Short}}
{{if .HasAvailableLocalFlags}}
Options:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}
{{end}}{{if .HasAvailableInheritedFlags}}
Global Options:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}
{{end}}{{if .Long}}
{{.Long}}
{{end}}
{{if .HasExample}}
Examples:
{{.Example}}
{{end}}`)

	pf := root.PersistentFlags()
	pf.StringVar(&opts.ConfigFile, "config", "", "Specify configuration file path")
	pf.StringVar(&opts.DebugLog, "debug-log", "", "Append timestamped debug lines to FILE")
	pf.BoolVar(&opts.DryRun, "dry-run", false, "Show actions without modifying files")
	pf.StringVar(&opts.OnConflict, "on-conflict", "skip", "Resolve label_file key conflicts: first|last|skip")
	pf.BoolVarP(&opts.Quiet, "quiet", "q", false, "Suppress non-essential output")
	pf.BoolVar(&opts.Verbose, "verbose", false, "Enable detailed output")
	pf.BoolVarP(&opts.Yes, "yes", "y", false, `Assume "yes" for all confirmations`)
	pf.BoolVar(&opts.ForceUnlock, "force-unlock", false, "Remove a stale system-wide lock and exit")

	root.AddCommand(
		newGenerateCmd(opts),
		newAddCmd(opts),
		newRemoveCmd(opts),
		newCleanCmd(opts),
		newSurveyCmd(opts),
		newTemplateCmd(opts),
		newConfigCmd(opts),
	)

	return root
}

// resolveFormatFlags maps survey's four mutually-exclusive --format-*
// bool flags (enforced by cobra's MarkFlagsMutuallyExclusive, so at most
// one can be true) to the single format string survey.Run expects.
// Rolling these up as separate named flags, rather than one --format
// string flag plus a --plain alias, means `daplabel survey --help`
// documents every valid choice explicitly instead of requiring a
// separate explanation of what strings --format accepts.
// mustBool returns the value of a bool flag, panicking on lookup error.
// These helpers eliminate repetitive error-checking boilerplate for flag
// lookups that can only fail if the flag was not registered with the
// exact name and type used here — a programmer error that should surface
// as a panic during development rather than be returned to the user.
func mustBool(cmd *cobra.Command, name string) bool {
	v, err := cmd.Flags().GetBool(name)
	if err != nil {
		panic(err)
	}
	return v
}

func mustString(cmd *cobra.Command, name string) string {
	v, err := cmd.Flags().GetString(name)
	if err != nil {
		panic(err)
	}
	return v
}

// validSurveyFormats is the set of format strings survey.Run accepts
// (SPECIFICATION.md §7.5.1). Used by resolveFormatFlags to decide
// whether a DAPLABEL_DEFAULT_SURVEY_FORMAT config value is usable, and
// by configcmd.Render to flag invalid values in `daplabel config` output.
var validSurveyFormats = map[string]bool{
	"tree":  true,
	"plain": true,
	"table": true,
	"json":  true,
}

// resolveFormatFlags maps survey's four mutually-exclusive --format-*
// bool flags (enforced by cobra's MarkFlagsMutuallyExclusive, so at most
// one can be true) to the single format string survey.Run expects.
//
// configDefault is the resolved DAPLABEL_DEFAULT_SURVEY_FORMAT value
// (empty string if unset). Precedence:
//  1. Explicit --format-* flag on the command line (highest).
//  2. configDefault, if it is a recognised format string.
//  3. "plain" (the hardcoded default).
//
// An invalid configDefault is silently ignored at runtime — visibility
// for misconfigured values is handled by `daplabel config` (configcmd).
func resolveFormatFlags(cmd *cobra.Command, configDefault string) (string, error) {
	for _, name := range []string{"format-plain", "format-table", "format-json"} {
		v, err := cmd.Flags().GetBool(name)
		if err != nil {
			return "", err
		}
		if v {
			return strings.TrimPrefix(name, "format-"), nil
		}
	}
	// Check --format-tree explicitly (it's the only one not in the loop above).
	if v, err := cmd.Flags().GetBool("format-tree"); err == nil && v {
		return "tree", nil
	}
	// No explicit flag: use config default if valid, otherwise "plain".
	if validSurveyFormats[configDefault] {
		return configDefault, nil
	}
	return "plain", nil
}

func newGenerateCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate [OPTIONS] [PATHS...]",
		Short: "Extract inline service labels to external label files",
		Long: `
  Label File Handling:
    * A service with no existing label_file reference gets a new one
      created and referenced.
    * A service that already has one or more gets its inline labels
      appended to the first, exactly as add does.
    * Two services that reference the same label file have their
      extractions merged into one write.

  Overwrite Behaviour:
    Existing keys in the target label file are not overwritten unless
    --force is given.

  PATH Resolution:
    PATHS defaults to the current directory.
    Multiple paths can be passed as a comma-separated list
      (e.g., "./proj1,./proj2").`,
		Example: `  daplabel generate .
  daplabel generate -r ~/compose-projects/
  daplabel generate --recursive ~/proj1,~/proj2`,
		RunE: func(cmd *cobra.Command, args []string) error {
			recursive := mustBool(cmd, "recursive")
			force := mustBool(cmd, "force")

			paths := discovery.ExpandCommaSeparated(args)
			if len(paths) == 0 {
				paths = []string{"."}
			}

			confirmTimeout, warn := confirmTimeoutValue(opts.ConfigFile)
			if warn != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), warn)
			}

			code, runErr := generate.Run(cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin(), paths, recursive, generate.Options{
				Force:          force,
				OnConflict:     opts.OnConflict,
				DryRun:         opts.DryRun,
				Yes:            opts.Yes,
				DebugLog:       debuglog.New(opts.DebugLog),
				ConfirmTimeout: confirmTimeout,
			})
			if code != 0 {
				return &ExitError{Code: code, Err: runErr}
			}
			return nil
		},
	}
	cmd.Flags().BoolP("force", "f", false, "Overwrite an existing key already present in the target label file")
	cmd.Flags().BoolP("recursive", "r", false, "Scan immediate subdirectories")
	return cmd
}

// isPathLike reports whether s looks like a filesystem path (contains a
// path separator). This is used to distinguish a typo'd directory argument
// (e.g. "./nonexistent") from a bare label key (e.g. "com.example.managed")
// when os.Stat fails — the user can always use "." to explicitly reference
// the current directory.
func isPathLike(s string) bool {
	return strings.Contains(s, "/") || strings.Contains(s, "\\")
}

func parseAddArgs(args []string, explicitService string, serviceAll bool) (service string, labels []string, paths []string, err error) {
	if serviceAll || explicitService != "" {
		for _, a := range args {
			// Expand comma-separated path arguments before checking
			// os.Stat, so "./a,./b" is treated as two paths rather
			// than one non-existent path.
			if strings.Contains(a, ",") {
				expanded := discovery.ExpandCommaSeparated([]string{a})
				for _, ea := range expanded {
					if fi, statErr := os.Stat(ea); statErr == nil && fi.IsDir() {
						paths = append(paths, ea)
					} else if statErr != nil {
						return "", nil, nil, fmt.Errorf("path %q does not exist (use %q for the current directory)", ea, ".")
					} else {
						return "", nil, nil, fmt.Errorf("path %q is not a directory (use %q for the current directory)", ea, ".")
					}
				}
				continue
			}
			if strings.Contains(a, "=") {
				labels = append(labels, a)
			} else if fi, statErr := os.Stat(a); statErr == nil && fi.IsDir() {
				paths = append(paths, a)
			} else {
				return "", nil, nil, fmt.Errorf("path %q does not exist (use %q for the current directory)", a, ".")
			}
		}
		return explicitService, labels, paths, nil
	}

	if len(args) == 0 {
		return "", nil, nil, fmt.Errorf("service name required (or use --service)")
	}

	service = args[0]
	rest := args[1:]

	if len(rest) > 0 {
		last := rest[len(rest)-1]
		if fi, statErr := os.Stat(last); statErr == nil && fi.IsDir() {
			paths = append(paths, last)
			rest = rest[:len(rest)-1]
		} else if statErr != nil && isPathLike(last) {
			return "", nil, nil, fmt.Errorf("path %q does not exist (use %q for the current directory)", last, ".")
		}
	}

	labels = append(labels, rest...)

	return service, labels, paths, nil
}

func selectAddServices(composeFile string, services []string, explicitService string, serviceAll bool, template string) ([]string, error) {
	if serviceAll {
		return services, nil
	}

	if explicitService != "" {
		for _, s := range services {
			if s == explicitService {
				return []string{explicitService}, nil
			}
		}
		return nil, fmt.Errorf("service %q not found", explicitService)
	}

	if len(services) == 1 {
		return services, nil
	}

	if template != "" {
		return nil, fmt.Errorf("%d services found; use --service NAME or --service-all", len(services))
	}

	return services, nil
}

// confirmTimeoutValue loads configuration and parses DAPLABEL_CONFIRM_TIMEOUT.
// On success it returns the duration and an empty warning string. On parse
// failure it returns the 5-minute default and a warning to print to stderr.
func confirmTimeoutValue(configFile string) (time.Duration, string) {
	cfg, err := config.Load(configFile, nil)
	if err != nil {
		return 5 * time.Minute, fmt.Sprintf("Warning: loading configuration for DAPLABEL_CONFIRM_TIMEOUT: %v; using 5m default", err)
	}
	if cfg.ConfirmTimeout.Value == "" {
		return 5 * time.Minute, ""
	}
	d, err := time.ParseDuration(cfg.ConfirmTimeout.Value)
	if err != nil {
		return 5 * time.Minute, fmt.Sprintf("Warning: DAPLABEL_CONFIRM_TIMEOUT=%q is not a valid duration; using 5m default", cfg.ConfirmTimeout.Value)
	}
	return d, ""
}

func newAddCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [OPTIONS] [SERVICE] [LABEL[=VALUE]...] [PATH...]",
		Short: "Add one or more service labels",
		Long: `
  Label Arguments:
    LABEL arguments accept KEY=VALUE pairs or a bare KEY (which sets an empty
    value). An explicit command-line KEY=VALUE overrides the same key coming
    from a template, since it is the more specific expression of intent.

  Write Target (default: external label file):
    By default, labels are written to an external label file referenced
    via label_file in the Compose service definition:
    * A service with no existing label_file reference gets a new file
      named <service>.labels created alongside the Compose file and
      referenced automatically.
    * A service that already has one or more label_file references gets
      the new labels appended to the first rather than creating another
      file.
    --inline writes directly into the service's own labels: block in the
    Compose file instead of an external label file.

  Overwrite Behaviour:
    Existing labels are not overwritten unless --force is given. A key
    present in any of the service's label_file references counts as
    existing, not just keys in the target file.

  Template Mode:
    --template NAME loads labels from a template in the configured
    template directory (DAPLABEL_TEMPLATE_DIR). Template values may
    contain $SERVICE_NAME, $APP_DIR, and $APP_NAME substitution variables,
    expanded per-service at apply time.

  Empty and Missing File Handling:
    --on-empty-create creates an empty label file (with label_file
      reference) when no labels would actually be written.
    --on-none-create creates a referenced label file that does not yet
      exist on disk instead of warning and skipping it.

  Multi-Project Mode (--recursive, --service, or --service-all):
    --service NAME applies to the named service in every project.
    --service-all (-a) applies to all services in every project.
    Without either flag:
      - Single-service project: labels are applied to that service.
      - Multi-service project: direct labels are applied to every
        service; --template requires --service or --service-all.

  PATH Resolution:
    PATH defaults to the current directory.
    With multi-project flags: All positional arguments are treated as labels or paths.
    Without multi-project flags: The first argument is the service name. The last
      argument is treated as a PATH if it exists as a directory on disk.
    Multiple paths can be passed as a comma-separated list (e.g., "./proj1,./proj2").`,
		Example: `  # Single label to a service in the current directory
  daplabel add web com.example.tier=frontend

  # Multiple labels at once
  daplabel add web com.example.tier=frontend com.example.env=prod

  # Bare key (empty value)
  daplabel add web com.example.managed

  # Explicit project path
  daplabel add web com.example.tier=frontend ./myproject

  # Apply a template (labels from DAPLABEL_TEMPLATE_DIR/mytemplate)
  daplabel add -t mytemplate web ./project

  # Template plus an explicit override for one key
  daplabel add -t mytemplate web com.example.env=staging ./project

  # Write inline into the Compose file's labels: block
  daplabel add -i web com.example.tier=prod

  # Inline with unquoted YAML values
  daplabel add -i --values-no-quote web com.example.port=8080

  # Overwrite an existing key's value
  daplabel add -f web com.example.tier=prod

  # Create an empty label file even when no labels would be written
  daplabel add -e web com.example.tier=frontend

  # Create a missing referenced label file instead of skipping
  daplabel add -n web com.example.tier=frontend

  # Dry-run: show what would change without writing
  daplabel add --dry-run web com.example.tier=frontend

  # Non-interactive: skip all confirmation prompts
  daplabel add -y web com.example.tier=frontend

  # Recursive: apply to every service in every subdirectory
  daplabel add -r -a com.example.env=prod ~/projects/

  # Recursive: apply to a named service across projects
  daplabel add -r -s web com.example.tier=prod ~/projects/

  # Resolve cross-file key conflicts by keeping the first value
  daplabel add --on-conflict=first web com.example.tier=frontend`,
		RunE: func(cmd *cobra.Command, args []string) error {
			recursive := mustBool(cmd, "recursive")
			explicitService := mustString(cmd, "service")
			serviceAll := mustBool(cmd, "service-all")
			template := mustString(cmd, "template")
			onEmptyCreate := mustBool(cmd, "on-empty-create")
			onNoneCreate := mustBool(cmd, "on-none-create")
			force := mustBool(cmd, "force")
			valuesNoQuote := mustBool(cmd, "values-no-quote")
			inline := mustBool(cmd, "inline")

			if len(args) == 0 && explicitService == "" && !serviceAll {
				fmt.Fprintln(cmd.ErrOrStderr(), "daplabel add [OPTIONS] [SERVICE] [LABEL[=VALUE]...] [PATH...] [flags]")
				fmt.Fprintln(cmd.ErrOrStderr())
				fmt.Fprintln(cmd.ErrOrStderr(), "Try: daplabel  add --help          For further info")
				return &ExitError{Code: 2, Err: fmt.Errorf("service name required (or use --service)")}
			}

			service, labelArgStrs, paths, parseErr := parseAddArgs(args, explicitService, serviceAll)
			if parseErr != nil {
				return &ExitError{Code: 2, Err: parseErr}
			}

			paths = discovery.ExpandCommaSeparated(paths)
			if len(paths) == 0 {
				paths = []string{"."}
			}

			if len(labelArgStrs) == 0 && template == "" {
				return &ExitError{Code: 2, Err: fmt.Errorf("no labels to add: supply LABEL[=VALUE] arguments and/or --template")}
			}

			targets, warnings, err := discovery.ResolveTargets(recursive, paths)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			for _, w := range warnings {
				fmt.Fprintln(cmd.ErrOrStderr(), w.String())
			}
			if len(targets) == 0 {
				return &ExitError{Code: 4, Err: fmt.Errorf("no valid Compose files found")}
			}

			var templateDir string
			if template != "" {
				cfg, cerr := config.Load(opts.ConfigFile, nil)
				if cerr != nil {
					return &ExitError{Code: 3, Err: fmt.Errorf("loading configuration: %w", cerr)}
				}
				templateDir = cfg.TemplateDir.Value
			}

			confirmTimeout, warn := confirmTimeoutValue(opts.ConfigFile)
			if warn != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), warn)
			}

			labelArgs := make([]add.LabelArg, len(labelArgStrs))
			for i, s := range labelArgStrs {
				labelArgs[i] = add.ParseLabelArg(s)
			}

			type workItem struct {
				composeFile string
				service     string
			}
			var plan []workItem
			var skipped []string

			for _, composeFile := range targets {
				services, serr := yamlbackend.GetServices(composeFile)
				if serr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s: %v\n", composeFile, serr)
					skipped = append(skipped, composeFile)
					continue
				}

				// Single-project backward-compatible path: let add.Run validate
				// the service name itself, preserving exact exit codes (e.g.
				// code 2 for service-not-found).
				if len(targets) == 1 && explicitService == "" && !serviceAll {
					plan = append(plan, workItem{composeFile: composeFile, service: service})
					continue
				}

				selected, selErr := selectAddServices(composeFile, services, service, serviceAll, template)
				if selErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s: %v\n", composeFile, selErr)
					skipped = append(skipped, composeFile)
					continue
				}

				for _, svc := range selected {
					plan = append(plan, workItem{composeFile: composeFile, service: svc})
				}
			}

			if len(plan) == 0 {
				if len(skipped) > 0 {
					return &ExitError{Code: 1, Err: fmt.Errorf("no services could be selected")}
				}
				return &ExitError{Code: 4, Err: fmt.Errorf("no valid Compose files found")}
			}

			isBatch := len(plan) > 1 || len(targets) > 1
			effectiveYes := opts.Yes
			if isBatch && !opts.Yes && !opts.DryRun {
				// Pre-flight conflict warnings: the user should see which
				// keys already exist before deciding on the batch prompt.
				for _, item := range plan {
					if derr := add.DetectConflicts(cmd.ErrOrStderr(), item.composeFile, item.service, labelArgs, add.Options{
						Template:      template,
						TemplateDir:   templateDir,
						OnConflict:    opts.OnConflict,
						Inline:        inline,
						ValuesNoQuote: valuesNoQuote,
					}); derr != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s: %v\n", item.composeFile, derr)
					}
				}

				msg := fmt.Sprintf("Apply %d label(s) to %d service(s) across %d project(s)", len(labelArgs), len(plan), len(targets))
				resp := prompt.Confirm(cmd.OutOrStdout(), cmd.InOrStdin(), msg)
				switch resp {
				case prompt.No:
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				case prompt.Quit:
					return &ExitError{Code: 1, Err: fmt.Errorf("aborted")}
				case prompt.Yes, prompt.All:
					effectiveYes = true
					// Batch confirmation implies --force for the individual
					// operations (SPECIFICATION.md §Prompt Mode vs. Force Mode:
					// "Choosing 'y' or 'a' carries an implied --force for that
					// conflict/operation").
					force = true
				}
			}

			hadErrors := false
			for _, item := range plan {
				code, runErr := add.Run(cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin(), item.composeFile, item.service, labelArgs, add.Options{
					Force:                       force,
					SuppressExistingKeyWarnings: isBatch,
					OnEmptyCreate:               onEmptyCreate,
					OnNoneCreate:                onNoneCreate,
					OnConflict:                  opts.OnConflict,
					ValuesNoQuote:               valuesNoQuote,
					Inline:                      inline,
					Template:                    template,
					TemplateDir:                 templateDir,
					DryRun:                      opts.DryRun,
					Yes:                         effectiveYes,
					ConfirmTimeout:              confirmTimeout,
				})
				if code != 0 {
					if !isBatch {
						return &ExitError{Code: code, Err: runErr}
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s: %v\n", item.composeFile, runErr)
					hadErrors = true
				}
			}

			if hadErrors {
				return &ExitError{Code: 1, Err: fmt.Errorf("one or more operations had errors")}
			}
			return nil
		},
	}
	cmd.Flags().BoolP("force", "f", false, "Overwrite an existing key's value")
	cmd.Flags().BoolP("inline", "i", false, "Write labels into the service's inline labels: block instead of a label file")
	cmd.Flags().BoolP("on-empty-create", "e", false, "Create empty label file if no labels would be written")
	cmd.Flags().BoolP("on-none-create", "n", false, "Create missing referenced label file instead of skipping")
	cmd.Flags().BoolP("recursive", "r", false, "Scan immediate subdirectories")
	cmd.Flags().StringP("service", "s", "", "Target service name (for multi-project mode)")
	cmd.Flags().BoolP("service-all", "a", false, "Apply to every service in every project")
	cmd.Flags().StringP("template", "t", "", "Apply labels from a template")
	cmd.Flags().Bool("values-no-quote", false, "Write inline values as unquoted YAML scalars (only affects --inline)")
	return cmd
}

func newRemoveCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove [OPTIONS] [SERVICE] KEY... [PATHS...]",
		Short: "Remove one or more label keys from a service",
		Long: `More Info:
  File & Block Handling:
    If removing a key empties out a label_file, that file and its reference
    are deleted by default. Use --on-empty-create to preserve them. An
    emptied inline labels: block is always removed outright.

  Multi-Project Mode (--recursive, --service, or --service-all):
    * --service NAME applies the removal to the named service in every project.
    * --service-all (-a) applies the removal to all services in every project.
    * Without either flag, SERVICE is the first positional argument and only
      one project is processed (single-target mode).

  PATH Resolution:
    * PATHS defaults to the current directory.
    * With multi-project flags: All positional arguments are treated as keys or paths.
    * Without multi-project flags: The first argument is the service name. The last
      argument is treated as a PATH if it exists as a directory on disk.
    * Multiple paths can be passed as a comma-separated list (e.g., "./proj1,./proj2").`,
		Example: `  # Single key from a named service, current directory
  daplabel remove web com.example.label

  # Multiple keys at once
  daplabel remove web com.example.env com.example.tier

  # Keep the emptied file and reference
  daplabel remove -e web com.example.label ./project

  # Remove a key from every service across multiple projects
  daplabel remove -r -a com.example.managed ~/projects/

  # Remove a key from a named service across comma-separated paths
  daplabel remove -s web com.example.label ./proj1,./proj2`,
		RunE: func(cmd *cobra.Command, args []string) error {
			recursive := mustBool(cmd, "recursive")
			explicitService := mustString(cmd, "service")
			serviceAll := mustBool(cmd, "service-all")
			onEmptyCreate := mustBool(cmd, "on-empty-create")
			onNoneCreate := mustBool(cmd, "on-none-create")
			valuesNoQuote := mustBool(cmd, "values-no-quote")

			if len(args) == 0 {
				if explicitService == "" && !serviceAll {
					fmt.Fprintln(cmd.ErrOrStderr(), "daplabel remove [OPTIONS] [SERVICE] KEY... [PATHS...] [flags]")
					fmt.Fprintln(cmd.ErrOrStderr())
					fmt.Fprintln(cmd.ErrOrStderr(), "Try: daplabel  remove --help          For further info")
					return &ExitError{Code: 2, Err: fmt.Errorf("requires at least 1 arg(s), only received 0")}
				}
			}

			var service string
			var keys []string
			var paths []string

			if serviceAll || explicitService != "" {
				for _, a := range args {
					// Expand comma-separated path arguments before checking
					// os.Stat, so "./a,./b" is treated as two paths rather
					// than one non-existent path.
					if strings.Contains(a, ",") {
						expanded := discovery.ExpandCommaSeparated([]string{a})
						for _, ea := range expanded {
							if fi, statErr := os.Stat(ea); statErr == nil && fi.IsDir() {
								paths = append(paths, ea)
							} else if statErr != nil {
								return &ExitError{Code: 2, Err: fmt.Errorf("path %q does not exist (use %q for the current directory)", ea, ".")}
							} else {
								return &ExitError{Code: 2, Err: fmt.Errorf("path %q is not a directory (use %q for the current directory)", ea, ".")}
							}
						}
						continue
					}
					if fi, statErr := os.Stat(a); statErr == nil && fi.IsDir() {
						paths = append(paths, a)
					} else {
						keys = append(keys, a)
					}
				}
				service = explicitService
			} else {
				service = args[0]
				rest := args[1:]

				last := ""
				if len(rest) > 0 {
					last = rest[len(rest)-1]
				}

				if last != "" {
					if fi, statErr := os.Stat(last); statErr == nil && fi.IsDir() {
						paths = append(paths, last)
						keys = rest[:len(rest)-1]
					} else if statErr != nil && isPathLike(last) {
						return &ExitError{Code: 2, Err: fmt.Errorf("path %q does not exist (use %q for the current directory)", last, ".")}
					} else {
						keys = rest
					}
				}
			}

			if len(keys) == 0 {
				return &ExitError{Code: 2, Err: fmt.Errorf("no keys to remove: supply one or more label KEY arguments")}
			}

			paths = discovery.ExpandCommaSeparated(paths)
			if len(paths) == 0 {
				paths = []string{"."}
			}

			targets, warnings, derr := discovery.ResolveTargets(recursive, paths)
			if derr != nil {
				return &ExitError{Code: 1, Err: derr}
			}
			for _, w := range warnings {
				fmt.Fprintln(cmd.ErrOrStderr(), w.String())
			}
			if len(targets) == 0 {
				return &ExitError{Code: 4, Err: fmt.Errorf("no valid Compose files found")}
			}

			type workItem struct {
				composeFile string
				service     string
			}
			var plan []workItem
			var skipped []string

			for _, composeFile := range targets {
				services, serr := yamlbackend.GetServices(composeFile)
				if serr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s: %v\n", composeFile, serr)
					skipped = append(skipped, composeFile)
					continue
				}

				// Single-project backward-compatible path: let remove.Run
				// validate the service name itself.
				if len(targets) == 1 && explicitService == "" && !serviceAll {
					plan = append(plan, workItem{composeFile: composeFile, service: service})
					continue
				}

				selected, selErr := selectAddServices(composeFile, services, service, serviceAll, "")
				if selErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s: %v\n", composeFile, selErr)
					skipped = append(skipped, composeFile)
					continue
				}

				for _, svc := range selected {
					plan = append(plan, workItem{composeFile: composeFile, service: svc})
				}
			}

			if len(plan) == 0 {
				if len(skipped) > 0 {
					return &ExitError{Code: 1, Err: fmt.Errorf("no services could be selected")}
				}
				return &ExitError{Code: 4, Err: fmt.Errorf("no valid Compose files found")}
			}

			isBatch := len(plan) > 1 || len(targets) > 1
			effectiveYes := opts.Yes

			confirmTimeout, warn := confirmTimeoutValue(opts.ConfigFile)
			if warn != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), warn)
			}

			if isBatch && !opts.Yes && !opts.DryRun {
				msg := fmt.Sprintf("Remove %d key(s) from %d service(s) across %d project(s)?", len(keys), len(plan), len(targets))
				resp := prompt.Confirm(cmd.OutOrStdout(), cmd.InOrStdin(), msg)
				switch resp {
				case prompt.No:
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				case prompt.Quit:
					return &ExitError{Code: 1, Err: fmt.Errorf("aborted")}
				case prompt.Yes, prompt.All:
					effectiveYes = true
				}
			}

			hadErrors := false
			for _, item := range plan {
				code, runErr := remove.Run(cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin(), item.composeFile, item.service, keys, remove.Options{
					OnEmptyCreate:  onEmptyCreate,
					OnNoneCreate:   onNoneCreate,
					ValuesNoQuote:  valuesNoQuote,
					DryRun:         opts.DryRun,
					Yes:            effectiveYes,
					DebugLog:       debuglog.New(opts.DebugLog),
					ConfirmTimeout: confirmTimeout,
				})
				if code != 0 {
					if !isBatch {
						return &ExitError{Code: code, Err: runErr}
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s: %v\n", item.composeFile, runErr)
					hadErrors = true
				}
			}

			if hadErrors {
				return &ExitError{Code: 1, Err: fmt.Errorf("one or more operations had errors")}
			}
			return nil
		},
	}
	cmd.Flags().BoolP("service-all", "a", false, "Apply to every service in every project")
	cmd.Flags().BoolP("on-empty-create", "e", false, "Keep and create empty label file if all labels are removed")
	cmd.Flags().BoolP("on-none-create", "n", false, "Accepted for symmetry with 'add'; has no effect on remove")
	cmd.Flags().BoolP("recursive", "r", false, "Scan immediate subdirectories")
	cmd.Flags().StringP("service", "s", "", "Target service name (for multi-project mode)")
	cmd.Flags().Bool("values-no-quote", false, "Accepted for symmetry with 'add'; has no effect on remove")
	return cmd
}

func newCleanCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean [OPTIONS] [PATHS...]",
		Short: "Remove .pre-label backup files from project directories",
		Long: `
Scope:
  Only removes files whose names end with the .pre-label suffix;
  nothing else in the directory is touched.

Project Discovery:
  Project directories are discovered the same way as for survey and
  generate: a directory containing a recognised Compose file is treated
  as a project. With --recursive, the immediate subdirectories of each
  PATH are scanned for projects.

PATH Resolution:
  PATHS defaults to the current directory.
  Multiple paths can be passed as a comma-separated list
    (e.g., "./proj1,./proj2").`,
		Example: `  # Remove backup files from the current directory
  daplabel clean

  # Preview what would be removed
  daplabel clean --dry-run .

  # Remove backup files non-interactively
  daplabel clean --yes .

  # Remove backup files from every project under a parent directory
  daplabel clean -r ~/compose-projects/

  # Remove backup files from multiple projects
  daplabel clean ~/proj1,~/proj2`,
		RunE: func(cmd *cobra.Command, args []string) error {
			recursive := mustBool(cmd, "recursive")

			paths := discovery.ExpandCommaSeparated(args)
			if len(paths) == 0 {
				paths = []string{"."}
			}

			code, runErr := clean.Run(cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin(), paths, recursive, clean.Options{
				DryRun: opts.DryRun,
				Yes:    opts.Yes,
			})
			if code != 0 {
				return &ExitError{Code: code, Err: runErr}
			}
			return nil
		},
	}
	cmd.Flags().BoolP("recursive", "r", false, "Scan immediate subdirectories")
	return cmd
}

func newSurveyCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "survey [OPTIONS] [PATHS...]",
		Short: "Inspect services, labels, and label files (read-only)",
		Long: `
  Filtering:
    --filter KEY or --filter KEY=VALUE shows only services matching
      the given label (repeatable; all given filters must match).
    --missing KEY shows only services that do NOT have the given key
      (repeatable; must be missing all given keys).

  Display Options:
    --no-summary suppresses the Summary block at the end of output.
    --no-wrap disables column wrapping for --format-table.

  PATH Resolution:
    PATHS defaults to the configured parent directory.
    With --recursive, the immediate subdirectories of each PATH are
      scanned for projects.
    Multiple paths can be passed as a comma-separated list
      (e.g., "./proj1,./proj2").`,
		Example: `  daplabel survey .
  daplabel survey -r ~/compose-projects/
  daplabel survey --format-plain ~/compose-projects/nginx-proxy
  daplabel survey --format-table -r ~/compose-projects/
  daplabel survey --format-json --recursive ~/compose-projects/ | jq .
  daplabel survey --filter diun.enable=true -r ~/compose-projects/
  daplabel survey --missing diun.enable --recursive ~/compose-projects/`,
		RunE: func(cmd *cobra.Command, args []string) error {
			recursive := mustBool(cmd, "recursive")

			// Load config early: needed for both the default PATH
			// (when no args given) and the default survey format
			// (DAPLABEL_DEFAULT_SURVEY_FORMAT).
			cfg, cfgErr := config.Load(opts.ConfigFile, nil)
			if cfgErr != nil {
				return &ExitError{Code: 3, Err: fmt.Errorf("loading configuration: %w", cfgErr)}
			}

			format, err := resolveFormatFlags(cmd, cfg.DefaultSurveyFormat.Value)
			if err != nil {
				return err
			}

			filterArgs, err := cmd.Flags().GetStringArray("filter")
			if err != nil {
				return err
			}
			filters := make([]survey.KeyValueFilter, len(filterArgs))
			for i, a := range filterArgs {
				filters[i] = survey.ParseFilterArg(a)
			}
			missing, err := cmd.Flags().GetStringArray("missing")
			if err != nil {
				return err
			}

			paths := discovery.ExpandCommaSeparated(args)
			if len(paths) == 0 {
				paths = []string{cfg.ParentDir.Value}
			}

			noSummary := mustBool(cmd, "no-summary")
			noWrap := mustBool(cmd, "no-wrap")

			code, err := survey.Run(cmd.OutOrStdout(), cmd.ErrOrStderr(), paths, survey.Options{
				Recursive: recursive,
				Format:    format,
				Filter:    filters,
				Missing:   missing,
				NoSummary: noSummary,
				NoWrap:    noWrap,
			})
			if code != 0 {
				return &ExitError{Code: code, Err: err}
			}
			return nil
		},
	}
	cmd.Flags().StringArray("filter", nil, "Only show services matching KEY or KEY=VALUE (repeatable)")
	cmd.Flags().Bool("format-json", false, "Use the JSON output format")
	cmd.Flags().Bool("format-plain", false, "Use the flat, non-tree output format")
	cmd.Flags().Bool("format-table", false, "Use the plaintext table output format")
	cmd.Flags().Bool("format-tree", false, "Use the tree output format (default)")
	cmd.MarkFlagsMutuallyExclusive("format-tree", "format-plain", "format-table", "format-json")
	cmd.Flags().StringArray("missing", nil, "Only show services missing KEY (repeatable)")
	cmd.Flags().Bool("no-summary", false, "Suppress the Summary block at the end of output")
	cmd.Flags().Bool("no-wrap", false, "Disable column wrapping for --format-table")
	cmd.Flags().BoolP("recursive", "r", false, "Scan immediate subdirectories")

	return cmd
}

func newTemplateCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "template",
		Short: "Manage label templates",
		Long: `
Commands:
  list        Show available templates.
  apply       Apply a template to a service.
  create      Create a new template from labels.
  edit        Edit a template in the configured editor.
  remove      Remove a template from the template directory.

Templates allow the application of predefined labels from a template
file to one or more services, and can utilise some
template variables.

Template Variables:
  $SERVICE_NAME, $APP_DIR, and $APP_NAME are expanded per-service
    at apply time.`,
		Example: `  daplabel template list
  daplabel template apply prod web ./project
  daplabel template create staging ENV=staging LOG_LEVEL=debug
  daplabel template create empty-template
  daplabel template edit prod
  daplabel template remove prod`,
	}
	cmd.AddCommand(
		newTemplateListCmd(opts),
		newTemplateApplyCmd(opts),
		newTemplateCreateCmd(opts),
		newTemplateEditCmd(opts),
		newTemplateRemoveCmd(opts),
	)
	return cmd
}

// templateDir loads configuration and returns the resolved template
// directory, or a clear *ExitError if none is configured — every
// template subcommand needs this first.
func templateDir(opts *Options) (string, error) {
	cfg, err := config.Load(opts.ConfigFile, nil)
	if err != nil {
		return "", &ExitError{Code: 3, Err: fmt.Errorf("loading configuration: %w", err)}
	}
	return cfg.TemplateDir.Value, nil
}

func newTemplateListCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show available templates",
		Long: `list shows every template currently in the configured template directory
(DAPLABEL_TEMPLATE_DIR), one per line, sorted by name.`,
		Example: `  daplabel template list
  daplabel --config ./ci.conf template list`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := templateDir(opts)
			if err != nil {
				return err
			}
			names, err := templatecmd.List(dir)
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			if len(names) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No templates found.")
				return nil
			}
			for _, name := range names {
				fmt.Fprintln(cmd.OutOrStdout(), name)
			}
			return nil
		},
	}
}

func newTemplateApplyCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply TEMPLATE SERVICE [PATH]",
		Short: "Apply a template to a service",
		Long: `More Info:
  Behaviour:
    This is exactly add --template TEMPLATE SERVICE [PATH] with no direct
    labels of its own — see daplabel add --help for the full set of shared
    behaviour (label_file vs --inline, --force, --on-empty-create,
    --on-none-create).

  PATH Resolution:
    * PATH defaults to the current directory.
    * A comma-separated list of paths may be given in a single argument
      (e.g., "./proj1,./proj2") — they are expanded and each project
      is processed in turn.`,
		Example: `  daplabel template apply prod web
  daplabel template apply prod web ./project
  daplabel template apply -i staging web
  daplabel template apply prod web ./proj1,./proj2`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			templateName := args[0]
			service := args[1]
			path := "."
			if len(args) == 3 {
				path = args[2]
			}

			onEmptyCreate := mustBool(cmd, "on-empty-create")
			onNoneCreate := mustBool(cmd, "on-none-create")
			force := mustBool(cmd, "force")
			valuesNoQuote := mustBool(cmd, "values-no-quote")
			inline := mustBool(cmd, "inline")

			dir, err := templateDir(opts)
			if err != nil {
				return err
			}

			applyPaths := discovery.ExpandCommaSeparated([]string{path})
			hadErrors := false
			first := true
			for _, ap := range applyPaths {
				targets, warnings, derr := discovery.ResolveTargets(false, []string{ap})
				if derr != nil {
					return &ExitError{Code: 1, Err: derr}
				}
				for _, w := range warnings {
					fmt.Fprintln(cmd.ErrOrStderr(), w.String())
				}
				if len(targets) == 0 {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: no valid Compose file found at %s\n", ap)
					hadErrors = true
					continue
				}
				for _, composeFile := range targets {
					if !first {
						fmt.Fprintln(cmd.OutOrStdout())
					}
					first = false
					code, runErr := add.Run(cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin(), composeFile, service, nil, add.Options{
						Force:         force,
						OnEmptyCreate: onEmptyCreate,
						OnNoneCreate:  onNoneCreate,
						OnConflict:    opts.OnConflict,
						ValuesNoQuote: valuesNoQuote,
						Inline:        inline,
						Template:      templateName,
						TemplateDir:   dir,
						DryRun:        opts.DryRun,
						Yes:           opts.Yes,
					})
					if code != 0 {
						fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s: %v\n", composeFile, runErr)
						hadErrors = true
					}
				}
			}
			if hadErrors {
				return &ExitError{Code: 1, Err: fmt.Errorf("one or more operations had errors")}
			}
			return nil
		},
	}
	cmd.Flags().BoolP("force", "f", false, "Overwrite an existing key's value")
	cmd.Flags().BoolP("inline", "i", false, "Write labels into the service's inline labels: block instead of a label file")
	cmd.Flags().BoolP("on-empty-create", "e", false, "Create empty label file if the template has no labels")
	cmd.Flags().BoolP("on-none-create", "n", false, "Create missing referenced label file instead of skipping")
	cmd.Flags().Bool("values-no-quote", false, "Write inline values as unquoted YAML scalars (only affects --inline)")
	return cmd
}

func newTemplateCreateCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create NAME [LABEL=VALUE...]",
		Short: "Create a new template from labels",
		Long: `More Info:
  Template Variables:
    A value may contain $SERVICE_NAME/$APP_DIR/$APP_NAME — they aren't
    expanded until the template is applied.

  Empty Templates:
    If no LABEL=VALUE pairs are given, an empty template file is created.

  Merging:
    Running create again for a name that already exists merges into it:
    an existing key is left untouched unless --force is given, and a new
    key is appended. If no labels are given and the template already
    exists, it is left unchanged.`,
		Example: `  daplabel template create staging ENV=staging LOG_LEVEL=debug
  daplabel template create prod com.example.owner='$SERVICE_NAME'
  daplabel template create -f staging LOG_LEVEL=info
  daplabel template create empty-template`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			force := mustBool(cmd, "force")

			dir, err := templateDir(opts)
			if err != nil {
				return err
			}

			labels := make([]labelfile.Label, len(args)-1)
			for i, s := range args[1:] {
				a := add.ParseLabelArg(s)
				labels[i] = labelfile.Label{Key: a.Key, Value: a.Value}
			}

			confirmTimeout, warn := confirmTimeoutValue(opts.ConfigFile)
			if warn != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), warn)
			}

			code, runErr := templatecmd.Create(cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin(), dir, name, labels, force, opts.DryRun, opts.Yes, confirmTimeout)
			if code != 0 {
				return &ExitError{Code: code, Err: runErr}
			}
			return nil
		},
	}
	cmd.Flags().BoolP("force", "f", false, "Overwrite an existing key's value")
	return cmd
}

func newTemplateEditCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "edit NAME",
		Short: "Edit a template in the configured editor",
		Long: `More Info:
  * The template must already exist in the configured template directory.
  * The editor process inherits this command's terminal, so interactive
    editors such as nano or vim work normally.`,
		Example: `  daplabel template edit prod
  DAPLABEL_EDITOR=vim daplabel template edit staging`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			cfg, err := config.Load(opts.ConfigFile, nil)
			if err != nil {
				return &ExitError{Code: 3, Err: fmt.Errorf("loading configuration: %w", err)}
			}
			dir := cfg.TemplateDir.Value
			if dir == "" {
				return &ExitError{Code: 3, Err: fmt.Errorf("no template directory configured (DAPLABEL_TEMPLATE_DIR)")}
			}

			if err := templatecmd.Edit(dir, name, cfg.Editor.Value); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}
}

func newTemplateRemoveCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "remove NAME",
		Short: "Remove a template from the template directory",
		Long: `More Info:
  * A confirmation prompt is shown unless -y/--yes is given.
  * Use --dry-run to preview the removal without deleting anything.`,
		Example: `  daplabel template remove prod
  daplabel template remove -y prod
  daplabel template remove --dry-run prod`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			dir, err := templateDir(opts)
			if err != nil {
				return err
			}

			code, runErr := templatecmd.Remove(cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin(), dir, name, opts.DryRun, opts.Yes)
			if code != 0 {
				return &ExitError{Code: code, Err: runErr}
			}
			return nil
		},
	}
}

func newConfigCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Display resolved configuration and where each value came from",
		Long: `More Info:
  * Shows every file-based location that was checked during resolution
    and whether each one existed and was actually used.
  * Never modifies configuration.`,
		Example: `  daplabel config
  daplabel --config ./ci.conf config`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(opts.ConfigFile, nil)
			if err != nil {
				return &ExitError{Code: 3, Err: fmt.Errorf("loading configuration: %w", err)}
			}
			sources, err := config.Sources(opts.ConfigFile)
			if err != nil {
				return &ExitError{Code: 3, Err: fmt.Errorf("checking configuration sources: %w", err)}
			}
			configcmd.Render(cmd.OutOrStdout(), cfg, sources)
			return nil
		},
	}
}
