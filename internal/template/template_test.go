package template

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prod"), []byte("ENV=$APP_NAME-prod\nOWNER=$SERVICE_NAME\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	labels, err := Load(dir, "prod")
	if err != nil {
		t.Fatal(err)
	}
	want := []Label{{"ENV", "$APP_NAME-prod"}, {"OWNER", "$SERVICE_NAME"}}
	if len(labels) != len(want) {
		t.Fatalf("Load = %v, want %v", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Errorf("Load[%d] = %v, want %v", i, labels[i], want[i])
		}
	}
}

func TestLoad_missingTemplateIsAnError(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir, "nonexistent"); err == nil {
		t.Error("expected an error for a template that doesn't exist")
	}
}

func TestExpand(t *testing.T) {
	vars := Vars{ServiceName: "web", AppDir: "/opt/apps/myapp", AppName: "myapp"}

	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"exact whole-token SERVICE_NAME", "$SERVICE_NAME", "web"},
		{"exact whole-token APP_DIR", "$APP_DIR", "/opt/apps/myapp"},
		{"exact whole-token APP_NAME", "$APP_NAME", "myapp"},
		{"embedded in a larger string", "svc=$SERVICE_NAME;dir=$APP_DIR", "svc=web;dir=/opt/apps/myapp"},
		{"double-dollar escape produces a literal $ then literal text", "$$SERVICE_NAME", "$SERVICE_NAME"},
		{"prefix match is not substituted (whole-token rule)", "$SERVICE_NAME_EXTRA", "$SERVICE_NAME_EXTRA"},
		{"unrecognised variable left unchanged", "$UNKNOWN_VAR", "$UNKNOWN_VAR"},
		{"bare trailing dollar left unchanged", "price is $", "price is $"},
		{"no variables at all", "plain-value", "plain-value"},
		{"two escaped dollars back to back", "$$$$SERVICE_NAME", "$$SERVICE_NAME"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Expand(tc.value, vars)
			if got != tc.want {
				t.Errorf("Expand(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}
