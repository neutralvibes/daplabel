package yamlbackend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSetLabelFileRef_createsSequenceWhenNoneExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    image: alpine\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	added, err := SetLabelFileRef(root, "web", "web.labels")
	if err != nil {
		t.Fatal(err)
	}
	if !added {
		t.Fatal("expected added=true for a brand new label_file entry")
	}

	out, err := Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "label_file:") || !strings.Contains(string(out), "web.labels") {
		t.Errorf("expected a label_file entry in output, got:\n%s", out)
	}

	// And it round-trips back through the reader correctly.
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	refs, err := GetLabelFileRefs(path, "web")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0] != "web.labels" {
		t.Errorf("GetLabelFileRefs = %v, want [web.labels]", refs)
	}
}

func TestSetLabelFileRef_appendsToExistingSequence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    label_file:\n      - shared/common.labels\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	added, err := SetLabelFileRef(root, "web", "web.labels")
	if err != nil {
		t.Fatal(err)
	}
	if !added {
		t.Fatal("expected added=true")
	}

	out, _ := Marshal(root)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	refs, err := GetLabelFileRefs(path, "web")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[0] != "shared/common.labels" || refs[1] != "web.labels" {
		t.Errorf("GetLabelFileRefs = %v, want [shared/common.labels web.labels] (existing entry first, unchanged)", refs)
	}
}

func TestSetLabelFileRef_alreadyPresentIsANoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    label_file:\n      - web.labels\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	added, err := SetLabelFileRef(root, "web", "web.labels")
	if err != nil {
		t.Fatal(err)
	}
	if added {
		t.Error("expected added=false when the ref already exists")
	}
}

func TestSetLabelFileRef_unknownServiceErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    image: alpine\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetLabelFileRef(root, "nonexistent", "x.labels"); err == nil {
		t.Error("expected an error for a service that doesn't exist")
	}
}

func TestSetInlineLabel_createsMappingAndQuotesByDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    image: alpine\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	added, changed, err := SetInlineLabel(root, "web", "com.example.tier", "prod", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !added || !changed {
		t.Errorf("added=%v changed=%v, want both true", added, changed)
	}

	out, _ := Marshal(root)
	if !strings.Contains(string(out), `"prod"`) {
		t.Errorf("expected the value to be double-quoted by default (Decision 33), got:\n%s", out)
	}

	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	labels, err := GetLabels(path, "web")
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 1 || labels[0].Key != "com.example.tier" || labels[0].Value != "prod" {
		t.Errorf("GetLabels = %v, want [{com.example.tier prod}]", labels)
	}
}

func TestSetInlineLabel_unquotedWhenRequested(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    image: alpine\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := SetInlineLabel(root, "web", "com.example.tier", "prod", false, false); err != nil {
		t.Fatal(err)
	}
	out, _ := Marshal(root)
	if strings.Contains(string(out), `"prod"`) {
		t.Errorf("expected an unquoted scalar with quote=false, got:\n%s", out)
	}
	if !strings.Contains(string(out), "prod") {
		t.Errorf("expected the value to still be present, got:\n%s", out)
	}
}

func TestSetInlineLabel_existingKeyNotOverwrittenWithoutForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    labels:\n      com.example.tier: \"staging\"\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	added, changed, err := SetInlineLabel(root, "web", "com.example.tier", "prod", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if added || changed {
		t.Errorf("added=%v changed=%v, want both false when key exists and force=false", added, changed)
	}

	out, _ := Marshal(root)
	if !strings.Contains(string(out), "staging") || strings.Contains(string(out), "prod") {
		t.Errorf("expected the original value untouched, got:\n%s", out)
	}
}

func TestSetInlineLabel_forceOverwritesInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    labels:\n      com.example.a: \"1\"\n      com.example.tier: \"staging\"\n      com.example.b: \"2\"\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	added, changed, err := SetInlineLabel(root, "web", "com.example.tier", "prod", true, true)
	if err != nil {
		t.Fatal(err)
	}
	if added {
		t.Error("expected added=false for an in-place update (key already existed)")
	}
	if !changed {
		t.Error("expected changed=true")
	}

	out, _ := Marshal(root)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	labels, err := GetLabels(path, "web")
	if err != nil {
		t.Fatal(err)
	}
	// Position preserved: still the second of three keys, not moved to the end.
	want := []Label{{"com.example.a", "1"}, {"com.example.tier", "prod"}, {"com.example.b", "2"}}
	if len(labels) != len(want) {
		t.Fatalf("GetLabels = %v, want %v", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Errorf("GetLabels[%d] = %v, want %v", i, labels[i], want[i])
		}
	}
}

