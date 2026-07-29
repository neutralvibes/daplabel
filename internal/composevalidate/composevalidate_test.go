package composevalidate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// dockerComposeAvailable reports whether a real `docker compose` is
// callable in this environment. Per DECISIONS.md Decision 30, exercising
// ComposeConfig's actual validation behaviour requires a Docker-capable
// runner (CI is required to provide one); this sandbox may not be one,
// so tests that need real validation are skipped rather than faked —
// consistent with Decision 30's deliberate choice not to introduce a
// test double here.
func dockerComposeAvailable() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	cmd := exec.Command("docker", "compose", "version")
	return cmd.Run() == nil
}

func TestComposeConfig_validFile(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(path, []byte("services:\n  web:\n    image: alpine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ComposeConfig(path); err != nil {
		t.Errorf("ComposeConfig on a valid file: %v", err)
	}
}

func TestComposeConfig_invalidFileIsRejected(t *testing.T) {
	if !dockerComposeAvailable() {
		t.Skip("docker compose not available in this environment (Decision 30 requires a Docker-capable runner for this test)")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	// Missing the required top-level structure entirely.
	if err := os.WriteFile(path, []byte("this is not: [valid, compose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ComposeConfig(path)
	if err == nil {
		t.Fatal("expected ComposeConfig to reject a malformed Compose file, got nil error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the rejected file", err)
	}
}

func TestComposeConfig_dockerNotRunnableProducesAClearError(t *testing.T) {
	if dockerComposeAvailable() {
		t.Skip("this test exercises the 'docker is not available at all' branch, which needs an environment without a working docker compose")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(path, []byte("services:\n  web:\n    image: alpine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ComposeConfig(path)
	if err == nil {
		t.Fatal("expected an error when docker compose cannot be run, got nil")
	}
	if !strings.Contains(err.Error(), "docker compose") {
		t.Errorf("error %q doesn't explain that docker compose is required", err)
	}
}
