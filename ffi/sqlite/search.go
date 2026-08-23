package sqlite

import (
	"context"
	"fmt"
	"strings"
)

// IndexSearchRow is one ranked production-index match with retrieval metadata.
const maxSearchMarkdownBytes = 64 * 1024 * 1024

type IndexSearchRow struct {
	DocumentID   int
	ID           string
	VirtualPath  string
	CollectionID int
	Collection   string
	RelativePath string
	Title        string
	ContentHash  string
	Markdown     string
	Snippet      string
	Context      string
	ModifiedAt   string
	Rank         float64
}

// SearchIndex searches active documents. The query is an already-safe FTS5
// expression constructed by the Ard search layer.
func (db *DB) SearchIndex(query string, collectionIDs []int, limit int) ([]IndexSearchRow, error) {
	if limit <= 0 {
		return []IndexSearchRow{}, nil
	}
	if limit > 100 {
		return nil, fmt.Errorf("search limit %d exceeds maximum 100", limit)
	}
	var filter strings.Builder
	args := make([]any, 0, len(collectionIDs)+2)
	args = append(args, query)
	if len(collectionIDs) > 0 {
		filter.WriteString(" AND documents.collection_id IN (")
		for index, id := range collectionIDs {
			if index > 0 {
				filter.WriteByte(',')
			}
			filter.WriteByte('?')
			args = append(args, id)
		}
		filter.WriteByte(')')
	}
	args = append(args, limit)

	statement := fmt.Sprintf(`
		WITH ranked AS (
			SELECT documents_fts.rowid AS document_id,
			       bm25(documents_fts, 1.5, 5.0, 1.0) AS rank,
			       snippet(documents_fts, -1, '[', ']', ' … ', 24) AS snippet
			FROM documents_fts
			JOIN documents ON documents.id = documents_fts.rowid
			WHERE documents_fts MATCH ? AND documents.active = 1%s
			ORDER BY rank ASC, documents_fts.rowid ASC
			LIMIT ?
		)
		SELECT documents.id,
		       collections.id,
		       collections.name,
		       documents.relative_path,
		       documents.title,
		       documents.content_hash,
		       content.markdown,
		       ranked.snippet,
		       COALESCE((
			       SELECT collection_contexts.description
			       FROM collection_contexts
			       WHERE collection_contexts.collection_id = documents.collection_id
			         AND (
			           collection_contexts.path_prefix = ''
			           OR documents.relative_path = collection_contexts.path_prefix COLLATE BINARY
			           OR (
			             substr(documents.relative_path, 1, length(collection_contexts.path_prefix)) = collection_contexts.path_prefix COLLATE BINARY
			             AND substr(documents.relative_path, length(collection_contexts.path_prefix) + 1, 1) = '/'
			           )
			         )
			       ORDER BY length(collection_contexts.path_prefix) DESC
			       LIMIT 1
		       ), ''),
		       documents.updated_at,
		       ranked.rank
		FROM ranked
		JOIN documents ON documents.id = ranked.document_id
		JOIN collections ON collections.id = documents.collection_id
		JOIN content ON content.hash = documents.content_hash
		ORDER BY ranked.rank ASC, documents.id ASC
	`, filter.String())

	rows, err := db.conn.QueryContext(context.Background(), statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]IndexSearchRow, 0, limit)
	totalMarkdownBytes := 0
	for rows.Next() {
		var result IndexSearchRow
		if err := rows.Scan(
			&result.DocumentID,
			&result.CollectionID,
			&result.Collection,
			&result.RelativePath,
			&result.Title,
			&result.ContentHash,
			&result.Markdown,
			&result.Snippet,
			&result.Context,
			&result.ModifiedAt,
			&result.Rank,
		); err != nil {
			return nil, err
		}
		totalMarkdownBytes += len(result.Markdown)
		if totalMarkdownBytes > maxSearchMarkdownBytes {
			return nil, fmt.Errorf("search results exceed %d bytes of original content", maxSearchMarkdownBytes)
		}
		result.ID = ShortContentID(result.ContentHash)
		result.VirtualPath = result.Collection + "/" + result.RelativePath
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// UpsertCollectionContext records descriptive context for a path prefix.
func (db *DB) UpsertCollectionContext(collectionID int, pathPrefix, description string) error {
	_, err := db.conn.ExecContext(context.Background(), `
		INSERT INTO collection_contexts(collection_id, path_prefix, description)
		VALUES (?, ?, ?)
		ON CONFLICT(collection_id, path_prefix)
		DO UPDATE SET description = excluded.description
	`, collectionID, pathPrefix, description)
	return err
}
