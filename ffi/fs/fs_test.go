package fs

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"testing"
	"time"
)

func TestWalkStreamsMatchingFiles(t *testing.T) {
	root := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("root.md", "root")
	write("nested/child.md", "child")
	write("nested/skip.md", "skip")
	write("nested/text.txt", "text")
	write(".hidden/secret.md", "secret")
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(root, "nested"), filepath.Join(root, "linked")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "root.md"), filepath.Join(root, "alias.md")); err != nil {
			t.Fatal(err)
		}
	}

	scanner, err := OpenScanner(root)
	if err != nil {
		t.Fatal(err)
	}
	defer scanner.Close()
	entries := make(chan Entry, 1)
	done := make(chan error, 1)
	go func() {
		done <- scanner.Walk("**/*.md", []string{"**/skip.md"}, entries)
		close(entries)
	}()
	var paths []string
	for entry := range entries {
		paths = append(paths, entry.RelativePath)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	want := []string{"nested/child.md", "root.md"}
	if len(paths) != len(want) || paths[0] != want[0] || paths[1] != want[1] {
		t.Fatalf("got %v, want %v", paths, want)
	}
}

func TestWalkPrunesExactlyIgnoredDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "archive", "hidden.md"), []byte("hidden"), 0o644); err != nil {
		t.Fatal(err)
	}
	scanner, err := OpenScanner(root)
	if err != nil {
		t.Fatal(err)
	}
	defer scanner.Close()
	entries := make(chan Entry)
	done := make(chan error, 1)
	go func() {
		done <- scanner.Walk("**/*.md", []string{"archive"}, entries)
		close(entries)
	}()
	for entry := range entries {
		t.Fatalf("ignored directory emitted %q", entry.RelativePath)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestReadAndHashRejectsChangedMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := Entry{RootPath: filepath.Dir(path), RelativePath: "doc.md", AbsolutePath: path, SizeBytes: int(info.Size()), MtimeNS: int(info.ModTime().UnixNano())}
	scanner, err := OpenScanner(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer scanner.Close()
	result, err := scanner.ReadAndHash(entry)
	if err != nil || result.Hash == "" || result.Markdown != "first" {
		t.Fatalf("unexpected read: result=%#v err=%v", result, err)
	}

	changed := info.ModTime().Add(time.Second)
	if err := os.Chtimes(path, changed, changed); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.ReadAndHash(entry); err == nil {
		t.Fatal("expected changed metadata to be rejected")
	}
}

func TestReadAndHashRejectsReplacementEscapingRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink replacement requires Windows symlink privileges")
	}
	root := t.TempDir()
	outside := t.TempDir()
	insidePath := filepath.Join(root, "doc.md")
	if err := os.WriteFile(insidePath, []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(insidePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := Entry{RootPath: root, RelativePath: "doc.md", AbsolutePath: insidePath, SizeBytes: int(info.Size()), MtimeNS: int(info.ModTime().UnixNano())}
	scanner, err := OpenScanner(root)
	if err != nil {
		t.Fatal(err)
	}
	defer scanner.Close()
	if err := os.Remove(insidePath); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(outside, "doc.md")
	if err := os.WriteFile(outsidePath, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, insidePath); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.ReadAndHash(entry); err == nil {
		t.Fatal("expected escaping symlink replacement to be rejected")
	}
}

func TestScannerPinsRootAcrossRename(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("root replacement requires Windows symlink privileges")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	insidePath := filepath.Join(root, "doc.md")
	if err := os.WriteFile(insidePath, []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(insidePath)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := OpenScanner(root)
	if err != nil {
		t.Fatal(err)
	}
	defer scanner.Close()
	moved := filepath.Join(parent, "moved")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "doc.md"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	entry := Entry{RootPath: root, RelativePath: "doc.md", AbsolutePath: insidePath, SizeBytes: int(info.Size()), MtimeNS: int(info.ModTime().UnixNano())}
	result, err := scanner.ReadAndHash(entry)
	if err != nil {
		t.Fatal(err)
	}
	if result.Markdown != "inside" {
		t.Fatalf("scanner followed replaced root: %q", result.Markdown)
	}
}

func TestUnicodePathsAndNanosecondMtimesRoundTrip(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Skip("nanosecond mtimes require a supported 64-bit target")
	}
	root := t.TempDir()
	relative := filepath.Join("日本語", "naïve 🧠.md")
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("unicode body"), 0o644); err != nil {
		t.Fatal(err)
	}
	requested := time.Unix(1_700_000_000, 123_456_789)
	if err := os.Chtimes(path, requested, requested); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.ModTime().UnixNano() != requested.UnixNano() {
		t.Skipf("filesystem rounded nanosecond mtime to %s", info.ModTime())
	}

	scanner, err := OpenScanner(root)
	if err != nil {
		t.Fatal(err)
	}
	defer scanner.Close()
	entry, err := scanner.Inspect(relative)
	if err != nil {
		t.Fatal(err)
	}
	if entry.RelativePath != filepath.ToSlash(relative) {
		t.Fatalf("Unicode path changed: got %q", entry.RelativePath)
	}
	if entry.MtimeNS != int(info.ModTime().UnixNano()) {
		t.Fatalf("nanosecond mtime changed: got %d want %d", entry.MtimeNS, info.ModTime().UnixNano())
	}
	result, err := scanner.ReadAndHash(entry)
	if err != nil {
		t.Fatal(err)
	}
	if result.RelativePath != filepath.ToSlash(relative) || result.Markdown != "unicode body" {
		t.Fatalf("unexpected Unicode read result: %#v", result)
	}

	changed := info.ModTime().Add(time.Millisecond)
	if err := os.Chtimes(path, changed, changed); err != nil {
		t.Fatal(err)
	}
	changedInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if changedInfo.ModTime().UnixNano() == info.ModTime().UnixNano() {
		t.Skip("filesystem did not retain a subsecond mtime change")
	}
	if _, err := scanner.ReadAndHash(entry); err == nil {
		t.Fatal("subsecond timestamp change was not detected")
	}
}

func TestReadAndHashReportsInaccessibleFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "private.md")
	if err := os.WriteFile(path, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	scanner, err := OpenScanner(root)
	if err != nil {
		t.Fatal(err)
	}
	defer scanner.Close()
	entry, err := scanner.Inspect("private.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Skipf("filesystem cannot remove read permissions: %v", err)
	}
	defer os.Chmod(path, 0o600)
	if _, err := scanner.ReadAndHash(entry); err == nil {
		t.Skip("current user or filesystem bypasses file permission bits")
	}
}

func TestByteBudgetBlocksAndTracksPeak(t *testing.T) {
	budget, err := NewByteBudget(10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := budget.Reserve(8); err != nil {
		t.Fatal(err)
	}
	reserved := make(chan struct{})
	go func() {
		budget.Reserve(4)
		close(reserved)
	}()
	select {
	case <-reserved:
		t.Fatal("reservation should block")
	case <-time.After(20 * time.Millisecond):
	}
	if err := budget.Release(8); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reserved:
	case <-time.After(time.Second):
		t.Fatal("reservation did not wake")
	}
	if budget.Peak() > 10 {
		t.Fatalf("peak exceeded limit: %d", budget.Peak())
	}
	if _, err := budget.Reserve(11); err == nil {
		t.Fatal("expected oversized reservation error")
	}
	if err := budget.Release(5); err == nil || budget.Healthy() {
		t.Fatal("expected invalid release to poison accounting health")
	}
}
