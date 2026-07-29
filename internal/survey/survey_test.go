package survey

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

func TestRun_badFormat(t *testing.T) {
	var out, warn bytes.Buffer
	code, err := Run(&out, &warn, []string{t.TempDir()}, Options{Format: "xml"})
	if code != 2 {
		t.Errorf("got exit code %d, want 2 (SPECIFICATION.md §10.1)", code)
	}
	if err == nil {
		t.Error("expected an error for an unrecognised format")
	}
}

func TestRun_noTargetsFound(t *testing.T) {
	var out, warn bytes.Buffer
	code, err := Run(&out, &warn, []string{t.TempDir()}, Options{Format: "tree"})
	// When no Compose files or warnings exist, exit code 4 is returned
	// before any output is produced (no tree, no summary).
	if code != 4 {
		t.Errorf("got exit code %d, want 4 (SPECIFICATION.md §10.1)", code)
	}
	if err == nil {
		t.Error("expected an error when no Compose files are found")
	}
}

// TestRun_serviceWithNoLabelsDoesNotAbortScan mirrors the original
// tests/unit/survey_crash_regression.bats: a service with no labels at
// all, or a project with a real per-file error, must never stop the scan
// partway through — every project passed in still gets its own report.
// See DECISIONS.md Decisions 22/24 for what this protects against.
func TestRun_serviceWithNoLabelsDoesNotAbortScan(t *testing.T) {
	parent := t.TempDir()
	writeFile(t, filepath.Join(parent, "no-labels-app", "compose.yml"), `
services:
  bare:
    image: alpine
`)
	writeFile(t, filepath.Join(parent, "second-app", "compose.yml"), `
services:
  web:
    labels:
      com.example.a: "1"
`)

	var out, warn bytes.Buffer
	code, err := Run(&out, &warn, []string{parent}, Options{Recursive: true, Format: "tree"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("got exit code %d, want 0", code)
	}

	got := out.String()
	if !strings.Contains(got, "no-labels-app") {
		t.Error("expected the no-labels project to still be reported")
	}
	if !strings.Contains(got, "second-app") {
		t.Error("expected the second project to still be reported (scan must not abort)")
	}
	if !strings.Contains(got, "com.example.a=1") {
		t.Error("expected the second project's label to be reported")
	}
}

func TestRun_tree_and_plain_report_identical_information(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), `
services:
  web:
    labels:
      com.example.a: "1"
    label_file:
      - ./web.labels
`)
	writeFile(t, filepath.Join(dir, "web.labels"), "com.example.from.file=yes\n")

	var treeOut, plainOut, warn bytes.Buffer
	if _, err := Run(&treeOut, &warn, []string{dir}, Options{Format: "tree"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(&plainOut, &warn, []string{dir}, Options{Format: "plain"}); err != nil {
		t.Fatal(err)
	}

	// SPECIFICATION.md §7.5.1: both formats must present identical
	// underlying information. We don't compare the raw text (the whole
	// point is the presentation differs) — we check that every piece of
	// real data shows up in both.
	for _, want := range []string{"com.example.a", "1", "web.labels", "com.example.from.file", "yes"} {
		if !strings.Contains(treeOut.String(), want) {
			t.Errorf("tree output missing %q", want)
		}
		if !strings.Contains(plainOut.String(), want) {
			t.Errorf("plain output missing %q", want)
		}
	}
}

func TestRun_footerBreakdown(t *testing.T) {
	parent := t.TempDir()
	writeFile(t, filepath.Join(parent, "app1", "compose.yml"), `
services:
  labelled:
    labels:
      com.example.a: "1"
  bare:
    image: alpine
`)
	writeFile(t, filepath.Join(parent, "app2", "compose.yml"), `
services:
  another:
    labels:
      com.example.b: "1"
`)

	var out, warn bytes.Buffer
	code, err := Run(&out, &warn, []string{parent}, Options{Recursive: true, Format: "tree"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("got exit code %d, want 0", code)
	}

	got := out.String()
	if !strings.Contains(got, "2 projects") && !strings.Contains(got, "3 services") {
		t.Errorf("expected a summary block, got:\n%s", got)
	}
	if !strings.Contains(got, "1 no labels") {
		t.Errorf("expected summary to mention services without labels, got:\n%s", got)
	}
	// The summary must be the LAST thing printed, not the first.
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if !strings.Contains(lines[len(lines)-1], "service") {
		t.Errorf("expected the summary to be the final lines, got last line: %q (full output:\n%s)", lines[len(lines)-1], got)
	}
}

func TestRun_noLabelsPlaceholder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), `
services:
  bare:
    image: alpine
`)

	for _, format := range []string{"tree", "plain"} {
		t.Run(format, func(t *testing.T) {
			var out, warn bytes.Buffer
			if _, err := Run(&out, &warn, []string{dir}, Options{Format: format}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "(no labels)") {
				t.Errorf("expected \"(no labels)\" for a service with nothing, got:\n%s", out.String())
			}
		})
	}
}

