import json
import tempfile
import unittest
from pathlib import Path
from urllib.error import URLError

from garmin_sync import (
    _canonical_json_bytes,
    _raw_ingest_records,
    upload_raw_to_fitnessd,
)


class _Response:
    def __init__(self, payload: bytes = b'{"records":2,"archives":0}'):
        self.payload = payload

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return False

    def read(self):
        return self.payload


class FitnessdIngestTests(unittest.TestCase):
    def test_raw_json_is_canonical_and_activity_details_get_activity_date(self) -> None:
        self.assertEqual(
            _canonical_json_bytes({"b": 2, "a": [True, "é"]}),
            b'{"a":[true,"\xc3\xa9"],"b":2}',
        )
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "raw"
            (root / "activities").mkdir(parents=True)
            (root / "activity_details").mkdir()
            (root / "activities" / "42.json").write_bytes(
                _canonical_json_bytes(
                    {"activityId": 42, "startTimeLocal": "2026-08-20T07:00:00", "latitude": 40.1}
                )
            )
            (root / "activity_details" / "42.json").write_bytes(
                _canonical_json_bytes({"activity_id": 42, "metric_time_series": [{"latitude": 40.1}]})
            )
            records = _raw_ingest_records(Path(directory))
            details = next(record for record in records if record["dataset"] == "activity_details")
            activities = next(record for record in records if record["dataset"] == "activities")
            self.assertEqual(details["record_date"], "2026-08-20")
            self.assertEqual(activities["payload_raw"]["latitude"], 40.1)
            self.assertEqual(details["payload_raw"]["metric_time_series"][0]["latitude"], 40.1)

    def test_upload_retries_with_same_canonical_batch(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "raw" / "sleep"
            root.mkdir(parents=True)
            (root / "2026-08-20.json").write_bytes(
                _canonical_json_bytes(
                    {"sleep": {"raw": 1}, "hrv": {}, "resting_heart_rate": {}}
                )
            )
            calls: list[bytes] = []
            delays: list[float] = []

            def opener(request, timeout):
                self.assertEqual(timeout, 30)
                calls.append(request.data)
                if len(calls) == 1:
                    raise URLError("temporary")
                return _Response()

            result = upload_raw_to_fitnessd(
                Path(directory),
                {"FITNESS_API_URL": "https://fitnessd.example", "FITNESS_INGEST_TOKEN": "secret"},
                urlopen_fn=opener,
                sleep_fn=delays.append,
            )
            self.assertEqual(result["records"], 2)
            self.assertEqual(len(calls), 2)
            self.assertEqual(calls[0], calls[1])
            self.assertEqual(delays, [1.0])
            self.assertIn(b'"payload_raw":{"hrv":{},"resting_heart_rate":{},"sleep":{"raw":1}}', calls[0])

    def test_upload_splits_cached_records_at_server_batch_limit(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "raw" / "sleep"
            root.mkdir(parents=True)
            for index in range(101):
                (root / f"record-{index}.json").write_bytes(
                    _canonical_json_bytes({"sleep": {"index": index}})
                )
            batch_sizes: list[int] = []

            def opener(request, timeout):
                self.assertEqual(timeout, 30)
                size = request.data.count(b'"dataset":"sleep"')
                batch_sizes.append(size)
                return _Response(json.dumps({"records": size, "archives": 0}).encode())

            result = upload_raw_to_fitnessd(
                Path(directory),
                {"FITNESS_API_URL": "https://fitnessd.example", "FITNESS_INGEST_TOKEN": "secret"},
                urlopen_fn=opener,
                sleep_fn=lambda _delay: None,
            )

            self.assertEqual(batch_sizes, [100, 1])
            self.assertEqual(result, {"records": 101, "archives": 0})


if __name__ == "__main__":
    unittest.main()
