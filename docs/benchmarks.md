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

## Incremental indexing baseline

`benchmark_update.ard` creates 2,000 real Markdown files, runs the bounded indexing pipeline, performs a no-change update, and then forces verification. A concurrent filesystem metadata probe records the slowest observed response while initial indexing runs.

Run it with:

```sh
ard run benchmark_update.ard
```

Recorded on the same machine and toolchain as the FTS5 baseline:

| Run | Initial update | Files/s | Max FS probe | No-change update | Bodies read | Verify update | Peak reserved source bytes | Ending Go heap |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 | 242 ms | 8,266 | 0.057 ms | 49 ms | 0 | 70 ms | 92,558 | 3,756,352 bytes |
| 2 | 242 ms | 8,247 | 0.090 ms | 47 ms | 0 | 68 ms | 92,558 | 4,579,888 bytes |
| 3 | 280 ms | 7,130 | 0.043 ms | 45 ms | 0 | 79 ms | 92,558 | 2,278,912 bytes |

The no-change path opens no document bodies and the filesystem remained responsive during this small-corpus run. Reserved source bytes describe the pipeline's file-content budget, not total process memory; the ending heap measurement provides additional context but is not peak RSS.
