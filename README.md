# kb

A fast, local knowledge base CLI written in [Ard](https://ard.run) and backed by SQLite full-text search.

The initial scope is directory-based Markdown indexing, deterministic lexical search, and original-document retrieval. Inference is not required by the core system. See [ADR 0002](docs/adrs/0002-build-local-knowledge-base-on-ard-and-sqlite.md) for the architecture.

## Development

```sh
ard format .
ard format --check .
ard check main.ard
ard test
ard build main.ard --out kb
```

Run from source with:

```sh
ard run main.ard
```

Generated files are written under `ard-out/` and are not committed.

## Documentation

Project documentation and Architecture Decision Records are indexed in [`docs/README.md`](docs/README.md).
