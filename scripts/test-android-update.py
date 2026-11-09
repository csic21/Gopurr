#!/usr/bin/env python3
"""Regressions for the Android update feed."""

import hashlib
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location("android_update", ROOT / "scripts/package-android-update.py")
UPDATE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(UPDATE)
TAG = "v0.6.29"
VERSION = TAG[1:]


class AndroidUpdateTest(unittest.TestCase):
    def fixture(self, directory):
        directory.mkdir()
        for index, abi in enumerate(UPDATE.ABIS):
            (directory / f"gopurr-{VERSION}-android-{abi}.apk").write_bytes(bytes(range(index + 1, 32 + index)))
        notes = Path(directory) / "notes.md"
        notes.write_text("Gopurr update summary.\n\n- Mobile updates.\n\n下载：\n\n- APK\n", encoding="utf-8")
        return directory, notes

    def test_manifest_authenticates_each_apk_and_omits_download_section(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory, notes = self.fixture(Path(temporary) / "dist")
            manifest = UPDATE.build_manifest(TAG, 1000042, directory, notes, "csic21/Gopurr")
            self.assertEqual(manifest["schema"], 1)
            self.assertEqual(manifest["version"], VERSION)
            self.assertEqual(manifest["versionCode"], 1000042)
            self.assertEqual(manifest["notes"], "Gopurr update summary.\n\n- Mobile updates.")
            self.assertEqual(set(manifest["assets"]), set(UPDATE.ABIS))
            for abi in UPDATE.ABIS:
                name = f"gopurr-{VERSION}-android-{abi}.apk"
                data = (directory / name).read_bytes()
                self.assertEqual(manifest["assets"][abi]["size"], len(data))
                self.assertEqual(manifest["assets"][abi]["sha256"], hashlib.sha256(data).hexdigest())
                self.assertEqual(manifest["assets"][abi]["url"],
                                 f"https://github.com/csic21/Gopurr/releases/download/{TAG}/{name}")
            output = UPDATE.write_manifest(manifest, directory / "update-android.json")
            self.assertEqual(json.loads(output.read_text()), manifest)
            self.assertEqual(output.stat().st_mode & 0o777, 0o644)

    def test_missing_or_ambiguous_release_inputs_fail_closed(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory, notes = self.fixture(Path(temporary) / "dist")
            for tag, code, repository in [
                ("0.6.29", 1000042, "csic21/Gopurr"),
                ("v0.6", 1000042, "csic21/Gopurr"),
                (TAG, 0, "csic21/Gopurr"),
                (TAG, 2100000001, "csic21/Gopurr"),
                (TAG, 1000042, "not-a-repository"),
            ]:
                with self.subTest(tag=tag, code=code, repository=repository):
                    with self.assertRaises(ValueError):
                        UPDATE.build_manifest(tag, code, directory, notes, repository)
            (directory / f"gopurr-{VERSION}-android-x86_64.apk").unlink()
            with self.assertRaises(ValueError):
                UPDATE.build_manifest(TAG, 1000042, directory, notes, "csic21/Gopurr")

    def test_symlink_apk_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory, notes = self.fixture(Path(temporary) / "dist")
            target = Path(temporary) / "outside.apk"
            target.write_bytes(b"outside")
            apk = directory / f"gopurr-{VERSION}-android-arm64-v8a.apk"
            apk.unlink()
            apk.symlink_to(target)
            with self.assertRaises(ValueError):
                UPDATE.build_manifest(TAG, 1000042, directory, notes, "csic21/Gopurr")


if __name__ == "__main__":
    unittest.main()
