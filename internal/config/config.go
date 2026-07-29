// Package config implements SPECIFICATION.md §5 configuration loading and
// precedence: CLI > environment > user config file > system config file >
// defaults. Informed by src/lib/core.sh's lbl_load_config, but designed
// fresh in Go (DECISIONS.md Decision 34).
package config

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// Config holds the resolved values of every supported configuration key
// (SPECIFICATION.md §5.4), plus, for each, which precedence tier the
// active value came from — needed by `daplabel config` (§7.7) to report
// not just the value but where it came from.
type Config struct {
	ParentDir           Value // DAPLABEL_PARENT_DIR
	TemplateDir         Value // DAPLABEL_TEMPLATE_DIR
	Editor              Value // DAPLABEL_EDITOR
	ListSafe            Value // DAPLABEL_LIST_SAFE
	DefaultSurveyFormat Value // DAPLABEL_DEFAULT_SURVEY_FORMAT
	ConfirmTimeout      Value // DAPLABEL_CONFIRM_TIMEOUT
}

// Source identifies which precedence tier (SPECIFICATION.md §5.2) a
// Config value was ultimately taken from.
type Source int

const (
	SourceDefault Source = iota
	SourceSystemFile
	SourceUserFile
	SourceEnv
	SourceCLI
)

func (s Source) String() string {
	switch s {
	case SourceCLI:
		return "command line"
	case SourceEnv:
		return "environment variable"
	case SourceUserFile:
		return "user configuration file"
	case SourceSystemFile:
		return "system configuration file"
	default:
		return "default"
	}
}

// Value is a configuration value together with the precedence tier it
// was resolved from.
type Value struct {
	Value  string
	Source Source
}

// defaults holds SPECIFICATION.md §5.4's built-in defaults, applied when
// no higher-precedence tier sets a key. DAPLABEL_PARENT_DIR defaults to
// "." — the current directory — since that is the only sensible default
// for "root directory to scan" with no other information available.
var defaults = map[string]string{
	"DAPLABEL_PARENT_DIR":            ".",
	"DAPLABEL_TEMPLATE_DIR":          "",
	"DAPLABEL_EDITOR":                "",
	"DAPLABEL_LIST_SAFE":             "",
	"DAPLABEL_DEFAULT_SURVEY_FORMAT": "",
	"DAPLABEL_CONFIRM_TIMEOUT":       "5m",
}

// systemConfigPath is SPECIFICATION.md §5.3's fixed system configuration
// file location.
const systemConfigPath = "/etc/daplabel/config"

// invokingUserHomeDir resolves the home directory user configuration
// discovery should use. SPECIFICATION.md §5.3: "If executed via sudo,
// configuration shall default to the invoking user where possible." A
// plain os.UserHomeDir() under sudo returns root's home (typically
// /root), not the person who ran sudo — daplabel is a tool for managing
// someone's own Compose projects and templates, and a config file
// written as that person, sitting in their own home directory, should
// still be found when they run daplabel via sudo. SUDO_USER is set by
// sudo itself whenever it's used, so its presence is what "executed via
// sudo" means here.
//
// "Where possible": if SUDO_USER is set but doesn't resolve to a real
// account (user.Lookup fails — a plausible, if unusual, PAM/container
// edge case), this falls back to the normal, unprivileged
// os.UserHomeDir() resolution rather than erroring the whole command
// over a config-discovery nicety.
func invokingUserHomeDir() (string, error) {
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		if u, err := user.Lookup(sudoUser); err == nil && u.HomeDir != "" {
			return u.HomeDir, nil
		}
	}
	return os.UserHomeDir()
}

// userConfigPaths returns SPECIFICATION.md §5.3's user configuration
// discovery locations, in the order they should be checked
// ($XDG_CONFIG_HOME first, then ~/.config as a fallback).
func userConfigPaths() []string {
	var paths []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "daplabel", "config"))
	}
	if home, err := invokingUserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "daplabel", "config"))
	}
	return paths
}

// parseConfigFile reads a simple `KEY=value` configuration file
// (SPECIFICATION.md §5.1). Only recognised keys (those present in
// defaults) are processed; unrecognised keys and blank/comment lines are
// silently ignored, per §5.1. A missing file is not an error — it simply
// contributes nothing at its tier.
func parseConfigFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	// A close error on a file only ever opened for reading carries no
	// actionable information (nothing was written that could be lost) and
	// nothing else in this function would change behaviour based on it —
	// explicitly ignored rather than left as a bare, unexplained call.
	defer func() { _ = f.Close() }()

	out := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if _, recognised := defaults[key]; !recognised {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return out, nil
}

