package labelfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteLabels_newFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web.labels")
	content, skipped, err := WriteLabels(path, []Label{
		{Key: "com.example.a", Value: "1"},
		{Key: "com.example.b", Value: "2"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none for a brand new file", skipped)
	}
	want := "com.example.a=1\ncom.example.b=2\n"
	if string(content) != want {
		t.Errorf("content = %q, want %q", content, want)
	}
}

func TestWriteLabels_appendsNewKeysPreservingExistingLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web.labels")
	if err := os.WriteFile(path, []byte("com.example.a=1\ncom.example.b=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	content, skipped, err := WriteLabels(path, []Label{{Key: "com.example.c", Value: "3"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none", skipped)
	}
	want := "com.example.a=1\ncom.example.b=2\ncom.example.c=3\n"
	if string(content) != want {
		t.Errorf("content = %q, want %q", content, want)
	}
}

func TestWriteLabels_existingKeySkippedWithoutForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web.labels")
	if err := os.WriteFile(path, []byte("com.example.a=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	content, skipped, err := WriteLabels(path, []Label{
		{Key: "com.example.a", Value: "999"},
		{Key: "com.example.b", Value: "2"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || skipped[0] != "com.example.a" {
		t.Errorf("skipped = %v, want [com.example.a]", skipped)
	}
	want := "com.example.a=1\ncom.example.b=2\n"
	if string(content) != want {
		t.Errorf("content = %q, want %q (original value untouched)", content, want)
	}
}

func TestWriteLabels_forceUpdatesInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web.labels")
	if err := os.WriteFile(path, []byte("com.example.a=1\ncom.example.tier=staging\ncom.example.b=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	content, skipped, err := WriteLabels(path, []Label{{Key: "com.example.tier", Value: "prod"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none when force=true", skipped)
	}
	want := "com.example.a=1\ncom.example.tier=prod\ncom.example.b=2\n"
	if string(content) != want {
		t.Errorf("content = %q, want %q (updated in place, not moved)", content, want)
	}
}

func TestWriteLabels_preservesMalformedAndBlankLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web.labels")
	original := "com.example.a=1\n\n# a comment line, not KEY=VALUE\ncom.example.b=2\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	content, _, err := WriteLabels(path, []Label{{Key: "com.example.c", Value: "3"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := original + "com.example.c=3\n"
	if string(content) != want {
		t.Errorf("content = %q, want %q", content, want)
	}
}