func TestSetInlineLabel_listForm_appendsNewEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    labels:\n      - \"com.example.a=1\"\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	added, changed, err := SetInlineLabel(root, "web", "com.example.b", "2", true, false)
	if err != nil {
		t.Fatalf("SetInlineLabel: %v", err)
	}
	if !added || !changed {
		t.Errorf("added=%v changed=%v, want both true", added, changed)
	}

	out, _ := Marshal(root)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	labels, err := GetLabels(path, "web")
	if err != nil {
		t.Fatal(err)
	}
	want := []Label{{"com.example.a", "1"}, {"com.example.b", "2"}}
	if len(labels) != len(want) {
		t.Fatalf("GetLabels = %v, want %v", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Errorf("GetLabels[%d] = %v, want %v", i, labels[i], want[i])
		}
	}
}

func TestSetInlineLabel_listForm_updatesExistingEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    labels:\n      - \"com.example.a=1\"\n      - \"com.example.b=2\"\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	added, changed, err := SetInlineLabel(root, "web", "com.example.a", "new", true, true)
	if err != nil {
		t.Fatalf("SetInlineLabel: %v", err)
	}
	if added {
		t.Error("expected added=false for an in-place update")
	}
	if !changed {
		t.Error("expected changed=true")
	}

	out, _ := Marshal(root)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	labels, err := GetLabels(path, "web")
	if err != nil {
		t.Fatal(err)
	}
	// Position preserved: still first of two entries, not moved to the end.
	want := []Label{{"com.example.a", "new"}, {"com.example.b", "2"}}
	if len(labels) != len(want) {
		t.Fatalf("GetLabels = %v, want %v", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Errorf("GetLabels[%d] = %v, want %v", i, labels[i], want[i])
		}
	}
}

func TestSetInlineLabel_listForm_skipsWithoutForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    labels:\n      - \"com.example.a=1\"\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	added, changed, err := SetInlineLabel(root, "web", "com.example.a", "2", true, false)
	if err != nil {
		t.Fatalf("SetInlineLabel: %v", err)
	}
	if added || changed {
		t.Errorf("added=%v changed=%v, want both false when key exists and force=false", added, changed)
	}

	out, _ := Marshal(root)
	if strings.Contains(string(out), "=2") {
		t.Errorf("expected the original value untouched, got:\n%s", out)
	}
}

func TestRemoveLabelFileRef_removesOnlyNamedRef(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    label_file:\n      - a.labels\n      - b.labels\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveLabelFileRef(root, "web", "a.labels")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Error("expected removed=true")
	}

	out, _ := Marshal(root)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	refs, err := GetLabelFileRefs(path, "web")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0] != "b.labels" {
		t.Errorf("GetLabelFileRefs = %v, want [b.labels]", refs)
	}
}

func TestRemoveLabelFileRef_removesLabelFileKeyEntirelyWhenEmptied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    image: alpine\n    label_file:\n      - a.labels\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveLabelFileRef(root, "web", "a.labels"); err != nil {
		t.Fatal(err)
	}

	out, err := Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "label_file") {
		t.Errorf("expected label_file: removed entirely once emptied, got:\n%s", out)
	}
	if !strings.Contains(string(out), "image") {
		t.Errorf("expected the rest of the service to remain, got:\n%s", out)
	}
}

func TestRemoveLabelFileRef_notPresentIsANoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    label_file:\n      - a.labels\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveLabelFileRef(root, "web", "nonexistent.labels")
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Error("expected removed=false for a ref that was never there")
	}
}

func TestRemoveLabelFileRef_noLabelFileAtAllIsANoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    image: alpine\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveLabelFileRef(root, "web", "a.labels")
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Error("expected removed=false when there's no label_file at all")
	}
}

func TestRemoveInlineLabelKeys_removesOnlyNamedKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    labels:\n      com.example.a: \"1\"\n      com.example.b: \"2\"\n      com.example.c: \"3\"\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveInlineLabelKeys(root, "web", []string{"com.example.a", "com.example.c"})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 {
		t.Errorf("removed = %v, want 2 keys", removed)
	}

	out, _ := Marshal(root)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	labels, err := GetLabels(path, "web")
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 1 || labels[0].Key != "com.example.b" {
		t.Errorf("GetLabels = %v, want only com.example.b remaining", labels)
	}
}

