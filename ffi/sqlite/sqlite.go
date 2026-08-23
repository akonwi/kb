// Package sqlite provides the narrow SQLite boundary used by kb.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// DB owns one SQLite connection pool.
type DB struct {
	handle    *sql.DB
	conn      *sql.Conn
	closeOnce sync.Once
	closeErr  error
}

// SearchResult is one BM25-ranked FTS5 match.
type SearchResult struct {
	ID    int
	Path  string
	Title string
	Rank  float64
}

func isMemoryDatabase(path string) bool {
	if path == ":memory:" {
		return true
	}
	parsed, err := url.Parse(path)
	if err != nil || !strings.EqualFold(parsed.Scheme, "file") {
		return false
	}
	return strings.EqualFold(parsed.Query().Get("mode"), "memory") || parsed.Opaque == ":memory:" || parsed.Path == ":memory:"
}

// Open opens SQLite and verifies the connection before returning it.
func Open(path string) (*DB, error) {
	handle, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	conn, err := handle.Conn(context.Background())
	if err != nil {
		handle.Close()
		return nil, err
	}
	if _, err := conn.ExecContext(context.Background(), "PRAGMA busy_timeout = 5000"); err != nil {
		conn.Close()
		handle.Close()
		return nil, fmt.Errorf("configure busy timeout: %w", err)
	}
	if err := conn.PingContext(context.Background()); err != nil {
		conn.Close()
		handle.Close()
		return nil, fmt.Errorf("ping SQLite: %w", err)
	}
	var journalMode string
	if err := conn.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		conn.Close()
		handle.Close()
		return nil, fmt.Errorf("read journal mode: %w", err)
	}
	if !isMemoryDatabase(path) && !strings.EqualFold(journalMode, "wal") {
		if err := conn.QueryRowContext(context.Background(), "PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil {
			conn.Close()
			handle.Close()
			return nil, err
		}
		if !strings.EqualFold(journalMode, "wal") {
			conn.Close()
			handle.Close()
			return nil, fmt.Errorf("SQLite WAL mode is required for %q; filesystem returned %q", path, journalMode)
		}
	}
	if _, err := conn.ExecContext(context.Background(), "PRAGMA synchronous = FULL"); err != nil {
		conn.Close()
		handle.Close()
		return nil, fmt.Errorf("configure synchronous mode: %w", err)
	}
	if _, err := conn.ExecContext(context.Background(), "PRAGMA cache_size = -20000"); err != nil {
		conn.Close()
		handle.Close()
		return nil, fmt.Errorf("configure page cache: %w", err)
	}
	if _, err := conn.ExecContext(context.Background(), "PRAGMA mmap_size = 268435456"); err != nil {
		conn.Close()
		handle.Close()
		return nil, fmt.Errorf("configure mmap size: %w", err)
	}
	if _, err := conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); err != nil {
		conn.Close()
		handle.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	return &DB{handle: handle, conn: conn}, nil
}

// Close releases the dedicated connection and its pool.
func (db *DB) Close() error {
	db.closeOnce.Do(func() {
		db.closeErr = errors.Join(db.conn.Close(), db.handle.Close())
	})
	return db.closeErr
}

// Search returns ranked matches from the trigger-maintained production index.
// It is retained as a narrow assertion helper for indexing and migration tests;
// application search uses SearchIndex.
func (db *DB) Search(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		return []SearchResult{}, nil
	}
	rows, err := db.conn.QueryContext(context.Background(), `
		SELECT rowid, path, title, bm25(documents_fts, 1.0, 5.0, 1.0) AS rank
		FROM documents_fts
		WHERE documents_fts MATCH ?
		ORDER BY rank ASC, rowid ASC
		LIMIT ?
	`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]SearchResult, 0, limit)
	for rows.Next() {
		var result SearchResult
		if err := rows.Scan(&result.ID, &result.Path, &result.Title, &result.Rank); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
