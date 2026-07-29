package labelfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveLabels_removesNamedKeysPreservingOthers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web.labels")
	if err := os.WriteFile(path, []byte("com.example.a=1\ncom.example.b=2\ncom.example.c=3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	content, removed, err := RemoveLabels(path, []string{"com.example.a", "com.example.c"})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 {
		t.Errorf("removed = %v, want 2 keys", removed)
	}
	want := "com.example.b=2\n"
	if string(content) != want {
		t.Errorf("content = %q, want %q", content, want)
	}
}

func TestRemoveLabels_keyNotPresentIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web.labels")
	if err := os.WriteFile(path, []byte("com.example.a=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	content, removed, err := RemoveLabels(path, []string{"com.example.nonexistent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want none", removed)
	}
	if string(content) != "com.example.a=1\n" {
		t.Errorf("content = %q, want unchanged", content)
	}
}

func TestRemoveLabels_removingEverythingLeavesEmptyContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web.labels")
	if err := os.WriteFile(path, []byte("com.example.a=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	content, removed, err := RemoveLabels(path, []string{"com.example.a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 {
		t.Errorf("removed = %v, want 1 key", removed)
	}
	if string(content) != "" {
		t.Errorf("content = %q, want empty", content)
	}
}

func TestRemoveLabels_missingFileIsNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.labels")
	content, removed, err := RemoveLabels(path, []string{"com.example.a"})
	if err != nil {
		t.Fatal(err)
	}
	if content != nil || removed != nil {
		t.Errorf("content=%v removed=%v, want both nil for a missing file", content, removed)
	}
}

func TestRemoveLabels_preservesMalformedAndBlankLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web.labels")
	original := "com.example.a=1\n\n# a comment, not KEY=VALUE\ncom.example.b=2\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	content, removed, err := RemoveLabels(path, []string{"com.example.a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 {
		t.Errorf("removed = %v, want 1 key", removed)
	}
	want := "\n# a comment, not KEY=VALUE\ncom.example.b=2\n"
	if string(content) != want {
		t.Errorf("content = %q, want %q", content, want)
	}
}
