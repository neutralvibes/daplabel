package templatecmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neutralvibes/daplabel/internal/labelfile"
)

func TestList_missingDirIsEmptyNotError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	names, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("names = %v, want none", names)
	}
}

func TestList_noConfiguredDirIsAnError(t *testing.T) {
	_, err := List("")
	if err == nil {
		t.Fatal("expected an error when no template directory is configured")
	}
}

func TestList_returnsSortedFileNamesOnly(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"staging", "prod", "dev"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("ENV="+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "a-subdirectory"), 0o755); err != nil {
		t.Fatal(err)
	}

	names, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"dev", "prod", "staging"}
	if len(names) != len(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestCreate_writesNewTemplate(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code, err := Create(&out, &errOut, strings.NewReader(""), dir, "prod",
		[]labelfile.Label{{Key: "ENV", Value: "prod"}, {Key: "LOG_LEVEL", Value: "warn"}},
		false, false, true, 5*time.Minute)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	got, rerr := os.ReadFile(filepath.Join(dir, "prod"))
	if rerr != nil {
		t.Fatalf("reading created template: %v", rerr)
	}
	want := "ENV=prod\nLOG_LEVEL=warn\n"
	if string(got) != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestCreate_createsTemplateDirectoryIfMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "templates", "nested")
	var out, errOut bytes.Buffer
	code, err := Create(&out, &errOut, strings.NewReader(""), dir, "prod",
		[]labelfile.Label{{Key: "ENV", Value: "prod"}}, false, false, true, 5*time.Minute)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "prod")); statErr != nil {
		t.Errorf("expected the template directory to be created, got: %v", statErr)
	}
}

func TestCreate_existingKeyNotOverwrittenWithoutForce(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prod"), []byte("ENV=orig\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code, err := Create(&out, &errOut, strings.NewReader(""), dir, "prod",
		[]labelfile.Label{{Key: "ENV", Value: "new"}}, false, false, true, 5*time.Minute)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "prod"))
	if string(got) != "ENV=orig\n" {
		t.Errorf("content = %q, want unchanged", got)
	}
	if !strings.Contains(errOut.String(), "ENV") {
		t.Errorf("expected a warning naming the skipped key, got: %s", errOut.String())
	}
}

func TestCreate_forceOverwritesExistingKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prod"), []byte("ENV=orig\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code, err := Create(&out, &errOut, strings.NewReader(""), dir, "prod",
		[]labelfile.Label{{Key: "ENV", Value: "new"}}, true, false, true, 5*time.Minute)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "prod"))
	if string(got) != "ENV=new\n" {
		t.Errorf("content = %q, want overwritten", got)
	}
}

func TestCreate_emptyTemplate(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code, err := Create(&out, &errOut, strings.NewReader(""), dir, "prod", nil, false, false, true, 5*time.Minute)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}

	got, rerr := os.ReadFile(filepath.Join(dir, "prod"))
	if rerr != nil {
		t.Fatalf("reading created template: %v", rerr)
	}
	if len(got) != 0 {
		t.Errorf("expected empty file, got %q", got)
	}
}

func TestCreate_emptyTemplateAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prod"), []byte("ENV=prod\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code, err := Create(&out, &errOut, strings.NewReader(""), dir, "prod", nil, false, false, true, 5*time.Minute)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}

	got, _ := os.ReadFile(filepath.Join(dir, "prod"))
	if string(got) != "ENV=prod\n" {
		t.Errorf("content = %q, want unchanged", got)
	}
	if !strings.Contains(out.String(), "already exists") {
		t.Errorf("expected notice that template already exists, got: %s", out.String())
	}
}

func TestCreate_emptyTemplateDryRun(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code, err := Create(&out, &errOut, strings.NewReader(""), dir, "prod", nil, false, true, true, 5*time.Minute)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "prod")); !os.IsNotExist(statErr) {
		t.Error("expected no file to be created with --dry-run")
	}
	if !strings.Contains(out.String(), "dry run") {
		t.Errorf("expected a dry-run notice, got: %s", out.String())
	}
}

func TestCreate_emptyTemplateDeclined(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code, err := Create(&out, &errOut, strings.NewReader("n\n"), dir, "prod", nil, false, false, false, 5*time.Minute)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "prod")); !os.IsNotExist(statErr) {
		t.Error("expected no file to be created after declining the prompt")
	}
}

func TestCreate_noTemplateDirConfigured(t *testing.T) {
	var out, errOut bytes.Buffer
	code, err := Create(&out, &errOut, strings.NewReader(""), "", "prod",
		[]labelfile.Label{{Key: "ENV", Value: "prod"}}, false, false, true, 5*time.Minute)
	if err == nil {
		t.Fatal("expected an error when no template directory is configured")
	}
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

func TestCreate_dryRunMakesNoChanges(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code, err := Create(&out, &errOut, strings.NewReader(""), dir, "prod",
		[]labelfile.Label{{Key: "ENV", Value: "prod"}}, false, true, true, 5*time.Minute)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "prod")); !os.IsNotExist(statErr) {
		t.Error("expected no file to be created with --dry-run")
	}
	if !strings.Contains(out.String(), "dry run") {
		t.Errorf("expected a dry-run notice, got: %s", out.String())
	}
}

