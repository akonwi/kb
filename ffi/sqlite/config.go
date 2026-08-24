package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const configVersion = 1

type ConfigFile struct {
	Version            int                `json:"version"`
	Collections        []ConfigCollection `json:"collections"`
	CollectionsPresent bool               `json:"-"`
}

type ConfigCollection struct {
	Name           string          `json:"name"`
	RootPath       string          `json:"root_path"`
	GlobPattern    string          `json:"glob_pattern"`
	IgnorePatterns []string        `json:"ignore_patterns"`
	Contexts       []ConfigContext `json:"contexts"`
}

type ConfigContext struct {
	PathPrefix  string `json:"path_prefix"`
	Description string `json:"description"`
}

// ExportConfigJSON serializes mutable collection configuration, never indexed
// documents. Ordering is deterministic for useful diffs.
func (db *DB) ExportConfigJSON() (string, error) {
	collections, err := db.ListCollections()
	if err != nil {
		return "", err
	}
	output := ConfigFile{Version: configVersion, Collections: make([]ConfigCollection, 0, len(collections))}
	for _, collection := range collections {
		entry := ConfigCollection{
			Name: collection.Name, RootPath: collection.RootPath,
			GlobPattern: collection.GlobPattern, IgnorePatterns: collection.IgnorePatterns,
			Contexts: make([]ConfigContext, 0),
		}
		rows, err := db.conn.QueryContext(context.Background(), `
			SELECT path_prefix, description
			FROM collection_contexts
			WHERE collection_id = ?
			ORDER BY path_prefix
		`, collection.ID)
		if err != nil {
			return "", err
		}
		for rows.Next() {
			var item ConfigContext
			if err := rows.Scan(&item.PathPrefix, &item.Description); err != nil {
				rows.Close()
				return "", err
			}
			entry.Contexts = append(entry.Contexts, item)
		}
		if err := rows.Close(); err != nil {
			return "", err
		}
		output.Collections = append(output.Collections, entry)
	}
	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded) + "\n", nil
}

// DecodeConfigJSON strictly decodes one configuration document. Validation and
// normalization are owned by the Ard configuration layer.
func DecodeConfigJSON(data string) (ConfigFile, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	decoder.DisallowUnknownFields()
	var config ConfigFile
	if err := decoder.Decode(&config); err != nil {
		return ConfigFile{}, fmt.Errorf("decode config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return ConfigFile{}, fmt.Errorf("decode config: trailing JSON value")
		}
		return ConfigFile{}, fmt.Errorf("decode config: %w", err)
	}
	config.CollectionsPresent = config.Collections != nil
	return config, nil
}

// ImportConfig atomically upserts validated collections and replaces contexts
// for each included collection. replace removes collections absent from the
// imported snapshot.
func (db *DB) ImportConfig(config ConfigFile, replace bool) (int, error) {
	tx, err := db.conn.BeginTx(context.Background(), nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	importedIDs := make([]int, 0, len(config.Collections))
	for _, collection := range config.Collections {
		ignores, err := json.Marshal(collection.IgnorePatterns)
		if err != nil {
			return 0, err
		}
		var existingRoot, existingGlob, existingIgnores string
		existingErr := tx.QueryRowContext(context.Background(), `
			SELECT root_path, glob_pattern, ignore_patterns
			FROM collections WHERE name = ? COLLATE NOCASE
		`, collection.Name).Scan(&existingRoot, &existingGlob, &existingIgnores)
		if existingErr != nil && existingErr != sql.ErrNoRows {
			return 0, existingErr
		}
		rulesChanged := existingErr == nil && (existingRoot != collection.RootPath || existingGlob != collection.GlobPattern || existingIgnores != string(ignores))

		var id int
		err = tx.QueryRowContext(context.Background(), `
			INSERT INTO collections(name, root_path, glob_pattern, ignore_patterns)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(name) DO UPDATE SET
			  root_path = excluded.root_path,
			  glob_pattern = excluded.glob_pattern,
			  ignore_patterns = excluded.ignore_patterns,
			  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			RETURNING id
		`, collection.Name, collection.RootPath, collection.GlobPattern, string(ignores)).Scan(&id)
		if err != nil {
			return 0, err
		}
		importedIDs = append(importedIDs, id)
		if rulesChanged {
			if _, err := tx.ExecContext(context.Background(), "DELETE FROM documents WHERE collection_id = ?", id); err != nil {
				return 0, err
			}
		}
		if _, err := tx.ExecContext(context.Background(), "DELETE FROM collection_contexts WHERE collection_id = ?", id); err != nil {
			return 0, err
		}
		for _, item := range collection.Contexts {
			if _, err := tx.ExecContext(context.Background(), `
				INSERT INTO collection_contexts(collection_id, path_prefix, description)
				VALUES (?, ?, ?)
			`, id, item.PathPrefix, item.Description); err != nil {
				return 0, err
			}
		}
	}
	if replace {
		if len(importedIDs) == 0 {
			if _, err := tx.ExecContext(context.Background(), "DELETE FROM collections"); err != nil {
				return 0, err
			}
		} else {
			placeholders := strings.TrimSuffix(strings.Repeat("?,", len(importedIDs)), ",")
			arguments := make([]any, len(importedIDs))
			for index, id := range importedIDs {
				arguments[index] = id
			}
			if _, err := tx.ExecContext(context.Background(), "DELETE FROM collections WHERE id NOT IN ("+placeholders+")", arguments...); err != nil {
				return 0, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(config.Collections), nil
}
