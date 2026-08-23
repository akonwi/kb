#!/usr/bin/env bash
set -euo pipefail

DRY_RUN=false
SKIP_LINUX_SMOKE=false
while [[ ${1:-} == --* ]]; do
  case "$1" in
    --dry-run) DRY_RUN=true ;;
    --skip-linux-smoke) SKIP_LINUX_SMOKE=true ;;
    *) echo "Error: unknown option: $1" >&2; exit 2 ;;
  esac
  shift
done

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 [--dry-run] [--skip-linux-smoke] <version>" >&2
  echo "  e.g. $0 --dry-run --skip-linux-smoke v0.1.0" >&2
  exit 2
fi

VERSION=$1
SEMVER_PATTERN='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*))?$'
if [[ ! $VERSION =~ $SEMVER_PATTERN ]]; then
  echo "Error: version must be a v-prefixed semantic version without build metadata (for example, v0.1.0 or v1.0.0-rc.1)" >&2
  exit 2
fi
SEMVER=${VERSION#v}
if [[ $SKIP_LINUX_SMOKE == true && $DRY_RUN == false ]]; then
  echo "Error: --skip-linux-smoke is allowed only with --dry-run" >&2
  exit 2
fi

REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
OUTDIR="$REPO_ROOT/ard-out/release"
RELEASE_COMMIT=$(git -C "$REPO_ROOT" rev-parse HEAD)
SOURCE_DATE_EPOCH=$(git -C "$REPO_ROOT" show -s --format=%ct "$RELEASE_COMMIT")
export SOURCE_DATE_EPOCH

EXPECTED_ARD_VERSION=v0.38.0
EXPECTED_GO_VERSION='go version go1.25.0 darwin/arm64'
EXPECTED_PYTHON_VERSION='Python 3.9.6'
EXPECTED_MACOS_VERSION=26.5
EXPECTED_ZLIB_VERSION=1.2.12
EXPECTED_CONTAINER_VERSION='container CLI version 1.2.2 (build: release, commit: unspeci)'
LINUX_SMOKE_IMAGE='alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce'
RELEASE_GOEXPERIMENT=jsonv2
RELEASE_GOAMD64=v1
RELEASE_GOARM64=v8.0

PLATFORMS=(
  darwin/arm64
  darwin/amd64
  linux/arm64
  linux/amd64
)

cd "$REPO_ROOT"
if [[ -n $(git status --porcelain) ]]; then
  if [[ $DRY_RUN == false ]]; then
    echo "Error: working tree has uncommitted changes. Commit or stash first." >&2
    exit 1
  fi
  echo "Warning: dry run includes uncommitted changes." >&2
fi
if git rev-parse "refs/tags/$VERSION" >/dev/null 2>&1; then
  echo "Error: tag $VERSION already exists" >&2
  exit 1
fi

for command in ard go python3; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "Error: required command not found: $command" >&2
    exit 1
  fi
done

if [[ $(ard version) != "$EXPECTED_ARD_VERSION" ]]; then
  echo "Error: releases require Ard $EXPECTED_ARD_VERSION" >&2
  exit 1
fi
if [[ $(go version) != "$EXPECTED_GO_VERSION" ]]; then
  echo "Error: releases require $EXPECTED_GO_VERSION" >&2
  exit 1
fi
if [[ $(python3 --version) != "$EXPECTED_PYTHON_VERSION" ]]; then
  echo "Error: releases require $EXPECTED_PYTHON_VERSION" >&2
  exit 1
fi
if [[ $(sw_vers -productVersion) != "$EXPECTED_MACOS_VERSION" ]]; then
  echo "Error: releases require macOS $EXPECTED_MACOS_VERSION" >&2
  exit 1
fi
ZLIB_VERSION=$(python3 -c 'import zlib; print(zlib.ZLIB_VERSION + "/" + zlib.ZLIB_RUNTIME_VERSION)')
if [[ $ZLIB_VERSION != "$EXPECTED_ZLIB_VERSION/$EXPECTED_ZLIB_VERSION" ]]; then
  echo "Error: releases require Python zlib $EXPECTED_ZLIB_VERSION" >&2
  exit 1
fi

remote_tag_exists() {
  local status
  git ls-remote --exit-code --tags origin "refs/tags/$VERSION" >/dev/null 2>&1
  status=$?
  case $status in
    0) return 0 ;;
    2) return 1 ;;
    *)
      echo "Error: failed to query remote tag $VERSION (git ls-remote exit $status)" >&2
      exit 1
      ;;
  esac
}

