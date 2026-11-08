#!/usr/bin/env python3
"""Regressions for Android release metadata and fail-closed signing."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parent.parent
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("manifest", ROOT / "scripts/prepare-android-manifest.py")
manifest = importlib.util.module_from_spec(spec)
spec.loader.exec_module(manifest)
VERSION = json.loads((ROOT / "mygo.json").read_text())["version"]
SOURCE = ROOT / "native/android/AndroidManifest.xml"
DEBUGGABLE = f"{{{manifest.ANDROID}}}debuggable"


class AndroidReleaseTest(unittest.TestCase):
    def test_release_disables_debugging_without_mutating_development_manifest(self):
        original = SOURCE.read_bytes()
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "AndroidManifest.xml"
            self.assertEqual(manifest.prepare("release", f"v{VERSION}", "1000042", output), (VERSION, "1000042"))
            self.assertEqual(ET.parse(output).getroot().find("application").get(DEBUGGABLE), "false")
        self.assertEqual(SOURCE.read_bytes(), original)

    def test_development_build_remains_debuggable(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "AndroidManifest.xml"
            manifest.prepare("debug", "", "1", output)
            self.assertEqual(ET.parse(output).getroot().find("application").get(DEBUGGABLE), "true")

    def test_wrong_tag_or_invalid_version_code_never_produces_a_manifest(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "AndroidManifest.xml"
            for version, code in [("v999.0.0", "42"), ("not-a-version", "42"),
                                  (VERSION, "0"), (VERSION, "-1"), (VERSION, "1.2"),
                                  (VERSION, "01"), (VERSION, "2100000001")]:
                with self.subTest(version=version, code=code):
                    with self.assertRaises(ValueError):
                        manifest.prepare("release", version, code, output)
                    self.assertFalse(output.exists())

    def test_missing_signing_configuration_fails_before_building(self):
        env = {name: value for name, value in os.environ.items() if not name.startswith("GOPURR_ANDROID_")}
        result = subprocess.run(["bash", "scripts/build-native-android.sh", "arm64-v8a", "release", f"v{VERSION}"],
                                cwd=ROOT, env=env, text=True, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Release requires an Android signing keystore", result.stderr)
        self.assertNotIn("Built", result.stdout)


if __name__ == "__main__":
    unittest.main()
