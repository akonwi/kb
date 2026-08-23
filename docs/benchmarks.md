# Benchmarks

Benchmarks use deterministic generated corpora so changes can be compared under the same workload.

## Historical FTS5 driver spike

The initial isolated FTS5 spike and its temporary Go helpers were removed after the production schema, indexing pipeline, and end-to-end benchmarks superseded them. Its recorded baseline is retained as decision history: `modernc.org/sqlite` 1.57.0 indexed 10,000 generated documents in 95–103 ms, repeated warm ranked searches averaged 2.46–2.54 ms, and the database occupied 7,487,488 bytes on the documented Apple M3 Pro development machine. This established that the selected CGO-free driver included FTS5 and BM25; current performance should be measured with the production benchmarks below.

## Incremental indexing baseline

`benchmark_update.ard` creates 2,000 real Markdown files, runs the bounded indexing and Goldmark extraction pipeline, performs a no-change update, and then forces verification. A concurrent filesystem metadata probe records the slowest observed response while initial indexing runs.

Run it with:

```sh
ard run benchmark_update.ard
```

Recorded on the same machine and toolchain as the FTS5 baseline:

| Run | Initial update | Files/s | Max FS probe | No-change update | Bodies read | Verify update | Peak reserved source bytes | Ending Go heap |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 | 272 ms | 7,342 | 0.093 ms | 64 ms | 0 | 106 ms | 92,558 | 3,649,960 bytes |
| 2 | 267 ms | 7,482 | 0.028 ms | 65 ms | 0 | 102 ms | 92,558 | 2,820,400 bytes |
| 3 | 301 ms | 6,635 | 0.049 ms | 65 ms | 0 | 118 ms | 92,558 | 4,942,424 bytes |

The no-change path opens no document bodies and the filesystem remained responsive during this small-corpus run. Reserved source bytes describe the pipeline's file-content budget, not total process memory; the ending heap measurement provides additional context but is not peak RSS.

## End-to-end lexical search baseline

`benchmark_search.ard` creates and indexes 5,000 Markdown documents, then measures a two-term search returning 10 joined results with snippets and original content. The warm measurement repeats on one open store. The cold-connection measurement opens the database, verifies migrations, searches, and closes it for each iteration; it does not flush the operating system's filesystem cache.

```sh
ard run benchmark_search.ard
```

| Run | Warm search average | Cold-connection average | Database size |
|---:|---:|---:|---:|
| 1 | 1.874 ms | 3.185 ms | 17,154,048 bytes |
| 2 | 1.822 ms | 2.925 ms | 17,162,240 bytes |
| 3 | 2.303 ms | 2.808 ms | 17,154,048 bytes |

These timings include safe query construction, BM25 ranking, collection/document/content joins, snippet generation, score conversion, and construction of 10 Ard result values.

## Indexing and SQLite tuning

Tuning used 10,000 generated Markdown files on the same local APFS development machine. Each configuration was measured repeatedly with warm tool/build caches. The benchmark now accepts `KB_BENCH_DOCUMENTS`, `KB_BENCH_BODY_REPEATS`, `KB_BENCH_READERS`, `KB_BENCH_BATCH_SIZE`, `KB_BENCH_BATCH_BYTES`, and `KB_BENCH_IN_FLIGHT_BYTES` so results can be reproduced without source edits.

The selected indexing defaults are:

| Setting | Previous | Selected | Reason |
|---|---:|---:|---|
| Reader workers | 4 | 4 | Best balance of initial/verify latency and filesystem probe responsiveness; 6–8 readers did not consistently improve throughput. |
| Transaction documents | 64 | 512 | Captured most of the measured transaction-overhead improvement while bounding metadata-only write-lock intervals; the byte cap independently bounds large-file transactions. |
| Transaction source bytes | 8 MiB | 4 MiB | Similar or better throughput on 1 MiB documents with a lower retained-byte peak; 16 MiB was slower and less responsive. |
| In-flight source bytes | 64 MiB | 48 MiB | Within roughly 1–2% of larger budgets on 1 MiB files while lowering the hard memory budget by 25%; larger budgets did not improve 4 MiB-file throughput. |

Representative median results from three interleaved baseline/selected runs in separate worktrees:

| Configuration | Initial 10k update | No-change update | Verify update |
|---|---:|---:|---:|
| Previous defaults and SQLite settings | 1.632 s | 0.333 s | 0.520 s |
| Selected defaults and SQLite settings | 1.270 s | 0.273 s | 0.491 s |

The selected profile reduced median initial-index time by about 22%, no-change time by about 18%, and verify time by about 6% in the interleaved comparison. The benchmark reports both filesystem-operation time and timer wake delay so scheduler stalls are not hidden by starting the probe clock late.

The selected SQLite connection settings are WAL journal mode, `synchronous=FULL`, a 5-second busy timeout, a 20 MiB page cache, default file-backed temporary storage, and a maximum 256 MiB memory-map window. WAL preserves concurrent readers during writes. `FULL` was retained because it measured close to `NORMAL` while providing stronger durability for collection configuration, which is not merely derived index data. The page-cache and mmap settings materially improved the repeated 10,000-file workload without eagerly allocating the mmap maximum. The 48 MiB budget bounds retained source bodies only, not total process RSS; SQLite's page cache is separately bounded, mmap pages are demand-loaded, and temporary work remains file-backed.

WAL is required for on-disk databases and is verified during open rather than silently accepting a fallback mode. Collections should therefore live in a database on a local filesystem with working shared-memory/WAL semantics; unsupported network filesystems fail explicitly.

These values are conservative local defaults, not universal maxima. Network filesystems, cold caches, spinning disks, and very large Markdown documents require separate release-platform measurements.
