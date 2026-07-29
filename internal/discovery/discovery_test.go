package discovery

import (
	"os"
	"path/filepath"
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

func TestIsRecognisedFilename(t *testing.T) {
	cases := map[string]bool{
		"compose.yml":                 true,
		"compose.yaml":                true,
		"docker-compose.yml":          true,
		"docker-compose.yaml":         true,
		"docker-compose.override.yml": false, // override sibling is never itself a recognised target, Decision 23
		"random.yml":                  false,
		"":                            false,
	}
	for name, want := range cases {
		if got := IsRecognisedFilename(name); got != want {
			t.Errorf("IsRecognisedFilename(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestDiscoverComposeFiles(t *testing.T) {
	t.Run("none present", func(t *testing.T) {
		dir := t.TempDir()
		_, found, warn := DiscoverComposeFiles(dir)
		if found || warn != nil {
			t.Fatalf("got found=%v warn=%v, want found=false warn=nil", found, warn)
		}
	})

	t.Run("exactly one present", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "compose.yml"), "services: {}\n")
		file, found, warn := DiscoverComposeFiles(dir)
		if !found || warn != nil {
			t.Fatalf("got found=%v warn=%v, want found=true warn=nil", found, warn)
		}
		if want := filepath.Join(dir, "compose.yml"); file != want {
			t.Errorf("got file=%q, want %q", file, want)
		}
	})

	t.Run("multiple present is ambiguous (§6.4)", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "compose.yml"), "services: {}\n")
		writeFile(t, filepath.Join(dir, "docker-compose.yml"), "services: {}\n")
		_, found, warn := DiscoverComposeFiles(dir)
		if found {
			t.Fatal("expected found=false when multiple Compose files are present")
		}
		if warn == nil {
			t.Fatal("expected a warning when multiple Compose files are present")
		}
	})
}

func TestFindOverrideFile(t *testing.T) {
	t.Run("override present, matching naming style", func(t *testing.T) {
		dir := t.TempDir()
		base := filepath.Join(dir, "docker-compose.yml")
		writeFile(t, base, "services: {}\n")
		writeFile(t, filepath.Join(dir, "docker-compose.override.yml"), "services: {}\n")

		path, found := FindOverrideFile(base)
		if !found {
			t.Fatal("expected override file to be found")
		}
		if want := filepath.Join(dir, "docker-compose.override.yml"); path != want {
			t.Errorf("got %q, want %q", path, want)
		}
	})

	t.Run("no override present", func(t *testing.T) {
		dir := t.TempDir()
		base := filepath.Join(dir, "compose.yaml")
		writeFile(t, base, "services: {}\n")
		if _, found := FindOverrideFile(base); found {
			t.Fatal("expected no override file to be found")
		}
	})

	t.Run("compose.yml naming style looks for compose.override.yml, not docker-compose variant", func(t *testing.T) {
		dir := t.TempDir()
		base := filepath.Join(dir, "compose.yml")
		writeFile(t, base, "services: {}\n")
		// Wrong-style override present — must NOT be picked up.
		writeFile(t, filepath.Join(dir, "docker-compose.override.yml"), "services: {}\n")
		if _, found := FindOverrideFile(base); found {
			t.Fatal("expected no match: override naming style must match the base file's own style")
		}
	})
}

func TestResolveLabelFileRef(t *testing.T) {
	cases := []struct {
		name, composeFile, ref, want string
	}{
		{"relative", "/projects/db/compose.yml", "./db.labels", "/projects/db/db.labels"},
		{"relative no dot-slash", "/projects/db/compose.yml", "db.labels", "/projects/db/db.labels"},
		{"absolute passthrough", "/projects/db/compose.yml", "/etc/labels/db.labels", "/etc/labels/db.labels"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveLabelFileRef(c.composeFile, c.ref)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestDiscoverApplications_oneLevelOnly(t *testing.T) {
	parent := t.TempDir()
	writeFile(t, filepath.Join(parent, "app1", "compose.yml"), "services: {}\n")
	writeFile(t, filepath.Join(parent, "app2", "docker-compose.yaml"), "services: {}\n")
	// Nested app must NOT be discovered — §6.5, one level only.
	writeFile(t, filepath.Join(parent, "app1", "nested", "compose.yml"), "services: {}\n")
	// A subdirectory with no Compose file at all is simply skipped, no warning.
	if err := os.MkdirAll(filepath.Join(parent, "not-an-app"), 0o755); err != nil {
		t.Fatal(err)
	}

	files, warnings, err := DiscoverApplications(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2: %v", len(files), files)
	}
}

func TestResolveTargets(t *testing.T) {
	t.Run("explicit file, recognised", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "compose.yml")
		writeFile(t, f, "services: {}\n")
		targets, warnings, err := ResolveTargets(false, []string{f})
		if err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 0 || len(targets) != 1 || targets[0] != f {
			t.Fatalf("got targets=%v warnings=%v", targets, warnings)
		}
	})

	t.Run("explicit file, not recognised", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "random.yml")
		writeFile(t, f, "services: {}\n")
		targets, warnings, err := ResolveTargets(false, []string{f})
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 0 || len(warnings) != 1 {
			t.Fatalf("got targets=%v warnings=%v, want 0 targets and 1 warning", targets, warnings)
		}
	})

	t.Run("path not found", func(t *testing.T) {
		targets, warnings, err := ResolveTargets(false, []string{"/does/not/exist"})
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 0 || len(warnings) != 1 {
			t.Fatalf("got targets=%v warnings=%v, want 0 targets and 1 warning", targets, warnings)
		}
	})

	t.Run("directory, non-recursive", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "compose.yml"), "services: {}\n")
		targets, _, err := ResolveTargets(false, []string{dir})
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 1 {
			t.Fatalf("got %d targets, want 1", len(targets))
		}
	})

	t.Run("directory, recursive scans immediate children only", func(t *testing.T) {
		parent := t.TempDir()
		writeFile(t, filepath.Join(parent, "app1", "compose.yml"), "services: {}\n")
		targets, _, err := ResolveTargets(true, []string{parent})
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 1 {
			t.Fatalf("got %d targets, want 1", len(targets))
		}
	})
}
