package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

const configVersion = 1

type configFile struct {
	Version     int                `json:"version"`
	Collections []configCollection `json:"collections"`
}

type configCollection struct {
	Name           string          `json:"name"`
	RootPath       string          `json:"root_path"`
	GlobPattern    string          `json:"glob_pattern"`
	IgnorePatterns []string        `json:"ignore_patterns"`
	Contexts       []configContext `json:"contexts"`
}

type configContext struct {
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
	output := configFile{Version: configVersion, Collections: make([]configCollection, 0, len(collections))}
	for _, collection := range collections {
		entry := configCollection{
			Name: collection.Name, RootPath: collection.RootPath,
			GlobPattern: collection.GlobPattern, IgnorePatterns: collection.IgnorePatterns,
			Contexts: make([]configContext, 0),
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
			var item configContext
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

func canonicalConfigRoot(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", path)
	}
	return resolved, nil
}

func validateConfig(config *configFile) error {
	if config.Version != configVersion {
		return fmt.Errorf("unsupported config version %d", config.Version)
	}
	if config.Collections == nil {
		return fmt.Errorf("config collections must be an explicit JSON array")
	}
	seenNames := make(map[string]struct{}, len(config.Collections))
	for index := range config.Collections {
		collection := &config.Collections[index]
		validatedName, err := validateCollectionName(collection.Name)
		if err != nil {
			return fmt.Errorf("collection %q: %w", collection.Name, err)
		}
		collection.Name = validatedName
		key := strings.ToLower(collection.Name)
		if _, exists := seenNames[key]; exists {
			return fmt.Errorf("duplicate collection name %q", collection.Name)
		}
		seenNames[key] = struct{}{}
		if collection.RootPath == "" {
			return fmt.Errorf("collection %q root_path is required", collection.Name)
		}
		root, err := canonicalConfigRoot(collection.RootPath)
		if err != nil {
			return fmt.Errorf("collection %q root: %w", collection.Name, err)
		}
		collection.RootPath = root
		if collection.GlobPattern == "" || !doublestar.ValidatePattern(collection.GlobPattern) {
			return fmt.Errorf("collection %q has invalid glob %q", collection.Name, collection.GlobPattern)
		}
		for _, pattern := range collection.IgnorePatterns {
			if pattern == "" || !doublestar.ValidatePattern(pattern) {
				return fmt.Errorf("collection %q has invalid ignore pattern %q", collection.Name, pattern)
			}
		}
		seenContexts := make(map[string]struct{}, len(collection.Contexts))
		for contextIndex := range collection.Contexts {
			item := &collection.Contexts[contextIndex]
			item.PathPrefix = strings.Trim(strings.TrimSpace(item.PathPrefix), "/")
			if item.PathPrefix == "." || item.PathPrefix == ".." || strings.HasPrefix(item.PathPrefix, "../") || strings.Contains(item.PathPrefix, "/../") {
				return fmt.Errorf("collection %q has invalid context prefix %q", collection.Name, item.PathPrefix)
			}
			if _, exists := seenContexts[item.PathPrefix]; exists {
				return fmt.Errorf("collection %q has duplicate context prefix %q", collection.Name, item.PathPrefix)
			}
			seenContexts[item.PathPrefix] = struct{}{}
		}
	}
	return nil
}

// ImportConfigJSON validates the complete document before atomically upserting
// collections and replacing contexts for each included collection. replace
// removes collections absent from the imported snapshot.
func (db *DB) ImportConfigJSON(data string, replace bool) (int, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	decoder.DisallowUnknownFields()
	var config configFile
	if err := decoder.Decode(&config); err != nil {
		return 0, fmt.Errorf("decode config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return 0, fmt.Errorf("decode config: trailing JSON value")
		}
		return 0, fmt.Errorf("decode config: %w", err)
	}
	if err := validateConfig(&config); err != nil {
		return 0, err
	}

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
