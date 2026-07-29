package cliapp

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// dockerComposeAvailable mirrors composevalidate's own test helper — see
// DECISIONS.md Decision 30.
func dockerComposeAvailable() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	cmd := exec.Command("docker", "compose", "version")
	return cmd.Run() == nil
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// run executes the real root command exactly as main() does, capturing
// its output and unwrapping any *ExitError for the exit code main()
// would have used.
func run(t *testing.T, stdin string, args ...string) (stdout, stderr string, code int, err error) {
	t.Helper()
	root := NewRootCmd(BuildInfo{Version: "test"})
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)

	err = root.Execute()
	code = 0
	if err != nil {
		code = 1
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.Code
		}
	}
	return outBuf.String(), errBuf.String(), code, err
}

func TestAddCmd_directoryLastArgIsTreatedAsPath(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "myproject")
	composeFile := filepath.Join(project, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")

	_, stderr, code, err := run(t, "",
		"add", "--dry-run", "web", "com.example.a=1", project)
	if err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
}

func TestAddCmd_noPathArgDefaultsToCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWD) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	_, stderr, code, runErr := run(t, "", "add", "--dry-run", "web", "com.example.a=1")
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
}

func TestAddCmd_serviceNotFoundReturnsExitCode2(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")

	_, _, code, runErr := run(t, "", "add", "--dry-run", "nonexistent", "com.example.a=1", dir)
	if runErr == nil {
		t.Fatal("expected an error for a nonexistent service")
	}
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

// TestAddCmd_batchPromptImpliesForce covers SPECIFICATION.md §Prompt Mode
// vs. Force Mode: when the user confirms the batch prompt with y/a, that
// choice implies --force for every operation in the batch. Existing keys
// are overwritten, and the warning must not mention the --force flag.
func TestAddCmd_batchPromptImpliesForce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"),
		"services:\n"+
			"  database:\n"+
			"    image: alpine\n"+
			"    label_file:\n      - database.labels\n"+
			"  server:\n"+
			"    image: alpine\n"+
			"    label_file:\n      - server.labels\n")

	writeFile(t, filepath.Join(dir, "database.labels"), "diun.enable=true\ndiun.metadata.app=immich\n")
	writeFile(t, filepath.Join(dir, "server.labels"), "diun.enable=true\n")

	stdout, stderr, code, runErr := run(t, "y\n",
		"add", "--service-all", "diun.enable=false", dir)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}

	if strings.Contains(stderr, "use --force") {
		t.Errorf("prompt-mode warning must not mention --force, got stderr:\n%s", stderr)
	}
	for _, svc := range []string{"database", "server"} {
		want := fmt.Sprintf("Warning: %s: label \"diun.enable\" already exists", svc)
		if !strings.Contains(stderr, want) {
			t.Errorf("expected overwrite warning for service %q in stderr, got:\n%s", svc, stderr)
		}
	}

	gotDB, err := os.ReadFile(filepath.Join(dir, "database.labels"))
	if err != nil {
		t.Fatalf("reading database.labels: %v", err)
	}
	wantDB := "diun.enable=false\ndiun.metadata.app=immich\n"
	if string(gotDB) != wantDB {
		t.Errorf("database.labels = %q, want %q", gotDB, wantDB)
	}

	gotServer, err := os.ReadFile(filepath.Join(dir, "server.labels"))
	if err != nil {
		t.Fatalf("reading server.labels: %v", err)
	}
	wantServer := "diun.enable=false\n"
	if string(gotServer) != wantServer {
		t.Errorf("server.labels = %q, want %q", gotServer, wantServer)
	}

	if !strings.Contains(stdout, "Apply 1 label(s) to 2 service(s) across 1 project(s)") {
		t.Errorf("expected batch prompt summary in stdout, got:\n%s", stdout)
	}
}

