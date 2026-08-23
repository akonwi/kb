package sqlite

import (
	"context"
	"fmt"
	"os"
)

// DatabaseStatus summarizes indexed state without scanning document content.
type DatabaseStatus struct {
	Collections       int
	ActiveDocuments   int
	InactiveDocuments int
	ContentRows       int
	ContentBytes      int
	FTSRows           int
}

// CollectionStatus summarizes one collection.
type CollectionStatus struct {
	Name              string
	RootPath          string
	ActiveDocuments   int
	InactiveDocuments int
	IndexedBytes      int
	UpdatedAt         string
}

// Status contains global and per-collection counts.
type Status struct {
	Database    DatabaseStatus
	Collections []CollectionStatus
}

// ReadStatus returns maintenance counters in stable collection-name order.
func (db *DB) ReadStatus() (Status, error) {
	var status Status
	err := db.conn.QueryRowContext(context.Background(), `
		SELECT
		  (SELECT count(*) FROM collections),
		  (SELECT count(*) FROM documents WHERE active = 1),
		  (SELECT count(*) FROM documents WHERE active = 0),
		  (SELECT count(*) FROM content),
		  (SELECT COALESCE(sum(length(markdown)), 0) FROM content),
		  (SELECT count(*) FROM documents_fts)
	`).Scan(
		&status.Database.Collections,
		&status.Database.ActiveDocuments,
		&status.Database.InactiveDocuments,
		&status.Database.ContentRows,
		&status.Database.ContentBytes,
		&status.Database.FTSRows,
	)
	if err != nil {
		return Status{}, err
	}
	rows, err := db.conn.QueryContext(context.Background(), `
		SELECT collections.name, collections.root_path,
		       count(documents.id) FILTER (WHERE documents.active = 1),
		       count(documents.id) FILTER (WHERE documents.active = 0),
		       COALESCE(sum(documents.size_bytes) FILTER (WHERE documents.active = 1), 0),
		       collections.updated_at
		FROM collections
		LEFT JOIN documents ON documents.collection_id = collections.id
		GROUP BY collections.id
		ORDER BY collections.name
	`)
	if err != nil {
		return Status{}, err
	}
	defer rows.Close()
	status.Collections = make([]CollectionStatus, 0)
	for rows.Next() {
		var item CollectionStatus
		if err := rows.Scan(
			&item.Name,
			&item.RootPath,
			&item.ActiveDocuments,
			&item.InactiveDocuments,
			&item.IndexedBytes,
			&item.UpdatedAt,
		); err != nil {
			return Status{}, err
		}
		status.Collections = append(status.Collections, item)
	}
	return status, rows.Err()
}

// CleanupStats describes safely removed stale rows.
type CleanupStats struct {
	InactiveDocuments int
	OrphanedContent   int
	Vacuumed          bool
	Warning           string
}

// Cleanup removes inactive documents and unreferenced content. Vacuum is
// optional because it requires an exclusive operation and may be expensive.
func (db *DB) Cleanup(vacuum bool) (CleanupStats, error) {
	tx, err := db.conn.BeginTx(context.Background(), nil)
	if err != nil {
		return CleanupStats{}, err
	}
	defer tx.Rollback()
	var stats CleanupStats
	result, err := tx.ExecContext(context.Background(), "DELETE FROM documents WHERE active = 0")
	if err != nil {
		return CleanupStats{}, err
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return CleanupStats{}, err
	}
	stats.InactiveDocuments = int(removed)
	result, err = tx.ExecContext(context.Background(), `
		DELETE FROM content
		WHERE NOT EXISTS (SELECT 1 FROM documents WHERE documents.content_hash = content.hash)
	`)
	if err != nil {
		return CleanupStats{}, err
	}
	removed, err = result.RowsAffected()
	if err != nil {
		return CleanupStats{}, err
	}
	stats.OrphanedContent = int(removed)
	if _, err := tx.ExecContext(context.Background(), "INSERT INTO documents_fts(documents_fts) VALUES('optimize')"); err != nil {
		return CleanupStats{}, err
	}
	if err := tx.Commit(); err != nil {
		return CleanupStats{}, err
	}
	if vacuum {
		if _, err := db.conn.ExecContext(context.Background(), "VACUUM"); err != nil {
			stats.Warning = "cleanup committed but vacuum failed: " + err.Error()
			return stats, nil
		}
		stats.Vacuumed = true
	}
	return stats, nil
}

// DoctorCheck is one independently reported diagnostic.
type DoctorCheck struct {
	Name   string
	OK     bool
	Detail string
}

// Doctor validates SQLite, relational, FTS, and collection-root health.
func (db *DB) Doctor() ([]DoctorCheck, error) {
	checks := make([]DoctorCheck, 0)
	var quick string
	if err := db.conn.QueryRowContext(context.Background(), "PRAGMA quick_check").Scan(&quick); err != nil {
		return nil, err
	}
	checks = append(checks, DoctorCheck{Name: "sqlite", OK: quick == "ok", Detail: quick})

	rows, err := db.conn.QueryContext(context.Background(), "PRAGMA foreign_key_check")
	if err != nil {
		return nil, err
	}
	foreignProblems := 0
	for rows.Next() {
		foreignProblems++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	checks = append(checks, DoctorCheck{
		Name: "foreign keys", OK: foreignProblems == 0,
		Detail: fmt.Sprintf("%d violation(s)", foreignProblems),
	})

	var active, indexed int
	if err := db.conn.QueryRowContext(context.Background(), `
		SELECT (SELECT count(*) FROM documents WHERE active = 1),
		       (SELECT count(*) FROM documents_fts)
	`).Scan(&active, &indexed); err != nil {
		return nil, err
	}
	var mismatched int
	if err := db.conn.QueryRowContext(context.Background(), `
		SELECT count(*) FROM (
		  SELECT documents.id
		  FROM documents
		  JOIN content ON content.hash = documents.content_hash
		  LEFT JOIN documents_fts ON documents_fts.rowid = documents.id
		  WHERE documents.active = 1 AND (
		    documents_fts.rowid IS NULL OR documents_fts.path IS NOT documents.relative_path
		    OR documents_fts.title IS NOT documents.title OR documents_fts.body IS NOT content.searchable_text
		  )
		  UNION ALL
		  SELECT documents_fts.rowid
		  FROM documents_fts
		  LEFT JOIN documents ON documents.id = documents_fts.rowid AND documents.active = 1
		  WHERE documents.id IS NULL
		)
	`).Scan(&mismatched); err != nil {
		return nil, err
	}
	ftsOK := active == indexed && mismatched == 0
	integrityDetail := "integrity check passed"
	if _, err := db.conn.ExecContext(context.Background(), "INSERT INTO documents_fts(documents_fts) VALUES('integrity-check')"); err != nil {
		ftsOK = false
		integrityDetail = "integrity check failed: " + err.Error()
	}
	checks = append(checks, DoctorCheck{
		Name: "full-text index", OK: ftsOK,
		Detail: fmt.Sprintf("%d active document(s), %d FTS row(s), %d mismatch(es); %s", active, indexed, mismatched, integrityDetail),
	})

	collections, err := db.ListCollections()
	if err != nil {
		return nil, err
	}
	for _, collection := range collections {
		info, statErr := os.Stat(collection.RootPath)
		ok := statErr == nil && info.IsDir()
		detail := collection.RootPath
		if statErr != nil {
			detail = statErr.Error()
		} else if !info.IsDir() {
			detail = "not a directory: " + collection.RootPath
		}
		checks = append(checks, DoctorCheck{Name: "collection " + collection.Name, OK: ok, Detail: detail})
	}
	return checks, nil
}
