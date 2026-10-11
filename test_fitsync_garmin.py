import json
import unittest
from datetime import date
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import patch

from fitsync_garmin.auth import GarminSyncError, cancel_login, resume_login, start_login
from fitsync_garmin.datasets import sync_weight_measurements
from fitsync_garmin.sleep import summarize_sleep
from fitsync_garmin.uploader import MOBILE_MAX_RECORDS, upload_mobile, split_mobile_records


class _TokenClient:
    def dumps(self):
        return json.dumps({"di_token": "token"})


class _FakeGarmin:
    next_status = "connected"
    last = None

    def __init__(self, email, password, return_on_mfa=False):
        self.email = email
        self.password = password
        self.client = _TokenClient()
        _FakeGarmin.last = self

    def login(self):
        return self.next_status, None

    def resume_login(self, state, code):
        if code != "123456":
            raise RuntimeError("incorrect code")
        return None, None


class _UploadResponse:
    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return False

    def read(self):
        return b'{"records":1,"archives":0}'


class _WeightApi:
    def __init__(self, payload):
        self.payload = payload
        self.calls = []

    def get_weigh_ins(self, start, end):
        self.calls.append(("get_weigh_ins", start, end))
        return self.payload

    def __getattr__(self, name):
        if name in {"get_body_composition", "get_blood_pressure", "get_training_status"}:
            raise AssertionError(f"disabled Garmin endpoint was called: {name}")
        raise AttributeError(name)


class GarminCoreTests(unittest.TestCase):
    def tearDown(self):
        cancel_login("fixture-login")
        cancel_login("anything")

    def test_login_connected_never_returns_password(self):
        result = start_login("person@example.com", "password", garmin_factory=_FakeGarmin)
        self.assertEqual(result["status"], "connected")
        self.assertNotIn("password", json.dumps(result))

    def test_mfa_resume_uses_live_client_and_expires(self):
        _FakeGarmin.next_status = "needs_mfa"
        with patch("fitsync_garmin.auth.time.monotonic", side_effect=[100, 100, 701]):
            result = start_login("person@example.com", "password", garmin_factory=_FakeGarmin)
            with self.assertRaises(GarminSyncError) as raised:
                resume_login(result["login_id"], "123456")
        self.assertEqual(raised.exception.code, "GARMIN_MFA_EXPIRED")
        result = start_login("person@example.com", "password", garmin_factory=_FakeGarmin)
        self.assertEqual(result["status"], "needs_mfa")
        connected = resume_login(result["login_id"], "123456")
        self.assertEqual(connected["status"], "connected")
        _FakeGarmin.next_status = "connected"

    def test_mobile_batches_are_limited(self):
        records = [{"dataset": "sleep", "record_key": str(i), "record_date": "2026-01-01", "payload_raw": {"i": i}} for i in range(MOBILE_MAX_RECORDS + 1)]
        batches = split_mobile_records(records)
        self.assertEqual([len(batch) for batch in batches], [MOBILE_MAX_RECORDS, 1])

    def test_cancelled_mfa_cannot_be_resumed(self):
        _FakeGarmin.next_status = "needs_mfa"
        result = start_login("person@example.com", "password", garmin_factory=_FakeGarmin)
        cancel_login(result["login_id"])
        with self.assertRaises(GarminSyncError) as raised:
            resume_login(result["login_id"], "123456")
        self.assertEqual(raised.exception.code, "GARMIN_MFA_EXPIRED")
        _FakeGarmin.next_status = "connected"

    def test_incorrect_mfa_code_is_a_safe_auth_error(self):
        _FakeGarmin.next_status = "needs_mfa"
        result = start_login("person@example.com", "password", garmin_factory=_FakeGarmin)
        with self.assertRaises(GarminSyncError) as raised:
            resume_login(result["login_id"], "000000")
        self.assertEqual(raised.exception.code, "GARMIN_AUTH_FAILED")
        _FakeGarmin.next_status = "connected"

    def test_successful_mobile_upload_removes_local_retry_data(self):
        with TemporaryDirectory() as directory:
            work_dir = Path(directory)
            raw_dir = work_dir / "raw" / "sleep"
            raw_dir.mkdir(parents=True)
            (raw_dir / "2026-01-01.json").write_text('{"sleep": {"score": 80}}', encoding="utf-8")
            result = upload_mobile(
                {"athlete_id": "00000000-0000-4000-8000-000000000001", "api_base_url": "https://api.example.test", "work_dir": str(work_dir), "started_at": "2026-01-01T00:00:00Z"},
                "access-token",
                urlopen_fn=lambda _request, timeout: _UploadResponse(),
            )
            self.assertEqual(result["records_sent"], 1)
            self.assertFalse((work_dir / "manifest.json").exists())
            self.assertFalse((work_dir / "raw").exists())

    def test_mobile_extended_measurements_fetch_weight_without_composition_endpoints(self):
        with TemporaryDirectory() as directory:
            work_dir = Path(directory)
            api = _WeightApi({"dateWeightList": [{"calendarDate": "2026-09-03", "weight": 81.2}]})

            self.assertTrue(sync_weight_measurements(api, date(2026, 9, 1), date(2026, 9, 3), work_dir))
            self.assertEqual(api.calls, [("get_weigh_ins", "2026-09-01", "2026-09-03")])
            self.assertEqual(
                json.loads((work_dir / "raw" / "body_measurements" / "2026-09-03.json").read_text()),
                {"weight": {"dateWeightList": [{"calendarDate": "2026-09-03", "weight": 81.2}]}},
            )

    def test_mobile_extended_measurements_omits_empty_weight_response(self):
        with TemporaryDirectory() as directory:
            work_dir = Path(directory)
            api = _WeightApi({"dateWeightList": [], "totalAverage": {}})

            self.assertFalse(sync_weight_measurements(api, date(2026, 9, 1), date(2026, 9, 3), work_dir))
            self.assertFalse((work_dir / "body_measurements.json").exists())

    def test_curated_sleep_uses_canonical_24_hour_names(self):
        result = summarize_sleep(
            "2026-09-04",
            {"dailySleepDTO": {"calendarDate": "2026-09-04", "sleepTimeSeconds": 22608, "napTimeSeconds": 1800}},
            None,
            None,
        )
        self.assertEqual(result["main_sleep_hours"], 6.28)
        self.assertEqual(result["nap_sleep_hours"], 0.5)
        self.assertEqual(result["total_sleep_24h"], 6.78)
        self.assertEqual(result["total_sleep_hours"], 6.28)
        self.assertEqual(result["total_sleep_including_naps_hours"], 6.78)


if __name__ == "__main__":
    unittest.main()
