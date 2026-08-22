# Benchmarks

Performance spikes use a deterministic generated corpus so storage choices can be compared under the same workload.

## FTS5 spike

`benchmark.ard` inserts 10,000 representative Markdown-sized documents into a new on-disk FTS5 index, then executes the same ranked search 500 times on the warm connection.

Run it with:

```sh
ard run benchmark.ard
```

Reported measurements:

- cold batch indexing duration;
- average warm search latency;
- SQLite database size;
- Go heap allocated at the end of the run.

The heap measurement is a point-in-time value rather than peak resident memory. These measurements cover the storage spike, not the eventual filesystem traversal or Markdown parsing pipeline.

### Baseline: modernc SQLite

Recorded with `modernc.org/sqlite` 1.57.0 on 2026-08-22 using Ard 0.38.0 and Go 1.25.0 on macOS 26.5, Apple M3 Pro arm64, with 18 GiB memory. The working tree was based on commit `12b5b4b` with the uncommitted FTS5 spike applied.

| Run | Cold index | Warm search average | Database size | Ending Go heap |
|---:|---:|---:|---:|---:|
| 1 | 98 ms | 2.478 ms | 7,487,488 bytes | 12,858,952 bytes |
| 2 | 103 ms | 2.542 ms | 7,487,488 bytes | 12,411,368 bytes |
| 3 | 95 ms | 2.456 ms | 7,487,488 bytes | 12,445,704 bytes |

This confirms that `modernc.org/sqlite` includes FTS5 and its built-in BM25 function in the tested build. It does not yet decide the production driver; distribution, concurrency, and end-to-end indexing behavior still need evaluation.
