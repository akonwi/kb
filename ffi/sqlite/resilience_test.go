package sqlite

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInterruptedBatchRollsBackEveryDocument(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "interrupted.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.conn.ExecContext(context.Background(), `
		CREATE TABLE collections (
		  id INTEGER PRIMARY KEY,
		  current_generation INTEGER NOT NULL
		);
		CREATE TABLE content (
		  hash TEXT PRIMARY KEY,
		  markdown TEXT NOT NULL,
		  searchable_text TEXT NOT NULL,
		  extraction_version INTEGER NOT NULL
		);
		CREATE TABLE documents (
		  id INTEGER PRIMARY KEY,
		  collection_id INTEGER NOT NULL REFERENCES collections(id),
		  relative_path TEXT NOT NULL,
		  title TEXT NOT NULL,
		  content_hash TEXT NOT NULL REFERENCES content(hash),
		  size_bytes INTEGER NOT NULL,
		  mtime_ns INTEGER NOT NULL,
		  active INTEGER NOT NULL,
		  seen_generation INTEGER NOT NULL,
		  updated_at TEXT NOT NULL DEFAULT '',
		  UNIQUE(collection_id, relative_path)
		);
		INSERT INTO collections(id, current_generation) VALUES (1, 1);
	`); err != nil {
		t.Fatal(err)
	}

	_, err = db.ApplyIndexBatch(1, 1, []IndexChange{
		{
			RelativePath: "first.md", Title: "First", ContentHash: strings.Repeat("a", 64),
			Markdown: "atomic batch", SearchableText: "atomic batch", SizeBytes: 12,
			MtimeNS: 1, HasContent: true, ExtractionVersion: 1,
		},
		{RelativePath: "missing.md", HasContent: false},
	})
	if err == nil || !strings.Contains(err.Error(), "metadata-only observation") {
		t.Fatalf("expected injected interruption, got %v", err)
	}
	var documents, content int
	if err := db.conn.QueryRowContext(context.Background(), `
		SELECT (SELECT count(*) FROM documents), (SELECT count(*) FROM content)
	`).Scan(&documents, &content); err != nil {
		t.Fatal(err)
	}
	if documents != 0 || content != 0 {
		t.Fatalf("partially committed batch: documents=%d content=%d", documents, content)
	}
}

func TestConcurrentReaderObservesCommittedFTSSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.sqlite")
	writer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	createProductionSearchSchema(t, writer)
	if _, err := writer.ApplyIndexBatch(1, 1, []IndexChange{productionChange(0)}); err != nil {
		t.Fatal(err)
	}
	reader, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	start := make(chan struct{})
	started := make(chan struct{})
	readerReady := make(chan struct{})
	writerErrors := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-start
		close(started)
		<-readerReady
		for index := 1; index <= 50; index++ {
			if _, err := writer.ApplyIndexBatch(1, 1, []IndexChange{productionChange(index)}); err != nil {
				writerErrors <- err
				return
			}
		}
	}()

	close(start)
	<-started
	initial, err := reader.SearchIndex("\"concurrent\"", nil, 100)
	if err != nil || len(initial) != 1 {
		t.Fatalf("initial production read: count=%d err=%v", len(initial), err)
	}
	close(readerReady)

	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	reads := 1
	for {
		select {
		case <-done:
			select {
			case err := <-writerErrors:
				t.Fatal(err)
			default:
			}
			results, err := reader.SearchIndex("\"concurrent\"", nil, 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 51 {
				t.Fatalf("expected all committed production rows, got %d", len(results))
			}
			if reads < 1 {
				t.Fatal("expected at least one synchronized concurrent read")
			}
			return
		case <-deadline.C:
			t.Fatal("timed out waiting for concurrent production writer")
		default:
			results, err := reader.SearchIndex("\"concurrent\"", nil, 100)
			if err != nil {
				t.Fatalf("concurrent production read: %v", err)
			}
			if len(results) < 1 || len(results) > 51 {
				t.Fatalf("reader observed impossible snapshot size %d", len(results))
			}
			reads++
			runtime.Gosched()
		}
	}
}

func productionChange(index int) IndexChange {
	body := fmt.Sprintf("concurrent snapshot %d", index)
	return IndexChange{
		RelativePath: fmt.Sprintf("document-%03d.md", index),
		Title:        "Concurrent", ContentHash: fmt.Sprintf("%064x", index+1),
		Markdown: body, SearchableText: body, SizeBytes: len(body), MtimeNS: index + 1,
		HasContent: true, ExtractionVersion: 1,
	}
}

func createProductionSearchSchema(t *testing.T, db *DB) {
	t.Helper()
	_, err := db.conn.ExecContext(context.Background(), `
		CREATE TABLE collections (
		  id INTEGER PRIMARY KEY, name TEXT NOT NULL COLLATE NOCASE UNIQUE,
		  root_path TEXT NOT NULL, glob_pattern TEXT NOT NULL, ignore_patterns TEXT NOT NULL,
		  created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '',
		  current_generation INTEGER NOT NULL
		);
		CREATE TABLE collection_contexts (
		  id INTEGER PRIMARY KEY, collection_id INTEGER NOT NULL,
		  path_prefix TEXT NOT NULL, description TEXT NOT NULL
		);
		CREATE TABLE content (
		  hash TEXT PRIMARY KEY, markdown TEXT NOT NULL, searchable_text TEXT NOT NULL,
		  extraction_version INTEGER NOT NULL
		);
		CREATE TABLE documents (
		  id INTEGER PRIMARY KEY, collection_id INTEGER NOT NULL REFERENCES collections(id),
		  relative_path TEXT NOT NULL, title TEXT NOT NULL,
		  content_hash TEXT NOT NULL REFERENCES content(hash), size_bytes INTEGER NOT NULL,
		  mtime_ns INTEGER NOT NULL, active INTEGER NOT NULL, seen_generation INTEGER NOT NULL,
		  created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '',
		  UNIQUE(collection_id, relative_path)
		);
		CREATE VIRTUAL TABLE documents_fts USING fts5(path, title, body);
		CREATE TRIGGER documents_fts_insert AFTER INSERT ON documents WHEN new.active = 1 BEGIN
		  INSERT INTO documents_fts(rowid, path, title, body)
		  SELECT new.id, new.relative_path, new.title, searchable_text FROM content WHERE hash = new.content_hash;
		END;
		CREATE TRIGGER documents_fts_update AFTER UPDATE OF title, content_hash, active ON documents BEGIN
		  DELETE FROM documents_fts WHERE rowid = old.id;
		  INSERT INTO documents_fts(rowid, path, title, body)
		  SELECT new.id, new.relative_path, new.title, searchable_text FROM content
		  WHERE hash = new.content_hash AND new.active = 1;
		END;
		CREATE TRIGGER documents_fts_delete AFTER DELETE ON documents BEGIN
		  DELETE FROM documents_fts WHERE rowid = old.id;
		END;
		INSERT INTO collections(id, name, root_path, glob_pattern, ignore_patterns, current_generation)
		VALUES (1, 'concurrent', '/tmp', '**/*.md', '[]', 1);
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCrashDuringWriteRollsBackUncommittedTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess crash test")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "write-crash.sqlite")
	ready := filepath.Join(directory, "write-started")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.ExecContext(context.Background(), `
		CREATE TABLE crash_documents (id INTEGER PRIMARY KEY, body TEXT NOT NULL);
		INSERT INTO crash_documents(id, body) VALUES (1, 'committed');
	`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	command := crashHelperCommand(t, "write", path, ready)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	registerProcessCleanup(t, command)
	waitForFile(t, ready, 10*time.Second)
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var count int
	if err := reopened.conn.QueryRowContext(context.Background(), "SELECT count(*) FROM crash_documents").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("crash exposed an uncommitted row; count=%d", count)
	}
	assertIntegrity(t, reopened)
}

func TestCrashDuringMigrationRecoversOldSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess crash test")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "migration-crash.sqlite")
	ready := filepath.Join(directory, "migration-before-commit")
	command := crashHelperCommand(t, "migration", path, ready)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	registerProcessCleanup(t, command)
	waitForFile(t, ready, 10*time.Second)
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	version, err := db.CurrentVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != 0 {
		t.Fatalf("crashed migration advanced schema version to %d", version)
	}
	exists, err := db.TableExists("crash_table")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("uncommitted migration table survived process death")
	}
	assertIntegrity(t, db)

	applied, err := db.ApplyMigration(Migration{Version: 1, Name: "recover", SQL: "CREATE TABLE recovered (id INTEGER PRIMARY KEY)"})
	if err != nil || !applied {
		t.Fatalf("migration did not recover after crash: applied=%v err=%v", applied, err)
	}
}

func TestCrashAfterMigrationCommitPreservesNewSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess crash test")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "committed-crash.sqlite")
	ready := filepath.Join(directory, "committed")
	command := crashHelperCommand(t, "committed", path, ready)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	registerProcessCleanup(t, command)
	waitForFile(t, ready, 10*time.Second)
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	version, err := db.CurrentVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("committed migration was lost; version=%d", version)
	}
	exists, err := db.TableExists("committed_table")
	if err != nil || !exists {
		t.Fatalf("committed schema missing: exists=%v err=%v", exists, err)
	}
	assertIntegrity(t, db)
}

