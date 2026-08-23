package sqlite

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type qmdConfig struct {
	GlobalContext     string                   `yaml:"global_context"`
	EditorURI         string                   `yaml:"editor_uri"`
	EditorURITemplate string                   `yaml:"editor_uri_template"`
	EditorURICamel    string                   `yaml:"editorUri"`
	EditorURIKebab    string                   `yaml:"editor-uri"`
	Collections       map[string]qmdCollection `yaml:"collections"`
	Models            map[string]any           `yaml:"models"`
}

type qmdCollection struct {
	Path             string            `yaml:"path"`
	Pattern          string            `yaml:"pattern"`
	Ignore           []string          `yaml:"ignore"`
	Context          map[string]string `yaml:"context"`
	Update           string            `yaml:"update"`
	IncludeByDefault *bool             `yaml:"includeByDefault"`
}

// QMDImportResult describes a QMD configuration migration.
type QMDImportResult struct {
	Imported    int
	Collections []string
	Warnings    []string
}

var qmdBraceRangePattern = regexp.MustCompile(`\{[^{}]*\.\.[^{}]*\}`)

var qmdDefaultIgnorePatterns = []string{
	"**/node_modules/**",
	"**/.git/**",
	"**/.cache/**",
	"**/vendor/**",
	"**/dist/**",
	"**/build/**",
}

func normalizeQMDContextPrefix(prefix string) string {
	return strings.Trim(strings.TrimSpace(prefix), "/")
}

func hasTopLevelComma(pattern string) bool {
	braceDepth := 0
	bracketDepth := 0
	escaped := false
	for _, character := range pattern {
		if escaped {
			escaped = false
			continue
		}
		if character == '\\' {
			escaped = true
			continue
		}
		switch character {
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case ',':
			if braceDepth == 0 && bracketDepth == 0 {
				return true
			}
		}
	}
	return false
}

func unsupportedQMDGlobSyntax(pattern string) string {
	if hasTopLevelComma(pattern) {
		return "comma unions"
	}
	if strings.HasPrefix(pattern, "!") {
		return "leading negation"
	}
	if qmdBraceRangePattern.MatchString(pattern) {
		return "brace ranges"
	}
	for _, marker := range []string{"@(", "!(", "+(", "?(", "*(", "[[:"} {
		if strings.Contains(pattern, marker) {
			return "extglob/POSIX classes"
		}
	}
	return ""
}

func qmdIgnorePatterns(configured []string) ([]string, error) {
	seen := make(map[string]struct{}, len(qmdDefaultIgnorePatterns)+len(configured))
	patterns := make([]string, 0, len(qmdDefaultIgnorePatterns)+len(configured))
	all := make([]string, 0, len(qmdDefaultIgnorePatterns)+len(configured))
	all = append(all, qmdDefaultIgnorePatterns...)
	all = append(all, configured...)
	for _, pattern := range all {
		if syntax := unsupportedQMDGlobSyntax(pattern); syntax != "" {
			return nil, fmt.Errorf("QMD ignore pattern %q uses unsupported %s", pattern, syntax)
		}
		if _, exists := seen[pattern]; !exists {
			patterns = append(patterns, pattern)
			seen[pattern] = struct{}{}
		}
	}
	return patterns, nil
}

