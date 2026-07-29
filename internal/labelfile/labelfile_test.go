package labelfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRead(t *testing.T) {
	t.Run("existing file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "web.labels")
		if err := os.WriteFile(path, []byte("com.example.a=1\n\ncom.example.b=2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		labels, exists, err := Read(path)
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatal("expected exists=true")
		}
		want := []Label{{"com.example.a", "1"}, {"com.example.b", "2"}}
		if len(labels) != len(want) || labels[0] != want[0] || labels[1] != want[1] {
			t.Errorf("got %v, want %v", labels, want)
		}
	})

	t.Run("missing file is not an error", func(t *testing.T) {
		labels, exists, err := Read(filepath.Join(t.TempDir(), "does-not-exist.labels"))
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatal("expected exists=false")
		}
		if labels != nil {
			t.Errorf("got %v, want nil", labels)
		}
	})
}

func TestDetectConflicts(t *testing.T) {
	t.Run("key in exactly one file is not a conflict", func(t *testing.T) {
		files := []FileLabels{
			{Path: "a.labels", Exists: true, Labels: []Label{{"only.in.a", "1"}}},
			{Path: "b.labels", Exists: true, Labels: []Label{{"only.in.b", "2"}}},
		}
		if got := DetectConflicts(files); len(got) != 0 {
			t.Errorf("got %v, want no conflicts", got)
		}
	})

	t.Run("key in multiple files is reported, not resolved", func(t *testing.T) {
		files := []FileLabels{
			{Path: "a.labels", Exists: true, Labels: []Label{{"shared", "from-a"}}},
			{Path: "b.labels", Exists: true, Labels: []Label{{"shared", "from-b"}}},
		}
		got := DetectConflicts(files)
		if len(got) != 1 {
			t.Fatalf("got %d conflicts, want 1: %v", len(got), got)
		}
		if got[0].Key != "shared" || len(got[0].Occurrences) != 2 {
			t.Fatalf("got %+v", got[0])
		}
		if got[0].Occurrences[0].Value != "from-a" || got[0].Occurrences[1].Value != "from-b" {
			t.Errorf("occurrences not in file order: %+v", got[0].Occurrences)
		}
	})

	t.Run("a missing file contributes nothing", func(t *testing.T) {
		files := []FileLabels{
			{Path: "a.labels", Exists: false},
			{Path: "b.labels", Exists: true, Labels: []Label{{"k", "v"}}},
		}
		if got := DetectConflicts(files); len(got) != 0 {
			t.Errorf("got %v, want no conflicts", got)
		}
	})
}

func TestMergeLabelFiles(t *testing.T) {
	t.Run("no conflicts returns all keys", func(t *testing.T) {
		files := []FileLabels{
			{Path: "a.labels", Exists: true, Labels: []Label{{"a.key", "1"}}},
			{Path: "b.labels", Exists: true, Labels: []Label{{"b.key", "2"}}},
		}
		merged, conflicts := MergeLabelFiles(files, "skip")
		if len(conflicts) != 0 {
			t.Errorf("got %d conflicts, want 0", len(conflicts))
		}
		if len(merged) != 2 {
			t.Fatalf("got %d merged, want 2", len(merged))
		}
		if merged[0].Key != "a.key" || merged[1].Key != "b.key" {
			t.Errorf("got %v, want [a.key b.key]", merged)
		}
	})

	t.Run("same value in multiple files is not a conflict", func(t *testing.T) {
		files := []FileLabels{
			{Path: "a.labels", Exists: true, Labels: []Label{{"shared", "same"}}},
			{Path: "b.labels", Exists: true, Labels: []Label{{"shared", "same"}}},
		}
		merged, conflicts := MergeLabelFiles(files, "skip")
		if len(conflicts) != 0 {
			t.Errorf("got %d conflicts, want 0", len(conflicts))
		}
		if len(merged) != 1 || merged[0].Value != "same" {
			t.Errorf("got %v, want [{shared same}]", merged)
		}
	})

	t.Run("skip omits conflicting keys", func(t *testing.T) {
		files := []FileLabels{
			{Path: "a.labels", Exists: true, Labels: []Label{{"conflict", "from-a"}, {"unique", "1"}}},
			{Path: "b.labels", Exists: true, Labels: []Label{{"conflict", "from-b"}}},
		}
		merged, conflicts := MergeLabelFiles(files, "skip")
		if len(conflicts) != 1 || conflicts[0].Key != "conflict" {
			t.Errorf("got %v, want [{conflict ...}]", conflicts)
		}
		if len(merged) != 1 || merged[0].Key != "unique" {
			t.Errorf("got %v, want [{unique 1}]", merged)
		}
	})

	t.Run("first keeps first file's value", func(t *testing.T) {
		files := []FileLabels{
			{Path: "a.labels", Exists: true, Labels: []Label{{"k", "from-a"}}},
			{Path: "b.labels", Exists: true, Labels: []Label{{"k", "from-b"}}},
		}
		merged, conflicts := MergeLabelFiles(files, "first")
		if len(conflicts) != 1 {
			t.Fatalf("got %d conflicts, want 1", len(conflicts))
		}
		if len(merged) != 1 || merged[0].Value != "from-a" {
			t.Errorf("got %v, want [{k from-a}]", merged)
		}
	})

	t.Run("last keeps last file's value", func(t *testing.T) {
		files := []FileLabels{
			{Path: "a.labels", Exists: true, Labels: []Label{{"k", "from-a"}}},
			{Path: "b.labels", Exists: true, Labels: []Label{{"k", "from-b"}}},
		}
		merged, conflicts := MergeLabelFiles(files, "last")
		if len(conflicts) != 1 {
			t.Fatalf("got %d conflicts, want 1", len(conflicts))
		}
		if len(merged) != 1 || merged[0].Value != "from-b" {
			t.Errorf("got %v, want [{k from-b}]", merged)
		}
	})

	t.Run("missing file contributes nothing", func(t *testing.T) {
		files := []FileLabels{
			{Path: "a.labels", Exists: false},
			{Path: "b.labels", Exists: true, Labels: []Label{{"k", "v"}}},
		}
		merged, conflicts := MergeLabelFiles(files, "skip")
		if len(conflicts) != 0 {
			t.Errorf("got %d conflicts, want 0", len(conflicts))
		}
		if len(merged) != 1 || merged[0].Key != "k" {
			t.Errorf("got %v, want [{k v}]", merged)
		}
	})

	t.Run("empty string defaults to skip", func(t *testing.T) {
		files := []FileLabels{
			{Path: "a.labels", Exists: true, Labels: []Label{{"k", "from-a"}}},
			{Path: "b.labels", Exists: true, Labels: []Label{{"k", "from-b"}}},
		}
		merged, conflicts := MergeLabelFiles(files, "")
		if len(conflicts) != 1 {
			t.Fatalf("got %d conflicts, want 1", len(conflicts))
		}
		if len(merged) != 0 {
			t.Errorf("got %v, want []", merged)
		}
	})
}
