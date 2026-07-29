package generate

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

// dockerComposeAvailable mirrors composevalidate's own test helper —
// see DECISIONS.md Decision 30: any test reaching a real commit that
// changes a Compose file needs a Docker-capable environment.
func dockerComposeAvailable() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	cmd := exec.Command("docker", "compose", "version")
	return cmd.Run() == nil
}

func TestRun_noComposeFilesFound(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{dir}, false, Options{Yes: true})
	if err == nil {
		t.Fatal("expected an error when no Compose files are found")
	}
	if code != 4 {
		t.Errorf("code = %d, want 4", code)
	}
}

func TestRun_serviceWithNoInlineLabelsIsSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{dir}, false, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(out.String(), "nothing to generate") {
		t.Errorf("expected a 'nothing to generate' message, got: %s", out.String())
	}
	if _, statErr := os.Stat(filepath.Join(dir, "web.labels")); !os.IsNotExist(statErr) {
		t.Error("expected no label file to be created for a service with no inline labels")
	}
}

func TestRun_extractsInlineLabelsToNewFile(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n    labels:\n      com.example.a: \"1\"\n      com.example.b: \"2\"\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{dir}, false, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v (stderr: %s)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("code = %d; stderr: %s", code, errOut.String())
	}

	labelFile := readFile(t, filepath.Join(dir, "web.labels"))
	if labelFile != "com.example.a=1\ncom.example.b=2\n" {
		t.Errorf("web.labels content = %q", labelFile)
	}

	composeContent := readFile(t, composeFile)
	if strings.Contains(composeContent, "com.example.a") {
		t.Errorf("expected inline labels removed from Compose file, got:\n%s", composeContent)
	}
	if !strings.Contains(composeContent, "label_file:") || !strings.Contains(composeContent, "web.labels") {
		t.Errorf("expected a label_file reference added, got:\n%s", composeContent)
	}
}

func TestRun_appendsToExistingLabelFileRef(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n    label_file:\n      - web.labels\n    labels:\n      com.example.new: \"1\"\n")
	writeFile(t, filepath.Join(dir, "web.labels"), "com.example.existing=orig\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{dir}, false, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	got := readFile(t, filepath.Join(dir, "web.labels"))
	want := "com.example.existing=orig\ncom.example.new=1\n"
	if got != want {
		t.Errorf("web.labels content = %q, want %q", got, want)
	}

	composeContent := readFile(t, composeFile)
	if strings.Contains(composeContent, "com.example.new") {
		t.Errorf("expected the migrated inline label removed, got:\n%s", composeContent)
	}
	// No second label_file ref should have been added.
	if strings.Count(composeContent, "web.labels") != 1 {
		t.Errorf("expected exactly one web.labels reference, got:\n%s", composeContent)
	}
}

func TestRun_existingKeyLeftInlineWithoutForce(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n    label_file:\n      - web.labels\n    labels:\n      com.example.a: \"new\"\n      com.example.b: \"1\"\n")
	writeFile(t, filepath.Join(dir, "web.labels"), "com.example.a=orig\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{dir}, false, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	got := readFile(t, filepath.Join(dir, "web.labels"))
	if got != "com.example.a=orig\ncom.example.b=1\n" {
		t.Errorf("web.labels content = %q", got)
	}

	// com.example.a couldn't be migrated (conflict without --force), so
	// it must remain inline; com.example.b, successfully migrated,
	// should be gone from the inline block.
	composeContent := readFile(t, composeFile)
	if !strings.Contains(composeContent, "com.example.a") {
		t.Errorf("expected the conflicting label to remain inline, got:\n%s", composeContent)
	}
	if strings.Contains(composeContent, "com.example.b") {
		t.Errorf("expected the successfully migrated label removed from inline, got:\n%s", composeContent)
	}
	if !strings.Contains(errOut.String(), "com.example.a") {
		t.Errorf("expected a warning naming the conflicting key, got: %s", errOut.String())
	}
}

