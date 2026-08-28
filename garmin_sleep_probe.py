"""Read-only Garmin Connect probe focused on sleep and recovery data."""

from __future__ import annotations

import argparse
import getpass
import json
import os
import sys
from datetime import date, timedelta
from pathlib import Path
from typing import Any

from garminconnect import (
    Garmin,
    GarminConnectAuthenticationError,
    GarminConnectConnectionError,
    GarminConnectTooManyRequestsError,
)


DEFAULT_TOKEN_DIR = Path(__file__).resolve().parent / ".private" / "garmin"


def _nested(value: Any, *keys: str) -> Any:
    for key in keys:
        if not isinstance(value, dict):
            return None
        value = value.get(key)
    return value


def _first(*values: Any) -> Any:
    return next((value for value in values if value is not None), None)


def _hours(seconds: Any) -> float | None:
    if not isinstance(seconds, (int, float)):
        return None
    return round(seconds / 3600, 2)


def summarize_sleep(
    requested_date: str,
    sleep_payload: dict[str, Any],
    hrv_payload: dict[str, Any] | None,
    rhr_payload: dict[str, Any] | None,
) -> dict[str, Any]:
    """Reduce Garmin's changing response shapes to stable analysis fields."""
    daily = sleep_payload.get("dailySleepDTO") or sleep_payload
    scores = daily.get("sleepScores") if isinstance(daily, dict) else {}
    scores = scores if isinstance(scores, dict) else {}
    hrv_summary = _nested(hrv_payload, "hrvSummary") or {}
    sleep_seconds = daily.get("sleepTimeSeconds") if isinstance(daily, dict) else None
    nap_seconds = daily.get("napTimeSeconds") if isinstance(daily, dict) else None
    total_sleep_seconds = None
    if isinstance(sleep_seconds, (int, float)) or isinstance(nap_seconds, (int, float)):
        total_sleep_seconds = (sleep_seconds or 0) + (nap_seconds or 0)

    resting_hr = _first(
        _nested(rhr_payload, "allMetrics", "metricsMap", "WELLNESS_RESTING_HEART_RATE", 0, "value"),
        _nested(daily, "restingHeartRate"),
    )
    # Some API responses use a list under the metric name. Handle that shape
    # without relying on integer traversal in _nested.
    if resting_hr is None:
        metrics = _nested(rhr_payload, "allMetrics", "metricsMap", "WELLNESS_RESTING_HEART_RATE")
        if isinstance(metrics, list) and metrics and isinstance(metrics[0], dict):
            resting_hr = metrics[0].get("value")

    return {
        "date": _first(daily.get("calendarDate") if isinstance(daily, dict) else None, requested_date),
        "sleep_start": _first(
            daily.get("sleepStartTimestampLocal") if isinstance(daily, dict) else None,
            daily.get("sleepStartTimestampGMT") if isinstance(daily, dict) else None,
        ),
        "sleep_end": _first(
            daily.get("sleepEndTimestampLocal") if isinstance(daily, dict) else None,
            daily.get("sleepEndTimestampGMT") if isinstance(daily, dict) else None,
        ),
        "total_sleep_hours": _hours(sleep_seconds),
        "nap_hours": _hours(nap_seconds),
        "total_sleep_including_naps_hours": _hours(total_sleep_seconds),
        "deep_sleep_hours": _hours(daily.get("deepSleepSeconds") if isinstance(daily, dict) else None),
        "light_sleep_hours": _hours(daily.get("lightSleepSeconds") if isinstance(daily, dict) else None),
        "rem_sleep_hours": _hours(daily.get("remSleepSeconds") if isinstance(daily, dict) else None),
        "awake_hours": _hours(daily.get("awakeSleepSeconds") if isinstance(daily, dict) else None),
        "sleep_score": _first(
            _nested(scores, "overall", "value"),
            daily.get("sleepScore") if isinstance(daily, dict) else None,
        ),
        "sleep_score_qualifier": _nested(scores, "overall", "qualifierKey"),
        "average_spo2": daily.get("averageSpO2Value") if isinstance(daily, dict) else None,
        "lowest_spo2": daily.get("lowestSpO2Value") if isinstance(daily, dict) else None,
        "average_respiration": daily.get("averageRespirationValue") if isinstance(daily, dict) else None,
        "average_sleep_stress": daily.get("avgSleepStress") if isinstance(daily, dict) else None,
        "resting_heart_rate": resting_hr,
        "overnight_hrv": _first(
            hrv_summary.get("lastNightAvg") if isinstance(hrv_summary, dict) else None,
            _nested(hrv_payload, "lastNightAvg"),
        ),
        "hrv_weekly_average": hrv_summary.get("weeklyAvg") if isinstance(hrv_summary, dict) else None,
        "hrv_status": hrv_summary.get("status") if isinstance(hrv_summary, dict) else None,
    }


