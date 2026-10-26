// Package document opens, edits and saves one text file without ever writing
// bytes it did not read.
//
// The editor this serves is deliberately plain: a text buffer with a cursor,
// a selection and undo. The package's real work is the part a plain editor
// usually gets wrong — deciding what may be edited at all, and refusing to
// save over a file that moved on disk. A file is editable only when it was
// read whole, is UTF-8 text, has one newline convention, and is a regular
// file reached without following a link. Saving compares the bytes on disk
// with the bytes that were loaded and reports ErrConflict instead of
// overwriting a concurrent edit.
package document

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// MaxBytes is the largest file this editor opens for editing. A larger file is
// still previewed, but read-only: an editor that silently truncated a buffer
// and then saved it would destroy the rest of the file.
const MaxBytes = 1 << 20

// ErrConflict reports that the file on disk is not the file that was loaded.
var ErrConflict = errors.New("the file changed on disk since it was opened")

// ErrNotEditable reports that the document was opened read-only.
var ErrNotEditable = errors.New("this document is read-only")

// Doc is one opened file. Text is the model's copy, always with line feeds;
// Newline records how the file writes them back.
type Doc struct {
	Root string
	Rel  string
	Path string

	Text       string
	Newline    string // "\n" or "\r\n"
	Mode       fs.FileMode
	Size       int64
	Editable   bool
	Reason     string // why a read-only document is read-only
	Binary     bool
	Truncated  bool
	snapshot   [sha256.Size]byte
	snapshotOK bool
}

// Open reads rel under root. It never follows a symbolic link, never escapes
// root, and never reads past MaxBytes. A file it cannot edit is returned with
// Editable false and Reason set, because a preview that says why is more
// useful than an error that offers nothing.
func Open(root, rel string) (Doc, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Doc{}, err
	}
	rel, err = cleanRel(rel)
	if err != nil {
		return Doc{}, err
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := insideRoot(root, path); err != nil {
		return Doc{}, err
	}
	if err := noSymlinkPath(root, path); err != nil {
		return Doc{}, err
	}
	st, err := os.Lstat(path)
	if err != nil {
		return Doc{}, err
	}
	if !st.Mode().IsRegular() {
		return Doc{}, fmt.Errorf("%s is not a regular file", rel)
	}
	doc := Doc{Root: root, Rel: rel, Path: path, Mode: st.Mode(), Size: st.Size(), Newline: "\n"}
	if st.Size() > MaxBytes {
		doc.Reason = fmt.Sprintf("larger than the %d KiB editing limit", MaxBytes/1024)
		doc.Truncated = true
	}
	raw, err := readBounded(path)
	if err != nil {
		return Doc{}, err
	}
	doc.snapshot = sha256.Sum256(raw)
	doc.snapshotOK = true
	if doc.Truncated {
		doc.Text = string(raw)
		return doc, nil
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		doc.Binary = true
		doc.Reason = "binary file"
		return doc, nil
	}
	if !utf8.Valid(raw) {
		doc.Reason = "not valid UTF-8"
		return doc, nil
	}
	text, newline, mixed := splitNewlines(string(raw))
	doc.Text, doc.Newline = text, newline
	if mixed {
		doc.Reason = "mixed line endings; saving would rewrite them"
		return doc, nil
	}
	doc.Editable = true
	return doc, nil
}

// Save writes text and returns the updated document. It refuses when the file
// is read-only, when it is no longer a regular file, or when its bytes differ
// from the ones that were loaded.
func Save(doc Doc, text string) (Doc, error) {
	if !doc.Editable {
		return doc, ErrNotEditable
	}
	if err := noSymlinkPath(doc.Root, doc.Path); err != nil {
		return doc, err
	}
	st, err := os.Lstat(doc.Path)
	if err != nil {
		return doc, err
	}
	if !st.Mode().IsRegular() {
		return doc, fmt.Errorf("%s is no longer a regular file", doc.Rel)
	}
	current, err := readBounded(doc.Path)
	if err != nil {
		return doc, err
	}
	if !doc.snapshotOK || sha256.Sum256(current) != doc.snapshot {
		return doc, ErrConflict
	}
	doc.Text = text
	out := encode(text, doc.Newline)
	if len(out) > MaxBytes {
		return doc, fmt.Errorf("the document is larger than the %d KiB limit", MaxBytes/1024)
	}
	if err := writeAtomic(doc.Path, out, doc.Mode.Perm()); err != nil {
		return doc, err
	}
	doc.snapshot = sha256.Sum256(out)
	doc.Size = int64(len(out))
	return doc, nil
}

// NewText builds an in-memory document, for a diff or another read-only body.
func NewText(rel, text string) Doc {
	return Doc{Rel: rel, Text: text, Newline: "\n"}
}

func cleanRel(rel string) (string, error) {
	if strings.TrimSpace(rel) == "" || strings.ContainsRune(rel, 0) {
		return "", errors.New("empty path")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s is not relative to the project", rel)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%s escapes the project", rel)
	}
	return clean, nil
}

func insideRoot(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s escapes the project", path)
	}
	return nil
}

// noSymlinkPath refuses a link anywhere between root and the file's folder.
// The final name is checked by the caller's Lstat, which refuses a link too.
func noSymlinkPath(root, path string) error {
	dir := filepath.Dir(path)
	for dir != root && strings.HasPrefix(dir, root) {
		st, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is reached through a symbolic link", path)
		}
		dir = filepath.Dir(dir)
	}
	return nil
}

func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBytes {
		data = data[:MaxBytes]
	}
	return data, nil
}

// splitNewlines turns one file's body into model text. mixed is true when the
// file uses both conventions (or a lone CR), which this editor will not write
// back faithfully, so it refuses to edit rather than silently normalise.
func splitNewlines(text string) (out, newline string, mixed bool) {
	crlf := strings.Count(text, "\r\n")
	lf := strings.Count(text, "\n") - crlf
	cr := strings.Count(text, "\r") - crlf
	switch {
	case crlf > 0 && (lf > 0 || cr > 0):
		return text, "\n", true
	case cr > 0:
		return text, "\n", true
	case crlf > 0:
		return strings.ReplaceAll(text, "\r\n", "\n"), "\r\n", false
	}
	return text, "\n", false
}

func encode(text, newline string) []byte {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if newline == "\r\n" {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	return []byte(text)
}

// writeAtomic writes a private part file beside path and renames it over the
// name. The permission bits are the file's own: an executable script stays
// executable. Where the filesystem has no rename-over semantics the part is
// removed and the error returned; a half-written target is never the outcome.
func writeAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tuios-edit-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}
