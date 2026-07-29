package add

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// dockerComposeAvailable mirrors composevalidate's own test helper:
// DECISIONS.md Decision 30 deliberately has no test double for `docker
// compose config` (it's called directly, unconditionally, by commit()
// whenever a Compose file changes), so any test that reaches a real
// commit against a changed Compose file needs a Docker-capable
// environment — the same tradeoff Decision 30 accepts, in exchange for
// requiring one in CI.
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

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestDetectConflicts_warnsBeforeBatchPrompt(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    label_file:\n      - web.labels\n  api:\n    label_file:\n      - api.labels\n")
	writeFile(t, filepath.Join(dir, "web.labels"), "diun.enable=true\n")
	writeFile(t, filepath.Join(dir, "api.labels"), "diun.enable=true\n")

	var errOut bytes.Buffer
	if err := DetectConflicts(&errOut, composeFile, "web", []LabelArg{{Key: "diun.enable", Value: "false"}}, Options{OnConflict: "skip"}); err != nil {
		t.Fatalf("DetectConflicts: %v", err)
	}
	if !strings.Contains(errOut.String(), "Warning: web: label \"diun.enable\" already exists") {
		t.Errorf("expected conflict warning for web, got: %s", errOut.String())
	}

	errOut.Reset()
	if err := DetectConflicts(&errOut, composeFile, "api", []LabelArg{{Key: "diun.enable", Value: "false"}}, Options{OnConflict: "skip"}); err != nil {
		t.Fatalf("DetectConflicts: %v", err)
	}
	if !strings.Contains(errOut.String(), "Warning: api: label \"diun.enable\" already exists") {
		t.Errorf("expected conflict warning for api, got: %s", errOut.String())
	}

	// A service that does not have the key should not produce a warning.
	errOut.Reset()
	writeFile(t, filepath.Join(dir, "compose2.yml"), "services:\n  web:\n    image: alpine\n")
	if err := DetectConflicts(&errOut, filepath.Join(dir, "compose2.yml"), "web", []LabelArg{{Key: "diun.enable", Value: "false"}}, Options{OnConflict: "skip"}); err != nil {
		t.Fatalf("DetectConflicts: %v", err)
	}
	if errOut.String() != "" {
		t.Errorf("expected no warning for service without key, got: %s", errOut.String())
	}
}

func TestRun_serviceNotFound(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "nonexistent",
		[]LabelArg{{Key: "a", Value: "1"}}, Options{Yes: true})
	if err == nil {
		t.Fatal("expected an error for a nonexistent service")
	}
	if code != 2 {
		t.Errorf("code = %d, want 2 (invalid arguments)", code)
	}
}

func TestRun_noLabelsGiven(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web", nil, Options{Yes: true})
	if err == nil {
		t.Fatal("expected an error when no labels and no template are given")
	}
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

func TestRun_createsNewLabelFileAndRef(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web",
		[]LabelArg{{Key: "com.example.a", Value: "1"}, {Key: "com.example.b", Value: "2"}},
		Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v (stderr: %s)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, errOut.String())
	}

	labelFile := filepath.Join(dir, "web.labels")
	got := readFile(t, labelFile)
	want := "com.example.a=1\ncom.example.b=2\n"
	if got != want {
		t.Errorf("label file content = %q, want %q", got, want)
	}

	composeContent := readFile(t, composeFile)
	if !strings.Contains(composeContent, "label_file:") || !strings.Contains(composeContent, "web.labels") {
		t.Errorf("expected a label_file reference added to the compose file, got:\n%s", composeContent)
	}

	// A .pre-label backup should not exist for a file that didn't
	// previously exist.
	if _, err := os.Stat(labelFile + ".pre-label"); !os.IsNotExist(err) {
		t.Errorf("unexpected backup for a brand new file")
	}
}

