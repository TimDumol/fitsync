"""Exercise the Android vendor's token writer under a permissive Unix umask."""

import importlib.util
import json
import os
from pathlib import Path
import stat
import sys
import tempfile
from types import ModuleType
import unittest
from unittest.mock import patch


@unittest.skipUnless(os.name == "posix", "Unix filesystem permission checks")
class GarminTokenPermissionsTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        source = Path(__file__).parent / "apps/android/app/src/production/python/garminconnect"
        package = ModuleType("_fitsync_vendor_permissions")
        package.__path__ = [str(source)]
        spec = importlib.util.spec_from_file_location(package.__name__ + ".client", source / "client.py")
        module = importlib.util.module_from_spec(spec)
        with patch.dict(sys.modules, {package.__name__: package}):
            spec.loader.exec_module(module)
        cls.client_type = module.Client

    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name) / "tokens"
        self.client = self.client_type.__new__(self.client_type)
        self.client.di_token = "synthetic-access"
        self.client.di_refresh_token = "synthetic-refresh"
        self.client.di_client_id = "synthetic-client"

    def dump_with_permissive_umask(self):
        previous = os.umask(0o022)
        try:
            self.client.dump(str(self.root))
        finally:
            os.umask(previous)

    def test_new_and_existing_files_are_owner_only(self):
        self.dump_with_permissive_umask()
        token = self.root / "garmin_tokens.json"
        for existing in (False, True):
            if existing:
                self.root.chmod(0o755)
                token.chmod(0o644)
                self.dump_with_permissive_umask()
            self.assertEqual(stat.S_IMODE(self.root.stat().st_mode), 0o700)
            self.assertEqual(stat.S_IMODE(token.stat().st_mode), 0o600)
            self.assertEqual(json.loads(token.read_text())["di_refresh_token"], "synthetic-refresh")

    @unittest.skipUnless(hasattr(os, "O_NOFOLLOW"), "Requires symlink-safe open")
    def test_symlink_cannot_redirect_token_write(self):
        self.root.mkdir()
        target = Path(self.directory.name) / "unrelated.json"
        target.write_text("preserved")
        (self.root / "garmin_tokens.json").symlink_to(target)
        with self.assertRaises(OSError):
            self.dump_with_permissive_umask()
        self.assertEqual(target.read_text(), "preserved")
