package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// DocumentMetadata supports no-read change detection during a filesystem scan.
type DocumentMetadata struct {
	ID                int
	RelativePath      string
	ContentHash       string
	Title             string
	SizeBytes         int
	MtimeNS           int
	Active            bool
	ExtractionVersion int
}

// IndexChange is one filesystem observation sent to the SQLite writer.
type IndexChange struct {
	RelativePath      string
	Title             string
	ContentHash       string
	Markdown          string
	SearchableText    string
	SizeBytes         int
	MtimeNS           int
	HasContent        bool
	ExtractionVersion int
}

// BatchStats describes one committed indexing batch.
type BatchStats struct {
	Inserted int
	Updated  int
	Seen     int
}

// ContentLookup returns original and derived content without nullable pointers.
type ContentLookup struct {
	Found          bool
	Markdown       string
	SearchableText string
}

// ListDocumentMetadata returns all known paths, including inactive documents
// that may reappear.
func (db *DB) ListDocumentMetadata(collectionID int) ([]DocumentMetadata, error) {
	rows, err := db.conn.QueryContext(context.Background(), `
		SELECT d.id, d.relative_path, d.content_hash, d.title, d.size_bytes, d.mtime_ns, d.active,
		       content.extraction_version
		FROM documents d
		JOIN content ON content.hash = d.content_hash
		WHERE d.collection_id = ?
		ORDER BY relative_path
	`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	metadata := make([]DocumentMetadata, 0)
	for rows.Next() {
		var item DocumentMetadata
		if err := rows.Scan(
			&item.ID,
			&item.RelativePath,
			&item.ContentHash,
			&item.Title,
			&item.SizeBytes,
			&item.MtimeNS,
			&item.Active,
			&item.ExtractionVersion,
		); err != nil {
			return nil, err
		}
		metadata = append(metadata, item)
	}
	return metadata, rows.Err()
}

// LookupDocumentContent returns stored source and derived text for an active path.
func (db *DB) LookupDocumentContent(collectionID int, relativePath string) (ContentLookup, error) {
	var content ContentLookup
	err := db.conn.QueryRowContext(context.Background(), `
		SELECT content.markdown, content.searchable_text
		FROM documents
		JOIN content ON content.hash = documents.content_hash
		WHERE documents.collection_id = ? AND documents.relative_path = ? AND documents.active = 1
	`, collectionID, relativePath).Scan(&content.Markdown, &content.SearchableText)
	if err == sql.ErrNoRows {
		return ContentLookup{}, nil
	}
	if err != nil {
		return ContentLookup{}, err
	}
	content.Found = true
	return content, nil
}

// BeginUpdate advances and returns the generation owned by a new scan.
func (db *DB) BeginUpdate(collectionID int) (int, error) {
	var generation int
	err := db.conn.QueryRowContext(context.Background(), `
		UPDATE collections
		SET current_generation = current_generation + 1,
		    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?
		RETURNING current_generation
	`, collectionID).Scan(&generation)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("collection %d does not exist", collectionID)
	}
	return generation, err
}

func verifyGeneration(tx *sql.Tx, collectionID, generation int) error {
	var current int
	if err := tx.QueryRow("SELECT current_generation FROM collections WHERE id = ?", collectionID).Scan(&current); err != nil {
		return err
	}
	if current != generation {
		return fmt.Errorf("update generation %d was superseded by generation %d", generation, current)
	}
	return nil
}