func TestAddCmd_noComposeFileReturnsExitCode4(t *testing.T) {
	dir := t.TempDir() // empty, no Compose file at all

	_, _, code, runErr := run(t, "", "add", "--dry-run", "web", "com.example.a=1", dir)
	if runErr == nil {
		t.Fatal("expected an error when no Compose file is found")
	}
	if code != 4 {
		t.Errorf("code = %d, want 4", code)
	}
}

func TestAddCmd_dryRunFlagReachesAddPackage(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	original := "services:\n  web:\n    image: alpine\n"
	writeFile(t, composeFile, original)

	stdout, stderr, code, runErr := run(t, "", "add", "--dry-run", "--yes", "web", "com.example.a=1", dir)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(stdout, "dry run") {
		t.Errorf("expected dry-run confirmation in output, got: %s", stdout)
	}
	got, _ := os.ReadFile(composeFile)
	if string(got) != original {
		t.Error("compose file was modified despite --dry-run")
	}
}

func TestAddCmd_forceAndValuesNoQuoteFlagsParse(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")

	// --dry-run avoids needing real docker for this wiring-only check;
	// it only confirms the flags are read and passed through without
	// error, not their write-path effect (covered in internal/add's own
	// tests).
	_, stderr, code, runErr := run(t, "",
		"add", "--dry-run", "--force", "--inline", "--values-no-quote", "web", "com.example.a=1", dir)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
}

func TestAddCmd_missingTemplateDirectoryErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")

	_, _, code, runErr := run(t, "", "add", "--dry-run", "--template", "prod", "web", dir)
	if runErr == nil {
		t.Fatal("expected an error: no template directory is configured in this test's environment")
	}
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

func TestGenerateCmd_dryRunAcrossMultipleServices(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	original := "services:\n  web:\n    labels:\n      com.example.a: \"1\"\n"
	writeFile(t, composeFile, original)

	stdout, stderr, code, runErr := run(t, "", "generate", "--dry-run", "--yes", dir)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dry run") {
		t.Errorf("expected dry-run notice, got: %s", stdout)
	}
	got, _ := os.ReadFile(composeFile)
	if string(got) != original {
		t.Error("compose file was modified despite --dry-run")
	}
}

func TestGenerateCmd_noPathArgDefaultsToCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWD) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	_, stderr, code, runErr := run(t, "", "generate", "--dry-run", "--yes")
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
}

func TestGenerateCmd_noComposeFileReturnsExitCode4(t *testing.T) {
	dir := t.TempDir()
	_, _, code, runErr := run(t, "", "generate", "--dry-run", dir)
	if runErr == nil {
		t.Fatal("expected an error when no Compose file is found")
	}
	if code != 4 {
		t.Errorf("code = %d, want 4", code)
	}
}

func TestGenerateCmd_recursiveFlagParses(t *testing.T) {
	parent := t.TempDir()
	writeFile(t, filepath.Join(parent, "app1", "compose.yml"), "services:\n  web:\n    image: alpine\n")

	_, stderr, code, runErr := run(t, "", "generate", "--recursive", "--dry-run", "--yes", parent)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
}

func TestHelp_generateCommandDocumentsForce(t *testing.T) {
	stdout, _, _, err := run(t, "", "generate", "--help")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"--force", "--recursive"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected --help output to document %q, got:\n%s", want, stdout)
		}
	}
}
func TestRemoveCmd_directoryLastArgIsTreatedAsPath(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "myproject")
	composeFile := filepath.Join(project, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    label_file:\n      - web.labels\n")
	writeFile(t, filepath.Join(project, "web.labels"), "com.example.a=1\ncom.example.b=2\n")

	_, stderr, code, runErr := run(t, "", "remove", "--yes", "web", "com.example.a", project)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	got, _ := os.ReadFile(filepath.Join(project, "web.labels.pre-label"))
	if string(got) != "com.example.a=1\ncom.example.b=2\n" {
		t.Errorf("expected a backup of the original label file, got %q", got)
	}
}