func TestRun_treeFormatSeparatesProjectsWithBlankLine(t *testing.T) {
	// When two paths are passed (non-recursive), each path becomes its
	// own root tree and they are separated by a blank line. With a
	// single --recursive parent, all projects are siblings under one
	// root and have no blank line between them — this test tests both
	// scenarios.

	// Scenario 1: single --recursive parent → siblings, no blank line.
	parent := t.TempDir()
	writeFile(t, filepath.Join(parent, "aaa-app", "compose.yml"), "services:\n  svc:\n    image: alpine\n")
	writeFile(t, filepath.Join(parent, "bbb-app", "compose.yml"), "services:\n  svc:\n    image: alpine\n")

	var out, warn bytes.Buffer
	if _, err := Run(&out, &warn, []string{parent}, Options{Recursive: true, Format: "tree"}); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	if !strings.Contains(got, "[aaa-app]") || !strings.Contains(got, "[bbb-app]") {
		t.Fatalf("expected both project names present in bracket notation, got:\n%s", got)
	}
	// Under a single --recursive root, aaa-app and bbb-app are
	// siblings — no blank line between their project entries.
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	aaaLine := -1
	bbbLine := -1
	for i, l := range lines {
		if strings.Contains(l, "[aaa-app]") {
			aaaLine = i
		}
		if strings.Contains(l, "[bbb-app]") {
			bbbLine = i
		}
	}
	if aaaLine < 0 || bbbLine < 0 {
		t.Fatalf("could not locate both project lines, got:\n%s", got)
	}
	// bbb-app's subtree (4 lines) follows aaa-app's bracket node;
	// lines between: [├──: compose.yml, │   └── svc, │       └── (no labels)]
	// → 4 lines apart.
	if bbbLine != aaaLine+5 {
		t.Errorf("expected [bbb-app] 5 lines after [aaa-app] in single-root mode (blank separator between siblings), got offset %d:\n%s", bbbLine-aaaLine, got)
	}
	// There should only be one occurrence of each project bracket
	// (they're not duplicated under separate root trees).
	if strings.Count(got, "[aaa-app]") != 1 || strings.Count(got, "[bbb-app]") != 1 {
		t.Errorf("expected each project to appear exactly once, got:\n%s", got)
	}

	// Scenario 2: two separate non-recursive paths → two root trees,
	// blank line between them.
	aaaDir := filepath.Join(parent, "aaa-app")
	bbbDir := filepath.Join(parent, "bbb-app")

	var out2, warn2 bytes.Buffer
	if _, err := Run(&out2, &warn2, []string{aaaDir, bbbDir}, Options{Format: "tree"}); err != nil {
		t.Fatal(err)
	}

	got2 := out2.String()
	if !strings.Contains(got2, "[aaa-app]") || !strings.Contains(got2, "[bbb-app]") {
		t.Fatalf("expected both project names in two-path mode, got:\n%s", got2)
	}
	// The second project must be preceded by a blank line (separate
	// root trees).
	lines2 := strings.Split(strings.TrimRight(got2, "\n"), "\n")
	aaaLine2 := -1
	bbbLine2 := -1
	for i, l := range lines2 {
		if strings.Contains(l, "[aaa-app]") {
			aaaLine2 = i
		}
		if strings.Contains(l, "[bbb-app]") {
			bbbLine2 = i
		}
	}
	if aaaLine2 < 0 || bbbLine2 < 0 {
		t.Fatalf("could not locate both project lines in two-path mode, got:\n%s", got2)
	}
	// In two-path mode: root line 0 is aaa-app bracket, then subtree
	// (3 lines), then blank separator (line 4), then bbb-app bracket
	// (line 5) → 5 lines apart.
	if bbbLine2 != aaaLine2+5 {
		t.Errorf("expected [bbb-app] 5 lines after [aaa-app] in two-path mode (due to blank line separator), got offset %d:\n%s", bbbLine2-aaaLine2, got2)
	}
}

func TestRun_labelFileConflictReportedNotResolved(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), `
services:
  web:
    label_file:
      - ./a.labels
      - ./b.labels
`)
	writeFile(t, filepath.Join(dir, "a.labels"), "shared.key=from-a\n")
	writeFile(t, filepath.Join(dir, "b.labels"), "shared.key=from-b\n")

	var out, warn bytes.Buffer
	if _, err := Run(&out, &warn, []string{dir}, Options{Format: "plain"}); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	// Both conflicting values must be visible — survey never picks one
	// (DECISIONS.md Decision 35) — and it must not have needed to read
	// stdin or invoke any conflict-resolution logic to produce this.
	if !strings.Contains(got, "from-a") || !strings.Contains(got, "from-b") {
		t.Errorf("expected both conflicting values reported, got: %s", got)
	}
}

