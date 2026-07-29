package config

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

func isolateHome(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	// userConfigPaths() checks SUDO_USER before HOME (see config.go) so
	// that daplabel finds the invoking user's real config when actually
	// run via sudo. That's deliberate for production, but it means a
	// test that isolates HOME without also isolating SUDO_USER is only
	// isolated when the test binary itself wasn't invoked via sudo. Any
	// test using this helper wants to test its own HOME/XDG_CONFIG_HOME
	// setup, not whatever the ambient invocation happened to be — clear
	// SUDO_USER here so that's true unconditionally.
	t.Setenv("SUDO_USER", "")
	return home
}

func TestUserConfigPaths_sudoUsesInvokingUsersHome(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skip("cannot determine current user in this environment")
	}
	t.Setenv("SUDO_USER", me.Username)
	t.Setenv("HOME", "/definitely-not-the-real-home") // what root's $HOME would be under sudo
	t.Setenv("XDG_CONFIG_HOME", "")

	paths := userConfigPaths()
	want := filepath.Join(me.HomeDir, ".config", "daplabel", "config")
	if len(paths) == 0 || paths[len(paths)-1] != want {
		t.Errorf("paths = %v, want last entry %q (the invoking user's real home, not root's $HOME)", paths, want)
	}
}

func TestUserConfigPaths_noSudoUserUsesHOME(t *testing.T) {
	t.Setenv("SUDO_USER", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	paths := userConfigPaths()
	want := filepath.Join(home, ".config", "daplabel", "config")
	if len(paths) == 0 || paths[len(paths)-1] != want {
		t.Errorf("paths = %v, want %q", paths, want)
	}
}

func TestUserConfigPaths_unresolvableSudoUserFallsBackToHOME(t *testing.T) {
	t.Setenv("SUDO_USER", "this-user-almost-certainly-does-not-exist-daplabel-test")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	paths := userConfigPaths()
	want := filepath.Join(home, ".config", "daplabel", "config")
	if len(paths) == 0 || paths[len(paths)-1] != want {
		t.Errorf("paths = %v, want fallback to $HOME %q when SUDO_USER doesn't resolve", paths, want)
	}
}

func TestSources_reportsExistenceAndUsage(t *testing.T) {
	home := isolateHome(t)
	userConfigDir := filepath.Join(home, ".config", "daplabel")
	if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userConfigDir, "config"), []byte("DAPLABEL_PARENT_DIR=/from/user/file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sources, err := Sources("")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) < 2 {
		t.Fatalf("sources = %v, want at least system file + user file", sources)
	}

	sys := sources[0]
	if sys.Tier != SourceSystemFile || sys.Exists || sys.Used {
		t.Errorf("system source = %+v, want not exists/not used (this sandbox has no /etc/daplabel/config)", sys)
	}

	userSrc := sources[len(sources)-1]
	if userSrc.Tier != SourceUserFile || !userSrc.Exists || !userSrc.Used {
		t.Errorf("user source = %+v, want exists=true used=true", userSrc)
	}
}

