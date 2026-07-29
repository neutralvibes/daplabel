package remove

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

// dockerComposeAvailable mirrors composevalidate's own test helper — see
// DECISIONS.md Decision 30.
func dockerComposeAvailable() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	cmd := exec.Command("docker", "compose", "version")
	return cmd.Run() == nil
}

func TestRun_serviceNotFound(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "nonexistent", []string{"com.example.a"}, Options{Yes: true})
	if err == nil {
		t.Fatal("expected an error for a nonexistent service")
	}
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

func TestRun_noKeysGiven(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web", nil, Options{Yes: true})
	if err == nil {
		t.Fatal("expected an error when no keys are given")
	}
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

func TestRun_keyNotFoundAnywhereWarnsAndMakesNoChanges(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    labels:\n      com.example.a: \"1\"\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web", []string{"com.example.nonexistent"}, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(errOut.String(), "com.example.nonexistent") {
		t.Errorf("expected a warning naming the not-found key, got: %s", errOut.String())
	}
	if !strings.Contains(out.String(), "Nothing to remove") {
		t.Errorf("expected a 'nothing to remove' message, got: %s", out.String())
	}
	composeContent := readFile(t, composeFile)
	if !strings.Contains(composeContent, "com.example.a") {
		t.Error("expected the unrelated existing label to remain untouched")
	}
}

func TestRun_removesFromLabelFilePreservingOtherKeys(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    label_file:\n      - web.labels\n")
	writeFile(t, filepath.Join(dir, "web.labels"), "com.example.a=1\ncom.example.b=2\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web", []string{"com.example.a"}, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v (stderr %s)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("code = %d; stderr: %s", code, errOut.String())
	}

	got := readFile(t, filepath.Join(dir, "web.labels"))
	if got != "com.example.b=2\n" {
		t.Errorf("web.labels content = %q, want only com.example.b remaining", got)
	}
	// The label_file ref should remain, since the file isn't empty.
	composeContent := readFile(t, composeFile)
	if !strings.Contains(composeContent, "label_file:") {
		t.Errorf("expected the label_file reference to remain, got:\n%s", composeContent)
	}
}

func TestRun_emptyingLabelFileDeletesItAndItsRefByDefault(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	labelFile := filepath.Join(dir, "web.labels")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n    label_file:\n      - web.labels\n")
	writeFile(t, labelFile, "com.example.a=1\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web", []string{"com.example.a"}, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	if _, statErr := os.Stat(labelFile); !os.IsNotExist(statErr) {
		t.Error("expected the emptied label file to be deleted")
	}
	backup, berr := os.ReadFile(labelFile + ".pre-label")
	if berr != nil {
		t.Fatalf("expected a backup of the deleted file, got: %v", berr)
	}
	if string(backup) != "com.example.a=1\n" {
		t.Errorf("backup content = %q", backup)
	}
	composeContent := readFile(t, composeFile)
	if strings.Contains(composeContent, "label_file") {
		t.Errorf("expected the label_file reference removed too, got:\n%s", composeContent)
	}
}

func TestRun_onEmptyCreateKeepsEmptiedFileAndRef(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	labelFile := filepath.Join(dir, "web.labels")
	writeFile(t, composeFile, "services:\n  web:\n    label_file:\n      - web.labels\n")
	writeFile(t, labelFile, "com.example.a=1\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web", []string{"com.example.a"}, Options{Yes: true, OnEmptyCreate: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	got, statErr := os.ReadFile(labelFile)
	if statErr != nil {
		t.Fatalf("expected the label file to still exist, got: %v", statErr)
	}
	if string(got) != "" {
		t.Errorf("content = %q, want empty", got)
	}
	composeContent := readFile(t, composeFile)
	if !strings.Contains(composeContent, "label_file:") {
		t.Errorf("expected the label_file reference kept with --on-empty-create, got:\n%s", composeContent)
	}
}

func TestRun_removesInlineLabel(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n    labels:\n      com.example.a: \"1\"\n      com.example.b: \"2\"\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web", []string{"com.example.a"}, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	composeContent := readFile(t, composeFile)
	if strings.Contains(composeContent, "com.example.a") {
		t.Errorf("expected com.example.a removed, got:\n%s", composeContent)
	}
	if !strings.Contains(composeContent, "com.example.b") {
		t.Errorf("expected com.example.b to remain, got:\n%s", composeContent)
	}
}

func TestRun_emptyingInlineLabelsRemovesTheKeyEntirely(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n    labels:\n      com.example.a: \"1\"\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web", []string{"com.example.a"}, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	composeContent := readFile(t, composeFile)
	if strings.Contains(composeContent, "labels") {
		t.Errorf("expected the labels: key removed entirely once emptied, got:\n%s", composeContent)
	}
}

func TestRun_removesFromBothInlineAndLabelFileSimultaneously(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	// An inconsistent-but-real-world-possible state: the same key present
	// both inline and in a referenced label_file.
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n    label_file:\n      - web.labels\n    labels:\n      com.example.a: \"1\"\n")
	writeFile(t, filepath.Join(dir, "web.labels"), "com.example.a=1\ncom.example.b=2\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web", []string{"com.example.a"}, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	composeContent := readFile(t, composeFile)
	if strings.Contains(composeContent, "com.example.a") {
		t.Errorf("expected com.example.a removed from inline too, got:\n%s", composeContent)
	}
	labelFileContent := readFile(t, filepath.Join(dir, "web.labels"))
	if labelFileContent != "com.example.b=2\n" {
		t.Errorf("web.labels content = %q, want com.example.a removed from there too", labelFileContent)
	}
}

func TestRun_missingReferencedLabelFileWarnsButDoesNotFail(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	// web.labels is referenced but does not exist on disk.
	writeFile(t, composeFile, "services:\n  web:\n    label_file:\n      - web.labels\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web", []string{"com.example.a"}, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(errOut.String(), "does not exist") {
		t.Errorf("expected a warning about the missing label_file, got: %s", errOut.String())
	}
}

func TestRun_dryRunMakesNoChanges(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	original := "services:\n  web:\n    labels:\n      com.example.a: \"1\"\n"
	writeFile(t, composeFile, original)

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web", []string{"com.example.a"}, Options{Yes: true, DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if readFile(t, composeFile) != original {
		t.Error("compose file was modified despite --dry-run")
	}
	if !strings.Contains(out.String(), "dry run") {
		t.Errorf("expected dry-run notice, got: %s", out.String())
	}
}

func TestRun_promptDeclinedMakesNoChanges(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	original := "services:\n  web:\n    labels:\n      com.example.a: \"1\"\n"
	writeFile(t, composeFile, original)

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader("n\n"), composeFile, "web", []string{"com.example.a"}, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if readFile(t, composeFile) != original {
		t.Error("compose file was modified despite declining the prompt")
	}
}

func TestRun_removesInlineLabelListForm(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n    labels:\n      - \"com.example.a=1\"\n      - \"com.example.b=2\"\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web", []string{"com.example.a"}, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d; stderr: %s", code, errOut.String())
	}

	composeContent := readFile(t, composeFile)
	if strings.Contains(composeContent, "com.example.a") {
		t.Errorf("expected com.example.a removed, got:\n%s", composeContent)
	}
	if !strings.Contains(composeContent, "com.example.b") {
		t.Errorf("expected com.example.b to remain, got:\n%s", composeContent)
	}
	if !strings.Contains(composeContent, "labels:") {
		t.Errorf("expected labels: to remain (list not emptied), got:\n%s", composeContent)
	}
}

func TestRun_emptyingListFormLabelsRemovesTheKeyEntirely(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    image: alpine\n    labels:\n      - \"com.example.a=1\"\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), composeFile, "web", []string{"com.example.a"}, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	composeContent := readFile(t, composeFile)
	if strings.Contains(composeContent, "labels") {
		t.Errorf("expected the labels: key removed entirely once emptied, got:\n%s", composeContent)
	}
}

func TestRun_malformedComposeFileIsComposeParsingError(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yml")
	writeFile(t, composeFile, "services:\n  web:\n    labels:\n      - not: a: valid: list: item\n")

	var out, errOut bytes.Buffer
	code, _ := Run(&out, &errOut, strings.NewReader(""), composeFile, "web", []string{"a"}, Options{Yes: true})
	if code != 5 {
		t.Errorf("code = %d, want 5 (compose parsing error)", code)
	}
}