func TestRemoveInlineLabelKeys_removesLabelsKeyEntirelyWhenEmptied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    image: alpine\n    labels:\n      com.example.a: \"1\"\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveInlineLabelKeys(root, "web", []string{"com.example.a"}); err != nil {
		t.Fatal(err)
	}

	out, err := Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "labels") {
		t.Errorf("expected the labels: key itself to be removed once emptied, got:\n%s", out)
	}
	if !strings.Contains(string(out), "image") {
		t.Errorf("expected the rest of the service to remain, got:\n%s", out)
	}
}

func TestRemoveInlineLabelKeys_noLabelsIsANoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    image: alpine\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveInlineLabelKeys(root, "web", []string{"com.example.a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want none", removed)
	}
}

func TestRemoveInlineLabelKeys_listFormPartialRemoval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    labels:\n      - \"com.example.a=1\"\n      - \"com.example.b=2\"\n      - \"com.example.c=3\"\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveInlineLabelKeys(root, "web", []string{"com.example.a", "com.example.c"})
	if err != nil {
		t.Fatalf("RemoveInlineLabelKeys: %v", err)
	}
	if len(removed) != 2 {
		t.Errorf("removed = %v, want 2 keys", removed)
	}
	if !containsStr(removed, "com.example.a") || !containsStr(removed, "com.example.c") {
		t.Errorf("removed = %v, want com.example.a and com.example.c", removed)
	}

	out, _ := Marshal(root)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	labels, err := GetLabels(path, "web")
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 1 || labels[0].Key != "com.example.b" || labels[0].Value != "2" {
		t.Errorf("GetLabels = %v, want only com.example.b=2 remaining", labels)
	}
	// labels: key must still be present since the list isn't empty.
	if !strings.Contains(string(out), "labels:") {
		t.Errorf("expected labels: key to remain, got:\n%s", out)
	}
}

func TestRemoveInlineLabelKeys_listFormFullRemovalDeletesLabelsKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, "services:\n  web:\n    image: alpine\n    labels:\n      - \"com.example.a=1\"\n      - \"com.example.b=2\"\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveInlineLabelKeys(root, "web", []string{"com.example.a", "com.example.b"})
	if err != nil {
		t.Fatalf("RemoveInlineLabelKeys: %v", err)
	}
	if len(removed) != 2 {
		t.Errorf("removed = %v, want 2 keys", removed)
	}

	out, err := Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "labels") || strings.Contains(string(out), "com.example.a") || strings.Contains(string(out), "com.example.b") {
		t.Errorf("expected the labels: key itself and all entries to be removed once emptied, got:\n%s", out)
	}
	if !strings.Contains(string(out), "image") {
		t.Errorf("expected the rest of the service (image:) to remain, got:\n%s", out)
	}
}

func TestRemoveInlineLabelKeys_listFormEntryWithNoEqualsSign(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	// A list entry with no "=" is a key with an empty value — same as how GetLabels reads it.
	writeTestFile(t, path, "services:\n  web:\n    labels:\n      - \"com.example.a=1\"\n      - \"com.example.barekey\"\n")

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveInlineLabelKeys(root, "web", []string{"com.example.barekey"})
	if err != nil {
		t.Fatalf("RemoveInlineLabelKeys: %v", err)
	}
	if len(removed) != 1 || removed[0] != "com.example.barekey" {
		t.Errorf("removed = %v, want [com.example.barekey]", removed)
	}

	out, _ := Marshal(root)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	labels, err := GetLabels(path, "web")
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 1 || labels[0].Key != "com.example.a" || labels[0].Value != "1" {
		t.Errorf("GetLabels = %v, want only com.example.a=1 remaining", labels)
	}
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func TestMarshal_preservesCommentsOnUntouchedContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	writeTestFile(t, path, `# top-level comment
services:
  web:
    image: alpine # keep this image comment
    labels:
      com.example.a: "1" # existing label comment
`)

	root, err := LoadRootForEdit(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := SetInlineLabel(root, "web", "com.example.b", "2", true, false); err != nil {
		t.Fatal(err)
	}

	out, err := Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{"# top-level comment", "# keep this image comment", "# existing label comment"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected comment %q to survive the round trip, got:\n%s", want, got)
		}
	}
}
