#!/usr/bin/env python3
"""Create a deterministic kb release archive."""

from __future__ import annotations

import argparse
import gzip
import io
import os
from pathlib import Path
import tarfile


def add_file(archive: tarfile.TarFile, source: Path, name: str, mode: int, mtime: int) -> None:
    data = source.read_bytes()
    info = tarfile.TarInfo(name)
    info.size = len(data)
    info.mode = mode
    info.mtime = mtime
    info.uid = 0
    info.gid = 0
    info.uname = "root"
    info.gname = "root"
    archive.addfile(info, io.BytesIO(data))


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--license", required=True, type=Path)
    parser.add_argument("--readme", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()

    mtime = int(os.environ.get("SOURCE_DATE_EPOCH", "0"))
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with args.output.open("wb") as raw:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
                add_file(archive, args.binary, "kb", 0o755, mtime)
                add_file(archive, args.license, "LICENSE", 0o644, mtime)
                add_file(archive, args.readme, "README.md", 0o644, mtime)


if __name__ == "__main__":
    main()
