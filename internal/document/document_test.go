package document

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Security boundaries: a stale editor and a swapped name must never overwrite
// bytes it did not load. These cases are not observable in a screenshot.
func TestSaveChecksIdentityContentAndPreservesEncoding(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "script.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\r\necho before\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc, err := Open(root, "script.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Editable || strings.Contains(doc.Text, "\r") {
		t.Fatalf("CRLF document not loaded losslessly: %+v", doc)
	}
	if _, err := Save(doc, "#!/bin/sh\necho after\n"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "#!/bin/sh\r\necho after\r\n" {
		t.Fatalf("saved bytes = %q, %v", data, err)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o755 {
		t.Fatal("save lost executable bits")
	}
	if _, err := Save(doc, "stale\n"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale save = %v", err)
	}
}

func TestNoEditingOfPartialOrUnsafeFiles(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct{ name, text string }{
		{"large.txt", strings.Repeat("a", MaxBytes+1)},
		{"binary.dat", "a\x00b"}, {"invalid.txt", "\xff"},
		{"mixed.txt", "one\r\ntwo\n"},
	} {
		if err := os.WriteFile(filepath.Join(root, tc.name), []byte(tc.text), 0o644); err != nil {
			t.Fatal(err)
		}
		doc, err := Open(root, tc.name)
		if err != nil {
			t.Fatal(err)
		}
		if doc.Editable {
			t.Fatalf("%s is editable", tc.name)
		}
		if _, err := Save(doc, "replacement"); err == nil {
			t.Fatalf("saved unsupported %s", tc.name)
		}
	}
	if _, err := Open(root, "../escape"); err == nil {
		t.Fatal("path escaped root")
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Skip(err)
	}
	if _, err := Open(root, "link.txt"); err == nil {
		t.Fatal("preview followed a symlink")
	}
}

func TestEditorUndoSelectionAndUnicodeAreLossless(t *testing.T) {
	b := NewBuffer("hello 世界\nnext\n")
	b.Select(0, 5)
	b.Insert("你好")
	if b.Text() != "你好 世界\nnext\n" || !b.Dirty() {
		t.Fatalf("selection replacement = %q", b.Text())
	}
	b.Undo()
	if b.Text() != "hello 世界\nnext\n" || b.Dirty() {
		t.Fatal("undo did not restore the original document")
	}
	b.Redo()
	if b.Text() != "你好 世界\nnext\n" {
		t.Fatal("redo lost unicode")
	}
}
