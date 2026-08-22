// Package sqlite provides the narrow SQLite boundary used by kb.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// Document is the searchable representation inserted into FTS5.
type Document struct {
	Path  string
	Title string
	Body  string
}

// SearchResult is one BM25-ranked FTS5 match.
type SearchResult struct {
	ID    int
	Path  string
	Title string
	Rank  float64
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
	if err := conn.PingContext(context.Background()); err != nil {
		conn.Close()
		handle.Close()
		return nil, err
	}
	if _, err := conn.ExecContext(context.Background(), "PRAGMA busy_timeout = 5000"); err != nil {
		conn.Close()
		handle.Close()
		return nil, err
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

// FTS5Enabled reports whether this SQLite build includes FTS5.
func (db *DB) FTS5Enabled() (bool, error) {
	var enabled int
	err := db.conn.QueryRowContext(context.Background(), "SELECT sqlite_compileoption_used('ENABLE_FTS5')").Scan(&enabled)
	return enabled == 1, err
}

// CreateIndex creates the representative FTS5 schema used by the spike.
func (db *DB) CreateIndex() error {
	_, err := db.conn.ExecContext(context.Background(), `
		CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(
			path,
			title,
			body,
			tokenize = 'porter unicode61'
		)
	`)
	return err
}

// InsertDocuments inserts a batch with one prepared statement and transaction.
func (db *DB) InsertDocuments(documents []Document) error {
	tx, err := db.conn.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	insert, err := tx.Prepare("INSERT INTO documents_fts(path, title, body) VALUES (?, ?, ?)")
	if err != nil {
		return err
	}
	defer insert.Close()

	for _, document := range documents {
		if _, err := insert.Exec(document.Path, document.Title, document.Body); err != nil {
			return fmt.Errorf("insert %q: %w", document.Path, err)
		}
	}

	return tx.Commit()
}

// Search returns the strongest matches first. FTS5 BM25 ranks lower values as
// more relevant, so the SQL ordering is ascending.
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
