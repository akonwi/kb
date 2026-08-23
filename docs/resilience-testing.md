# Resilience testing

The SQLite adapter includes deterministic failure tests in `ffi/sqlite/resilience_test.go` and migration recovery tests in `ffi/sqlite/migrations_test.go`.

## Covered invariants

- **Interrupted batches:** an injected failure after the first index change rolls back both document metadata and content rows.
- **Concurrent readers:** repeated FTS5 reads observe complete committed snapshots while a second connection commits write batches; readers do not see impossible row counts or locking failures.
- **Process death during writes:** a subprocess is killed with an open write transaction. Reopening rolls back its row while preserving earlier committed data and passing `PRAGMA integrity_check`.
- **Process death during migrations:** a subprocess is killed after migration DDL starts but before commit. Reopening preserves migration version zero, removes uncommitted schema, passes integrity checking, and permits a corrected migration.
- **Process death after commit:** killing the process after the commit marker preserves both schema and migration history.
- **Migration failures:** malformed migrations roll back schema and history; checksum drift is rejected; a failed version can be corrected and retried safely.

Crash tests execute the compiled Go test binary as a child process and kill it with the operating system rather than simulating an ordinary returned error. They retain the production WAL journal mode, use a small test-only page cache plus a bounded multi-page transaction to verify uncommitted frames reach the WAL, and use a package-private pre-commit test seam to stop migrations at a deterministic transaction boundary. `go test -short` skips subprocess crash tests.

## Commands

```sh
go test ./ffi/sqlite -count=1
go test -race ./ffi/sqlite -count=1
# Useful for detecting timing-sensitive regressions:
go test ./ffi/sqlite -count=10
```
