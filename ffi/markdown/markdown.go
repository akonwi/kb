// Package markdown extracts stable searchable values from Markdown.
package markdown

import (
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Version changes whenever derived title or searchable-text semantics change.
const Version = 1

// Document is the small value returned across the Ard/Go boundary.
type Document struct {
	Title          string
	SearchableText string
}

var parser = goldmark.New(goldmark.WithExtensions(extension.GFM))

// Parse extracts a title and visible searchable text. The original source is
// retained separately by the indexing pipeline.
func Parse(source, filename string) Document {
	body := strings.TrimPrefix(stripFrontmatter(source), "\uFEFF")
	src := []byte(body)
	doc := parser.Parser().Parse(text.NewReader(src))
	extractor := &extractor{source: src}
	extractor.blocks(doc)

	title := extractor.title
	if title == "" {
		base := filepath.Base(filename)
		title = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return Document{Title: title, SearchableText: strings.Join(extractor.output, "\n")}
}

func stripFrontmatter(source string) string {
	lines := strings.Split(source, "\n")
	if len(lines) < 3 {
		return source
	}
	first := strings.TrimSuffix(strings.TrimPrefix(lines[0], "\uFEFF"), "\r")
	if first != "---" {
		return source
	}
	hasField := false
	for index := 1; index < len(lines); index++ {
		rawLine := strings.TrimSuffix(lines[index], "\r")
		if rawLine == "---" || rawLine == "..." {
			if hasField {
				return strings.Join(lines[index+1:], "\n")
			}
			return source
		}
		if strings.Contains(strings.TrimSpace(rawLine), ":") {
			hasField = true
		}
	}
	return source
}

type extractor struct {
	source []byte
	title  string
	output []string
}

func (e *extractor) append(value string) {
	normalized := strings.Join(strings.Fields(value), " ")
	if normalized != "" {
		e.output = append(e.output, normalized)
	}
}

func (e *extractor) blocks(node ast.Node) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch current := child.(type) {
		case *ast.Heading:
			value := e.inlineText(current)
			if e.title == "" {
				e.title = strings.Join(strings.Fields(value), " ")
			}
			e.append(value)
		case *ast.Paragraph, *ast.TextBlock:
			e.append(e.inlineText(child))
		case *ast.FencedCodeBlock:
			e.append(e.rawLines(current))
		case *ast.CodeBlock:
			e.append(e.rawLines(current))
		case *ast.HTMLBlock:
			// Raw HTML is markup rather than visible Markdown text.
		case *ast.ThematicBreak:
			// Structural only.
		case *extast.TableCell:
			e.append(e.inlineText(current))
		default:
			e.blocks(child)
		}
	}
}

func (e *extractor) inlineText(node ast.Node) string {
	var builder strings.Builder
	e.inline(node, &builder)
	return builder.String()
}

func visibleText(source []byte) []byte {
	resolved := util.ResolveEntityNames(source)
	output := make([]byte, 0, len(resolved))
	for index := 0; index < len(resolved); index++ {
		if resolved[index] == '\\' && index+1 < len(resolved) && util.IsPunct(resolved[index+1]) {
			index++
		}
		output = append(output, resolved[index])
	}
	return output
}

func (e *extractor) inline(node ast.Node, builder *strings.Builder) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch current := child.(type) {
		case *ast.Text:
			builder.Write(visibleText(current.Segment.Value(e.source)))
			if current.SoftLineBreak() || current.HardLineBreak() {
				builder.WriteByte(' ')
			}
		case *ast.String:
			builder.Write(visibleText(current.Value))
		case *ast.CodeSpan:
			for part := current.FirstChild(); part != nil; part = part.NextSibling() {
				if value, ok := part.(*ast.Text); ok {
					builder.Write(value.Segment.Value(e.source))
				}
			}
		case *ast.AutoLink:
			builder.Write(current.Label(e.source))
		case *ast.RawHTML:
			// Exclude inline markup.
		default:
			e.inline(child, builder)
		}
	}
}

func (e *extractor) rawLines(node ast.Node) string {
	var builder strings.Builder
	lines := node.Lines()
	for index := 0; index < lines.Len(); index++ {
		segment := lines.At(index)
		builder.Write(segment.Value(e.source))
	}
	return builder.String()
}
