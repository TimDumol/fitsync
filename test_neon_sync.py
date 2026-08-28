import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from neon_sync import (
    SCHEMA_SQL,
    collect_export_records,
    resolve_neon_target,
)


class NeonSyncTests(unittest.TestCase):
    def test_direct_database_url_does_not_require_api_key(self) -> None:
        target = resolve_neon_target(
            {"NEON_DATABASE_URL": "postgresql://fitness@example.test/neondb"}
        )

        self.assertEqual(
            target.connection_uri, "postgresql://fitness@example.test/neondb"
        )
        self.assertIsNone(target.project_id)

    @patch("neon_sync._api_get")
    def test_explicit_project_id_skips_account_wide_discovery(self, api_get) -> None:
        def fake_api_get(_api_key, path, query=None):
            self.assertNotEqual(path, "/projects")
            if path == "/projects/project-123/branches":
                return {"branches": [{"id": "branch-123", "primary": True}]}
            if path == "/projects/project-123/branches/branch-123/databases":
                return {
                    "databases": [
                        {"name": "neondb", "owner_name": "neondb_owner"}
                    ]
                }
            if path == "/projects/project-123/connection_uri":
                self.assertEqual(query["branch_id"], "branch-123")
                return {"uri": "postgresql://fitness@example.test/neondb"}
            self.fail(f"Unexpected Neon API path: {path}")

        api_get.side_effect = fake_api_get

        target = resolve_neon_target(
            {"NEON_API_KEY": "secret", "NEON_PROJECT_ID": "project-123"}
        )

        self.assertEqual(target.project_id, "project-123")
        self.assertEqual(target.database_name, "neondb")
        self.assertEqual(target.role_name, "neondb_owner")
        self.assertEqual(api_get.call_count, 3)

    def test_collects_only_curated_json_exports(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            data_dir = Path(temporary_directory)
            (data_dir / "sleep.json").write_text(
                json.dumps([{"date": "2026-08-18", "nap_hours": 0.8}]),
                encoding="utf-8",
            )
            (data_dir / "activities.json").write_text(
                json.dumps(
                    [
                        {
                            "activity_id": 123,
                            "start_time_local": "2026-08-18 08:00:00",
                            "activity_type": "running",
                        }
                    ]
                ),
                encoding="utf-8",
            )
            (data_dir / "training_snapshot.json").write_text(
                json.dumps({"captured_date": "2026-08-19", "race_predictions": {}}),
                encoding="utf-8",
            )
            details_dir = data_dir / "activity_details"
            details_dir.mkdir()
            (details_dir / "123.json").write_text(
                json.dumps({"activity_id": 123, "splits": []}), encoding="utf-8"
            )
            (data_dir / "ignored.csv").write_text("private,data", encoding="utf-8")

            records = collect_export_records(data_dir)

        self.assertEqual(len(records), 4)
        self.assertEqual(
            {record.dataset for record in records},
            {"sleep", "activities", "training_snapshot", "activity_details"},
        )
        sleep = next(record for record in records if record.dataset == "sleep")
        self.assertEqual(sleep.record_key, "2026-08-18")
        self.assertEqual(sleep.record_date.isoformat(), "2026-08-18")
        self.assertEqual(len(sleep.payload_sha256), 64)

    def test_schema_supports_structured_manual_workout_logs(self) -> None:
        self.assertIn("fitness.workout_sessions", SCHEMA_SQL)
        self.assertIn("fitness.workout_entries", SCHEMA_SQL)
        self.assertIn("fitness_agent.workout_sessions", SCHEMA_SQL)
        self.assertIn("fitness_agent.workout_entries", SCHEMA_SQL)
        self.assertIn("manual_chat_log", SCHEMA_SQL)

if __name__ == "__main__":
    unittest.main()
