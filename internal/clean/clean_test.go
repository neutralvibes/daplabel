package clean

import (
	"bytes"
	"os"
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

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat %s: %v", path, err)
	return false
}

func TestRun_noPreLabelFilesReportsNothingToClean(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{dir}, false, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(out.String(), "No .pre-label backup files found") {
		t.Errorf("expected 'no backup files' message, got: %s", out.String())
	}
}

func TestRun_removesPreLabelFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")
	writeFile(t, filepath.Join(dir, "compose.yml.pre-label"), "old content\n")
	writeFile(t, filepath.Join(dir, "web.labels.pre-label"), "old labels\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{dir}, false, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d; stderr: %s", code, errOut.String())
	}

	if fileExists(t, filepath.Join(dir, "compose.yml.pre-label")) {
		t.Error("compose.yml.pre-label was not removed")
	}
	if fileExists(t, filepath.Join(dir, "web.labels.pre-label")) {
		t.Error("web.labels.pre-label was not removed")
	}
	if !strings.Contains(out.String(), "Removed 2 .pre-label backup file(s) from") {
		t.Errorf("expected per-directory removal summary, got: %s", out.String())
	}
}

func TestRun_dryRunMakesNoChanges(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")
	writeFile(t, filepath.Join(dir, "compose.yml.pre-label"), "old content\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{dir}, false, Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !fileExists(t, filepath.Join(dir, "compose.yml.pre-label")) {
		t.Error("backup file was removed despite --dry-run")
	}
	if !strings.Contains(out.String(), "dry run") {
		t.Errorf("expected dry-run notice, got: %s", out.String())
	}
}

func TestRun_promptDeclinedMakesNoChanges(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")
	writeFile(t, filepath.Join(dir, "compose.yml.pre-label"), "old content\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader("n\n"), []string{dir}, false, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !fileExists(t, filepath.Join(dir, "compose.yml.pre-label")) {
		t.Error("backup file was removed despite declining the prompt")
	}
	if !strings.Contains(out.String(), "Skipped") {
		t.Errorf("expected skip message, got: %s", out.String())
	}
}

func TestRun_promptQuitMakesNoChanges(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")
	writeFile(t, filepath.Join(dir, "compose.yml.pre-label"), "old content\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader("q\n"), []string{dir}, false, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !fileExists(t, filepath.Join(dir, "compose.yml.pre-label")) {
		t.Error("backup file was removed despite quitting the prompt")
	}
}

func TestRun_promptAllProceeds(t *testing.T) {
	parent := t.TempDir()
	projA := filepath.Join(parent, "project-a")
	projB := filepath.Join(parent, "project-b")
	writeFile(t, filepath.Join(projA, "compose.yml"), "services:\n  web:\n    image: alpine\n")
	writeFile(t, filepath.Join(projB, "compose.yml"), "services:\n  db:\n    image: postgres\n")
	writeFile(t, filepath.Join(projA, "compose.yml.pre-label"), "old\n")
	writeFile(t, filepath.Join(projB, "compose.yml.pre-label"), "old\n")

	// 'a' on the first prompt should remove from project-a and skip the
	// second prompt, removing from project-b as well.
	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader("a\n"), []string{parent}, true, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if fileExists(t, filepath.Join(projA, "compose.yml.pre-label")) {
		t.Error("project-a backup was not removed after 'all' response")
	}
	if fileExists(t, filepath.Join(projB, "compose.yml.pre-label")) {
		t.Error("project-b backup was not removed after 'all' response")
	}
}

func TestRun_noComposeFileReturnsError(t *testing.T) {
	dir := t.TempDir()

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{dir}, false, Options{})
	if err == nil {
		t.Fatal("expected an error when no Compose file is found")
	}
	if code != 4 {
		t.Errorf("code = %d, want 4", code)
	}
}

func TestRun_recursiveCleansSubdirectories(t *testing.T) {
	parent := t.TempDir()
	projA := filepath.Join(parent, "project-a")
	projB := filepath.Join(parent, "project-b")
	writeFile(t, filepath.Join(projA, "compose.yml"), "services:\n  web:\n    image: alpine\n")
	writeFile(t, filepath.Join(projB, "compose.yml"), "services:\n  db:\n    image: postgres\n")
	writeFile(t, filepath.Join(projA, "compose.yml.pre-label"), "old\n")
	writeFile(t, filepath.Join(projB, "web.labels.pre-label"), "old\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{parent}, true, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d; stderr: %s", code, errOut.String())
	}
	if fileExists(t, filepath.Join(projA, "compose.yml.pre-label")) {
		t.Error("project-a backup was not removed")
	}
	if fileExists(t, filepath.Join(projB, "web.labels.pre-label")) {
		t.Error("project-b backup was not removed")
	}
}

func TestRun_subdirectoriesWithoutComposeFilesIgnored(t *testing.T) {
	parent := t.TempDir()
	proj := filepath.Join(parent, "project")
	other := filepath.Join(parent, "other")
	writeFile(t, filepath.Join(proj, "compose.yml"), "services:\n  web:\n    image: alpine\n")
	writeFile(t, filepath.Join(other, "compose.yml.pre-label"), "should remain\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{parent}, true, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d; stderr: %s", code, errOut.String())
	}
	if !fileExists(t, filepath.Join(other, "compose.yml.pre-label")) {
		t.Error("backup in non-project directory was incorrectly removed")
	}
}

func TestRun_onlyPreLabelFilesRemoved(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")
	writeFile(t, filepath.Join(dir, "compose.yml.pre-label"), "old content\n")
	writeFile(t, filepath.Join(dir, "web.labels"), "com.example.a=1\n")
	writeFile(t, filepath.Join(dir, "notes.txt"), "do not delete\n")

	var out, errOut bytes.Buffer
	code, err := Run(&out, &errOut, strings.NewReader(""), []string{dir}, false, Options{Yes: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !fileExists(t, filepath.Join(dir, "compose.yml")) {
		t.Error("compose.yml was incorrectly removed")
	}
	if !fileExists(t, filepath.Join(dir, "web.labels")) {
		t.Error("web.labels was incorrectly removed")
	}
	if !fileExists(t, filepath.Join(dir, "notes.txt")) {
		t.Error("notes.txt was incorrectly removed")
	}
}
