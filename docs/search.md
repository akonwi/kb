# Lexical search

Search uses SQLite FTS5 with BM25 ranking over three weighted fields:

- path: 1.5
- title: 5.0
- extracted Markdown body: 1.0

The Ard search layer converts user input into a safe FTS5 expression. Ordinary terms are quoted and combined with implicit AND semantics. Double-quoted input remains a phrase. A trailing `*` on an unquoted term enables explicit prefix search. Unmatched quotes, malformed prefixes, empty queries, queries over 4,096 characters, terms over 512 characters, more than 64 terms, unknown collections, and limits outside 1–100 are rejected before SQLite is called.

Ranked matches are joined to their collection, document metadata, original Markdown, nearest path context, and an FTS5 snippet. Raw BM25 values are converted monotonically to a higher-is-better score in `[0, 1)`.

The service provides human-readable and JSON formatting. Human output replaces terminal control characters from indexed values; JSON retains data through normal escaping. Stored documents are capped at 32 MiB, and content-inclusive results are capped at 64 MiB across a response. Search remains deterministic by using document ID as a tie-breaker after BM25 rank.