// installTestEditor creates a fake editor in a temporary directory, adds
// that directory to PATH, and returns a cleanup function. The fake editor
// writes its first argument (the file path) to a marker file so the test
// can verify it was invoked on the expected path.
func installTestEditor(t *testing.T, name, markerPath string) func() {
	t.Helper()
	binDir := t.TempDir()
	script := filepath.Join(binDir, name)
	body := fmt.Sprintf("#!/bin/sh\necho \"$1\" > %s\n", markerPath)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	_ = os.Setenv("PATH", binDir+":"+oldPath)
	return func() { _ = os.Setenv("PATH", oldPath) }
}

func TestEdit_rejectsSudo(t *testing.T) {
	t.Setenv("SUDO_USER", "bob")
	err := Edit("", "", "")
	if err == nil {
		t.Fatal("expected an error when running under sudo")
	}
	if !strings.Contains(err.Error(), "SECURITY-RISK") {
		t.Errorf("expected SECURITY-RISK error, got: %v", err)
	}
}

func TestEdit_opensExistingTemplate(t *testing.T) {
	// Edit() refuses to run at all under sudo (see TestEdit_rejectsSudo) —
	// this test isn't exercising that guard, so isolate SUDO_USER
	// regardless of how the test binary itself happened to be invoked.
	t.Setenv("SUDO_USER", "")

	dir := t.TempDir()
	path := filepath.Join(dir, "prod")
	if err := os.WriteFile(path, []byte("ENV=prod\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(t.TempDir(), "marker")
	cleanup := installTestEditor(t, "testeditor", marker)
	defer cleanup()

	if err := Edit(dir, "prod", "testeditor"); err != nil {
		t.Fatalf("Edit: %v", err)
	}

	got, rerr := os.ReadFile(marker)
	if rerr != nil {
		t.Fatalf("reading marker: %v", rerr)
	}
	if strings.TrimSpace(string(got)) != path {
		t.Errorf("editor called with %q, want %q", got, path)
	}
}

func TestEdit_defaultsToNano(t *testing.T) {
	t.Setenv("SUDO_USER", "")

	dir := t.TempDir()
	path := filepath.Join(dir, "prod")
	if err := os.WriteFile(path, []byte("ENV=prod\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(t.TempDir(), "marker")
	cleanup := installTestEditor(t, "nano", marker)
	defer cleanup()

	if err := Edit(dir, "prod", ""); err != nil {
		t.Fatalf("Edit: %v", err)
	}

	got, rerr := os.ReadFile(marker)
	if rerr != nil {
		t.Fatalf("reading marker: %v", rerr)
	}
	if strings.TrimSpace(string(got)) != path {
		t.Errorf("editor called with %q, want %q", got, path)
	}
}

func TestEdit_templateNotFound(t *testing.T) {
	t.Setenv("SUDO_USER", "")

	dir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "marker")
	cleanup := installTestEditor(t, "testeditor", marker)
	defer cleanup()

	err := Edit(dir, "missing", "testeditor")
	if err == nil {
		t.Fatal("expected an error when template does not exist")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestRemove_deletesExistingTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prod")
	if err := os.WriteFile(path, []byte("ENV=prod\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code, err := Remove(&out, &errOut, strings.NewReader("y\n"), dir, "prod", false, false)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("expected template to be removed")
	}
}

func TestRemove_templateNotFound(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code, err := Remove(&out, &errOut, strings.NewReader(""), dir, "missing", false, false)
	if err == nil {
		t.Fatal("expected an error when template does not exist")
	}
	if code != 4 {
		t.Errorf("code = %d, want 4", code)
	}
}

func TestRemove_dryRunDoesNotDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prod")
	if err := os.WriteFile(path, []byte("ENV=prod\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code, err := Remove(&out, &errOut, strings.NewReader(""), dir, "prod", true, false)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		t.Error("expected template to still exist after dry run")
	}
	if !strings.Contains(out.String(), "dry run") {
		t.Errorf("expected a dry-run notice, got: %s", out.String())
	}
}

func TestRemove_promptDeclinedDoesNotDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prod")
	if err := os.WriteFile(path, []byte("ENV=prod\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code, err := Remove(&out, &errOut, strings.NewReader("n\n"), dir, "prod", false, false)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		t.Error("expected template to still exist after declining")
	}
}

func TestRemove_yesSkipsPrompt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prod")
	if err := os.WriteFile(path, []byte("ENV=prod\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code, err := Remove(&out, &errOut, strings.NewReader(""), dir, "prod", false, true)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("expected template to be removed")
	}
}

func TestCreate_promptDeclinedMakesNoChanges(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code, err := Create(&out, &errOut, strings.NewReader("n\n"), dir, "prod",
		[]labelfile.Label{{Key: "ENV", Value: "prod"}}, false, false, false, 5*time.Minute)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "prod")); !os.IsNotExist(statErr) {
		t.Error("expected no file to be created after declining the prompt")
	}
}
