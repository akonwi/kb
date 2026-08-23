package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
)

// Migration is one ordered schema change owned by the Ard store module.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// beforeMigrationCommit is a package-private test seam used by subprocess
// crash tests to stop at a deterministic transaction boundary.
var beforeMigrationCommit func()

func migrationChecksum(migration Migration) string {
	sum := sha256.Sum256([]byte(migration.SQL))
	return hex.EncodeToString(sum[:])
}

func (db *DB) ensureMigrationTable() error {
	_, err := db.conn.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			checksum TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		)
	`)
	return err
}

// CurrentVersion returns the greatest applied migration version, or zero for a
// new database.
func (db *DB) CurrentVersion() (int, error) {
	if err := db.ensureMigrationTable(); err != nil {
		return 0, err
	}
	var version int
	err := db.conn.QueryRowContext(
		context.Background(),
		"SELECT COALESCE(MAX(version), 0) FROM schema_migrations",
	).Scan(&version)
	return version, err
}

// ApplyMigration atomically executes and records one migration. Ard owns the
// ordered plan and calls this method once for each migration.
func (db *DB) ApplyMigration(migration Migration) (applied bool, err error) {
	if err := db.ensureMigrationTable(); err != nil {
		return false, fmt.Errorf("initialize migration history: %w", err)
	}

	// Acquire the write lock before inspecting history so concurrent processes
	// cannot both decide that the same migration is pending.
	if _, err := db.conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		return false, fmt.Errorf("begin migration %d %q: %w", migration.Version, migration.Name, err)
	}
	committed := false
	defer func() {
		if !committed {
			_, rollbackErr := db.conn.ExecContext(context.Background(), "ROLLBACK")
			if err == nil && rollbackErr != nil {
				err = fmt.Errorf("roll back migration %d %q: %w", migration.Version, migration.Name, rollbackErr)
			}
		}
	}()

	checksum := migrationChecksum(migration)
	var existingName string
	var existingChecksum string
	err = db.conn.QueryRowContext(
		context.Background(),
		"SELECT name, checksum FROM schema_migrations WHERE version = ?",
		migration.Version,
	).Scan(&existingName, &existingChecksum)
	switch {
	case err == nil && existingName == migration.Name && existingChecksum == checksum:
		if _, err := db.conn.ExecContext(context.Background(), "ROLLBACK"); err != nil {
			return false, fmt.Errorf("finish existing migration check: %w", err)
		}
		committed = true
		return false, nil
	case err == nil:
		return false, fmt.Errorf(
			"migration version %d differs from recorded migration %q",
			migration.Version,
			existingName,
		)
	case err != sql.ErrNoRows:
		return false, fmt.Errorf("inspect migration %d %q: %w", migration.Version, migration.Name, err)
	}

	if _, err := db.conn.ExecContext(context.Background(), migration.SQL); err != nil {
		return false, fmt.Errorf("apply migration %d %q: %w", migration.Version, migration.Name, err)
	}
	if _, err := db.conn.ExecContext(
		context.Background(),
		"INSERT INTO schema_migrations(version, name, checksum) VALUES (?, ?, ?)",
		migration.Version,
		migration.Name,
		checksum,
	); err != nil {
		return false, fmt.Errorf("record migration %d %q: %w", migration.Version, migration.Name, err)
	}
	if beforeMigrationCommit != nil {
		beforeMigrationCommit()
	}
	if _, err := db.conn.ExecContext(context.Background(), "COMMIT"); err != nil {
		return false, fmt.Errorf("commit migration %d %q: %w", migration.Version, migration.Name, err)
	}
	committed = true
	return true, nil
}

// TableExists reports whether a table or virtual table exists. It is useful for
// migration verification and diagnostics.
func (db *DB) TableExists(name string) (bool, error) {
	var exists int
	err := db.conn.QueryRowContext(context.Background(), `
		SELECT EXISTS(
			SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?
		)
	`, name).Scan(&exists)
	return exists == 1, err
}
