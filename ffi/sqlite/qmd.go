package sqlite

import (
	"bytes"
	"fmt"
	"io"
	"sort"

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

// QMDSource is the typed result of decoding QMD's custom-tagged YAML schema.
type QMDSource struct {
	GlobalContext     string
	EditorURI         string
	EditorURITemplate string
	EditorURICamel    string
	EditorURIKebab    string
	ModelsPresent     bool
	Collections       []QMDSourceCollection
}

// QMDSourceCollection preserves one decoded QMD collection for Ard conversion.
type QMDSourceCollection struct {
	Name                string
	Path                string
	Pattern             string
	Ignore              []string
	Contexts            []QMDSourceContext
	Update              string
	IncludeByDefault    bool
	IncludeByDefaultSet bool
}

// QMDSourceContext is one path-prefix description from QMD YAML.
type QMDSourceContext struct {
	PathPrefix  string
	Description string
}

// QMDImportResult describes an applied QMD configuration migration.
type QMDImportResult struct {
	Imported    int
	Collections []string
	Warnings    []string
}

// DecodeQMDConfigYAML strictly decodes one QMD YAML document. Ard owns all
// conversion, compatibility, validation, warning, and defaulting policy.
func DecodeQMDConfigYAML(data string) (QMDSource, error) {
	decoder := yaml.NewDecoder(bytes.NewBufferString(data))
	var decoded qmdConfig
	if err := decoder.Decode(&decoded); err != nil {
		return QMDSource{}, fmt.Errorf("decode QMD config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return QMDSource{}, fmt.Errorf("decode QMD config: trailing YAML document")
		}
		return QMDSource{}, fmt.Errorf("decode QMD config: %w", err)
	}
	if decoded.Collections == nil {
		return QMDSource{}, fmt.Errorf("QMD config collections must be an explicit mapping")
	}

	names := make([]string, 0, len(decoded.Collections))
	for name := range decoded.Collections {
		names = append(names, name)
	}
	sort.Strings(names)

	source := QMDSource{
		GlobalContext: decoded.GlobalContext, EditorURI: decoded.EditorURI,
		EditorURITemplate: decoded.EditorURITemplate, EditorURICamel: decoded.EditorURICamel,
		EditorURIKebab: decoded.EditorURIKebab, ModelsPresent: len(decoded.Models) > 0,
		Collections: make([]QMDSourceCollection, 0, len(names)),
	}
	for _, name := range names {
		item := decoded.Collections[name]
		prefixes := make([]string, 0, len(item.Context))
		for prefix := range item.Context {
			prefixes = append(prefixes, prefix)
		}
		sort.Strings(prefixes)
		contexts := make([]QMDSourceContext, 0, len(prefixes))
		for _, prefix := range prefixes {
			contexts = append(contexts, QMDSourceContext{
				PathPrefix: prefix, Description: item.Context[prefix],
			})
		}
		includeByDefault := false
		if item.IncludeByDefault != nil {
			includeByDefault = *item.IncludeByDefault
		}
		source.Collections = append(source.Collections, QMDSourceCollection{
			Name: name, Path: item.Path, Pattern: item.Pattern, Ignore: item.Ignore,
			Contexts: contexts, Update: item.Update, IncludeByDefault: includeByDefault,
			IncludeByDefaultSet: item.IncludeByDefault != nil,
		})
	}
	return source, nil
}