// ApplyIndexBatch applies observations in one transaction. Metadata-only
// observations never touch indexed fields unless an inactive document reappears.
func (db *DB) ApplyIndexBatch(collectionID, generation int, changes []IndexChange) (BatchStats, error) {
	tx, err := db.conn.BeginTx(context.Background(), nil)
	if err != nil {
		return BatchStats{}, err
	}
	defer tx.Rollback()
	if err := verifyGeneration(tx, collectionID, generation); err != nil {
		return BatchStats{}, err
	}

	stats := BatchStats{}
	for _, change := range changes {
		if change.HasContent && len(change.Markdown) > MaxDocumentBytes {
			return BatchStats{}, fmt.Errorf("document %q exceeds %d-byte store limit", change.RelativePath, MaxDocumentBytes)
		}
		var existing DocumentMetadata
		err := tx.QueryRow(`
			SELECT d.id, d.relative_path, d.content_hash, d.title, d.size_bytes, d.mtime_ns, d.active,
			       content.extraction_version
			FROM documents d
			JOIN content ON content.hash = d.content_hash
			WHERE d.collection_id = ? AND d.relative_path = ?
		`, collectionID, change.RelativePath).Scan(
			&existing.ID,
			&existing.RelativePath,
			&existing.ContentHash,
			&existing.Title,
			&existing.SizeBytes,
			&existing.MtimeNS,
			&existing.Active,
			&existing.ExtractionVersion,
		)
		exists := err == nil
		if err != nil && err != sql.ErrNoRows {
			return BatchStats{}, err
		}

		if !change.HasContent {
			if !exists {
				return BatchStats{}, fmt.Errorf("metadata-only observation for unknown path %q", change.RelativePath)
			}
			statement := "UPDATE documents SET seen_generation = ? WHERE id = ?"
			if !existing.Active {
				statement = `
					UPDATE documents
					SET seen_generation = ?, active = 1,
					    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
					WHERE id = ?
				`
			}
			result, err := tx.Exec(statement, generation, existing.ID)
			if err != nil {
				return BatchStats{}, err
			}
			if changed, _ := result.RowsAffected(); changed != 1 {
				return BatchStats{}, fmt.Errorf("metadata observation did not update %q", change.RelativePath)
			}
			if existing.Active {
				stats.Seen++
			} else {
				stats.Updated++
			}
			continue
		}

		if _, err := tx.Exec(`
			INSERT INTO content(hash, markdown, searchable_text, extraction_version)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(hash) DO UPDATE SET
				searchable_text = excluded.searchable_text,
				extraction_version = excluded.extraction_version
			WHERE excluded.extraction_version > content.extraction_version
		`, change.ContentHash, change.Markdown, change.SearchableText, change.ExtractionVersion); err != nil {
			return BatchStats{}, err
		}

		if exists && existing.Active &&
			existing.ContentHash == change.ContentHash && existing.Title == change.Title &&
			existing.SizeBytes == change.SizeBytes && existing.MtimeNS == change.MtimeNS &&
			existing.ExtractionVersion == change.ExtractionVersion {
			if _, err := tx.Exec("UPDATE documents SET seen_generation = ? WHERE id = ?", generation, existing.ID); err != nil {
				return BatchStats{}, err
			}
			stats.Seen++
			continue
		}

		if exists {
			if _, err := tx.Exec(`
				UPDATE documents
				SET title = ?, content_hash = ?, size_bytes = ?, mtime_ns = ?,
				    active = 1, seen_generation = ?,
				    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				WHERE id = ?
			`, change.Title, change.ContentHash, change.SizeBytes, change.MtimeNS, generation, existing.ID); err != nil {
				return BatchStats{}, err
			}
			stats.Updated++
		} else {
			if _, err := tx.Exec(`
				INSERT INTO documents(
					collection_id, relative_path, title, content_hash,
					size_bytes, mtime_ns, active, seen_generation
				) VALUES (?, ?, ?, ?, ?, ?, 1, ?)
			`, collectionID, change.RelativePath, change.Title, change.ContentHash, change.SizeBytes, change.MtimeNS, generation); err != nil {
				return BatchStats{}, err
			}
			stats.Inserted++
		}
	}

	if err := tx.Commit(); err != nil {
		return BatchStats{}, err
	}
	return stats, nil
}

// FinalizeUpdate deactivates paths not observed by a successful scan.
func (db *DB) FinalizeUpdate(collectionID, generation int) (int, error) {
	tx, err := db.conn.BeginTx(context.Background(), nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := verifyGeneration(tx, collectionID, generation); err != nil {
		return 0, err
	}
	result, err := tx.Exec(`
		UPDATE documents
		SET active = 0, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE collection_id = ? AND active = 1 AND seen_generation <> ?
	`, collectionID, generation)
	if err != nil {
		return 0, err
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(removed), nil
}
