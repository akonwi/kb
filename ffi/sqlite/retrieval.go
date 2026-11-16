package sqlite

import (
	"context"
	"database/sql"
)

// RetrievedDocument contains original content and its canonical stored metadata.
type RetrievedDocument struct {
	ContentHash  string
	Collection   string
	RelativePath string
	VirtualPath  string
	Title        string
	Markdown     string
	SizeBytes    int
	ModifiedAt   string
}

func scanRetrieved(scanner interface{ Scan(...any) error }) (RetrievedDocument, error) {
	var document RetrievedDocument
	err := scanner.Scan(
		&document.ContentHash,
		&document.Collection,
		&document.RelativePath,
		&document.Title,
		&document.Markdown,
		&document.SizeBytes,
		&document.ModifiedAt,
	)
	if err != nil {
		return RetrievedDocument{}, err
	}
	document.VirtualPath = document.Collection + "/" + document.RelativePath
	return document, nil
}

// LookupDocumentByPath retrieves one active document by collection and
// relative path. found is false when no active document has that path.
func (db *DB) LookupDocumentByPath(collection, relativePath string) (document RetrievedDocument, found bool, err error) {
	document, err = scanRetrieved(db.conn.QueryRowContext(context.Background(), `
		SELECT content.hash, collections.name, documents.relative_path, documents.title,
		       content.markdown, documents.size_bytes, documents.updated_at
		FROM documents
		JOIN collections ON collections.id = documents.collection_id
		JOIN content ON content.hash = documents.content_hash
		WHERE collections.name = ? COLLATE NOCASE
		  AND documents.relative_path = ? COLLATE BINARY
		  AND documents.active = 1
	`, collection, relativePath))
	if err == sql.ErrNoRows {
		return RetrievedDocument{}, false, nil
	}
	if err != nil {
		return RetrievedDocument{}, false, err
	}
	return document, true, nil
}

// LookupDocumentByContentID retrieves content by a validated, lowercase hash
// prefix. Duplicate paths with the same complete hash are aliases, while a
// prefix that identifies multiple complete hashes is ambiguous. matches is 0
// when nothing matches, 1 when document holds the match, and 2 when the
// prefix is ambiguous.
func (db *DB) LookupDocumentByContentID(prefix string) (document RetrievedDocument, matches int, err error) {
	rows, err := db.conn.QueryContext(context.Background(), `
		SELECT DISTINCT content_hash
		FROM documents
		WHERE active = 1 AND content_hash LIKE ? || '%'
		ORDER BY content_hash
		LIMIT 2
	`, prefix)
	if err != nil {
		return RetrievedDocument{}, 0, err
	}
	defer rows.Close()
	hashes := make([]string, 0, 2)
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return RetrievedDocument{}, 0, err
		}
		hashes = append(hashes, hash)
	}
	if err := rows.Err(); err != nil {
		return RetrievedDocument{}, 0, err
	}
	if len(hashes) != 1 {
		return RetrievedDocument{}, len(hashes), nil
	}

	document, err = scanRetrieved(db.conn.QueryRowContext(context.Background(), `
		SELECT content.hash, collections.name, documents.relative_path, documents.title,
		       content.markdown, documents.size_bytes, documents.updated_at
		FROM documents
		JOIN collections ON collections.id = documents.collection_id
		JOIN content ON content.hash = documents.content_hash
		WHERE documents.active = 1 AND documents.content_hash = ?
		ORDER BY collections.name, documents.relative_path
		LIMIT 1
	`, hashes[0]))
	if err != nil {
		return RetrievedDocument{}, 0, err
	}
	return document, 1, nil
}