func TestRemoveCmd_serviceNotFoundReturnsExitCode2(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")

	_, _, code, runErr := run(t, "", "remove", "nonexistent", "com.example.a", dir)
	if runErr == nil {
		t.Fatal("expected an error for a nonexistent service")
	}
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

func TestRemoveCmd_noComposeFileReturnsExitCode4(t *testing.T) {
	dir := t.TempDir()
	_, _, code, runErr := run(t, "", "remove", "web", "com.example.a", dir)
	if runErr == nil {
		t.Fatal("expected an error when no Compose file is found")
	}
	if code != 4 {
		t.Errorf("code = %d, want 4", code)
	}
}

func TestRemoveCmd_dryRunMakesNoChanges(t *testing.T) {
	dir := t.TempDir()
	labelFile := filepath.Join(dir, "web.labels")
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    label_file:\n      - web.labels\n")
	writeFile(t, labelFile, "com.example.a=1\n")

	stdout, stderr, code, runErr := run(t, "", "remove", "--dry-run", "--yes", "web", "com.example.a", dir)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dry run") {
		t.Errorf("expected dry-run notice, got: %s", stdout)
	}
	got, _ := os.ReadFile(labelFile)
	if string(got) != "com.example.a=1\n" {
		t.Error("label file was modified despite --dry-run")
	}
}

func TestHelp_removeCommandDocumentsOnEmptyCreate(t *testing.T) {
	stdout, _, _, err := run(t, "", "remove", "--help")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"--on-empty-create", "--on-none-create", "--values-no-quote"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected --help output to document %q, got:\n%s", want, stdout)
		}
	}
}

func TestTemplateListCmd_noTemplatesFound(t *testing.T) {
	templateDir := t.TempDir()
	t.Setenv("DAPLABEL_TEMPLATE_DIR", templateDir)

	stdout, stderr, code, runErr := run(t, "", "template", "list")
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(stdout, "No templates found") {
		t.Errorf("expected a 'no templates found' message, got: %s", stdout)
	}
}

func TestTemplateListCmd_listsCreatedTemplates(t *testing.T) {
	templateDir := t.TempDir()
	t.Setenv("DAPLABEL_TEMPLATE_DIR", templateDir)
	writeFile(t, filepath.Join(templateDir, "prod"), "ENV=prod\n")
	writeFile(t, filepath.Join(templateDir, "dev"), "ENV=dev\n")

	stdout, stderr, code, runErr := run(t, "", "template", "list")
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(stdout, "dev") || !strings.Contains(stdout, "prod") {
		t.Errorf("expected both template names listed, got: %s", stdout)
	}
}

func TestTemplateCreateCmd_writesTemplateFile(t *testing.T) {
	templateDir := t.TempDir()
	t.Setenv("DAPLABEL_TEMPLATE_DIR", templateDir)

	_, stderr, code, runErr := run(t, "", "template", "create", "--yes", "prod", "ENV=prod", "LOG_LEVEL=warn")
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	got, rerr := os.ReadFile(filepath.Join(templateDir, "prod"))
	if rerr != nil {
		t.Fatalf("reading created template: %v", rerr)
	}
	if string(got) != "ENV=prod\nLOG_LEVEL=warn\n" {
		t.Errorf("content = %q", got)
	}
}

func TestTemplateCreateCmd_noTemplateDirConfiguredReturnsError(t *testing.T) {
	t.Setenv("DAPLABEL_TEMPLATE_DIR", "")

	_, _, code, runErr := run(t, "", "template", "create", "--yes", "prod", "ENV=prod")
	if runErr == nil {
		t.Fatal("expected an error when no template directory is configured")
	}
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

func TestTemplateApplyCmd_appliesTemplateWithVariableExpansion(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	templateDir := t.TempDir()
	t.Setenv("DAPLABEL_TEMPLATE_DIR", templateDir)
	writeFile(t, filepath.Join(templateDir, "prod"), "com.example.owner=$SERVICE_NAME\n")

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")

	_, stderr, code, runErr := run(t, "", "template", "apply", "--yes", "prod", "web", dir)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	got, rerr := os.ReadFile(filepath.Join(dir, "web.labels"))
	if rerr != nil {
		t.Fatalf("reading generated label file: %v", rerr)
	}
	if string(got) != "com.example.owner=web\n" {
		t.Errorf("content = %q, want $SERVICE_NAME expanded to 'web'", got)
	}
}

func TestTemplateApplyCmd_pathDefaultsToCurrentDirectory(t *testing.T) {
	templateDir := t.TempDir()
	t.Setenv("DAPLABEL_TEMPLATE_DIR", templateDir)
	writeFile(t, filepath.Join(templateDir, "prod"), "ENV=prod\n")

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWD) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	_, stderr, code, runErr := run(t, "", "template", "apply", "--dry-run", "--yes", "prod", "web")
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
}

