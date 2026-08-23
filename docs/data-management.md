# Data, backup, and recovery

## What `kb` stores

The SQLite database is the mutable source of truth for:

- Collections, roots, glob rules, and ignore rules
- Path contexts
- Document metadata and active/inactive state
- Original Markdown content captured at the last successful update
- Extracted searchable text and the FTS5 index

Source Markdown remains authoritative outside the database and is never modified by `kb`.

## Database location

| Platform | Default database |
|---|---|
| macOS | `~/Library/Application Support/kb/index.sqlite` |
| Linux/Unix | `$XDG_DATA_HOME/kb/index.sqlite`, otherwise `~/.local/share/kb/index.sqlite` |
| Windows | `%LOCALAPPDATA%\\kb\\index.sqlite` |

`KB_DATABASE` overrides the complete path. SQLite may create `index.sqlite-wal` and `index.sqlite-shm` beside the database while commands are running.

The database contains copies of indexed Markdown. Protect backups according to the sensitivity of the source material.

## Recommended portable backup

Back up both the original Markdown directories and a configuration snapshot:

```sh
kb config export ~/backups/kb-config.json
```

The snapshot is small and portable but does not contain documents. To rebuild on another machine:

```sh
kb config import ~/backups/kb-config.json
kb update
kb doctor
```

Every imported root must exist on the destination. Edit the JSON paths before import when directory layouts differ.

## Full database backup

For a consistent online SQLite backup, use the SQLite CLI's backup API:

```sh
DB="$HOME/Library/Application Support/kb/index.sqlite"  # macOS example
mkdir -p "$HOME/backups"
sqlite3 "$DB" ".backup '$HOME/backups/kb.sqlite'"
```

When `KB_DATABASE` is set, use that path instead.

Do not copy only `index.sqlite` while another `kb` process may be writing. WAL frames might not yet be checkpointed into the main file. If `sqlite3` is unavailable, stop all `kb` commands first and copy `index.sqlite`, `index.sqlite-wal`, and `index.sqlite-shm` together when those sidecars exist.

## Restore a full backup

Set `DB` to the active database path, then:

1. Stop all running `kb` commands.
2. Preserve the current database rather than overwriting it immediately.
3. Copy the backup into the configured database path.
4. Run health checks.

```sh
mv "$DB" "$DB.before-restore"
test ! -e "$DB-wal" || mv "$DB-wal" "$DB.before-restore-wal"
test ! -e "$DB-shm" || mv "$DB-shm" "$DB.before-restore-shm"
cp "$HOME/backups/kb.sqlite" "$DB"
kb doctor
kb status
```

Move all sidecars before opening the replacement database. Never remove or relocate WAL files while a process is using them.

## Recover by rebuilding

Because search data is derived from local Markdown, the safest recovery from suspected corruption is often a clean rebuild:

```sh
kb config export ~/backups/kb-config.json
mv "$DB" "$DB.suspect"
test ! -e "$DB-wal" || mv "$DB-wal" "$DB.suspect-wal"
test ! -e "$DB-shm" || mv "$DB-shm" "$DB.suspect-shm"
kb config import ~/backups/kb-config.json
kb update --verify
kb doctor
```

If the suspect database cannot export configuration, restore the most recent configuration snapshot and reindex the source directories. Keep the suspect database until recovery is verified.
