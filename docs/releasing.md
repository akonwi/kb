# Releasing `kb`

Releases are built locally and published as GitHub release archives for:

- macOS arm64
- macOS amd64
- Linux arm64
- Linux amd64

The release script also generates the formula used by [`akonwi/homebrew-tap`](https://github.com/akonwi/homebrew-tap).

## Prerequisites

- A clean `main` checkout
- An arm64 Mac running macOS 26.5
- Ard v0.38.0
- Go 1.25.0
- Python 3.9.6 linked against zlib 1.2.12
- Rosetta 2 for the macOS amd64 smoke test
- [Apple Container](https://github.com/apple/container) 1.2.2 with its system service running for Linux arm64/amd64 smoke tests
- `gh`, authenticated for GitHub publishing
- The `origin` remote pointing to `git@github.com:akonwi/kb.git`
- A sibling `../homebrew-tap` checkout

The GitHub repository must exist before pushing the first release.

## Version behavior

Development builds report:

```text
kb dev
```

Release builds inject the complete Git tag through Go linker flags:

```text
kb v0.1.0
```

The same version appears in `kb help`, `kb version`, and `kb --version`. Informational commands run without opening or creating the SQLite database.

## Test the release process

A dry run performs source checks, cross-compiles all four targets, verifies embedded build metadata, runs native macOS arm64/amd64 and containerized Linux arm64/amd64 database smoke tests, builds every binary and archive twice for byte comparison, calculates checksums, records the toolchain, and generates a Homebrew formula. It does not create a Git tag.

```sh
./scripts/release.sh --dry-run v0.1.0
```

Artifacts are written to `ard-out/release/`:

```text
kb-darwin-arm64.tar.gz
kb-darwin-amd64.tar.gz
kb-linux-arm64.tar.gz
kb-linux-amd64.tar.gz
SHA256SUMS.txt
TOOLCHAIN.txt
kb.rb
```

Each archive contains the stripped platform binary as `kb`, plus `LICENSE` and `README.md`. Archive ownership, modes, timestamps, ordering, and gzip metadata are normalized for reproducibility.

Ard 0.38 emits equivalent generated Go with process-dependent private temporary names and function ordering. The script runs Ard twice, canonicalizes each independent output using resolved Go object identities, and requires the generated trees to match. It then clears the Go build ID and builds each target from both trees with separate Go caches. Each binary and archive must compare byte-for-byte; owned Go adapters under `ffi/` are never rewritten. `TOOLCHAIN.txt` records the pinned macOS/Ard/Go/Python/zlib toolchain, Go experiment and architecture baselines, source commit, Linux smoke status, and smoke-image digest.

Apple Container is required by default. Linux arm64 runs natively; Linux amd64 uses Container's Rosetta support. Both use an Alpine multi-architecture image pinned by OCI digest. If Linux cannot be exercised locally, `--skip-linux-smoke` makes that omission explicit and prints a warning:

```sh
./scripts/release.sh --dry-run --skip-linux-smoke v0.1.0
```

The skip is accepted only with `--dry-run`; a real release always requires both Linux runtime tests. `TOOLCHAIN.txt` records whether Linux smoke testing passed or was skipped.

## Create a release

1. Update user documentation and release notes.
2. Commit all release-ready changes.
3. Run the release script without `--dry-run`:

   ```sh
   ./scripts/release.sh v0.1.0
   ```

4. Push the annotated tag:

   ```sh
   git push origin v0.1.0
   ```

5. Publish the generated artifacts:

   ```sh
   gh release create v0.1.0 \
     ard-out/release/*.tar.gz \
     ard-out/release/SHA256SUMS.txt \
     ard-out/release/TOOLCHAIN.txt \
     --title "v0.1.0" \
     --notes "Initial release"
   ```

6. Copy and validate the generated formula:

   ```sh
   cp ard-out/release/kb.rb ../homebrew-tap/Formula/kb.rb
   cd ../homebrew-tap
   brew audit --strict Formula/kb.rb
   ```

7. Commit and publish the tap update:

   ```sh
   git add Formula/kb.rb
   git commit -m "kb v0.1.0"
   git push
   ```

8. Test installation through Homebrew:

   ```sh
   brew update
   brew install akonwi/tap/kb
   kb version
   kb doctor
   ```

For later versions, replace `v0.1.0` consistently. Build metadata (`+suffix`) is intentionally unsupported. Before an actual release, the script fetches tags, requires a clean `main` exactly matching `origin/main`, validates that the tag is absent locally and remotely, captures the commit, and rechecks the branch, remote commit, tag, and working tree before creating the annotated tag.

## Windows

The core is intended to remain portable Go and contains Windows data-path support, but Windows release artifacts are intentionally excluded until they can be exercised on a real Windows system.
