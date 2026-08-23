# kb

A fast, private, local knowledge base for Markdown. `kb` indexes directories into SQLite FTS5, provides deterministic lexical search, and retrieves the original source without requiring embeddings, inference, a daemon, or a hosted service.

## Features

- Named directory collections with glob and ignore rules
- Incremental indexing that avoids rereading unchanged files
- Weighted BM25 full-text search with phrases and explicit prefixes
- Collection filters, snippets, path context, human output, and JSON output
- Retrieval by virtual path or stable content-derived ID
- One-based line ranges and numbered output
- Configuration snapshots and direct QMD configuration migration
- SQLite integrity, FTS consistency, cleanup, and status commands
- Bounded indexing memory and transaction sizes
- No symbolic-link traversal, background service, or inference dependency

## Install

### Homebrew

```sh
brew install akonwi/tap/kb
kb version
```

Homebrew packages are available for macOS and Linux on arm64 and amd64. Prebuilt archives and checksums are also available from [GitHub Releases](https://github.com/akonwi/kb/releases).

### From source

Building requires [Ard](https://ard.run) 0.38 or newer and Go 1.25 or newer:

```sh
cd /path/to/kb
ard build main.ard --out kb
mkdir -p ~/.local/bin
install -m 755 kb ~/.local/bin/kb
```

See [Getting started](docs/getting-started.md) for installation details and your first collection.

## Quick start

```sh
# Register a Markdown directory
kb collection add notes ~/Notes

# Build its index
kb update notes

# Search it
kb search sqlite migrations --collection notes

# Retrieve original Markdown
kb get notes/projects/database.md
```

`kb` does not watch directories. Run `kb update` after source files change; an update without a collection name refreshes every collection.

## Search

```sh
kb search sqlite migrations                 # implicit AND
kb search '"schema migration"'              # phrase
kb search 'migrat*'                         # prefix
kb search sqlite --collection notes --limit 20
kb search sqlite --json
```

Results include a virtual path such as `notes/projects/database.md` and a stable 16-character content ID.

## Retrieve

```sh
kb get notes/projects/database.md
kb get 4ef077069afe4fef
kb get notes/projects/database.md --lines 10:20 --number
kb get notes/projects/database.md --json
```

Line ranges are one-based and inclusive.

## Common commands

| Command | Purpose |
|---|---|
| `kb version` | Print the build version |
| `kb collection add NAME PATH` | Register a directory |
| `kb collection list` | List collections |
| `kb collection show NAME` | Show collection settings |
| `kb collection remove NAME` | Remove the index entry, never source files |
| `kb update [NAME]` | Incrementally refresh one or all collections |
| `kb update --verify` | Rehash every matching file |
| `kb search QUERY` | Search indexed Markdown |
| `kb get ID\|COLLECTION/PATH` | Retrieve original Markdown |
| `kb status` | Show collection and document counts |
| `kb doctor` | Check database, FTS, and collection health |
| `kb cleanup [--vacuum]` | Remove stale data and optionally reclaim pages |
| `kb config export FILE` | Export collection configuration |
| `kb config import FILE` | Import collection configuration |
| `kb config import-qmd` | Convert QMD collections and contexts |

Run `kb help` or read the complete [CLI reference](docs/cli.md).

## Migrate from QMD

```sh
# Auto-detect project-local or global QMD configuration
kb config import-qmd

# Review, then index
kb collection list
kb update
```

For a trusted configuration, `kb config import-qmd --update` imports and indexes in one command. QMD models, vectors, editor settings, and shell hooks are not imported; hooks are never executed. See [Migrating from QMD](docs/qmd-migration.md).

## Data and privacy

All indexing and search run locally. The SQLite database is stored under the platform-native per-user data directory:

| Platform | Default database |
|---|---|
| macOS | `~/Library/Application Support/kb/index.sqlite` |
| Linux/Unix | `$XDG_DATA_HOME/kb/index.sqlite`, otherwise `~/.local/share/kb/index.sqlite` |
| Windows | `%LOCALAPPDATA%\\kb\\index.sqlite` |

Set `KB_DATABASE` to override the complete path. `kb` reads source Markdown but never modifies or deletes it.

## Documentation

### User guides

- [Getting started](docs/getting-started.md)
- [CLI reference](docs/cli.md)
- [Migrating from QMD](docs/qmd-migration.md)
- [Data, backup, and recovery](docs/data-management.md)
- [Markdown indexing behavior](docs/markdown-indexing.md)
- [Lexical search behavior](docs/search.md)
- [Filesystem edge behavior](docs/filesystem-behavior.md)

### Engineering notes

- [Benchmarks and tuning](docs/benchmarks.md)
- [Resilience testing](docs/resilience-testing.md)
- [Release process](docs/releasing.md)
- [Architecture Decision Records](docs/adrs/)

## Development

```sh
ard format .
ard format --check .
ard check main.ard
ard test
go test ./...
ard build main.ard --out kb
```

Generated files are written under `ard-out/` and are not committed.

## License

[MIT](LICENSE)
