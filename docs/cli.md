# CLI reference

`kb` stores its SQLite database at `~/.local/share/kb/index.sqlite`. Set `KB_DATABASE` to use another path.

## Collections and indexing

```sh
kb collection add notes ~/Notes --glob '**/*.md' --ignore 'archive/**'
kb collection list
kb collection show notes
kb update notes
kb update --verify
kb collection remove notes
```

Collection names are case-insensitively unique and cannot contain path separators. Roots are canonicalized when added. An update without a collection name updates every collection. `--verify` rehashes files even when path, size, and nanosecond mtime are unchanged.

## Search and retrieval

```sh
kb search sqlite migrations
kb search '"schema migration"' migrat* --collection notes --limit 20
kb search sqlite --json

kb get notes/guides/sqlite.md
kb get 4ef077069afe4fef
kb get notes/guides/sqlite.md --lines 10:20 --number
kb get 4ef077069afe4fef --json
```

Virtual paths have the form `collection/relative-path`. Search results also expose a stable 16-character content-derived ID. Duplicate files with identical content share an ID; ID retrieval uses the lexicographically first path as canonical metadata. A detected hash-prefix collision is reported as ambiguous rather than returning the wrong content.

Line ranges are one-based and inclusive. Human `get` output preserves stored source unless line selection or numbering is requested. JSON returns metadata and the selected content.

Search JSON is an array. Human search output marks matches with square brackets. Query terms are implicit AND terms, quoted input is a phrase, and a trailing `*` requests prefix matching.

## Maintenance

```sh
kb status [--json]
kb cleanup [--vacuum] [--json]
kb doctor [--json]
```

`cleanup` permanently removes inactive document rows and unreferenced content, then optimizes FTS5. `--vacuum` additionally asks SQLite to reclaim database pages. `doctor` checks SQLite integrity, foreign keys, FTS row/content consistency, FTS5 integrity, and collection roots.

## Configuration snapshots

```sh
kb config export -
kb config export kb-config.json
kb config import kb-config.json
kb config import kb-config.json --replace
```

Configuration JSON contains versioned collections, glob/ignore rules, and path contexts—not indexed documents. Import validates the complete file before applying it in one transaction. Normal import upserts included collections. Changing a root or glob/ignore rule invalidates that collection's old index so the next `update` rebuilds it. `--replace` also removes collections absent from the explicit `collections` array; this cascades their indexed documents.

## Output and exit codes

Commands use human-readable output by default. Commands supporting `--json` write one valid JSON value to stdout. Diagnostics and warnings go to stderr. Raw Markdown is emitted only by human `get` output.

- `0`: success
- `1`: operational failure or unhealthy `doctor` result
- `2`: invalid command usage
