// Package fs provides bounded, streaming filesystem operations for indexing.
package fs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
)

// Entry is filesystem metadata collected without opening a document body.
type Entry struct {
	RootPath     string
	RelativePath string
	AbsolutePath string
	SizeBytes    int
	MtimeNS      int
}

// ReadResult is a stable file read and its content hash. Markdown extraction is
// deliberately deferred; Phase 3 indexes the raw body.
// Scanner pins a collection root descriptor for one complete update.
type Scanner struct {
	rootPath  string
	root      *os.Root
	closeOnce sync.Once
	closeErr  error
}

// OpenScanner opens and pins a canonical collection root.
func OpenScanner(path string) (*Scanner, error) {
	canonical, err := CanonicalDirectory(path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return nil, err
	}
	return &Scanner{rootPath: canonical, root: root}, nil
}

// Close releases the pinned collection root.
func (scanner *Scanner) Close() error {
	scanner.closeOnce.Do(func() {
		scanner.closeErr = scanner.root.Close()
	})
	return scanner.closeErr
}

// ReadResult is a stable document read.
type ReadResult struct {
	RelativePath   string
	Title          string
	Hash           string
	Markdown       string
	SearchableText string
	SizeBytes      int
	MtimeNS        int
}

// CanonicalDirectory resolves a directory to an absolute symlink-free path.
func CanonicalDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", path)
	}
	return resolved, nil
}

func validatePattern(pattern string) error {
	if !doublestar.ValidatePattern(pattern) {
		return fmt.Errorf("invalid glob pattern %q", pattern)
	}
	return nil
}

// ValidatePatterns checks the include and ignore patterns before an update.
func ValidatePatterns(pattern string, ignores []string) error {
	if err := validatePattern(pattern); err != nil {
		return err
	}
	for _, ignore := range ignores {
		if err := validatePattern(ignore); err != nil {
			return err
		}
	}
	return nil
}

func hiddenPath(relative string) bool {
	for _, part := range strings.Split(relative, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func ignoredPath(relative string, ignores []string) (bool, error) {
	for _, pattern := range ignores {
		matched, err := doublestar.Match(pattern, relative)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

// Walk emits matching regular files as they are discovered. The caller owns
// and closes output.
func (scanner *Scanner) Walk(pattern string, ignores []string, output chan<- Entry) error {
	if err := ValidatePatterns(pattern, ignores); err != nil {
		return err
	}

	return iofs.WalkDir(scanner.root.FS(), ".", func(path string, entry iofs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		relative := filepath.ToSlash(path)
		if hiddenPath(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		ignored, err := ignoredPath(relative, ignores)
		if err != nil {
			return err
		}
		if ignored {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		matched, err := doublestar.Match(pattern, relative)
		if err != nil || !matched {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		output <- Entry{
			RootPath:     scanner.rootPath,
			RelativePath: relative,
			AbsolutePath: filepath.Join(scanner.rootPath, filepath.FromSlash(relative)),
			SizeBytes:    int(info.Size()),
			MtimeNS:      int(info.ModTime().UnixNano()),
		}
		return nil
	})
}

// ReadAndHash reads a file only if its size and mtime still match the walked
// metadata, then verifies those values again after reading.
func (scanner *Scanner) ReadAndHash(entry Entry) (ReadResult, error) {
	file, err := scanner.root.Open(filepath.FromSlash(entry.RelativePath))
	if err != nil {
		return ReadResult{}, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return ReadResult{}, err
	}
	if !before.Mode().IsRegular() || int(before.Size()) != entry.SizeBytes || int(before.ModTime().UnixNano()) != entry.MtimeNS {
		return ReadResult{}, fmt.Errorf("file changed before read: %s", entry.RelativePath)
	}
	body, err := io.ReadAll(io.LimitReader(file, int64(entry.SizeBytes)+1))
	if err != nil {
		return ReadResult{}, err
	}
	if len(body) != entry.SizeBytes {
		return ReadResult{}, fmt.Errorf("file changed during read: %s", entry.RelativePath)
	}
	after, err := file.Stat()
	if err != nil {
		return ReadResult{}, err
	}
	if !after.Mode().IsRegular() || int(after.Size()) != entry.SizeBytes || int(after.ModTime().UnixNano()) != entry.MtimeNS {
		return ReadResult{}, fmt.Errorf("file changed during read: %s", entry.RelativePath)
	}

	sum := sha256.Sum256(body)
	markdown := string(body)
	return ReadResult{
		RelativePath:   entry.RelativePath,
		Title:          strings.TrimSuffix(filepath.Base(entry.RelativePath), filepath.Ext(entry.RelativePath)),
		Hash:           hex.EncodeToString(sum[:]),
		Markdown:       markdown,
		SearchableText: markdown,
		SizeBytes:      entry.SizeBytes,
		MtimeNS:        entry.MtimeNS,
	}, nil
}

// ByteBudget bounds the bytes retained by reader and writer stages.
type ByteBudget struct {
	mu      sync.Mutex
	ready   *sync.Cond
	limit   int
	used    int
	peak    int
	invalid bool
}

// NewByteBudget creates a positive byte budget.
func NewByteBudget(limit int) (*ByteBudget, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("byte budget must be positive")
	}
	budget := &ByteBudget{limit: limit}
	budget.ready = sync.NewCond(&budget.mu)
	return budget, nil
}

// Reserve waits until the complete reservation fits.
func (budget *ByteBudget) Reserve(bytes int) (int, error) {
	if bytes < 0 {
		return 0, fmt.Errorf("cannot reserve negative bytes")
	}
	if bytes > budget.limit {
		return 0, fmt.Errorf("file size %d exceeds byte budget %d", bytes, budget.limit)
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	for budget.used+bytes > budget.limit {
		budget.ready.Wait()
	}
	budget.used += bytes
	if budget.used > budget.peak {
		budget.peak = budget.used
	}
	return bytes, nil
}

// Release returns a prior reservation and rejects accounting underflow.
func (budget *ByteBudget) Release(bytes int) error {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if bytes < 0 || bytes > budget.used {
		budget.invalid = true
		return fmt.Errorf("invalid byte-budget release %d with %d reserved", bytes, budget.used)
	}
	budget.used -= bytes
	budget.ready.Broadcast()
	return nil
}

// Peak returns the greatest number of simultaneously reserved bytes.
func (budget *ByteBudget) Peak() int {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	return budget.peak
}

// Used returns bytes currently reserved.
func (budget *ByteBudget) Used() int {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	return budget.used
}

// Healthy reports whether every release respected reservation accounting.
func (budget *ByteBudget) Healthy() bool {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	return !budget.invalid
}