func crashHelperCommand(t *testing.T, mode, path, ready string) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestCrashHelperProcess$")
	command.Env = append(os.Environ(),
		"KB_CRASH_HELPER=1",
		"KB_CRASH_MODE="+mode,
		"KB_CRASH_DATABASE="+path,
		"KB_CRASH_READY="+ready,
	)
	return command
}

func registerProcessCleanup(t *testing.T, command *exec.Cmd) {
	t.Helper()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
}

func walSize(path string) int64 {
	info, err := os.Stat(path + "-wal")
	if err != nil {
		return 0
	}
	return info.Size()
}

func waitForWALGrowth(path string, baseline int64) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if walSize(path) > baseline {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	panic("timed out waiting for uncommitted WAL frames")
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func assertIntegrity(t *testing.T, db *DB) {
	t.Helper()
	var result string
	if err := db.conn.QueryRowContext(context.Background(), "PRAGMA integrity_check").Scan(&result); err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Fatalf("integrity_check: %s", result)
	}
}

func TestCrashHelperProcess(t *testing.T) {
	if os.Getenv("KB_CRASH_HELPER") != "1" {
		return
	}
	path := os.Getenv("KB_CRASH_DATABASE")
	db, err := Open(path)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	switch os.Getenv("KB_CRASH_MODE") {
	case "write":
		if _, err := db.conn.ExecContext(context.Background(), "PRAGMA cache_size = 1"); err != nil {
			panic(err)
		}
		baseline := walSize(path)
		if _, err := db.conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
			panic(err)
		}
		if _, err := db.conn.ExecContext(context.Background(), `
			WITH RECURSIVE sequence(value) AS (
			  SELECT 1 UNION ALL SELECT value + 1 FROM sequence WHERE value < 256
			)
			INSERT INTO crash_documents(body) SELECT randomblob(4096) FROM sequence
		`); err != nil {
			panic(err)
		}
		waitForWALGrowth(path, baseline)
		if err := os.WriteFile(os.Getenv("KB_CRASH_READY"), []byte("ready"), 0o600); err != nil {
			panic(err)
		}
		time.Sleep(time.Minute)
	case "migration":
		if _, err := db.conn.ExecContext(context.Background(), "PRAGMA cache_size = 1"); err != nil {
			panic(err)
		}
		baseline := walSize(path)
		beforeMigrationCommit = func() {
			waitForWALGrowth(path, baseline)
			if err := os.WriteFile(os.Getenv("KB_CRASH_READY"), []byte("ready"), 0o600); err != nil {
				panic(err)
			}
			time.Sleep(time.Minute)
		}
		_, err = db.ApplyMigration(Migration{
			Version: 1,
			Name:    "crash in progress",
			SQL: `
				CREATE TABLE crash_table (id INTEGER PRIMARY KEY, payload BLOB);
				WITH RECURSIVE sequence(value) AS (
				  SELECT 1 UNION ALL SELECT value + 1 FROM sequence WHERE value < 256
				)
				INSERT INTO crash_table(payload) SELECT randomblob(4096) FROM sequence;
			`,
		})
		if err != nil {
			panic(err)
		}
	case "committed":
		_, err = db.ApplyMigration(Migration{
			Version: 1,
			Name:    "committed",
			SQL:     "CREATE TABLE committed_table (id INTEGER PRIMARY KEY)",
		})
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(os.Getenv("KB_CRASH_READY"), []byte("ready"), 0o600); err != nil {
			panic(err)
		}
		time.Sleep(time.Minute)
	default:
		panic("unknown crash helper mode")
	}
}