def _token_dir_from_environment() -> Path:
    configured = os.getenv("GARMIN_TOKEN_DIR")
    return Path(configured).expanduser().resolve() if configured else DEFAULT_TOKEN_DIR


def login() -> Garmin:
    token_dir = _token_dir_from_environment()
    token_file = token_dir / "garmin_tokens.json"

    if token_file.exists():
        api = Garmin()
    else:
        print("No saved Garmin token was found. Credentials are used only for this login.")
        email = input("Garmin email: ").strip()
        password = getpass.getpass("Garmin password: ")
        api = Garmin(
            email,
            password,
            prompt_mfa=lambda: input("Garmin MFA code: ").strip(),
        )

    api.login(str(token_dir))
    return api


def fetch_day(api: Garmin, day: date, raw_dir: Path | None) -> dict[str, Any]:
    day_text = day.isoformat()
    sleep_payload = api.get_sleep_data(day_text)

    try:
        hrv_payload = api.get_hrv_data(day_text)
    except GarminConnectConnectionError:
        hrv_payload = None

    try:
        rhr_payload = api.get_rhr_day(day_text)
    except GarminConnectConnectionError:
        rhr_payload = None

    if raw_dir is not None:
        raw_dir.mkdir(parents=True, exist_ok=True)
        raw_file = raw_dir / f"{day_text}.json"
        raw_file.write_text(
            json.dumps(
                {"sleep": sleep_payload, "hrv": hrv_payload, "resting_heart_rate": rhr_payload},
                indent=2,
                ensure_ascii=False,
            ),
            encoding="utf-8",
        )

    return summarize_sleep(day_text, sleep_payload, hrv_payload, rhr_payload)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Fetch read-only Garmin sleep/recovery summaries."
    )
    parser.add_argument(
        "--end-date",
        type=date.fromisoformat,
        default=date.today(),
        help="Last Garmin calendar date to fetch (YYYY-MM-DD; default: today).",
    )
    parser.add_argument(
        "--days",
        type=int,
        default=1,
        help="Number of consecutive days ending on --end-date (default: 1).",
    )
    parser.add_argument(
        "--raw-dir",
        type=Path,
        help="Optional directory for full raw responses. Treat these files as sensitive.",
    )
    parser.add_argument(
        "--output",
        type=Path,
        help="Optional file for the compact JSON summary.",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.days < 1 or args.days > 366:
        print("--days must be between 1 and 366", file=sys.stderr)
        return 2

    try:
        api = login()
        first_day = args.end_date - timedelta(days=args.days - 1)
        summaries = [
            fetch_day(api, first_day + timedelta(days=offset), args.raw_dir)
            for offset in range(args.days)
        ]
        summary_json = json.dumps(summaries, indent=2, ensure_ascii=False)
        if args.output is not None:
            args.output.parent.mkdir(parents=True, exist_ok=True)
            args.output.write_text(summary_json, encoding="utf-8")
            print(f"Saved {len(summaries)} day(s) to {args.output}")
        else:
            print(summary_json)
        return 0
    except GarminConnectAuthenticationError as exc:
        print(f"Garmin authentication failed: {exc}", file=sys.stderr)
    except GarminConnectTooManyRequestsError as exc:
        print(f"Garmin rate-limited the login or request: {exc}", file=sys.stderr)
    except GarminConnectConnectionError as exc:
        print(f"Garmin request failed: {exc}", file=sys.stderr)
    except KeyboardInterrupt:
        print("\nCancelled.", file=sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
