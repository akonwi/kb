package markdown

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestParseFixtures(t *testing.T) {
	fixtures, err := filepath.Glob("testdata/*.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, markdownPath := range fixtures {
		markdownPath := markdownPath
		t.Run(filepath.Base(markdownPath), func(t *testing.T) {
			source, err := os.ReadFile(markdownPath)
			if err != nil {
				t.Fatal(err)
			}
			expectedBytes, err := os.ReadFile(markdownPath[:len(markdownPath)-len(filepath.Ext(markdownPath))] + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var expected Document
			if err := json.Unmarshal(expectedBytes, &expected); err != nil {
				t.Fatal(err)
			}
			actual := Parse(string(source), markdownPath)
			if actual != expected {
				t.Fatalf("got %#v\nwant %#v", actual, expected)
			}
		})
	}
}

func TestRenderedTextResolvesEntitiesEscapesAndVisibleAutolinks(t *testing.T) {
	actual := Parse("# A &amp; B\n\n\\*literal\\* www.example.com", "text.md")
	if actual.Title != "A & B" {
		t.Fatalf("unexpected title %q", actual.Title)
	}
	if actual.SearchableText != "A & B\n*literal* www.example.com" {
		t.Fatalf("unexpected searchable text %q", actual.SearchableText)
	}
}

func TestFrontmatterRequiresUnindentedClosingDelimiter(t *testing.T) {
	source := "\uFEFF---\r\ntitle: hidden\r\nnote: |\r\n  value\r\n  ---\r\nsecret: metadata\r\n---\r\n# Visible\r\n"
	actual := Parse(source, "frontmatter.md")
	if actual.Title != "Visible" || actual.SearchableText != "Visible" {
		t.Fatalf("frontmatter leaked into extraction: %#v", actual)
	}
}

func TestSetextHeadingSuppliesTitle(t *testing.T) {
	actual := Parse("Setext title\n============\n\nBody", "setext.md")
	if actual.Title != "Setext title" || actual.SearchableText != "Setext title\nBody" {
		t.Fatalf("unexpected extraction: %#v", actual)
	}
}

func TestParseConcurrently(t *testing.T) {
	var wait sync.WaitGroup
	errors := make(chan string, 32)
	for index := 0; index < 32; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 100; iteration++ {
				actual := Parse("# Concurrent\n\nVisible **text**", "concurrent.md")
				if actual.Title != "Concurrent" || !strings.Contains(actual.SearchableText, "Visible text") {
					errors <- "unexpected concurrent result"
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errors)
	for message := range errors {
		t.Fatal(message)
	}
}

func TestTopRuleWithoutFrontmatterFieldsIsSearchable(t *testing.T) {
	actual := Parse("---\n\nVisible paragraph\n\n---\n", "rule.md")
	if actual.SearchableText != "Visible paragraph" {
		t.Fatalf("unexpected text %q", actual.SearchableText)
	}
}
