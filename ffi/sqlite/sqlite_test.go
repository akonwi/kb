package sqlite

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
)

func TestMemoryDatabaseDetectionParsesSQLiteURIs(t *testing.T) {
	memoryPaths := []string{":memory:", "file::memory:?cache=shared", "file:temporary?mode=memory&cache=shared"}
	for _, path := range memoryPaths {
		if !isMemoryDatabase(path) {
			t.Errorf("expected memory database URI %q", path)
		}
	}
	diskPaths := []string{"notes-mode=memory.sqlite", "/tmp/mode=memory/index.sqlite", "file:notes.sqlite?cache=shared"}
	for _, path := range diskPaths {
		if isMemoryDatabase(path) {
			t.Errorf("ordinary database path misclassified as memory: %q", path)
		}
	}
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("standard shared-memory URI was rejected: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentOpenOfExistingWALDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent-open.sqlite")
	initial, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := initial.Close(); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errors := make(chan error, 16)
	var group sync.WaitGroup
	for index := 0; index < 16; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			db, err := Open(path)
			if err != nil {
				errors <- err
				return
			}
			if err := db.Close(); err != nil {
				errors <- err
			}
		}()
	}
	close(start)
	group.Wait()
	close(errors)
	for err := range errors {
		t.Errorf("concurrent open: %v", err)
	}
}

func TestOpenConfiguresMeasuredPragmas(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "pragmas.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	checks := []struct {
		name string
		want any
	}{
		{name: "journal_mode", want: "wal"},
		{name: "synchronous", want: 2},
		{name: "cache_size", want: -20000},
		{name: "foreign_keys", want: 1},
		{name: "busy_timeout", want: 5000},
	}
	for _, check := range checks {
		switch want := check.want.(type) {
		case string:
			var got string
			if err := db.conn.QueryRowContext(context.Background(), "PRAGMA "+check.name).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("PRAGMA %s=%q, want %q", check.name, got, want)
			}
		case int:
			var got int
			if err := db.conn.QueryRowContext(context.Background(), "PRAGMA "+check.name).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("PRAGMA %s=%d, want %d", check.name, got, want)
			}
		}
	}
	var mmapSize int64
	if err := db.conn.QueryRowContext(context.Background(), "PRAGMA mmap_size").Scan(&mmapSize); err != nil {
		t.Fatal(err)
	}
	if mmapSize < 0 || mmapSize > 268435456 {
		t.Fatalf("unexpected mmap_size %d", mmapSize)
	}
}
