# Getting started

`kb` indexes local Markdown directories into a private SQLite FTS5 database. It runs entirely on your machine and does not require a daemon, model, embedding service, or network connection after the binary is built.

## Install from source

Release archives are not published yet. Building currently requires:

- [Ard](https://ard.run) 0.38 or newer
- Go 1.25 or newer

From a source checkout on macOS or Linux:

```sh
cd /path/to/kb
ard build main.ard --out kb
mkdir -p ~/.local/bin
install -m 755 kb ~/.local/bin/kb
```

On Windows:

```powershell
ard build main.ard --out kb.exe
```

Move `kb.exe` into a directory on `PATH`.

Ensure `~/.local/bin` is on `PATH`, then verify the installation:

```sh
kb --version
kb help
```

## Create your first collection

A collection gives a directory a stable name inside the knowledge base:

```sh
kb collection add notes ~/Notes
kb update notes
```

By default, a collection indexes non-hidden Markdown files matching `**/*.md`. Customize traversal when adding it:

```sh
kb collection add work ~/work/docs \
  --glob '**/*.md' \
  --ignore 'archive/**' \
  --ignore 'generated/**'
```

Collection roots are canonicalized, symbolic links are not followed, and original files are never modified. `kb collection remove NAME` removes only the index configuration and indexed rows—not source Markdown.

Inspect configured collections:

```sh
kb collection list
kb collection show notes
```

## Search

```sh
kb search sqlite migrations
kb search '"schema migration"'
kb search 'migrat*'
kb search sqlite --collection notes --limit 20
kb search sqlite --json
```

Search behavior:

- Separate terms use implicit AND.
- Quoted text is an exact phrase.
- A trailing `*` requests prefix matching.
- `--collection` can be repeated to restrict the search.
- Results use deterministic weighted BM25 ranking over path, title, and body.

Human output includes a virtual path such as `notes/projects/database.md` and a stable content-derived ID.

## Retrieve original Markdown

```sh
kb get notes/projects/database.md
kb get 4ef077069afe4fef
kb get notes/projects/database.md --lines 10:20
kb get notes/projects/database.md --lines 10:20 --number
kb get notes/projects/database.md --json
```

Line ranges are one-based and inclusive. Without line selection or numbering, human `get` output preserves the stored Markdown.

## Keep the index current

`kb` does not watch the filesystem. Run an update after files change:

```sh
kb update notes   # one collection
kb update         # every collection
```

Normal updates avoid reading unchanged document bodies by comparing path, size, and nanosecond mtime. To rehash everything—for example, after restoring timestamps—run:

```sh
kb update --verify
```

## Inspect and maintain the database

```sh
kb status
kb doctor
kb cleanup
kb cleanup --vacuum
```

- `status` reports collection and document counts.
- `doctor` checks SQLite, foreign keys, FTS consistency, and collection roots.
- `cleanup` removes inactive rows and unreferenced content.
- `cleanup --vacuum` also asks SQLite to reclaim disk pages and may take longer.

Most reporting commands support `--json`; see the [CLI reference](cli.md).

## Configuration snapshots

SQLite is the live source of truth. Export a portable collection/context snapshot with:

```sh
kb config export ~/kb-config.json
```

Restore or merge it with:

```sh
kb config import ~/kb-config.json
kb update
```

`--replace` removes collections absent from the snapshot and should be used carefully:

```sh
kb config import ~/kb-config.json --replace
```

Snapshots contain collection roots, glob/ignore rules, and contexts. They do not contain Markdown documents or indexed content.

## Migrate from QMD

```sh
kb config import-qmd
kb collection list
kb update
```

For a trusted QMD config, import and index in one command:

```sh
kb config import-qmd --update
```

See [Migrating from QMD](qmd-migration.md) for auto-detection, safety behavior, and unsupported QMD settings.

## Database location

The database is stored in the platform-native user data directory:

| Platform | Default |
|---|---|
| macOS | `~/Library/Application Support/kb/index.sqlite` |
| Linux/Unix | `$XDG_DATA_HOME/kb/index.sqlite`, otherwise `~/.local/share/kb/index.sqlite` |
| Windows | `%LOCALAPPDATA%\\kb\\index.sqlite` |

Set `KB_DATABASE` to override the complete path. For all commands and environment behavior, see the [CLI reference](cli.md).

## Troubleshooting

```sh
kb doctor
kb status
kb update --verify
```

Common causes of failures include a moved or inaccessible collection root, an oversized Markdown file, invalid glob syntax, or a filesystem that does not support SQLite WAL semantics. Diagnostics are written to stderr, and invalid command usage exits with status 2.
