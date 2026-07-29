package writeops

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neutralvibes/daplabel/internal/atomicwrite"
)

func TestCommit_dryRunMakesNoChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	var set atomicwrite.Set
	set.Add(path, []byte("new content"))

	var out, errOut bytes.Buffer
	code, _, err := Commit(&out, &errOut, strings.NewReader(""), &set, "", false, true, true, "summary line", 5*time.Minute)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("file was created despite dryRun=true")
	}
	if !strings.Contains(out.String(), "summary line") || !strings.Contains(out.String(), "dry run") {
		t.Errorf("expected summary and dry-run notice in output, got: %s", out.String())
	}
}

func TestCommit_yesTrueSkipsPromptAndWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	var set atomicwrite.Set
	set.Add(path, []byte("new content"))

	var out, errOut bytes.Buffer
	code, _, err := Commit(&out, &errOut, strings.NewReader(""), &set, "", false, false, true, "summary line", 5*time.Minute)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	got, statErr := os.ReadFile(path)
	if statErr != nil {
		t.Fatalf("expected file to be written, got: %v", statErr)
	}
	if string(got) != "new content" {
		t.Errorf("content = %q", got)
	}
}

func TestCommit_promptDeclinedMakesNoChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	var set atomicwrite.Set
	set.Add(path, []byte("new content"))

	var out, errOut bytes.Buffer
	code, _, err := Commit(&out, &errOut, strings.NewReader("n\n"), &set, "", false, false, false, "summary line", 5*time.Minute)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("file was created despite declining the prompt")
	}
}

func TestCommit_promptQuitMakesNoChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	var set atomicwrite.Set
	set.Add(path, []byte("new content"))

	var out, errOut bytes.Buffer
	code, _, err := Commit(&out, &errOut, strings.NewReader("q\n"), &set, "", false, false, false, "summary line", 5*time.Minute)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "Aborted.") {
		t.Errorf("expected an 'Aborted.' message, got: %s", out.String())
	}
}
