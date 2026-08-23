# Filesystem edge behavior

Indexing uses a descriptor-pinned canonical collection root and streams regular files without following symbolic links.

## Large files

The default per-file limit is 32 MiB. A file exactly at the configured limit is accepted subject to the in-flight byte budget; a larger file makes the update fail. A failed update does not run stale-document finalization, so previously committed searchable content remains active. Writer batches committed before a later file error remain valid and are reconciled by the next update.

Tests exercise the same boundary behavior with a lower configured limit to avoid allocating tens of MiB in the normal test suite.

## Symbolic links

Symlinked files and directories discovered during traversal are ignored. Reads are relative to an `os.Root` descriptor, so replacing a file with a symlink or replacing/renaming the collection path during a scan cannot redirect reads outside the pinned root.

## Inaccessible files

Metadata or open/read failures fail the update and report the affected path. Because finalization only runs after a successful complete scan, inaccessible files do not cause unseen documents to be marked stale. Coverage combines a reader-level permission failure with the pipeline's deterministic failed-update/no-finalization test; the permission-specific test skips when the current user or filesystem bypasses Unix-style permission bits.

## Unicode paths

Relative paths preserve the Unicode spelling returned by the filesystem and are stored as SQLite text. Traversal, indexing, search metadata, and virtual-path retrieval are tested with non-ASCII scripts, combining-capable Latin text, and emoji. No cross-platform Unicode normalization is imposed, so callers should use the virtual path reported by `kb search` or `kb collection` workflows.

## Timestamp edge cases

On supported 64-bit targets, change detection compares relative path, byte size, and nanosecond mtime. Subsecond mtime changes are retained and detected when the filesystem supports that timestamp precision. If content changes while both size and mtime are deliberately restored, a normal update trusts metadata and skips the read; `kb update --verify` rehashes every file and repairs the index.
