package survey

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestRun_jsonFormat_neverEmitsNullForCollections is the core requirement
// this format was built to satisfy: every array field is `[]` when
// empty, never `null` — checked directly against the raw output text,
// since that's the actual thing a consumer would trip over, not just
// whether it unmarshals.
func TestRun_jsonFormat_neverEmitsNullForCollections(t *testing.T) {
	dir := t.TempDir()
	// A service with zero labels, and a file with zero services besides
	// that one — the two cases most likely to tempt a nil slice.
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  bare:\n    image: alpine\n")

	var out, warn bytes.Buffer
	if _, err := Run(&out, &warn, []string{dir}, Options{Format: "json"}); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out.String(), "null") {
		t.Errorf("JSON output must never contain null for a collection field, got:\n%s", out.String())
	}
}

func TestRun_jsonFormat_emptyResultIsEmptyArrayNotNull(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")

	var out, warn bytes.Buffer
	// A filter that matches nothing — the whole top-level array should
	// still be `[]`, not `null`.
	if _, err := Run(&out, &warn, []string{dir}, Options{
		Format: "json",
		Filter: []KeyValueFilter{{Key: "nothing.matches"}},
	}); err != nil {
		t.Fatal(err)
	}

	got := strings.TrimSpace(out.String())
	if got != "[]" {
		t.Errorf("got %q, want exactly \"[]\" for zero matching projects", got)
	}
}

func TestRun_jsonFormat_validAndWellShaped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), `
services:
  web:
    labels:
      com.example.a: "1"
    label_file:
      - ./web.labels
`)
	writeFile(t, filepath.Join(dir, "web.labels"), "com.example.b=2\n")

	var out, warn bytes.Buffer
	if _, err := Run(&out, &warn, []string{dir}, Options{Format: "json"}); err != nil {
		t.Fatal(err)
	}

	var projects []jsonProject
	if err := json.Unmarshal(out.Bytes(), &projects); err != nil {
		t.Fatalf("output was not valid JSON matching the expected shape: %v\noutput:\n%s", err, out.String())
	}
	if len(projects) != 1 {
		t.Fatalf("got %d projects, want 1", len(projects))
	}
	if len(projects[0].Files) != 1 {
		t.Fatalf("got %d files, want 1", len(projects[0].Files))
	}
	svc := projects[0].Files[0].Services[0]
	if len(svc.InlineLabels) != 1 || svc.InlineLabels[0].Key != "com.example.a" {
		t.Errorf("got inline labels %+v, want com.example.a", svc.InlineLabels)
	}
	if len(svc.LabelFiles) != 1 || len(svc.LabelFiles[0].Labels) != 1 || svc.LabelFiles[0].Labels[0].Key != "com.example.b" {
		t.Errorf("got label files %+v, want one ref with com.example.b", svc.LabelFiles)
	}
}

// TestRun_jsonFormat_noExtraOutput verifies nothing else — no footer
// breakdown, no stray text — is mixed into the JSON stream.
func TestRun_jsonFormat_noExtraOutput(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")

	var out, warn bytes.Buffer
	if _, err := Run(&out, &warn, []string{dir}, Options{Format: "json"}); err != nil {
		t.Fatal(err)
	}

	var v interface{}
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("entire stdout must be valid JSON with nothing appended, got error: %v\noutput:\n%s", err, out.String())
	}
}
