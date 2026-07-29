package composevalidate

import (
	"bytes"
	"fmt"
	"os/exec"
)

// ComposeConfig is the single call site (DECISIONS.md Decision 20) for
// confirming a Compose file is valid, per Decision 15's "Docker Compose
// itself is the validation authority." It shells out directly to `docker
// compose -f path config --quiet`, with no interface or test double
// around it — Decision 30 judged that unnecessary complexity given that
// `docker compose` is already a mandatory runtime dependency of the tool
// (Decision 11/15), in exchange for CI running on a Docker-capable
// runner.
//
// Every write-path command (`add`, `remove`, and eventually `generate`,
// `template apply`) must call this — and only this — function to check
// Compose validity before committing a change (internal/atomicwrite's
// Set.Commit takes it directly as its validate callback); no other
// function may invoke `docker compose ... config` redundantly (Decision
// 20's consequences: a second call site was found and removed once
// already, in the Bash implementation).
func ComposeConfig(path string) error {
	cmd := exec.Command("docker", "compose", "-f", path, "config", "--quiet")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if msg == "" {
			msg = err.Error()
		}
		if _, ok := err.(*exec.Error); ok {
			return fmt.Errorf("docker compose is required to validate Compose files but could not be run: %w", err)
		}
		return fmt.Errorf("docker compose config rejected %s: %s", path, msg)
	}
	return nil
}
