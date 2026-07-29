package yamlbackend

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCompose(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGetServices(t *testing.T) {
	t.Run("returns names sorted alphabetically", func(t *testing.T) {
		path := writeCompose(t, `
services:
  web:
    image: nginx
  api:
    image: myapi
  db:
    image: postgres
`)
		got, err := GetServices(path)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"api", "db", "web"}
		if !equalStrings(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("no services section is empty, not an error", func(t *testing.T) {
		path := writeCompose(t, "version: \"3\"\n")
		got, err := GetServices(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})

	t.Run("empty or comment-only file is empty, not an error", func(t *testing.T) {
		// A real case: a placeholder override file with nothing active
		// in it yet is well-formed YAML with no content — not malformed
		// input.
		path := writeCompose(t, "# nothing here yet\n")
		got, err := GetServices(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})

	t.Run("malformed YAML is an error", func(t *testing.T) {
		path := writeCompose(t, "services: [this is not a mapping\n")
		if _, err := GetServices(path); err == nil {
			t.Fatal("expected an error for malformed YAML")
		}
	})
}

func TestGetLabels(t *testing.T) {
	t.Run("preserves document order, does not sort", func(t *testing.T) {
		path := writeCompose(t, `
services:
  web:
    labels:
      zeta: "1"
      alpha: "2"
      mu: "3"
`)
		got, err := GetLabels(path, "web")
		if err != nil {
			t.Fatal(err)
		}
		want := []Label{{"zeta", "1"}, {"alpha", "2"}, {"mu", "3"}}
		if !equalLabels(got, want) {
			t.Errorf("got %v, want %v (order must match document, not be sorted)", got, want)
		}
	})

	t.Run("service with no labels is empty, not an error", func(t *testing.T) {
		path := writeCompose(t, "services:\n  web:\n    image: nginx\n")
		got, err := GetLabels(path, "web")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})

	t.Run("nonexistent service is empty, not an error", func(t *testing.T) {
		path := writeCompose(t, "services:\n  web:\n    image: nginx\n")
		got, err := GetLabels(path, "does-not-exist")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})

	t.Run("list-style labels are parsed, same as mapping-style", func(t *testing.T) {
		path := writeCompose(t, `
services:
  web:
    labels:
      - "com.example.zeta=1"
      - "com.example.alpha=2"
      - "com.example.novalue"
`)
		got, err := GetLabels(path, "web")
		if err != nil {
			t.Fatal(err)
		}
		want := []Label{
			{"com.example.zeta", "1"},
			{"com.example.alpha", "2"},
			{"com.example.novalue", ""},
		}
		if !equalLabels(got, want) {
			t.Errorf("got %v, want %v (list order preserved, not sorted)", got, want)
		}
	})

	t.Run("labels: null is empty, not an error", func(t *testing.T) {
		path := writeCompose(t, "services:\n  web:\n    labels: null\n")
		got, err := GetLabels(path, "web")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})
}

func TestGetLabelFileRefs(t *testing.T) {
	path := writeCompose(t, `
services:
  web:
    label_file:
      - ./web.labels
      - ../shared/common.labels
`)
	got, err := GetLabelFileRefs(path, "web")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"./web.labels", "../shared/common.labels"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalLabels(a, b []Label) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
