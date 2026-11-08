#!/usr/bin/env python3
"""Prepare build metadata without changing the checked-in development manifest."""
import argparse
import json
from pathlib import Path
import re
import xml.etree.ElementTree as ET

ANDROID = "http://schemas.android.com/apk/res/android"
ROOT = Path(__file__).resolve().parent.parent


def prepare(mode, version, version_code, output):
    configured = json.loads((ROOT / "mygo.json").read_text())["version"]
    version = version.removeprefix("v") if version else configured
    if not re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?", version):
        raise ValueError("Invalid Android version")
    if version != configured:
        raise ValueError("Release tag must match the version in mygo.json")
    if not re.fullmatch(r"[1-9][0-9]*", version_code) or int(version_code) > 2100000000:
        raise ValueError("Android version code must be between 1 and 2100000000")
    ET.register_namespace("android", ANDROID)
    tree = ET.parse(ROOT / "native/android/AndroidManifest.xml")
    tree.getroot().find("application").set(f"{{{ANDROID}}}debuggable", str(mode == "debug").lower())
    output.parent.mkdir(parents=True, exist_ok=True)
    tree.write(output, encoding="utf-8", xml_declaration=True)
    return version, version_code


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mode", choices=("debug", "release"), required=True)
    parser.add_argument("--version", default="")
    parser.add_argument("--version-code", required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    try:
        print(*prepare(args.mode, args.version, args.version_code, args.out))
    except ValueError as error:
        parser.exit(1, f"{error}\n")
