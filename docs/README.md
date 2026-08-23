# Project documentation

This directory contains project documentation and Architecture Decision Records (ADRs).

## Architecture Decision Records

ADRs document significant architectural decisions together with their context and consequences. They live in [`adrs/`](adrs/).

### Adding an ADR

1. Copy the structure from an existing ADR.
2. Use the next four-digit sequence number: `NNNN-short-title.md`.
3. Set its initial status to `Proposed`.
4. Describe the context, decision, consequences, and related records or resources.
5. Change the status to `Accepted` when the decision is adopted.

Accepted ADRs are historical records. If a decision changes, add a new ADR and link the affected record rather than rewriting its history.

Common statuses are `Proposed`, `Accepted`, `Deprecated`, and `Superseded`.

## Technical notes

- [Benchmarks](benchmarks.md)
- [Markdown indexing](markdown-indexing.md)
- [Lexical search](search.md)
- [CLI reference](cli.md)
- [Resilience testing](resilience-testing.md)
- [Filesystem edge behavior](filesystem-behavior.md)
- [Migrating from QMD](qmd-migration.md)

## ADR index

- [0001: Record architecture decisions](adrs/0001-record-architecture-decisions.md) — Accepted
- [0002: Build a local knowledge base on Ard and SQLite](adrs/0002-build-local-knowledge-base-on-ard-and-sqlite.md) — Accepted
- [0003: Use modernc SQLite for core storage](adrs/0003-use-modernc-sqlite-for-core-storage.md) — Accepted
