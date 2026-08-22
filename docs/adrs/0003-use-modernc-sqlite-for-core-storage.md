# 0003: Use modernc SQLite for core storage

## Status

Accepted

## Context

The knowledge base needs an embedded SQLite build with FTS5 and BM25 support. It should compile cleanly through Ard's Go backend and produce straightforward native binaries for supported platforms.

The core architecture does not require vector search or a loadable SQLite extension. Semantic indexing, if introduced, is optional and asynchronous, so sqlite-vec compatibility is not a requirement for the primary database driver.

A spike using `modernc.org/sqlite` 1.57.0 confirmed that its tested build includes FTS5 and BM25. On a generated 10,000-document corpus, cold batch indexing took 95–103 ms, repeated warm ranked searches averaged 2.46–2.54 ms, and the database occupied 7,487,488 bytes. These measurements cover SQLite and FTS5 operations rather than the eventual filesystem and Markdown pipeline.

CGO-backed SQLite may offer different performance characteristics, but it would add platform toolchain and release-build complexity.

## Decision

We will use `modernc.org/sqlite` through Go's `database/sql` package for core storage.

The SQLite adapter will:

- own database connections and their lifecycle;
- expose typed Ard-friendly parameters and result values;
- provide prepared, transactional batch operations for write-heavy paths;
- configure required connection-local pragmas explicitly;
- verify FTS5 availability in tests;
- keep SQL and resource management behind a narrow Go boundary while application policy remains in Ard.

Distribution will use the pure-Go SQLite implementation compiled into the application binary. The project will not depend on a system SQLite installation, CGO toolchain, or loadable extension for core indexing, lexical search, or retrieval.

This decision can be revisited if end-to-end benchmarks show that the selected driver prevents the project from meeting its performance or correctness goals.

## Consequences

- Builds remain CGO-free and do not depend on the host's SQLite configuration.
- FTS5 and BM25 are available consistently in the tested driver build.
- Packaging native binaries is simpler across supported platforms.
- The application controls the SQLite version through its Go module dependency.
- The project must maintain a focused adapter instead of using the existing generic Ard SQL package unchanged.
- SQLite driver updates require compatibility and performance testing.
- Performance may differ from the C SQLite library, so representative end-to-end benchmarks remain necessary.
- Future vector search may need a separate implementation or a later storage decision; it cannot constrain the lexical core today.

## Related

- [0001: Record architecture decisions](0001-record-architecture-decisions.md)
- [0002: Build a local knowledge base on Ard and SQLite](0002-build-local-knowledge-base-on-ard-and-sqlite.md)
- [FTS5 benchmark](../benchmarks.md)
