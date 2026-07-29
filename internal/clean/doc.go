// Package clean implements the `clean` command: removing .pre-label backup
// files created by other write-path commands (SPECIFICATION.md §8.4).
//
// It is a deletion-only operation: it never reads or modifies Docker Compose
// files, label files, or YAML structure. It only removes backup files in
// directories that contain recognised Compose files.
package clean
