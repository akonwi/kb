# CLI reference

`kb` stores its SQLite database in the platform-native per-user data directory:

| Platform | Default database |
|---|---|
| macOS | `~/Library/Application Support/kb/index.sqlite` |
| Linux and other Unix | `$XDG_DATA_HOME/kb/index.sqlite`, or `~/.local/share/kb/index.sqlite` when unset |
| Windows | `%LOCALAPPDATA%\\kb\\index.sqlite` |

`XDG_DATA_HOME` must be absolute. Set `KB_DATABASE` to override the complete database path on any platform; it takes precedence over platform and XDG defaults. If an older `~/.local/share/kb/index.sqlite` exists and the new native location does not, `kb` continues using the legacy database rather than silently opening an empty one. The legacy fallback also applies when `XDG_DATA_HOME` is set, preventing an upgrade from stranding a database created by an older build.

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

### QMD migration

```sh
kb config import-qmd
kb config import-qmd ~/.config/qmd/index.yml
kb config import-qmd --update
kb config import-qmd ~/.config/qmd/index.yml --include-nondefault
kb config import-qmd ~/.config/qmd/index.yml --replace --include-nondefault
```

This converts QMD collections, glob/ignore rules, and contexts. Project-local and standard global QMD config locations are auto-detected. Indexing is a separate `kb update` step unless `--update` is explicitly supplied. QMD's standard ignored directories are preserved. Model/editor settings, generated vector data, and update hooks are not imported; hooks are never executed. `includeByDefault=false` collections are skipped unless explicitly included. See [Migrating from QMD](qmd-migration.md) for mapping and validation details.

## Output and exit codes

Commands use human-readable output by default. Commands supporting `--json` write one valid JSON value to stdout. Diagnostics and warnings go to stderr. Raw Markdown is emitted only by human `get` output.

- `0`: success
- `1`: operational failure or unhealthy `doctor` result
- `2`: invalid command usage
