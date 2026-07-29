// Package composevalidate wraps `docker compose config` as the single validation call site (DECISIONS.md Decision 20, Decision 30 - deliberately not behind an interface). Informed by src/lib/file_ops.sh's lbl_validate_compose, but designed fresh in Go (DECISIONS.md Decision 34) - not yet built.
package composevalidate
