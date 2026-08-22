# 0002: Build a local knowledge base on Ard and SQLite

## Status

Accepted

## Context

The project will provide fast local indexing, search, and retrieval across collections of Markdown documents. Its essential behavior is managing directory-based collections, incrementally indexing their contents, searching them from a CLI, and retrieving original documents by path or stable identifier.

Existing tools in this space often couple indexing and search to local model inference. That increases resource usage, operational complexity, and indexing latency even when deterministic lexical search is sufficient. The core knowledge base should remain useful without a model runtime, external service, or background daemon.

The implementation should use Ard where it expresses application behavior well while retaining narrow access to mature Go libraries for infrastructure concerns.

## Decision

We will build the core application in Ard and use SQLite as the durable local store and source of truth.

The core system will provide:

- directory-based document collections;
- incremental indexing of Markdown files;
- full-text search through SQLite FTS5 with BM25 ranking;
- retrieval of original Markdown by virtual path or stable content-derived identifier;
- collection, indexing, search, retrieval, status, and maintenance operations through a CLI.

The core system will not depend on inference, embeddings, query expansion, or model-based reranking. If semantic search is introduced later, document changes will be processed asynchronously by an optional worker. Lexical search and document retrieval must continue to work when that worker is absent, stale, or failing.

### Component boundaries

Ard will own application and domain behavior, including:

- CLI parsing, validation, and output;
- collection and document semantics;
- indexing pipeline orchestration and backpressure;
- search query construction and result presentation;
- document retrieval and maintenance workflows;
- schema migration orchestration.

Small Go adapters will own integration with libraries or resources that benefit from native Go APIs:

- SQLite connections, FTS5, prepared statements, typed row scanning, and batched transactions;
- Goldmark parsing of changed Markdown into a title and searchable text;
- streaming, glob-aware filesystem traversal where a Go adapter is preferable to direct interop.

These adapters will expose simple Ard-friendly values and will not contain application policy.

### Indexing model

Indexing will use a bounded, streaming pipeline:

1. Walk collection directories incrementally.
2. Compare stored file metadata before opening a file.
3. Skip files whose path, size, and modification time are unchanged.
4. Read, hash, and parse only new or changed files with bounded concurrency.
5. Send document changes to a single SQLite writer.
6. Apply changes in bounded transactions and update the FTS5 index.
7. Provide an explicit verification mode that rehashes all files when required.

The original Markdown will be retained for retrieval. Goldmark-derived text will be used for search, allowing title weighting and avoiding dependence on Markdown syntax during ranking.

### Storage model

SQLite will store collection configuration, path contexts, document metadata, content, the FTS5 index, and schema migration history. Configuration import and export may be added, but a separate mutable configuration file will not be a second source of truth.

Document metadata will include enough filesystem information to avoid reopening unchanged files. Content hashes will support change detection, content deduplication, and stable short document identifiers.

Schema changes will use explicit, ordered migrations rather than ad hoc startup repairs.

## Consequences

- Indexing and lexical search remain local, deterministic, and independent of model availability.
- Routine updates can avoid reading and hashing unchanged documents.
- Bounded I/O and batched writes should keep the filesystem responsive and reduce SQLite transaction overhead.
- SQLite supplies persistence, full-text indexing, and relevance ranking without a separate search service.
- Goldmark provides standards-based Markdown handling without requiring Ard to consume its AST directly.
- Most product behavior remains in Ard, while lifecycle-sensitive infrastructure stays behind narrow Go boundaries.
- The project must select and package a SQLite build with FTS5 enabled.
- Filesystem metadata can occasionally report a file as unchanged despite content modification; verification mode provides a correctness fallback.
- A single SQLite source of truth simplifies consistency but requires explicit import/export for portable, human-editable configuration.
- Semantic capabilities, if added, require a separate job and worker design and may produce temporarily incomplete semantic coverage.

## Related

- [0001: Record architecture decisions](0001-record-architecture-decisions.md)