if [[ $DRY_RUN == false ]]; then
  if [[ $(git branch --show-current) != main ]]; then
    echo "Error: releases must be created from main" >&2
    exit 1
  fi
  if [[ $(git remote get-url origin) != git@github.com:akonwi/kb.git && $(git remote get-url origin) != https://github.com/akonwi/kb.git ]]; then
    echo "Error: origin must point to github.com/akonwi/kb" >&2
    exit 1
  fi
  git fetch origin main --tags
  if [[ $(git rev-parse origin/main) != "$RELEASE_COMMIT" ]]; then
    echo "Error: HEAD must exactly match origin/main" >&2
    exit 1
  fi
  if remote_tag_exists; then
    echo "Error: tag $VERSION already exists on origin" >&2
    exit 1
  fi
fi

checksum() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

echo "==> Verifying source"
ard format --check .
env \
  CGO_ENABLED=0 \
  GOEXPERIMENT="$RELEASE_GOEXPERIMENT" \
  GOAMD64="$RELEASE_GOAMD64" \
  GOARM64="$RELEASE_GOARM64" \
  GOFLAGS= \
  ard check main.ard
env \
  CGO_ENABLED=0 \
  GOEXPERIMENT="$RELEASE_GOEXPERIMENT" \
  GOAMD64="$RELEASE_GOAMD64" \
  GOARM64="$RELEASE_GOARM64" \
  GOFLAGS= \
  ard test
env \
  CGO_ENABLED=0 \
  GOEXPERIMENT="$RELEASE_GOEXPERIMENT" \
  GOAMD64="$RELEASE_GOAMD64" \
  GOARM64="$RELEASE_GOARM64" \
  GOFLAGS= \
  go test ./...

rm -rf "$OUTDIR"
mkdir -p "$OUTDIR"
HOST_OS=$(go env GOOS)
HOST_ARCH=$(go env GOARCH)
if [[ $HOST_OS != darwin || $HOST_ARCH != arm64 ]]; then
  echo "Error: the pinned release host is macOS arm64" >&2
  exit 1
fi
if ! arch -x86_64 /usr/bin/true >/dev/null 2>&1; then
  echo "Error: Rosetta 2 is required to test the macOS amd64 artifact" >&2
  exit 1
fi
if [[ $SKIP_LINUX_SMOKE == false ]]; then
  if ! command -v container >/dev/null 2>&1; then
    echo "Error: Apple Container CLI is required to test Linux artifacts" >&2
    exit 1
  fi
  if [[ $(container --version) != "$EXPECTED_CONTAINER_VERSION" ]]; then
    echo "Error: releases require $EXPECTED_CONTAINER_VERSION" >&2
    exit 1
  fi
  if ! container system status 2>/dev/null | grep -Eq '^status[[:space:]]+running$'; then
    echo "Error: Apple Container must be running to test Linux artifacts (or pass --skip-linux-smoke explicitly during a dry run)" >&2
    exit 1
  fi
else
  echo "Warning: Linux runtime smoke tests are explicitly disabled." >&2
fi
GENERATED_GO_CURRENT="$REPO_ROOT/ard-out/go/build"
GENERATED_GO_A="$OUTDIR/generated-go-a"
GENERATED_GO_B="$OUTDIR/generated-go-b"
LINK_FLAGS="-buildid= -s -w -X kb/ffi/buildinfo.Version=$VERSION"
cat > "$OUTDIR/TOOLCHAIN.txt" <<TOOLCHAIN
version=$VERSION
commit=$RELEASE_COMMIT
ard=$(ard version)
go=$(go version)
python=$(python3 --version)
zlib=$ZLIB_VERSION
macos=$(sw_vers -productVersion)
host=$HOST_OS/$HOST_ARCH
linux_smoke_runtime=$(command -v container >/dev/null 2>&1 && container --version || echo unavailable)
linux_smoke_image=$LINUX_SMOKE_IMAGE
linux_smoke=$([[ $SKIP_LINUX_SMOKE == true ]] && echo skipped || echo passed)
goexperiment=$RELEASE_GOEXPERIMENT
goamd64=$RELEASE_GOAMD64
goarm64=$RELEASE_GOARM64
TOOLCHAIN

echo
echo "==> Generating Go release source twice"
for pass in a b; do
  rm -rf "$GENERATED_GO_CURRENT"
  env \
    CGO_ENABLED=0 \
    GOEXPERIMENT="$RELEASE_GOEXPERIMENT" \
    GOAMD64="$RELEASE_GOAMD64" \
    GOARM64="$RELEASE_GOARM64" \
    GOFLAGS="-trimpath" \
    ard build main.ard --out "$OUTDIR/.generated-kb-$pass"
  rm -f "$OUTDIR/.generated-kb-$pass"
  # Ard emits functions and private temporaries from process-global maps and
  # counters. Canonicalize each independently generated representation.
  go run scripts/normalize_generated_go.go "$GENERATED_GO_CURRENT"
  if [[ $pass == a ]]; then
    cp -R "$GENERATED_GO_CURRENT" "$GENERATED_GO_A"
  else
    cp -R "$GENERATED_GO_CURRENT" "$GENERATED_GO_B"
  fi
done
diff -qr "$GENERATED_GO_A" "$GENERATED_GO_B"

build_binary() {
  local source=$1
  local cache=$2
  local goos=$3
  local goarch=$4
  local output=$5
  (
    cd "$source"
    env \
      GOOS="$goos" \
      GOARCH="$goarch" \
      CGO_ENABLED=0 \
      GOCACHE="$cache" \
      GOEXPERIMENT="$RELEASE_GOEXPERIMENT" \
      GOAMD64="$RELEASE_GOAMD64" \
      GOARM64="$RELEASE_GOARM64" \
      GOFLAGS= \
      go build -mod=mod -trimpath -ldflags "$LINK_FLAGS" -o "$output" .
  )
}

smoke_darwin() {
  local goarch=$1
  local binary=$2
  local probe=$3
  local -a runner=()
  if [[ $goarch == amd64 ]]; then
    runner=(arch -x86_64)
  fi

  test "$("${runner[@]}" "$binary" version)" = "kb $VERSION"
  test "$("${runner[@]}" "$binary" --version)" = "kb $VERSION"
  "${runner[@]}" "$binary" help | grep -Fq "kb $VERSION"
  rm -rf "$probe"
  env KB_DATABASE="$probe/index.sqlite" "${runner[@]}" "$binary" version >/dev/null
  env KB_DATABASE="$probe/index.sqlite" "${runner[@]}" "$binary" help >/dev/null
  test ! -e "$probe"
  env KB_DATABASE="$probe/index.sqlite" "${runner[@]}" "$binary" doctor >/dev/null
  rm -rf "$probe"
}

smoke_linux() {
  local goarch=$1
  local binary=$2
  local platform="linux/$goarch"
  local image=$LINUX_SMOKE_IMAGE
  local directory
  local output
  local -a runtime_options=(
    run --rm --platform "$platform"
    --mount "type=bind,source=$(dirname "$binary"),target=/release,readonly"
  )
  if [[ $goarch == amd64 ]]; then
    runtime_options+=(--rosetta)
  fi

  directory=$(dirname "$binary")
  test -d "$directory"
  output=$(container "${runtime_options[@]}" "$image" /release/kb version)
  test "$output" = "kb $VERSION"
  container "${runtime_options[@]}" "$image" /release/kb help \
    | grep -Fq "kb $VERSION"
  container "${runtime_options[@]}" -e KB_DATABASE=/tmp/kb/index.sqlite \
    "$image" /release/kb doctor >/dev/null
}

echo
echo "==> Building kb $VERSION"
for platform in "${PLATFORMS[@]}"; do
  GOOS=${platform%/*}
  GOARCH=${platform#*/}
  NAME="kb-$GOOS-$GOARCH"
  STAGE="$OUTDIR/$NAME"
  BINARY="$STAGE/kb"
  ARCHIVE="$OUTDIR/$NAME.tar.gz"
  mkdir -p "$STAGE"

  echo "  Building $NAME"

  # Ard currently has no linker-flags option. Build its generated Go workspace
  # directly so release metadata and an empty deterministic build ID are
  # applied without relying on GOFLAGS quoting. Build twice and require exact
  # equality before packaging.
  VERIFY_BINARY="$STAGE/kb.verify"
  build_binary "$GENERATED_GO_A" "$OUTDIR/go-cache-a" "$GOOS" "$GOARCH" "$BINARY"
  build_binary "$GENERATED_GO_B" "$OUTDIR/go-cache-b" "$GOOS" "$GOARCH" "$VERIFY_BINARY"
  cmp "$BINARY" "$VERIFY_BINARY"
  rm -f "$VERIFY_BINARY"

  python3 - "$BINARY" "$VERSION" <<'PY'
import pathlib
import sys
binary = pathlib.Path(sys.argv[1]).read_bytes()
version = sys.argv[2].encode()
if version not in binary:
    raise SystemExit(f"embedded version {sys.argv[2]!r} not found in {sys.argv[1]}")
PY

  if [[ $GOOS == darwin ]]; then
    smoke_darwin "$GOARCH" "$BINARY" "$OUTDIR/smoke-$NAME"
  elif [[ $SKIP_LINUX_SMOKE == false ]]; then
    smoke_linux "$GOARCH" "$BINARY"
  fi

  VERIFY_ARCHIVE="$OUTDIR/.$NAME.verify.tar.gz"
  python3 scripts/package_release.py \
    --binary "$BINARY" \
    --license LICENSE \
    --readme README.md \
    --output "$ARCHIVE"
  python3 scripts/package_release.py \
    --binary "$BINARY" \
    --license LICENSE \
    --readme README.md \
    --output "$VERIFY_ARCHIVE"
  cmp "$ARCHIVE" "$VERIFY_ARCHIVE"
  rm -f "$VERIFY_ARCHIVE"
done

rm -rf "$OUTDIR"/kb-*/ "$GENERATED_GO_A" "$GENERATED_GO_B" \
  "$OUTDIR/go-cache-a" "$OUTDIR/go-cache-b"

echo
echo "==> SHA256 checksums"
: > "$OUTDIR/SHA256SUMS.txt"
for archive in "$OUTDIR"/*.tar.gz; do
  printf '%s  %s\n' "$(checksum "$archive")" "$(basename "$archive")" \
    >> "$OUTDIR/SHA256SUMS.txt"
done
cat "$OUTDIR/SHA256SUMS.txt"

DARWIN_ARM64_SHA=$(checksum "$OUTDIR/kb-darwin-arm64.tar.gz")
DARWIN_AMD64_SHA=$(checksum "$OUTDIR/kb-darwin-amd64.tar.gz")
LINUX_ARM64_SHA=$(checksum "$OUTDIR/kb-linux-arm64.tar.gz")
LINUX_AMD64_SHA=$(checksum "$OUTDIR/kb-linux-amd64.tar.gz")
FORMULA="$OUTDIR/kb.rb"
cat > "$FORMULA" <<FORMULA
class Kb < Formula
  desc "Fast, private, local knowledge base for Markdown"
  homepage "https://github.com/akonwi/kb"
  version "$SEMVER"
  license "MIT"

  if OS.mac?
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "https://github.com/akonwi/kb/releases/download/$VERSION/kb-darwin-arm64.tar.gz"
      sha256 "$DARWIN_ARM64_SHA"
    elsif Hardware::CPU.intel? && Hardware::CPU.is_64_bit?
      url "https://github.com/akonwi/kb/releases/download/$VERSION/kb-darwin-amd64.tar.gz"
      sha256 "$DARWIN_AMD64_SHA"
    else
      odie "kb supports only arm64 and x86_64"
    end
  elsif OS.linux?
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "https://github.com/akonwi/kb/releases/download/$VERSION/kb-linux-arm64.tar.gz"
      sha256 "$LINUX_ARM64_SHA"
    elsif Hardware::CPU.intel? && Hardware::CPU.is_64_bit?
      url "https://github.com/akonwi/kb/releases/download/$VERSION/kb-linux-amd64.tar.gz"
      sha256 "$LINUX_AMD64_SHA"
    else
      odie "kb supports only arm64 and x86_64"
    end
  end

  def install
    bin.install "kb"
  end

  test do
    assert_equal "kb $VERSION", shell_output("#{bin}/kb version").strip
  end
end
FORMULA

echo
echo "==> Homebrew formula"
cat "$FORMULA"

echo
echo "Release artifacts are in:"
echo "  $OUTDIR"

if [[ $DRY_RUN == true ]]; then
  echo
  echo "==> Dry run complete; no tag was created and no publication commands are shown."
  exit 0
fi

echo
echo "==> Revalidating release state"
test "$(git rev-parse HEAD)" = "$RELEASE_COMMIT"
test "$(git branch --show-current)" = main
test -z "$(git status --porcelain)"
test "$(git ls-remote origin refs/heads/main | awk '{print $1}')" = "$RELEASE_COMMIT"
if remote_tag_exists; then
  echo "Error: tag $VERSION appeared on origin during the build" >&2
  exit 1
fi
echo "==> Tagging $VERSION at $RELEASE_COMMIT"
git tag -a "$VERSION" "$RELEASE_COMMIT" -m "$VERSION"

cat <<INSTRUCTIONS

Next steps:
  git push origin $VERSION
  gh release create $VERSION \\
    $OUTDIR/*.tar.gz \\
    $OUTDIR/SHA256SUMS.txt \\
    $OUTDIR/TOOLCHAIN.txt \\
    --title "$VERSION" \\
    --notes "TODO: write release notes"

Then update the Homebrew tap:
  cp $FORMULA ../homebrew-tap/Formula/kb.rb
  cd ../homebrew-tap
  brew audit --strict Formula/kb.rb
  git add Formula/kb.rb
  git commit -m "kb $VERSION"
  git push
INSTRUCTIONS