func TestHelp_templateApplyDocumentsSharedAddFlags(t *testing.T) {
	stdout, _, _, err := run(t, "", "template", "apply", "--help")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"--inline", "--force", "--on-empty-create", "--on-none-create"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected --help output to document %q, got:\n%s", want, stdout)
		}
	}
}

func TestConfigCmd_showsResolvedValuesAndSources(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("DAPLABEL_TEMPLATE_DIR", "/tmp/from-env")

	stdout, stderr, code, runErr := run(t, "", "config")
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(stdout, "DAPLABEL_TEMPLATE_DIR") || !strings.Contains(stdout, "/tmp/from-env") {
		t.Errorf("expected the env-sourced template dir shown, got: %s", stdout)
	}
	if !strings.Contains(stdout, "environment variable") {
		t.Errorf("expected the source tier shown, got: %s", stdout)
	}
	if !strings.Contains(stdout, "sources checked") {
		t.Errorf("expected the sources-checked section, got: %s", stdout)
	}
}

func TestConfigCmd_respectsExplicitConfigFlag(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "ci.conf")
	writeFile(t, explicit, "DAPLABEL_EDITOR=nano\n")

	stdout, stderr, code, runErr := run(t, "", "--config", explicit, "config")
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(stdout, "nano") {
		t.Errorf("expected the explicit config file's value shown, got: %s", stdout)
	}
	if !strings.Contains(stdout, explicit) {
		t.Errorf("expected the explicit config file's path shown in sources, got: %s", stdout)
	}
}