func TestRun_forceOverwritesExistingKeyInLabelFile(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n    label_file:\n      - web.labels\n    labels:\n      com.example.a: \"new\"\n")
	writeFile(t, filepath.Join(dir, "web.labels"), "com.example.a=orig\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{dir}, false, Options{Yes: true, Force: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	got := readFile(t, filepath.Join(dir, "web.labels"))
	if got != "com.example.a=new\n" {
		t.Errorf("web.labels content = %q, want overwritten", got)
	}
	composeContent := readFile(t, composeFile)
	if strings.Contains(composeContent, "com.example.a") {
		t.Errorf("expected the now-migrated label removed from inline, got:\n%s", composeContent)
	}
}

func TestRun_twoServicesSharingOneLabelFileBothMigrate(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, `services:
  api:
    image: alpine
    label_file:
      - shared.labels
    labels:
      com.example.api: "1"
  worker:
    image: alpine
    label_file:
      - shared.labels
    labels:
      com.example.worker: "2"
`)
	writeFile(t, filepath.Join(dir, "shared.labels"), "com.example.existing=orig\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{dir}, false, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v (stderr %s)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	got := readFile(t, filepath.Join(dir, "shared.labels"))
	// Both services' migrated labels must be present — this is exactly
	// the bug the byPath grouping in processFile exists to avoid: two
	// independent WriteLabels calls against the same stale on-disk
	// content would have clobbered one service's contribution.
	if !strings.Contains(got, "com.example.existing=orig") ||
		!strings.Contains(got, "com.example.api=1") ||
		!strings.Contains(got, "com.example.worker=2") {
		t.Errorf("shared.labels content = %q, want all three entries present", got)
	}
}

func TestRun_dryRunMakesNoChanges(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	original := "services:\n  web:\n    labels:\n      com.example.a: \"1\"\n"
	writeFile(t, composeFile, original)

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{dir}, false, Options{Yes: true, DryRun: true})
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
		t.Errorf("expected dry-run notice in output, got: %s", out.String())
	}
}

func TestRun_extractsListFormLabelsToNewFile(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	// n8n-shaped: labels written as a list of "KEY=VALUE" strings.
	writeFile(t, composeFile, "services:\n  n8n:\n    image: n8nio/n8n\n    labels:\n      - \"diun.enable=true\"\n      - \"diun.watch_repo=true\"\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{dir}, false, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v (stderr: %s)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("code = %d; stderr: %s", code, errOut.String())
	}

	labelFile := readFile(t, filepath.Join(dir, "n8n.labels"))
	if labelFile != "diun.enable=true\ndiun.watch_repo=true\n" {
		t.Errorf("n8n.labels content = %q", labelFile)
	}

	composeContent := readFile(t, composeFile)
	if strings.Contains(composeContent, "diun.enable") || strings.Contains(composeContent, "diun.watch_repo") {
		t.Errorf("expected all inline labels removed from Compose file, got:\n%s", composeContent)
	}
	if !strings.Contains(composeContent, "label_file:") || !strings.Contains(composeContent, "n8n.labels") {
		t.Errorf("expected a label_file reference added, got:\n%s", composeContent)
	}
	// The old "cannot edit list-form inline labels in place" warning must not appear.
	if strings.Contains(errOut.String(), "cannot edit list-form") {
		t.Errorf("expected no list-form rejection warning, got: %s", errOut.String())
	}
}

func TestRun_recursiveScansSubdirectories(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	parent := t.TempDir()
	writeFile(t, filepath.Join(parent, "app1", "compose.yml"), "services:\n  web:\n    image: alpine\n    labels:\n      com.example.a: \"1\"\n")
	writeFile(t, filepath.Join(parent, "app2", "compose.yml"), "services:\n  web:\n    image: alpine\n    labels:\n      com.example.b: \"2\"\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{parent}, true, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v (stderr %s)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if readFile(t, filepath.Join(parent, "app1", "web.labels")) != "com.example.a=1\n" {
		t.Error("app1's label file not generated correctly")
	}
	if readFile(t, filepath.Join(parent, "app2", "web.labels")) != "com.example.b=2\n" {
		t.Error("app2's label file not generated correctly")
	}
}