// ImportQMDConfigYAML converts QMD's YAML collection configuration into kb's
// versioned configuration and applies it through the same atomic importer.
// Relative collection paths are resolved against baseDir.
func (db *DB) ImportQMDConfigYAML(data, baseDir string, replace, includeNonDefault bool) (QMDImportResult, error) {
	decoder := yaml.NewDecoder(bytes.NewBufferString(data))
	var source qmdConfig
	if err := decoder.Decode(&source); err != nil {
		return QMDImportResult{}, fmt.Errorf("decode QMD config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return QMDImportResult{}, fmt.Errorf("decode QMD config: trailing YAML document")
		}
		return QMDImportResult{}, fmt.Errorf("decode QMD config: %w", err)
	}
	if source.Collections == nil {
		return QMDImportResult{}, fmt.Errorf("QMD config collections must be an explicit mapping")
	}
	absoluteBase, err := filepath.Abs(baseDir)
	if err != nil {
		return QMDImportResult{}, fmt.Errorf("resolve QMD config directory: %w", err)
	}

	names := make([]string, 0, len(source.Collections))
	for name := range source.Collections {
		names = append(names, name)
	}
	sort.Strings(names)

	converted := configFile{Version: configVersion, Collections: make([]configCollection, 0, len(names))}
	warnings := make([]string, 0)
	importedNames := make([]string, 0, len(names))
	mutedCollections := 0
	if len(source.Models) > 0 {
		warnings = append(warnings, "QMD model settings were not imported; kb lexical search does not require models")
	}
	if source.EditorURI != "" || source.EditorURITemplate != "" || source.EditorURICamel != "" || source.EditorURIKebab != "" {
		warnings = append(warnings, "QMD editor URI settings were not imported")
	}

	for _, name := range names {
		item := source.Collections[name]
		canonicalName, err := validateCollectionName(name)
		if err != nil {
			return QMDImportResult{}, fmt.Errorf("QMD collection %q: %w", name, err)
		}
		if item.IncludeByDefault != nil && !*item.IncludeByDefault && !includeNonDefault {
			existing, err := db.LookupCollection(canonicalName)
			if err != nil {
				return QMDImportResult{}, err
			}
			if existing.Found {
				return QMDImportResult{}, fmt.Errorf("collection %q already exists in kb but QMD marks it includeByDefault=false; remove it or pass --include-nondefault", canonicalName)
			}
			warnings = append(warnings, fmt.Sprintf("collection %q was skipped because includeByDefault=false; pass --include-nondefault to import it", canonicalName))
			mutedCollections++
			continue
		}
		root := item.Path
		if root == "" {
			return QMDImportResult{}, fmt.Errorf("QMD collection %q has no path", name)
		}
		if !filepath.IsAbs(root) {
			root = filepath.Join(absoluteBase, root)
		}
		pattern := item.Pattern
		if pattern == "" {
			pattern = "**/*.md"
		}
		if syntax := unsupportedQMDGlobSyntax(pattern); syntax != "" {
			return QMDImportResult{}, fmt.Errorf("QMD collection %q uses unsupported %s in pattern %q; split it into compatible collections", canonicalName, syntax, pattern)
		}
		ignorePatterns, err := qmdIgnorePatterns(item.Ignore)
		if err != nil {
			return QMDImportResult{}, fmt.Errorf("QMD collection %q: %w", canonicalName, err)
		}

		contexts := make(map[string]string, len(item.Context)+1)
		if source.GlobalContext != "" {
			contexts[""] = source.GlobalContext
		}
		for prefix, description := range item.Context {
			contexts[normalizeQMDContextPrefix(prefix)] = description
		}
		contextPrefixes := make([]string, 0, len(contexts))
		for prefix := range contexts {
			contextPrefixes = append(contextPrefixes, prefix)
		}
		sort.Strings(contextPrefixes)
		convertedContexts := make([]configContext, 0, len(contextPrefixes))
		for _, prefix := range contextPrefixes {
			convertedContexts = append(convertedContexts, configContext{
				PathPrefix: prefix, Description: contexts[prefix],
			})
		}

		converted.Collections = append(converted.Collections, configCollection{
			Name: canonicalName, RootPath: root, GlobPattern: pattern,
			IgnorePatterns: ignorePatterns, Contexts: convertedContexts,
		})
		importedNames = append(importedNames, canonicalName)
		if item.Update != "" {
			warnings = append(warnings, fmt.Sprintf("collection %q: QMD update hook was not imported or executed", canonicalName))
		}
		if item.IncludeByDefault != nil && !*item.IncludeByDefault {
			warnings = append(warnings, fmt.Sprintf("collection %q: includeByDefault=false was explicitly broadened by --include-nondefault", canonicalName))
		}
	}
	if replace && mutedCollections > 0 {
		return QMDImportResult{}, fmt.Errorf("--replace would omit %d includeByDefault=false collection(s); also pass --include-nondefault", mutedCollections)
	}

	encoded, err := json.Marshal(converted)
	if err != nil {
		return QMDImportResult{}, err
	}
	imported, err := db.ImportConfigJSON(string(encoded), replace)
	if err != nil {
		return QMDImportResult{}, err
	}
	return QMDImportResult{Imported: imported, Collections: importedNames, Warnings: warnings}, nil
}
