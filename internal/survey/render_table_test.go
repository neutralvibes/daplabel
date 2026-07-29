package survey

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_tableFormat_matchesRequestedLayout(t *testing.T) {
	parent := t.TempDir()

	writeFile(t, filepath.Join(parent, "open-webui", "compose.yml"), `
services:
  ollama-cpu:
    labels:
      diun.enable: "true"
  open-webui:
    labels:
      diun.enable: "true"
`)
	writeFile(t, filepath.Join(parent, "open-webui", "compose.override.yml"), `
services:
  open-webui:
    labels:
      diun.enable: "false"
      diun.watch_repo: "true"
`)
	writeFile(t, filepath.Join(parent, "pihole", "compose.yml"), `
services:
  cloudflared:
    labels:
      diun.enable: "true"
  nebula-sync:
    image: alpine
  pihole:
    labels:
      diun.enable: "true"
`)
	writeFile(t, filepath.Join(parent, "sonarr", "compose.yml"), `
services:
  sonarr:
    labels:
      diun.enable: "true"
      diun.metadata.app: "sonarr"
`)

	var out, warn bytes.Buffer
	code, err := Run(&out, &warn, []string{parent}, Options{Recursive: true, Format: "table"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("got exit code %d, want 0", code)
	}
	got := out.String()

	// Plaintext, not markdown: no pipe-table syntax and no bold/italic
	// markup anywhere in the output.
	for _, marker := range []string{"|", "**", "*"} {
		if strings.Contains(got, marker) {
			t.Errorf("table output contains markdown marker %q, want plain text:\n%s", marker, got)
		}
	}

	// Header carries the expected columns and no NOTES column.
	if !strings.Contains(got, "PROJECT") || !strings.Contains(got, "SOURCE") || !strings.Contains(got, "LABEL") {
		t.Fatalf("missing expected header columns, got:\n%s", got)
	}
	if strings.Contains(got, "NOTES") {
		t.Errorf("table output should not contain a NOTES column, got:\n%s", got)
	}

	lines := strings.Split(got, "\n")
	find := func(substrs ...string) bool {
		for _, l := range lines {
			ok := true
			for _, s := range substrs {
				if !strings.Contains(l, s) {
					ok = false
					break
				}
			}
			if ok {
				return true
			}
		}
		return false
	}

	// Project/File/Service shown once, then suppressed on repeat within
	// the same group; inline labels become yaml source.
	if !find("open-webui", "compose.yml", "ollama-cpu", "yaml", "diun.enable=true") {
		t.Errorf("expected ollama-cpu's row with project/file/service shown, got:\n%s", got)
	}

	// Second service in the same file: project and file blank, service shown.
	foundSuppressed := false
	for _, l := range lines {
		if strings.Contains(l, "open-webui") && strings.Contains(l, "diun.enable") &&
			!strings.Contains(l, "compose.yml") {
			foundSuppressed = true
		}
	}
	if !foundSuppressed {
		t.Errorf("expected open-webui's inline-label row with project/file suppressed, got:\n%s", got)
	}

	// Override file appears as a normal row without an (override) annotation.
	if strings.Contains(got, "(override)") {
		t.Errorf("table output should not contain '(override)' annotation, got:\n%s", got)
	}
	if !find("compose.override.yml", "open-webui", "yaml", "diun.enable=false") {
		t.Errorf("expected the override's diun.enable=false row, got:\n%s", got)
	}
	if !find("diun.watch_repo=true") {
		t.Errorf("expected the override's diun.watch_repo row, got:\n%s", got)
	}

	// A service with no inline labels and no label_file refs: one row
	// with label/source blank.
	if !find("nebula-sync") {
		t.Errorf("expected a row for nebula-sync, got:\n%s", got)
	}
	if find("nebula-sync", "(none)") {
		t.Errorf("nebula-sync row should not contain '(none)' placeholder, got:\n%s", got)
	}

	// Multiple labels on one service: service name suppressed on the
	// second label row.
	if !find("sonarr", "compose.yml", "sonarr", "yaml", "diun.enable=true") {
		t.Errorf("expected sonarr's first label row, got:\n%s", got)
	}
	if !find("diun.metadata.app=sonarr") {
		t.Errorf("expected sonarr's second label row, got:\n%s", got)
	}

	// Summary block still present.
	if !strings.Contains(got, "3 projects") || !strings.Contains(got, "6 services") {
		t.Errorf("expected the summary block, got:\n%s", got)
	}

	// Each project is followed by a blank separator line.
	if strings.Count(got, "\n\n") < 2 {
		t.Errorf("expected blank separators between project blocks, got:\n%s", got)
	}
}

func TestRun_tableFormat_carriesLabelFileAndConflictInformation(t *testing.T) {
	parent := t.TempDir()

	writeFile(t, filepath.Join(parent, "app", "compose.yml"), `
services:
  web:
    labels:
      diun.enable: "true"
    label_file:
      - labels/shared.env
      - labels/missing.env
`)
	writeFile(t, filepath.Join(parent, "app", "labels", "shared.env"), "foo=bar\n")

	var out, warn bytes.Buffer
	code, err := Run(&out, &warn, []string{parent}, Options{Recursive: true, Format: "table"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("got exit code %d, want 0", code)
	}
	got := out.String()

	// label_file sources are normalised to "file".
	if !strings.Contains(got, "file") {
		t.Errorf("expected a 'file' source column entry, got:\n%s", got)
	}
	if strings.Contains(got, "label_file") {
		t.Errorf("source column should not contain literal 'label_file', got:\n%s", got)
	}
	if !strings.Contains(got, "foo=bar") {
		t.Errorf("expected the shared.env file's label as KEY=VALUE, got:\n%s", got)
	}

	// Missing label_file is represented as a blank row with source=file
	// and no label value.
	if strings.Contains(got, textMissing) {
		t.Errorf("missing label_file should be silent (blank), not %q, got:\n%s", textMissing, got)
	}
}

func TestRun_tableFormat_noMatchesStillProducesValidTable(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")

	var out, warn bytes.Buffer
	code, err := Run(&out, &warn, []string{dir}, Options{
		Format: "table",
		Filter: []KeyValueFilter{{Key: "nothing.matches"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Errorf("got exit code %d, want 0", code)
	}
	if !strings.Contains(out.String(), "PROJECT") {
		t.Error("expected the header row even with zero matching rows")
	}
}

func TestRun_tableFormat_wrapsServiceNames(t *testing.T) {
	parent := t.TempDir()

	writeFile(t, filepath.Join(parent, "app", "compose.yml"), `
services:
  uk.co.company.prod.api:
    labels:
      diun.enable: "true"
  immich-machine-learning:
    labels:
      diun.enable: "true"
  postgres_db_replica:
    labels:
      diun.enable: "true"
  veryveryverylongservicename:
    labels:
      diun.enable: "true"
`)

	var out, warn bytes.Buffer
	code, err := Run(&out, &warn, []string{parent}, Options{Recursive: true, Format: "table"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("got exit code %d, want 0", code)
	}
	got := out.String()

	// Namespaced stack breaks at the dot.
	if !strings.Contains(got, "uk.co.") {
		t.Errorf("expected service name to wrap at the dot, got:\n%s", got)
	}
	// Multi-container grouping breaks at the hyphen.
	if !strings.Contains(got, "immich-machine-") || !strings.Contains(got, "learning") {
		t.Errorf("expected service name to wrap at the hyphen, got:\n%s", got)
	}
	// Database pattern breaks at the underscore.
	if !strings.Contains(got, "postgres_") || !strings.Contains(got, "db_replica") {
		t.Errorf("expected service name to wrap at the underscore, got:\n%s", got)
	}
	// No delimiter: hard break within the 20-char limit.
	if !strings.Contains(got, "veryveryverylongser") {
		t.Errorf("expected hard character break for delimiter-less name, got:\n%s", got)
	}
}

func TestRun_tableFormat_noWrapDisablesWrapping(t *testing.T) {
	parent := t.TempDir()

	longService := strings.Repeat("a", 25)
	writeFile(t, filepath.Join(parent, "app", "compose.yml"), `
services:
  `+longService+`:
    labels:
      diun.enable: "true"
`)

	var out, warn bytes.Buffer
	code, err := Run(&out, &warn, []string{parent}, Options{Recursive: true, Format: "table", NoWrap: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("got exit code %d, want 0", code)
	}
	got := out.String()

	if !strings.Contains(got, longService) {
		t.Errorf("expected the full unwrapped service name with --no-wrap, got:\n%s", got)
	}
}

func TestRun_tableFormat_noWrapDoesNotAffectOtherFormats(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), `
services:
  web:
    labels:
      diun.enable: "true"
`)

	var out, warn bytes.Buffer
	code, err := Run(&out, &warn, []string{dir}, Options{Format: "plain", NoWrap: true})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("got exit code %d, want 0", code)
	}
	if !strings.Contains(out.String(), "diun.enable=true") {
		t.Errorf("expected plain output to still render, got:\n%s", out.String())
	}
}

func TestRun_tableFormat_blankSeparatorsBetweenProjects(t *testing.T) {
	parent := t.TempDir()
	writeFile(t, filepath.Join(parent, "aaa", "compose.yml"), "services:\n  web:\n    labels:\n      a: '1'\n")
	writeFile(t, filepath.Join(parent, "bbb", "compose.yml"), "services:\n  web:\n    labels:\n      b: '2'\n")

	var out, warn bytes.Buffer
	code, err := Run(&out, &warn, []string{parent}, Options{Recursive: true, Format: "table"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("got exit code %d, want 0", code)
	}
	got := out.String()

	// There should be a blank line between the aaa block and the bbb block
	// (the Summary blank line is additional).
	if strings.Count(got, "\n\n") < 1 {
		t.Errorf("expected a blank line between project blocks, got:\n%s", got)
	}
}

func TestRun_tableFormat_serviceWithNoLabelsIsBlank(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  bare:\n    image: alpine\n")

	var out, warn bytes.Buffer
	code, err := Run(&out, &warn, []string{dir}, Options{Format: "table"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("got exit code %d, want 0", code)
	}
	got := out.String()

	if strings.Contains(got, "(none)") || strings.Contains(got, "(no labels)") {
		t.Errorf("service with no labels should leave label/source blank, got:\n%s", got)
	}
	if !strings.Contains(got, "bare") {
		t.Errorf("expected the bare service to appear, got:\n%s", got)
	}
}

func TestRun_tableFormat_emptyFileIsBlank(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n")

	var out, warn bytes.Buffer
	code, err := Run(&out, &warn, []string{dir}, Options{Format: "table"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("got exit code %d, want 0", code)
	}
	got := out.String()

	if strings.Contains(got, "(no services found)") {
		t.Errorf("empty file should leave service/label/source blank, got:\n%s", got)
	}
	if !strings.Contains(got, "compose.yml") {
		t.Errorf("expected the empty file row to appear, got:\n%s", got)
	}
}

func TestRun_tableFormat_labelValueWrapping(t *testing.T) {
	dir := t.TempDir()
	longValue := strings.Repeat("x", 60)
	writeFile(t, filepath.Join(dir, "compose.yml"), `
services:
  web:
    labels:
      diun.enable: "`+longValue+`"
`)

	var out, warn bytes.Buffer
	code, err := Run(&out, &warn, []string{dir}, Options{Format: "table"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("got exit code %d, want 0", code)
	}
	got := out.String()

	if !strings.Contains(got, "diun.enable=") {
		t.Errorf("expected label=value to be merged, got:\n%s", got)
	}
}