func TestSources_existingButEmptyFileIsNotUsed(t *testing.T) {
	home := isolateHome(t)
	userConfigDir := filepath.Join(home, ".config", "daplabel")
	if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Exists, but contributes nothing recognised.
	if err := os.WriteFile(filepath.Join(userConfigDir, "config"), []byte("# just a comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sources, err := Sources("")
	if err != nil {
		t.Fatal(err)
	}
	userSrc := sources[len(sources)-1]
	if !userSrc.Exists {
		t.Error("expected Exists=true for a file that's present on disk")
	}
	if userSrc.Used {
		t.Error("expected Used=false for a file with no recognised keys")
	}
}

func TestSources_explicitConfigFileReplacesDiscovery(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	explicit := filepath.Join(dir, "ci.conf")
	if err := os.WriteFile(explicit, []byte("DAPLABEL_PARENT_DIR=/from/explicit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sources, err := Sources(explicit)
	if err != nil {
		t.Fatal(err)
	}
	userSrc := sources[len(sources)-1]
	if userSrc.Path != explicit {
		t.Errorf("path = %q, want the explicit config file path %q", userSrc.Path, explicit)
	}
	if !userSrc.Used {
		t.Error("expected the explicit file to be used")
	}
}

func TestLoad_defaults(t *testing.T) {
	isolateHome(t)
	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ParentDir.Value != "." || cfg.ParentDir.Source != SourceDefault {
		t.Errorf("got %+v, want default \".\"", cfg.ParentDir)
	}
}

func TestLoad_userFileOverridesDefault(t *testing.T) {
	home := isolateHome(t)
	userConfigDir := filepath.Join(home, ".config", "daplabel")
	if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userConfigDir, "config"), []byte("DAPLABEL_PARENT_DIR=/from/user/file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ParentDir.Value != "/from/user/file" || cfg.ParentDir.Source != SourceUserFile {
		t.Errorf("got %+v, want value from user file", cfg.ParentDir)
	}
}

func TestLoad_envOverridesUserFile(t *testing.T) {
	home := isolateHome(t)
	userConfigDir := filepath.Join(home, ".config", "daplabel")
	if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userConfigDir, "config"), []byte("DAPLABEL_PARENT_DIR=/from/user/file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DAPLABEL_PARENT_DIR", "/from/env")

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ParentDir.Value != "/from/env" || cfg.ParentDir.Source != SourceEnv {
		t.Errorf("got %+v, want value from environment (§5.2 precedence)", cfg.ParentDir)
	}
}

func TestLoad_cliOverridesEverything(t *testing.T) {
	home := isolateHome(t)
	userConfigDir := filepath.Join(home, ".config", "daplabel")
	if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userConfigDir, "config"), []byte("DAPLABEL_PARENT_DIR=/from/user/file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DAPLABEL_PARENT_DIR", "/from/env")

	cfg, err := Load("", map[string]string{"DAPLABEL_PARENT_DIR": "/from/cli"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ParentDir.Value != "/from/cli" || cfg.ParentDir.Source != SourceCLI {
		t.Errorf("got %+v, want CLI value (highest precedence, §5.2)", cfg.ParentDir)
	}
}

func TestLoad_unrecognisedKeysIgnored(t *testing.T) {
	home := isolateHome(t)
	userConfigDir := filepath.Join(home, ".config", "daplabel")
	if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "SOME_UNKNOWN_KEY=whatever\nDAPLABEL_EDITOR=vim\n"
	if err := os.WriteFile(filepath.Join(userConfigDir, "config"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Editor.Value != "vim" {
		t.Errorf("got Editor=%q, want %q", cfg.Editor.Value, "vim")
	}
	// SOME_UNKNOWN_KEY simply has no field to check — its absence from
	// Config is itself the assertion that it was ignored (§5.1).
}

func TestLoad_explicitConfigFileOverridesDiscovery(t *testing.T) {
	isolateHome(t)
	explicit := filepath.Join(t.TempDir(), "custom.conf")
	if err := os.WriteFile(explicit, []byte("DAPLABEL_PARENT_DIR=/from/explicit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(explicit, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ParentDir.Value != "/from/explicit" || cfg.ParentDir.Source != SourceUserFile {
		t.Errorf("got %+v, want value from explicit --config path", cfg.ParentDir)
	}
}

func TestLoad_defaultSurveyFormat_defaultIsEmpty(t *testing.T) {
	isolateHome(t)
	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultSurveyFormat.Value != "" || cfg.DefaultSurveyFormat.Source != SourceDefault {
		t.Errorf("got %+v, want default empty string", cfg.DefaultSurveyFormat)
	}
}

func TestLoad_defaultSurveyFormat_fromConfigFile(t *testing.T) {
	home := isolateHome(t)
	userConfigDir := filepath.Join(home, ".config", "daplabel")
	if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userConfigDir, "config"), []byte("DAPLABEL_DEFAULT_SURVEY_FORMAT=plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultSurveyFormat.Value != "plain" || cfg.DefaultSurveyFormat.Source != SourceUserFile {
		t.Errorf("got %+v, want value from user file", cfg.DefaultSurveyFormat)
	}
}

func TestLoad_defaultSurveyFormat_envOverridesFile(t *testing.T) {
	home := isolateHome(t)
	userConfigDir := filepath.Join(home, ".config", "daplabel")
	if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userConfigDir, "config"), []byte("DAPLABEL_DEFAULT_SURVEY_FORMAT=plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DAPLABEL_DEFAULT_SURVEY_FORMAT", "table")

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultSurveyFormat.Value != "table" || cfg.DefaultSurveyFormat.Source != SourceEnv {
		t.Errorf("got %+v, want value from environment (§5.2 precedence)", cfg.DefaultSurveyFormat)
	}
}

func TestLoad_defaultSurveyFormat_cliOverridesEverything(t *testing.T) {
	home := isolateHome(t)
	userConfigDir := filepath.Join(home, ".config", "daplabel")
	if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userConfigDir, "config"), []byte("DAPLABEL_DEFAULT_SURVEY_FORMAT=plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DAPLABEL_DEFAULT_SURVEY_FORMAT", "table")

	cfg, err := Load("", map[string]string{"DAPLABEL_DEFAULT_SURVEY_FORMAT": "json"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultSurveyFormat.Value != "json" || cfg.DefaultSurveyFormat.Source != SourceCLI {
		t.Errorf("got %+v, want CLI value (highest precedence, §5.2)", cfg.DefaultSurveyFormat)
	}
}
