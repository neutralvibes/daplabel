package configcmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/neutralvibes/daplabel/internal/config"
)

func TestRender_showsEveryValueAndItsSource(t *testing.T) {
	cfg := &config.Config{
		ParentDir:           config.Value{Value: ".", Source: config.SourceDefault},
		TemplateDir:         config.Value{Value: "/tmp/templates", Source: config.SourceEnv},
		Editor:              config.Value{Value: "vim", Source: config.SourceUserFile},
		ListSafe:            config.Value{Value: "true", Source: config.SourceCLI},
		DefaultSurveyFormat: config.Value{Value: "plain", Source: config.SourceDefault},
	}

	var out bytes.Buffer
	Render(&out, cfg, nil)
	got := out.String()

	for _, want := range []string{
		"DAPLABEL_PARENT_DIR", "DAPLABEL_TEMPLATE_DIR", "DAPLABEL_EDITOR", "DAPLABEL_LIST_SAFE", "DAPLABEL_DEFAULT_SURVEY_FORMAT",
		"/tmp/templates", "vim", "true", "plain",
		"default", "environment variable", "user configuration file", "command line",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestRender_defaultSurveyFormat_validValueNoError(t *testing.T) {
	cfg := &config.Config{
		ParentDir:           config.Value{Value: ".", Source: config.SourceDefault},
		DefaultSurveyFormat: config.Value{Value: "tree", Source: config.SourceDefault},
	}
	var out bytes.Buffer
	Render(&out, cfg, nil)
	got := out.String()
	if strings.Contains(got, "ERROR") {
		t.Errorf("valid format 'tree' must not produce an ERROR annotation, got:\n%s", got)
	}
}

func TestRender_defaultSurveyFormat_emptyShownAsEmptyNoError(t *testing.T) {
	cfg := &config.Config{
		ParentDir:           config.Value{Value: ".", Source: config.SourceDefault},
		DefaultSurveyFormat: config.Value{Value: "", Source: config.SourceDefault},
	}
	var out bytes.Buffer
	Render(&out, cfg, nil)
	got := out.String()
	if !strings.Contains(got, "(empty)") {
		t.Errorf("expected empty value shown as '(empty)', got:\n%s", got)
	}
	if strings.Contains(got, "ERROR") {
		t.Errorf("empty value must not produce an ERROR annotation, got:\n%s", got)
	}
}

func TestRender_defaultSurveyFormat_invalidValueAnnotated(t *testing.T) {
	cfg := &config.Config{
		ParentDir:           config.Value{Value: ".", Source: config.SourceDefault},
		DefaultSurveyFormat: config.Value{Value: "--bubbles", Source: config.SourceUserFile},
	}
	var out bytes.Buffer
	Render(&out, cfg, nil)
	got := out.String()
	if !strings.Contains(got, "--bubbles") {
		t.Errorf("expected the invalid value to be shown, got:\n%s", got)
	}
	if !strings.Contains(got, "ERROR") {
		t.Errorf("expected an ERROR annotation for invalid format, got:\n%s", got)
	}
	if !strings.Contains(got, "see survey --help for valid formats") {
		t.Errorf("expected hint to valid formats, got:\n%s", got)
	}
}

func TestRender_emptyValueShownExplicitly(t *testing.T) {
	cfg := &config.Config{
		ParentDir: config.Value{Value: ".", Source: config.SourceDefault},
	}
	var out bytes.Buffer
	Render(&out, cfg, nil)
	if !strings.Contains(out.String(), "(empty)") {
		t.Errorf("expected an explicit marker for empty values, got:\n%s", out.String())
	}
}

func TestRender_isPlainTextNotMarkdown(t *testing.T) {
	cfg := &config.Config{ParentDir: config.Value{Value: ".", Source: config.SourceDefault}}
	var out bytes.Buffer
	Render(&out, cfg, []config.FileSource{
		{Tier: config.SourceSystemFile, Path: "/etc/daplabel/config", Exists: false},
	})
	got := out.String()
	for _, marker := range []string{"**", "| ", " |"} {
		if strings.Contains(got, marker) {
			t.Errorf("output contains markdown-like marker %q, want plain text:\n%s", marker, got)
		}
	}
}

func TestRender_showsSourceFileStatus(t *testing.T) {
	cfg := &config.Config{ParentDir: config.Value{Value: ".", Source: config.SourceDefault}}
	sources := []config.FileSource{
		{Tier: config.SourceSystemFile, Path: "/etc/daplabel/config", Exists: false, Used: false},
		{Tier: config.SourceUserFile, Path: "/home/user/.config/daplabel/config", Exists: true, Used: true},
	}
	var out bytes.Buffer
	Render(&out, cfg, sources)
	got := out.String()

	if !strings.Contains(got, "/etc/daplabel/config") || !strings.Contains(got, "not found") {
		t.Errorf("expected the system file's not-found status, got:\n%s", got)
	}
	if !strings.Contains(got, "/home/user/.config/daplabel/config") || !strings.Contains(got, "found, used") {
		t.Errorf("expected the user file's found/used status, got:\n%s", got)
	}
	if !strings.Contains(got, "system file") || !strings.Contains(got, "user file") {
		t.Errorf("expected tier labels for both sources, got:\n%s", got)
	}
}

func TestRender_noSourcesOmitsSourcesSection(t *testing.T) {
	cfg := &config.Config{ParentDir: config.Value{Value: ".", Source: config.SourceDefault}}
	var out bytes.Buffer
	Render(&out, cfg, nil)
	if strings.Contains(out.String(), "sources checked") {
		t.Errorf("expected no sources section when sources is nil, got:\n%s", out.String())
	}
}
