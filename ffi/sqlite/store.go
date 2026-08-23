package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// MaxDocumentBytes bounds original content retained by the store.
const MaxDocumentBytes = 32 * 1024 * 1024

// Collection is a directory registered in the knowledge base.
type Collection struct {
	ID             int
	Name           string
	RootPath       string
	GlobPattern    string
	IgnorePatterns []string
	CreatedAt      string
	UpdatedAt      string
}

// CollectionLookup avoids a nullable foreign pointer at the Ard boundary.
type CollectionLookup struct {
	Found      bool
	Collection Collection
}

// DocumentRecord is a document persisted in the core schema.
type DocumentRecord struct {
	ID           int
	CollectionID int
	RelativePath string
	Title        string
	ContentHash  string
	SizeBytes    int
	MtimeNS      int
}

func scanCollection(scanner interface{ Scan(...any) error }) (Collection, error) {
	var collection Collection
	var ignoreJSON string
	err := scanner.Scan(
		&collection.ID,
		&collection.Name,
		&collection.RootPath,
		&collection.GlobPattern,
		&ignoreJSON,
		&collection.CreatedAt,
		&collection.UpdatedAt,
	)
	if err != nil {
		return Collection{}, err
	}
	if err := json.Unmarshal([]byte(ignoreJSON), &collection.IgnorePatterns); err != nil {
		return Collection{}, fmt.Errorf("decode ignore patterns for %q: %w", collection.Name, err)
	}
	return collection, nil
}

func validateCollectionName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("collection name must be nonempty and contain no path separators")
	}
	return name, nil
}

// InsertCollection creates and returns a collection.
func (db *DB) InsertCollection(name, rootPath, globPattern string, ignorePatterns []string) (Collection, error) {
	name, err := validateCollectionName(name)
	if err != nil {
		return Collection{}, err
	}
	ignoreJSON, err := json.Marshal(ignorePatterns)
	if err != nil {
		return Collection{}, err
	}
	row := db.conn.QueryRowContext(context.Background(), `
		INSERT INTO collections(name, root_path, glob_pattern, ignore_patterns)
		VALUES (?, ?, ?, ?)
		RETURNING id, name, root_path, glob_pattern, ignore_patterns, created_at, updated_at
	`, name, rootPath, globPattern, string(ignoreJSON))
	return scanCollection(row)
}

// ListCollections returns collections in stable name order.
func (db *DB) ListCollections() ([]Collection, error) {
	rows, err := db.conn.QueryContext(context.Background(), `
		SELECT id, name, root_path, glob_pattern, ignore_patterns, created_at, updated_at
		FROM collections
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	collections := make([]Collection, 0)
	for rows.Next() {
		collection, err := scanCollection(rows)
		if err != nil {
			return nil, err
		}
		collections = append(collections, collection)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return collections, nil
}

// LookupCollection finds a collection by its case-insensitive name.
func (db *DB) LookupCollection(name string) (CollectionLookup, error) {
	row := db.conn.QueryRowContext(context.Background(), `
		SELECT id, name, root_path, glob_pattern, ignore_patterns, created_at, updated_at
		FROM collections
		WHERE name = ? COLLATE NOCASE
	`, name)
	collection, err := scanCollection(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return CollectionLookup{}, nil
		}
		return CollectionLookup{}, err
	}
	return CollectionLookup{Found: true, Collection: collection}, nil
}

// DeleteCollectionByName removes a collection and its cascaded documents.
func (db *DB) DeleteCollectionByName(name string) (bool, error) {
	result, err := db.conn.ExecContext(context.Background(), "DELETE FROM collections WHERE name = ? COLLATE NOCASE", name)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed > 0, err
}

// InsertDocument atomically stores content and its document metadata. FTS5 is
// maintained by schema triggers in the same transaction.
func (db *DB) InsertDocument(collectionID int, relativePath, title, hash, markdown, searchableText string, sizeBytes int, mtimeNS int) (DocumentRecord, error) {
	if len(markdown) > MaxDocumentBytes {
		return DocumentRecord{}, fmt.Errorf("document exceeds %d-byte store limit", MaxDocumentBytes)
	}
	tx, err := db.conn.BeginTx(context.Background(), nil)
	if err != nil {
		return DocumentRecord{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(context.Background(), `
		INSERT INTO content(hash, markdown, searchable_text)
		VALUES (?, ?, ?)
		ON CONFLICT(hash) DO NOTHING
	`, hash, markdown, searchableText); err != nil {
		return DocumentRecord{}, err
	}

	row := tx.QueryRowContext(context.Background(), `
		INSERT INTO documents(collection_id, relative_path, title, content_hash, size_bytes, mtime_ns)
		VALUES (?, ?, ?, ?, ?, ?)
		RETURNING id, collection_id, relative_path, title, content_hash, size_bytes, mtime_ns
	`, collectionID, relativePath, title, hash, sizeBytes, mtimeNS)
	var document DocumentRecord
	if err := row.Scan(
		&document.ID,
		&document.CollectionID,
		&document.RelativePath,
		&document.Title,
		&document.ContentHash,
		&document.SizeBytes,
		&document.MtimeNS,
	); err != nil {
		return DocumentRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return DocumentRecord{}, err
	}
	return document, nil
}

// DeleteCollection removes a collection by ID.
func (db *DB) DeleteCollection(id int) error {
	_, err := db.conn.ExecContext(context.Background(), "DELETE FROM collections WHERE id = ?", id)
	return err
}
