# Markdown indexing

The index retains each Markdown file exactly as read for document retrieval. A focused Go adapter built on Goldmark CommonMark with GFM extensions derives two search values from new or changed files:

- `Title`: the first heading at any level, falling back to the filename without its extension.
- `SearchableText`: visible block text separated by newlines.

Extraction follows these rules:

- YAML-style frontmatter delimited by `---` (or closed by `...`) is excluded when it contains at least one key-like `:` field.
- Heading text is searchable and the first heading supplies the title.
- Paragraphs, blockquotes, and list item text are searchable.
- Link labels are indexed without their destinations; automatic links retain their visible URL.
- Inline code and fenced or indented code block contents are searchable.
- GFM table cells are searchable in reading order.
- Markdown formatting markers, thematic rules, and raw HTML markup are not indexed.
- Empty documents have empty searchable text and use the filename fallback title.

Whitespace inside each extracted block is normalized for stable output, HTML entities and Markdown punctuation escapes are resolved, and frontmatter handles BOM/CRLF input while requiring an unindented closing delimiter.

Derived text carries an extraction version in SQLite. Increasing that version causes an otherwise unchanged document to be reread and refreshes its title, searchable text, and FTS row. Normally, the indexer invokes extraction only after filesystem metadata identifies a file as new or changed, when verification forces a reread, or when this extraction version changes.