// FileSource describes one file-based configuration location Load
// checks during resolution — for `daplabel config`'s "validate
// configuration sources" / "assist in debugging configuration
// resolution" duties (SPECIFICATION.md §7.7). Tier is always
// SourceSystemFile or SourceUserFile; the other three precedence tiers
// (defaults, environment, CLI) have no file location to report.
type FileSource struct {
	Tier   Source
	Path   string
	Exists bool
	// Used is true if this file actually contributed at least one
	// recognised key to the resolved configuration — the same
	// condition Load itself uses to decide whether a user file "wins"
	// over the next one in the discovery list (§5.3: first *discovered*
	// file, meaning first one with actual recognised content, not
	// merely first one present on disk).
	Used bool
}

// Sources reports every file-based location Load would check, in the
// order checked, without performing any resolution of its own — Load
// remains the single source of truth for the actual configuration
// values. explicitConfigFile mirrors Load's own parameter: when set, it
// replaces normal user-config discovery, exactly as Load treats it.
func Sources(explicitConfigFile string) ([]FileSource, error) {
	var out []FileSource

	sysVals, err := parseConfigFile(systemConfigPath)
	if err != nil {
		return nil, err
	}
	out = append(out, FileSource{
		Tier: SourceSystemFile, Path: systemConfigPath,
		Exists: fileExists(systemConfigPath), Used: len(sysVals) > 0,
	})

	userPaths := userConfigPaths()
	if explicitConfigFile != "" {
		userPaths = []string{explicitConfigFile}
	}
	claimed := false
	for _, p := range userPaths {
		vals, err := parseConfigFile(p)
		if err != nil {
			return nil, err
		}
		used := len(vals) > 0 && !claimed
		if used {
			claimed = true
		}
		out = append(out, FileSource{Tier: SourceUserFile, Path: p, Exists: fileExists(p), Used: used})
	}

	return out, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Load resolves every supported configuration key per SPECIFICATION.md
// §5.2's precedence order. cliOverrides carries values already parsed
// from command-line flags; pass nil/empty for a key not set on the
// command line.
//
// explicitConfigFile, if non-empty, is consulted in place of the normal
// user-config discovery locations (SPECIFICATION.md §5.3 permits an
// explicit override via --config).
func Load(explicitConfigFile string, cliOverrides map[string]string) (*Config, error) {
	resolved := make(map[string]Value, len(defaults))
	for k, v := range defaults {
		resolved[k] = Value{Value: v, Source: SourceDefault}
	}

	// Tier 4: system configuration file.
	sysVals, err := parseConfigFile(systemConfigPath)
	if err != nil {
		return nil, err
	}
	for k, v := range sysVals {
		resolved[k] = Value{Value: v, Source: SourceSystemFile}
	}

	// Tier 3: user configuration file (or the explicit --config path).
	userPaths := userConfigPaths()
	if explicitConfigFile != "" {
		userPaths = []string{explicitConfigFile}
	}
	for _, p := range userPaths {
		userVals, err := parseConfigFile(p)
		if err != nil {
			return nil, err
		}
		for k, v := range userVals {
			resolved[k] = Value{Value: v, Source: SourceUserFile}
		}
		if len(userVals) > 0 {
			break // first discovered file wins, per §5.3's ordered list
		}
	}

	// Tier 2: environment variables.
	for k := range defaults {
		if v, ok := os.LookupEnv(k); ok {
			resolved[k] = Value{Value: v, Source: SourceEnv}
		}
	}

	// Tier 1: command-line options — highest precedence.
	for k, v := range cliOverrides {
		if _, recognised := defaults[k]; recognised {
			resolved[k] = Value{Value: v, Source: SourceCLI}
		}
	}

	return &Config{
		ParentDir:           resolved["DAPLABEL_PARENT_DIR"],
		TemplateDir:         resolved["DAPLABEL_TEMPLATE_DIR"],
		Editor:              resolved["DAPLABEL_EDITOR"],
		ListSafe:            resolved["DAPLABEL_LIST_SAFE"],
		DefaultSurveyFormat: resolved["DAPLABEL_DEFAULT_SURVEY_FORMAT"],
		ConfirmTimeout:      resolved["DAPLABEL_CONFIRM_TIMEOUT"],
	}, nil
}