func TestAddCmd_everyServiceParsesLabelsNotServiceName(t *testing.T) {
	parent := t.TempDir()
	project := filepath.Join(parent, "test_daplabel")
	writeFile(t, filepath.Join(project, "compose.yml"), "services:\n  web:\n    image: alpine\n  api:\n    image: alpine\n")

	stdout, stderr, code, runErr := run(t, "",
		"add", "-r", "--dry-run", "--service-all", "diun.enable=false", parent)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dry run") {
		// --yes skips the prompt and the command commits, so stdout should
		// contain a dry-run notice when --dry-run is used. Without --dry-run
		// the confirmation prompt fires because multiple services are in the
		// plan. To keep the test non-interactive and deterministic, we use
		// --dry-run here, which surfaces a "dry run" summary in stdout.
		t.Errorf("expected dry-run summary in stdout, got:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestResolveFormatFlags_noFlagsNoConfigDefaultsToPlain(t *testing.T) {
	// When no --format-* flag is set and DAPLABEL_DEFAULT_SURVEY_FORMAT
	// is empty (unset), resolveFormatFlags must return "plain".
	root := NewRootCmd(BuildInfo{Version: "test"})
	surveyCmd, _, err := root.Find([]string{"survey"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate no flags set: cobra's default for bool flags is false.
	format, err := resolveFormatFlags(surveyCmd, "")
	if err != nil {
		t.Fatal(err)
	}
	if format != "plain" {
		t.Errorf("got %q, want %q (hardcoded default when no config and no flag)", format, "plain")
	}
}

func TestResolveFormatFlags_configDefaultUsedWhenNoFlag(t *testing.T) {
	root := NewRootCmd(BuildInfo{Version: "test"})
	surveyCmd, _, err := root.Find([]string{"survey"})
	if err != nil {
		t.Fatal(err)
	}
	format, err := resolveFormatFlags(surveyCmd, "table")
	if err != nil {
		t.Fatal(err)
	}
	if format != "table" {
		t.Errorf("got %q, want %q (config default should be used when no flag is set)", format, "table")
	}
}

func TestResolveFormatFlags_explicitFlagOverridesConfigDefault(t *testing.T) {
	root := NewRootCmd(BuildInfo{Version: "test"})
	surveyCmd, _, err := root.Find([]string{"survey"})
	if err != nil {
		t.Fatal(err)
	}
	// Set --format-tree explicitly, even though config says "plain".
	if err := surveyCmd.Flags().Set("format-tree", "true"); err != nil {
		t.Fatal(err)
	}
	format, err := resolveFormatFlags(surveyCmd, "plain")
	if err != nil {
		t.Fatal(err)
	}
	if format != "tree" {
		t.Errorf("got %q, want %q (explicit --format-tree must override config default)", format, "tree")
	}
}

func TestResolveFormatFlags_invalidConfigDefaultIgnored(t *testing.T) {
	root := NewRootCmd(BuildInfo{Version: "test"})
	surveyCmd, _, err := root.Find([]string{"survey"})
	if err != nil {
		t.Fatal(err)
	}
	// An invalid config value is silently ignored; "plain" is the fallback.
	format, err := resolveFormatFlags(surveyCmd, "--bubbles")
	if err != nil {
		t.Fatal(err)
	}
	if format != "plain" {
		t.Errorf("got %q, want %q (invalid config default should be silently ignored)", format, "plain")
	}
}

func TestResolveFormatFlags_explicitFormatPlainWorks(t *testing.T) {
	root := NewRootCmd(BuildInfo{Version: "test"})
	surveyCmd, _, err := root.Find([]string{"survey"})
	if err != nil {
		t.Fatal(err)
	}
	if err := surveyCmd.Flags().Set("format-plain", "true"); err != nil {
		t.Fatal(err)
	}
	format, err := resolveFormatFlags(surveyCmd, "tree")
	if err != nil {
		t.Fatal(err)
	}
	if format != "plain" {
		t.Errorf("got %q, want %q (explicit --format-plain must win)", format, "plain")
	}
}

func TestHelp_addCommandDocumentsAllFlags(t *testing.T) {
	stdout, _, _, err := run(t, "", "add", "--help")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"--inline",
		"--values-no-quote",
		"--template",
		"--force",
		"--recursive",
		"-r",
		"--service",
		"--service-all",
		"--on-empty-create",
		"--on-none-create",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected --help output to document %q, got:\n%s", want, stdout)
		}
	}
}

// TestAddCmd_serviceModeRejectsNonExistentPath verifies that when
// --service is used, a non-existent directory argument produces a clear
// error rather than being silently treated as a label key.
func TestAddCmd_serviceModeRejectsNonExistentPath(t *testing.T) {
	_, _, code, runErr := run(t, "", "add", "--dry-run", "--service", "web", "com.example.a=1", "/nonexistent")
	if runErr == nil {
		t.Fatal("expected an error for a non-existent path")
	}
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.Contains(runErr.Error(), "does not exist") {
		t.Errorf("expected error to mention 'does not exist', got: %v", runErr)
	}
}

// TestAddCmd_serviceAllModeRejectsNonExistentPath verifies that when
// --service-all is used, a non-existent directory argument produces a
// clear error rather than being silently treated as a label key.
func TestAddCmd_serviceAllModeRejectsNonExistentPath(t *testing.T) {
	_, _, code, runErr := run(t, "", "add", "--dry-run", "--service-all", "com.example.a=1", "/nonexistent")
	if runErr == nil {
		t.Fatal("expected an error for a non-existent path")
	}
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.Contains(runErr.Error(), "does not exist") {
		t.Errorf("expected error to mention 'does not exist', got: %v", runErr)
	}
}

// TestAddCmd_lastArgPathLikeRejectsNonExistentPath verifies that in the
// non-flag path (common case), a last argument that looks like a path
// but doesn't exist produces a clear error rather than being silently
// treated as a label key.
func TestAddCmd_lastArgPathLikeRejectsNonExistentPath(t *testing.T) {
	_, _, code, runErr := run(t, "", "add", "--dry-run", "web", "com.example.a=1", "./nonexistent-typo-dir")
	if runErr == nil {
		t.Fatal("expected an error for a non-existent path-like argument")
	}
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.Contains(runErr.Error(), "does not exist") {
		t.Errorf("expected error to mention 'does not exist', got: %v", runErr)
	}
}

// TestRemoveCmd_rejectsNonExistentPath verifies that a non-existent
// directory argument to remove produces a clear error rather than being
// silently treated as a label key to remove.
func TestRemoveCmd_rejectsNonExistentPath(t *testing.T) {
	_, _, code, runErr := run(t, "", "remove", "web", "com.example.a", "/nonexistent")
	if runErr == nil {
		t.Fatal("expected an error for a non-existent path")
	}
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.Contains(runErr.Error(), "does not exist") {
		t.Errorf("expected error to mention 'does not exist', got: %v", runErr)
	}
}

func TestRemoveCmd_everyServiceFlagParses(t *testing.T) {
	parent := t.TempDir()
	project := filepath.Join(parent, "test_daplabel")
	writeFile(t, filepath.Join(project, "compose.yml"), "services:\n  web:\n    image: alpine\n    label_file:\n      - web.labels\n  api:\n    image: alpine\n    label_file:\n      - api.labels\n")
	writeFile(t, filepath.Join(project, "web.labels"), "com.example.a=1\n")
	writeFile(t, filepath.Join(project, "api.labels"), "com.example.a=1\n")

	stdout, stderr, code, runErr := run(t, "",
		"remove", "-r", "--dry-run", "--yes", "--service-all", "com.example.a", parent)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dry run") {
		t.Errorf("expected dry-run summary in stdout, got:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestRemoveCmd_serviceFlagParses(t *testing.T) {
	parent := t.TempDir()
	project := filepath.Join(parent, "test_daplabel")
	writeFile(t, filepath.Join(project, "compose.yml"), "services:\n  web:\n    image: alpine\n    label_file:\n      - web.labels\n  api:\n    image: alpine\n")
	writeFile(t, filepath.Join(project, "web.labels"), "com.example.a=1\n")

	stdout, stderr, code, runErr := run(t, "",
		"remove", "-r", "--dry-run", "--yes", "--service", "web", "com.example.a", parent)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dry run") {
		t.Errorf("expected dry-run summary in stdout, got:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestRemoveCmd_recursiveFlagParses(t *testing.T) {
	parent := t.TempDir()
	writeFile(t, filepath.Join(parent, "app1", "compose.yml"), "services:\n  web:\n    image: alpine\n    label_file:\n      - web.labels\n")
	writeFile(t, filepath.Join(parent, "app1", "web.labels"), "com.example.a=1\n")

	stdout, stderr, code, runErr := run(t, "",
		"remove", "-r", "--dry-run", "--yes", "--service-all", "com.example.a", parent)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dry run") {
		t.Errorf("expected dry-run summary in stdout, got:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestRemoveCmd_commaSeparatedPaths(t *testing.T) {
	dir := t.TempDir()
	proj1 := filepath.Join(dir, "proj1")
	proj2 := filepath.Join(dir, "proj2")
	writeFile(t, filepath.Join(proj1, "compose.yml"), "services:\n  web:\n    image: alpine\n    label_file:\n      - web.labels\n")
	writeFile(t, filepath.Join(proj1, "web.labels"), "com.example.a=1\n")
	writeFile(t, filepath.Join(proj2, "compose.yml"), "services:\n  web:\n    image: alpine\n    label_file:\n      - web.labels\n")
	writeFile(t, filepath.Join(proj2, "web.labels"), "com.example.a=1\n")

	commaArg := proj1 + "," + proj2
	stdout, stderr, code, runErr := run(t, "",
		"remove", "--dry-run", "--yes", "--service-all", "com.example.a", commaArg)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dry run") {
		t.Errorf("expected dry-run summary in stdout, got:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestAddCmd_commaSeparatedPaths(t *testing.T) {
	dir := t.TempDir()
	proj1 := filepath.Join(dir, "proj1")
	proj2 := filepath.Join(dir, "proj2")
	writeFile(t, filepath.Join(proj1, "compose.yml"), "services:\n  web:\n    image: alpine\n")
	writeFile(t, filepath.Join(proj2, "compose.yml"), "services:\n  web:\n    image: alpine\n")

	commaArg := proj1 + "," + proj2
	stdout, stderr, code, runErr := run(t, "",
		"add", "--dry-run", "--yes", "--service-all", "com.example.a=1", commaArg)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dry run") {
		t.Errorf("expected dry-run summary in stdout, got:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestSurveyCmd_commaSeparatedPaths(t *testing.T) {
	dir := t.TempDir()
	proj1 := filepath.Join(dir, "proj1")
	proj2 := filepath.Join(dir, "proj2")
	writeFile(t, filepath.Join(proj1, "compose.yml"), "services:\n  web:\n    image: alpine\n")
	writeFile(t, filepath.Join(proj2, "compose.yml"), "services:\n  web:\n    image: alpine\n")

	commaArg := proj1 + "," + proj2
	stdout, stderr, code, runErr := run(t, "", "survey", "--format-plain", commaArg)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "proj1") || !strings.Contains(stdout, "proj2") {
		t.Errorf("expected both projects in survey output, got:\n%s", stdout)
	}
}

func TestGenerateCmd_commaSeparatedPaths(t *testing.T) {
	dir := t.TempDir()
	proj1 := filepath.Join(dir, "proj1")
	proj2 := filepath.Join(dir, "proj2")
	writeFile(t, filepath.Join(proj1, "compose.yml"), "services:\n  web:\n    labels:\n      com.example.a: \"1\"\n")
	writeFile(t, filepath.Join(proj2, "compose.yml"), "services:\n  web:\n    labels:\n      com.example.b: \"2\"\n")

	commaArg := proj1 + "," + proj2
	stdout, stderr, code, runErr := run(t, "",
		"generate", "--dry-run", "--yes", commaArg)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dry run") {
		t.Errorf("expected dry-run summary in stdout, got:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestCleanCmd_commaSeparatedPaths(t *testing.T) {
	dir := t.TempDir()
	proj1 := filepath.Join(dir, "proj1")
	proj2 := filepath.Join(dir, "proj2")
	writeFile(t, filepath.Join(proj1, "compose.yml"), "services:\n  web:\n    image: alpine\n")
	writeFile(t, filepath.Join(proj1, "web.labels.pre-label"), "backup\n")
	writeFile(t, filepath.Join(proj2, "compose.yml"), "services:\n  web:\n    image: alpine\n")
	writeFile(t, filepath.Join(proj2, "web.labels.pre-label"), "backup\n")

	commaArg := proj1 + "," + proj2
	stdout, stderr, code, runErr := run(t, "",
		"clean", "--dry-run", "--yes", commaArg)
	if runErr != nil {
		t.Fatalf("run: %v (stderr: %s)", runErr, stderr)
	}
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dry run") {
		t.Errorf("expected dry-run summary in stdout, got:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestHelp_removeCommandDocumentsNewFlags(t *testing.T) {
	stdout, _, _, err := run(t, "", "remove", "--help")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"--recursive", "-r", "--service", "--service-all", "--on-empty-create"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected --help output to document %q, got:\n%s", want, stdout)
		}
	}
}