func TestRun_appendsToFirstExistingLabelFileRef(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    label_file:\n      - web.labels\n      - shared/common.labels\n")
	writeFile(t, filepath.Join(dir, "web.labels"), "com.example.existing=orig\n")
	writeFile(t, filepath.Join(dir, "shared", "common.labels"), "com.example.shared=x\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web",
		[]LabelArg{{Key: "com.example.new", Value: "42"}}, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	got := readFile(t, filepath.Join(dir, "web.labels"))
	want := "com.example.existing=orig\ncom.example.new=42\n"
	if got != want {
		t.Errorf("web.labels content = %q, want %q", got, want)
	}

	// The second (non-first) ref must be left completely alone.
	shared := readFile(t, filepath.Join(dir, "shared", "common.labels"))
	if shared != "com.example.shared=x\n" {
		t.Errorf("shared/common.labels was modified, got %q", shared)
	}

	// No new label_file ref should have been added; compose file's
	// label_file list should be unchanged (2 entries, same order).
	composeContent := readFile(t, composeFile)
	if !strings.Contains(composeContent, "web.labels") || !strings.Contains(composeContent, "shared/common.labels") {
		t.Errorf("expected both original refs preserved, got:\n%s", composeContent)
	}
}

func TestRun_existingKeyNotOverwrittenWithoutForce(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    label_file:\n      - web.labels\n")
	writeFile(t, filepath.Join(dir, "web.labels"), "com.example.a=orig\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web",
		[]LabelArg{{Key: "com.example.a", Value: "new"}}, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	got := readFile(t, filepath.Join(dir, "web.labels"))
	if got != "com.example.a=orig\n" {
		t.Errorf("content = %q, want unchanged original", got)
	}
	if !strings.Contains(errOut.String(), "com.example.a") {
		t.Errorf("expected a warning naming the skipped key, got: %s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "use --force") {
		t.Errorf("expected warning to mention --force in non-interactive mode, got: %s", errOut.String())
	}
}

func TestRun_forceOverwritesExistingKey(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    label_file:\n      - web.labels\n")
	writeFile(t, filepath.Join(dir, "web.labels"), "com.example.a=orig\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web",
		[]LabelArg{{Key: "com.example.a", Value: "new"}}, Options{Yes: true, Force: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	got := readFile(t, filepath.Join(dir, "web.labels"))
	if got != "com.example.a=new\n" {
		t.Errorf("content = %q, want overwritten", got)
	}
}

func TestRun_backsUpExistingLabelFileBeforeAppending(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    label_file:\n      - web.labels\n")
	writeFile(t, filepath.Join(dir, "web.labels"), "com.example.a=1\n")

	var out, errOut bytes.Buffer
	_, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web",
		[]LabelArg{{Key: "com.example.b", Value: "2"}}, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	backup := readFile(t, filepath.Join(dir, "web.labels.pre-label"))
	if backup != "com.example.a=1\n" {
		t.Errorf("backup content = %q, want the pre-write content", backup)
	}
}

func TestRun_missingLabelFileRefWarnsByDefault(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    label_file:\n      - web.labels\n") // web.labels does not exist on disk

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web",
		[]LabelArg{{Key: "com.example.a", Value: "1"}}, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Errorf("code = %d, want 0 (a missing label_file is a warning, not a fatal error)", code)
	}
	if !strings.Contains(errOut.String(), "does not exist") {
		t.Errorf("expected a warning about the missing label_file, got: %s", errOut.String())
	}
	if _, statErr := os.Stat(filepath.Join(dir, "web.labels")); !os.IsNotExist(statErr) {
		t.Error("expected web.labels to remain uncreated without --on-none-create")
	}
}

func TestRun_onNoneCreateCreatesMissingLabelFile(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    label_file:\n      - web.labels\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web",
		[]LabelArg{{Key: "com.example.a", Value: "1"}}, Options{Yes: true, OnNoneCreate: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	got := readFile(t, filepath.Join(dir, "web.labels"))
	if got != "com.example.a=1\n" {
		t.Errorf("content = %q, want the label written into the newly created file", got)
	}
}

func TestRun_onEmptyCreateWhenAllLabelsSkipped(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")
	// A stray label file already exists on disk, at the exact path
	// add's naming convention would use, but the service doesn't
	// reference it yet — so this is still "no existing label_file
	// references" from add's point of view, and the single key
	// requested already exists in it.
	writeFile(t, filepath.Join(dir, "web.labels"), "com.example.a=preexisting\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web",
		[]LabelArg{{Key: "com.example.a", Value: "1"}}, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	composeContent := readFile(t, composeFile)
	if strings.Contains(composeContent, "label_file") {
		t.Errorf("expected no label_file reference added without --on-empty-create, got:\n%s", composeContent)
	}

	// Now with --on-empty-create: the reference should be added.
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	out.Reset()
	errOut.Reset()
	code, err = Run(&out, &errOut, strings.NewReader(""), composeFile, "web",
		[]LabelArg{{Key: "com.example.a", Value: "1"}}, Options{Yes: true, OnEmptyCreate: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	composeContent = readFile(t, composeFile)
	if !strings.Contains(composeContent, "label_file:") || !strings.Contains(composeContent, "web.labels") {
		t.Errorf("expected a label_file reference added with --on-empty-create, got:\n%s", composeContent)
	}
}

func TestRun_inlineMode_writesLabelsDirectly(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web",
		[]LabelArg{{Key: "com.example.tier", Value: "prod"}}, Options{Yes: true, Inline: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	composeContent := readFile(t, composeFile)
	if !strings.Contains(composeContent, `"prod"`) {
		t.Errorf("expected a quoted inline value by default, got:\n%s", composeContent)
	}
	if strings.Contains(composeContent, "label_file") {
		t.Errorf("inline mode should not add a label_file reference, got:\n%s", composeContent)
	}

	// No label file should have been created.
	if _, statErr := os.Stat(filepath.Join(dir, "web.labels")); !os.IsNotExist(statErr) {
		t.Error("inline mode should not create a label file")
	}
}

func TestRun_inlineMode_valuesNoQuote(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web",
		[]LabelArg{{Key: "com.example.tier", Value: "prod"}},
		Options{Yes: true, Inline: true, ValuesNoQuote: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	composeContent := readFile(t, composeFile)
	if strings.Contains(composeContent, `"prod"`) {
		t.Errorf("expected an unquoted value with --values-no-quote, got:\n%s", composeContent)
	}
}

func TestRun_dryRunMakesNoChanges(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	original := "services:\n  web:\n    image: alpine\n"
	writeFile(t, composeFile, original)

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web",
		[]LabelArg{{Key: "com.example.a", Value: "1"}}, Options{Yes: true, DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if readFile(t, composeFile) != original {
		t.Error("compose file was modified despite --dry-run")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "web.labels")); !os.IsNotExist(statErr) {
		t.Error("label file was created despite --dry-run")
	}
	if !strings.Contains(out.String(), "dry run") {
		t.Errorf("expected dry-run output to say so, got: %s", out.String())
	}
}

func TestRun_promptDeclinedMakesNoChanges(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader("n\n"), composeFile, "web",
		[]LabelArg{{Key: "com.example.a", Value: "1"}}, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "web.labels")); !os.IsNotExist(statErr) {
		t.Error("label file was created despite declining the confirmation prompt")
	}
}

func TestRun_promptAcceptedProceedsWithChange(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader("y\n"), composeFile, "web",
		[]LabelArg{{Key: "com.example.a", Value: "1"}}, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "web.labels")); statErr != nil {
		t.Error("label file was not created after accepting the confirmation prompt")
	}
}

func TestRun_templateAppliesExpandedLabels(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	tmplDir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")
	writeFile(t, filepath.Join(tmplDir, "prod"), "com.example.owner=$SERVICE_NAME\ncom.example.app=$APP_NAME\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web", nil,
		Options{Yes: true, Template: "prod", TemplateDir: tmplDir})
	if err != nil {
		t.Fatalf("Run: %v (stderr %s)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	got := readFile(t, filepath.Join(dir, "web.labels"))
	appName := filepath.Base(dir)
	want := "com.example.owner=web\ncom.example.app=" + appName + "\n"
	if got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestRun_explicitLabelOverridesTemplateKey(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	tmplDir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")
	writeFile(t, filepath.Join(tmplDir, "prod"), "com.example.tier=staging\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web",
		[]LabelArg{{Key: "com.example.tier", Value: "prod-override"}},
		Options{Yes: true, Template: "prod", TemplateDir: tmplDir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	got := readFile(t, filepath.Join(dir, "web.labels"))
	if got != "com.example.tier=prod-override\n" {
		t.Errorf("content = %q, want the explicit value to win", got)
	}
}

func TestRun_malformedComposeFileIsComposeParsingError(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    labels:\n      - not: a: valid: list: item\n")

	var out, errOut bytes.Buffer
	code, _ := Run(&out, &errOut, strings.NewReader(""), composeFile, "web",
		[]LabelArg{{Key: "a", Value: "1"}}, Options{Yes: true})
	if code != 5 {
		t.Errorf("code = %d, want 5 (compose parsing error)", code)
	}
}

// TestRun_migratesInlineAndSkipsDuplicateWithoutForce verifies that when
// a service has inline labels and add creates a new label file, the
// inline labels are migrated into the file first. An incoming key that
// matches a migrated inline key is skipped (not duplicated) with a
// warning, and the inline labels are removed from the Compose file.
// With --yes, the warning still mentions --force because the user is not
// being prompted.
func TestRun_migratesInlineAndSkipsDuplicateWithoutForce(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  apprise-api:\n    image: alpine\n    labels:\n      diun.enable: \"true\"\n      diun.metadata.app: apprise\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "apprise-api",
		[]LabelArg{{Key: "diun.enable", Value: "false"}},
		Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v (stderr: %s)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, errOut.String())
	}

	// The label file should contain the migrated inline value (true),
	// NOT the incoming value (false) — since --force was not used.
	labelFile := filepath.Join(dir, "apprise-api.labels")
	got := readFile(t, labelFile)
	want := "diun.enable=true\ndiun.metadata.app=apprise\n"
	if got != want {
		t.Errorf("label file content = %q, want %q", got, want)
	}

	// The warning should mention the skipped key and --force.
	if !strings.Contains(errOut.String(), "diun.enable") {
		t.Errorf("expected a warning naming the skipped key, got: %s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "use --force") {
		t.Errorf("expected warning to mention --force in non-interactive mode, got: %s", errOut.String())
	}

	// The Compose file should have a label_file reference and the
	// inline labels should be removed.
	composeContent := readFile(t, composeFile)
	if !strings.Contains(composeContent, "label_file:") || !strings.Contains(composeContent, "apprise-api.labels") {
		t.Errorf("expected a label_file reference in compose file, got:\n%s", composeContent)
	}
	if strings.Contains(composeContent, "diun.enable") || strings.Contains(composeContent, "diun.metadata.app") {
		t.Errorf("inline labels should have been removed from compose file, got:\n%s", composeContent)
	}
}

// TestRun_interactiveOverwritesExistingKeyOnProceed verifies that in
// prompt mode (no --yes, no --force), choosing to proceed overwrites an
// existing key. The warning must not mention --force, since the prompt
// itself is the user's chance to decline.
func TestRun_interactiveOverwritesExistingKeyOnProceed(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")
	writeFile(t, filepath.Join(dir, "web.labels"), "com.example.a=orig\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader("y\n"), composeFile, "web",
		[]LabelArg{{Key: "com.example.a", Value: "new"}}, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}

	got := readFile(t, filepath.Join(dir, "web.labels"))
	if got != "com.example.a=new\n" {
		t.Errorf("content = %q, want overwritten", got)
	}

	if strings.Contains(errOut.String(), "use --force") {
		t.Errorf("expected warning not to mention --force in interactive mode, got: %s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "com.example.a") {
		t.Errorf("expected a warning naming the key, got: %s", errOut.String())
	}
}

// TestRun_interactiveDeclinedLeavesExistingKey verifies that answering
// 'n' at the prompt leaves an existing key untouched, even though the
// prompt mode would otherwise imply force on proceed.
func TestRun_interactiveDeclinedLeavesExistingKey(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")
	writeFile(t, filepath.Join(dir, "web.labels"), "com.example.a=orig\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader("n\n"), composeFile, "web",
		[]LabelArg{{Key: "com.example.a", Value: "new"}}, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}

	got := readFile(t, filepath.Join(dir, "web.labels"))
	if got != "com.example.a=orig\n" {
		t.Errorf("content = %q, want unchanged original", got)
	}
}

// TestRun_interactiveOverwritesMigratedInlineKeyOnProceed matches the
// specification's real example: a service with inline labels has one of
// those incoming keys added without --force; in prompt mode, proceeding
// overwrites the migrated inline value.
func TestRun_interactiveOverwritesMigratedInlineKeyOnProceed(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  apprise-api:\n    image: alpine\n    labels:\n      diun.enable: \"true\"\n      diun.metadata.app: apprise\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader("y\n"), composeFile, "apprise-api",
		[]LabelArg{{Key: "diun.enable", Value: "false"}}, Options{})
	if err != nil {
		t.Fatalf("Run: %v (stderr: %s)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, errOut.String())
	}

	labelFile := filepath.Join(dir, "apprise-api.labels")
	got := readFile(t, labelFile)
	want := "diun.enable=false\ndiun.metadata.app=apprise\n"
	if got != want {
		t.Errorf("label file content = %q, want %q", got, want)
	}

	if strings.Contains(errOut.String(), "use --force") {
		t.Errorf("expected warning not to mention --force in interactive mode, got: %s", errOut.String())
	}

	composeContent := readFile(t, composeFile)
	if !strings.Contains(composeContent, "label_file:") || !strings.Contains(composeContent, "apprise-api.labels") {
		t.Errorf("expected a label_file reference in compose file, got:\n%s", composeContent)
	}
	if strings.Contains(composeContent, "diun.enable") || strings.Contains(composeContent, "diun.metadata.app") {
		t.Errorf("inline labels should have been removed from compose file, got:\n%s", composeContent)
	}
}

// TestRun_migratesInlineAndOverwritesWithForce verifies that with
// --force, an incoming key overwrites the migrated inline value in the
// label file, and the inline labels are still removed from the Compose
// file. No duplication occurs.
func TestRun_migratesInlineAndOverwritesWithForce(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  apprise-api:\n    image: alpine\n    labels:\n      diun.enable: \"true\"\n      diun.metadata.app: apprise\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "apprise-api",
		[]LabelArg{{Key: "diun.enable", Value: "false"}},
		Options{Yes: true, Force: true})
	if err != nil {
		t.Fatalf("Run: %v (stderr: %s)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, errOut.String())
	}

	// The label file should contain the forced value (false), not the
	// migrated inline value (true).
	labelFile := filepath.Join(dir, "apprise-api.labels")
	got := readFile(t, labelFile)
	want := "diun.enable=false\ndiun.metadata.app=apprise\n"
	if got != want {
		t.Errorf("label file content = %q, want %q", got, want)
	}

	// The Compose file should have a label_file reference and the
	// inline labels should be removed.
	composeContent := readFile(t, composeFile)
	if !strings.Contains(composeContent, "label_file:") || !strings.Contains(composeContent, "apprise-api.labels") {
		t.Errorf("expected a label_file reference in compose file, got:\n%s", composeContent)
	}
	if strings.Contains(composeContent, "diun.enable") || strings.Contains(composeContent, "diun.metadata.app") {
		t.Errorf("inline labels should have been removed from compose file, got:\n%s", composeContent)
	}
}

// TestRun_migratesMultipleInlineLabelsMixedAdd verifies that when a
// service has several inline labels and the user adds a mix of new keys
// and keys that overlap with existing inline ones, the migration and
// skip/force logic work correctly together.
func TestRun_migratesMultipleInlineLabelsMixedAdd(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n    labels:\n      com.example.a: \"1\"\n      com.example.b: \"2\"\n      com.example.c: \"3\"\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web",
		[]LabelArg{
			{Key: "com.example.a", Value: "overridden"}, // overlaps inline, no --force → skipped
			{Key: "com.example.d", Value: "4"},          // new key
		},
		Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v (stderr: %s)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, errOut.String())
	}

	// Label file: migrated inline values (a=1, b=2, c=3) plus new key
	// (d=4). com.example.a keeps its migrated value (1), not the
	// incoming "overridden".
	labelFile := filepath.Join(dir, "web.labels")
	got := readFile(t, labelFile)
	want := "com.example.a=1\ncom.example.b=2\ncom.example.c=3\ncom.example.d=4\n"
	if got != want {
		t.Errorf("label file content = %q, want %q", got, want)
	}

	// Warning about the skipped key.
	if !strings.Contains(errOut.String(), "com.example.a") {
		t.Errorf("expected a warning naming the skipped key, got: %s", errOut.String())
	}

	// Compose file: label_file ref added, all inline labels removed.
	composeContent := readFile(t, composeFile)
	if !strings.Contains(composeContent, "label_file:") || !strings.Contains(composeContent, "web.labels") {
		t.Errorf("expected a label_file reference in compose file, got:\n%s", composeContent)
	}
	if strings.Contains(composeContent, "com.example.a") || strings.Contains(composeContent, "com.example.b") || strings.Contains(composeContent, "com.example.c") {
		t.Errorf("inline labels should have been removed from compose file, got:\n%s", composeContent)
	}
}
