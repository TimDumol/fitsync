"""Verify the Android-only adapter without requiring Chaquopy or a Garmin account."""

import importlib.util
import json
from pathlib import Path
from types import ModuleType
import unittest
from unittest.mock import patch


class NativeAndroidBridgeTests(unittest.TestCase):
    def test_progress_and_arguments_reach_the_shared_runner(self):
        phone = ModuleType("phone_garmin_bridge")
        phone._invoke = lambda callback, fallback: callback()
        phone.start_login = lambda *args: None
        phone.resume_login = lambda *args: None
        phone.cancel_login = lambda *args: None
        runner = ModuleType("fitsync_garmin.runner")
        calls = []

        def sync(config, token, bearer, report):
            calls.append((config, token, bearer))
            report({"stage": "uploading", "completed": 1, "total": 2, "message": "Uploading"})
            return {"records_sent": 3}

        runner.sync = sync
        source = Path(__file__).parent / "apps/android/app/src/production/python/native_phone_bridge.py"
        spec = importlib.util.spec_from_file_location("native_phone_bridge", source)
        adapter = importlib.util.module_from_spec(spec)
        with patch.dict("sys.modules", {"phone_garmin_bridge": phone, "fitsync_garmin.runner": runner}):
            spec.loader.exec_module(adapter)

        class Progress:
            events = []

            def emit(self, raw):
                self.events.append(json.loads(raw))

        progress = Progress()
        result = adapter.sync('{"days":3}', "synthetic-session", "synthetic-bearer", progress)
        self.assertEqual(calls, [({"days": 3}, "synthetic-session", "synthetic-bearer")])
        self.assertEqual(result, {"records_sent": 3})
        self.assertEqual(progress.events[0]["stage"], "uploading")


if __name__ == "__main__":
    unittest.main()
