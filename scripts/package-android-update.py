#!/usr/bin/env python3
"""Build the Android update feed from signed release APKs."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import tempfile

VERSION = re.compile(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?")
REPOSITORY = re.compile(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+")
ABIS = ("arm64-v8a", "x86_64")
MAX_APK_BYTES = 320 * 1024 * 1024
MAX_NOTES_BYTES = 256 * 1024


def _digest(path):
    value = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def _asset(directory, version, abi, tag, repository):
    name = f"gopurr-{version}-android-{abi}.apk"
    path = directory / name
    if path.is_symlink() or not path.is_file() or path.stat().st_size == 0:
        raise ValueError(f"Missing Android release APK: {name}")
    size = path.stat().st_size
    if size > MAX_APK_BYTES:
        raise ValueError(f"Android release APK is too large: {name}")
    return {
        "url": f"https://github.com/{repository}/releases/download/{tag}/{name}",
        "size": size,
        "sha256": _digest(path),
    }


def build_manifest(tag, version_code, directory, notes_path, repository):
    if not tag.startswith("v") or not VERSION.fullmatch(tag[1:]):
        raise ValueError("Tag must be a stable vMAJOR.MINOR.PATCH version")
    version = tag[1:]
    if version_code < 1 or version_code > 2_100_000_000:
        raise ValueError("Android version code must be between 1 and 2100000000")
    if not REPOSITORY.fullmatch(repository):
        raise ValueError("Repository must be OWNER/REPO")
    directory = Path(directory)
    if not directory.is_dir():
        raise ValueError("The release artifact directory does not exist")
    notes_path = Path(notes_path)
    if not notes_path.is_file() or notes_path.stat().st_size > MAX_NOTES_BYTES:
        raise ValueError("Release notes are missing or too large")
    notes = notes_path.read_text(encoding="utf-8").strip()
    for marker in ("\n下载：", "\nDownload:"):
        if marker in notes:
            notes = notes.split(marker, 1)[0].strip()
            break
    if len(notes.encode("utf-8")) > MAX_NOTES_BYTES:
        raise ValueError("Release summary is too large")
    return {
        "schema": 1,
        "version": version,
        "versionCode": version_code,
        "tag": tag,
        "notes": notes,
        "assets": {abi: _asset(directory, version, abi, tag, repository) for abi in ABIS},
    }


def write_manifest(manifest, output):
    output = Path(output)
    output.parent.mkdir(parents=True, exist_ok=True)
    body = (json.dumps(manifest, ensure_ascii=False, indent=2) + "\n").encode("utf-8")
    fd, temporary = tempfile.mkstemp(prefix=output.name + ".", dir=output.parent)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(body)
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(temporary, 0o644)
        os.replace(temporary, output)
    finally:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--version-code", required=True, type=int)
    parser.add_argument("--dir", required=True, type=Path)
    parser.add_argument("--notes", required=True, type=Path)
    parser.add_argument("--repository", default="csic21/Gopurr")
    parser.add_argument("--out", type=Path)
    args = parser.parse_args()
    try:
        manifest = build_manifest(args.tag, args.version_code, args.dir, args.notes, args.repository)
        output = args.out or (args.dir / "update-android.json")
        write_manifest(manifest, output)
    except ValueError as error:
        parser.exit(1, f"{error}\n")
    print(output)


if __name__ == "__main__":
    main()