func TestRun_emptyOverrideFileIsNotAnError(t *testing.T) {
	// Regression test for a real bug found testing against real compose
	// projects: an empty (or comment-only) override file — a completely
	// normal placeholder state — was being reported as a parse error
	// instead of "no services here".
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), `
services:
  zerotier-one:
    labels:
      diun.enable: "true"
`)
	writeFile(t, filepath.Join(dir, "compose.override.yml"), "# nothing here yet\n")

	var out, warn bytes.Buffer
	code, err := Run(&out, &warn, []string{dir}, Options{Format: "tree"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("got exit code %d, want 0", code)
	}
	if strings.Contains(out.String(), "ERROR") {
		t.Errorf("empty override file must not be reported as an error, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "diun.enable=true") {
		t.Errorf("expected base file's label still reported, got: %s", out.String())
	}
}

func TestRun_missingLabelFileReported(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), `
services:
  web:
    label_file:
      - ./does-not-exist.labels
`)

	var out, warn bytes.Buffer
	if _, err := Run(&out, &warn, []string{dir}, Options{Format: "tree"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "MISSING") {
		t.Errorf("expected missing label_file to be flagged, got: %s", out.String())
	}
}

func TestRun_plainFormat_baseFolderHeader(t *testing.T) {
	parent := t.TempDir()
	writeFile(t, filepath.Join(parent, "myapp", "compose.yml"), `
services:
  web:
    labels:
      diun.enable: "true"
`)

	var out, warn bytes.Buffer
	if _, err := Run(&out, &warn, []string{parent}, Options{Recursive: true, Format: "plain"}); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	if !strings.HasPrefix(got, "Base Folder: "+parent) {
		t.Errorf("expected output to start with 'Base Folder: %s', got:\n%s", parent, got)
	}
}

func TestRun_plainFormat_inlineAndFileTags(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), `
services:
  web:
    labels:
      diun.enable: "true"
    label_file:
      - ./web.labels
  bare:
    image: alpine
`)
	writeFile(t, filepath.Join(dir, "web.labels"), "diun.metadata.app=web\n")

	var out, warn bytes.Buffer
	if _, err := Run(&out, &warn, []string{dir}, Options{Format: "plain"}); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	for _, want := range []string{
		"[inline]",
		"diun.enable=true",
		"[file: ./web.labels]",
		"diun.metadata.app=web",
		"(no labels)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("plain output missing %q, got:\n%s", want, got)
		}
	}
}

func TestRun_plainFormat_summaryLine(t *testing.T) {
	parent := t.TempDir()
	writeFile(t, filepath.Join(parent, "app1", "compose.yml"), `
services:
  labelled:
    labels:
      com.example.a: "1"
      com.example.b: "2"
  bare:
    image: alpine
`)
	writeFile(t, filepath.Join(parent, "app2", "compose.yml"), `
services:
  another:
    labels:
      com.example.c: "3"
    label_file:
      - ./another.labels
`)
	writeFile(t, filepath.Join(parent, "app2", "another.labels"), "com.example.d=4\n")

	var out, warn bytes.Buffer
	if _, err := Run(&out, &warn, []string{parent}, Options{Recursive: true, Format: "plain"}); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	// 2 projects, 3 services; 3 inline + 1 file = 4 total labels, 1 file, 1 no-labels service
	for _, want := range []string{
		"Summary:",
		"2 projects",
		"3 services",
		"4 total",
		"3 inline",
		"1 in 1 files",
		"1 services no labels",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("plain summary missing %q, got:\n%s", want, got)
		}
	}
}

func TestRun_plainFormat_overrideFileRendered(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), `
services:
  web:
    labels:
      diun.enable: "true"
`)
	writeFile(t, filepath.Join(dir, "compose.override.yml"), `
services:
  web:
    labels:
      diun.metadata.app: "web"
`)

	var out, warn bytes.Buffer
	if _, err := Run(&out, &warn, []string{dir}, Options{Format: "plain"}); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	// Both compose files should appear
	if !strings.Contains(got, "compose.yml") {
		t.Errorf("expected base compose file in output, got:\n%s", got)
	}
	if !strings.Contains(got, "compose.override.yml") {
		t.Errorf("expected override compose file in output, got:\n%s", got)
	}
	// Both labels should appear
	if !strings.Contains(got, "diun.enable=true") {
		t.Errorf("expected base label in output, got:\n%s", got)
	}
	if !strings.Contains(got, "diun.metadata.app=web") {
		t.Errorf("expected override label in output, got:\n%s", got)
	}
}

func TestRun_plainFormat_noSummary(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), `
services:
  web:
    labels:
      diun.enable: "true"
`)

	var out, warn bytes.Buffer
	if _, err := Run(&out, &warn, []string{dir}, Options{Format: "plain", NoSummary: true}); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	if strings.Contains(got, "Summary:") {
		t.Errorf("expected no summary with NoSummary=true, got:\n%s", got)
	}
}
