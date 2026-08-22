package sqlite

import "context"

// Collection is a directory registered in the knowledge base.
type Collection struct {
	ID             int
	Name           string
	RootPath       string
	GlobPattern    string
	IgnorePatterns string
	CreatedAt      string
	UpdatedAt      string
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

// InsertCollection creates and returns a collection.
func (db *DB) InsertCollection(name, rootPath, globPattern, ignorePatterns string) (Collection, error) {
	row := db.conn.QueryRowContext(context.Background(), `
		INSERT INTO collections(name, root_path, glob_pattern, ignore_patterns)
		VALUES (?, ?, ?, ?)
		RETURNING id, name, root_path, glob_pattern, ignore_patterns, created_at, updated_at
	`, name, rootPath, globPattern, ignorePatterns)

	var collection Collection
	err := row.Scan(
		&collection.ID,
		&collection.Name,
		&collection.RootPath,
		&collection.GlobPattern,
		&collection.IgnorePatterns,
		&collection.CreatedAt,
		&collection.UpdatedAt,
	)
	return collection, err
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
		var collection Collection
		if err := rows.Scan(
			&collection.ID,
			&collection.Name,
			&collection.RootPath,
			&collection.GlobPattern,
			&collection.IgnorePatterns,
			&collection.CreatedAt,
			&collection.UpdatedAt,
		); err != nil {
			return nil, err
		}
		collections = append(collections, collection)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return collections, nil
}

// InsertDocument atomically stores content and its document metadata. FTS5 is
// maintained by schema triggers in the same transaction.
func (db *DB) InsertDocument(collectionID int, relativePath, title, hash, markdown, searchableText string, sizeBytes int, mtimeNS int) (DocumentRecord, error) {
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

// DeleteCollection removes a collection. Foreign keys cascade through its
// documents, whose delete trigger removes corresponding FTS5 rows.
func (db *DB) DeleteCollection(id int) error {
	_, err := db.conn.ExecContext(context.Background(), "DELETE FROM collections WHERE id = ?", id)
	return err
}
