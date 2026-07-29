package survey

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFilterArg(t *testing.T) {
	cases := []struct {
		arg  string
		want KeyValueFilter
	}{
		{"diun.enable", KeyValueFilter{Key: "diun.enable", HasValue: false}},
		{"diun.enable=true", KeyValueFilter{Key: "diun.enable", Value: "true", HasValue: true}},
		{"diun.enable=", KeyValueFilter{Key: "diun.enable", Value: "", HasValue: true}},
	}
	for _, c := range cases {
		if got := ParseFilterArg(c.arg); got != c.want {
			t.Errorf("ParseFilterArg(%q) = %+v, want %+v", c.arg, got, c.want)
		}
	}
}

func TestRun_filterByKeyOnly(t *testing.T) {
	parent := t.TempDir()
	writeFile(t, filepath.Join(parent, "has-it", "compose.yml"), `
services:
  web:
    labels:
      diun.enable: "true"
`)
	writeFile(t, filepath.Join(parent, "lacks-it", "compose.yml"), `
services:
  web:
    image: alpine
`)

	var out, warn bytes.Buffer
	_, err := Run(&out, &warn, []string{parent}, Options{
		Recursive: true,
		Format:    "plain",
		Filter:    []KeyValueFilter{{Key: "diun.enable"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "has-it") {
		t.Error("expected the matching project to be shown")
	}
	if strings.Contains(got, "lacks-it") {
		t.Error("expected the non-matching project to be omitted entirely")
	}
	if !strings.Contains(got, "1 project") && !strings.Contains(got, "1 service") {
		t.Errorf("expected summary to reflect only the matched project, got: %s", got)
	}
}

func TestRun_filterByKeyAndValue(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), `
services:
  matches:
    labels:
      diun.enable: "true"
  wrong-value:
    labels:
      diun.enable: "false"
`)

	var out, warn bytes.Buffer
	_, err := Run(&out, &warn, []string{dir}, Options{
		Format: "plain",
		Filter: []KeyValueFilter{{Key: "diun.enable", Value: "true", HasValue: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "matches") {
		t.Error("expected the exact-value match to be shown")
	}
	if strings.Contains(got, "wrong-value") {
		t.Error("expected the different-value service to be excluded")
	}
}

// TestRun_filterMatchesQuotedAndUnquotedValuesIdentically verifies the
// specific concern raised when filtering was requested: a value written
// as `"true"` (a quoted YAML string) and one written as `true` (an
// unquoted YAML boolean literal) must match the same --filter value.
// Both are read via yaml.v3's Node.Value, which holds the literal scalar
// text for either form identically — this test proves that rather than
// assuming it.
func TestRun_filterMatchesQuotedAndUnquotedValuesIdentically(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), `
services:
  quoted:
    labels:
      diun.enable: "true"
  unquoted:
    labels:
      diun.enable: true
`)

	var out, warn bytes.Buffer
	_, err := Run(&out, &warn, []string{dir}, Options{
		Format: "plain",
		Filter: []KeyValueFilter{{Key: "diun.enable", Value: "true", HasValue: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "quoted") || !strings.Contains(got, "unquoted") {
		t.Errorf("expected both quoted and unquoted forms to match --filter diun.enable=true, got: %s", got)
	}
}

func TestRun_missing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), `
services:
  has-label:
    labels:
      diun.enable: "true"
  no-label:
    image: alpine
`)

	var out, warn bytes.Buffer
	_, err := Run(&out, &warn, []string{dir}, Options{
		Format:  "plain",
		Missing: []string{"diun.enable"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "has-label") {
		t.Error("expected the service that HAS the key to be excluded by --missing")
	}
	if !strings.Contains(got, "no-label") {
		t.Error("expected the service that lacks the key to be shown")
	}
}

func TestRun_missingKeyFoundOnlyInLabelFileStillExcludes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), `
services:
  web:
    label_file:
      - ./web.labels
`)
	writeFile(t, filepath.Join(dir, "web.labels"), "diun.enable=true\n")

	var out, warn bytes.Buffer
	_, err := Run(&out, &warn, []string{dir}, Options{
		Format:  "plain",
		Missing: []string{"diun.enable"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Service: web") {
		t.Error("expected a key found via label_file (not just inline) to still count for --missing")
	}
}

func TestRun_noMatchesOmitsProjectAndDoesNotCountIt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: alpine\n")

	var out, warn bytes.Buffer
	code, err := Run(&out, &warn, []string{dir}, Options{
		Format: "plain",
		Filter: []KeyValueFilter{{Key: "nothing.matches.this"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Errorf("got exit code %d, want 0 (zero matches is not itself an error)", code)
	}
	got := out.String()
	if !strings.Contains(got, "0 projects") || !strings.Contains(got, "0 services") {
		t.Errorf("expected the summary to reflect zero matches, got: %s", got)
	}
}
