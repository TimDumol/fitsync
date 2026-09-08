"""Incrementally sync Garmin sleep/recovery and activity summaries."""

from __future__ import annotations

import argparse
import csv
import json
import os
import time
import sys
import uuid
from datetime import date, timedelta
from pathlib import Path
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

from garminconnect import (
    Garmin,
    GarminConnectAuthenticationError,
    GarminConnectConnectionError,
    GarminConnectTooManyRequestsError,
)

from .sleep import fetch_day, login


class FitnessdSyncError(RuntimeError):
    """Raised for a safe-to-display Fitnessd upload failure."""


ACTIVITY_FIELDS = (
    "activity_id",
    "activity_name",
    "activity_type",
    "parent_activity_type",
    "start_time_local",
    "start_time_gmt",
    "distance_km",
    "duration_minutes",
    "moving_minutes",
    "elapsed_minutes",
    "average_pace_min_per_km",
    "average_speed_kmh",
    "max_speed_kmh",
    "elevation_gain_m",
    "elevation_loss_m",
    "average_heart_rate",
    "max_heart_rate",
    "calories",
    "aerobic_training_effect",
    "anaerobic_training_effect",
    "training_effect_label",
    "activity_training_load",
    "vo2_max",
    "average_running_cadence_spm",
    "max_running_cadence_spm",
    "average_power_w",
    "normalized_power_w",
    "max_power_w",
    "grade_adjusted_pace_min_per_km",
    "average_respiration_rate",
    "max_respiration_rate",
    "average_stride_length_cm",
    "average_ground_contact_time_ms",
    "average_ground_contact_balance_pct",
    "average_vertical_oscillation_cm",
    "average_vertical_ratio_pct",
    "min_temperature_c",
    "max_temperature_c",
    "estimated_water_loss_ml",
    "body_battery_change",
    "steps",
    "moderate_intensity_minutes",
    "vigorous_intensity_minutes",
    "fastest_1k_seconds",
    "fastest_mile_seconds",
    "hr_zone_1_minutes",
    "hr_zone_2_minutes",
    "hr_zone_3_minutes",
    "hr_zone_4_minutes",
    "hr_zone_5_minutes",
    "is_personal_record",
    "lap_count",
    "device_id",
    "manufacturer",
)

DAILY_RECOVERY_FIELDS = (
    "date",
    "steps",
    "step_goal",
    "distance_km",
    "active_minutes",
    "highly_active_minutes",
    "sedentary_hours",
    "total_calories",
    "active_calories",
    "resting_heart_rate",
    "minimum_heart_rate",
    "maximum_heart_rate",
    "average_stress",
    "maximum_stress",
    "stress_qualifier",
    "high_stress_minutes",
    "medium_stress_minutes",
    "low_stress_minutes",
    "rest_stress_minutes",
    "body_battery_at_wake",
    "body_battery_highest",
    "body_battery_lowest",
    "body_battery_most_recent",
    "body_battery_charged",
    "body_battery_drained",
    "moderate_intensity_minutes",
    "vigorous_intensity_minutes",
    "weekly_intensity_minutes",
    "floors_ascended",
    "average_spo2",
    "lowest_spo2",
    "average_waking_respiration",
)


def _rounded(value: Any, digits: int = 2) -> float | None:
    return round(float(value), digits) if isinstance(value, (int, float)) else None


def _seconds_to_minutes(value: Any) -> float | None:
    return _rounded(value / 60) if isinstance(value, (int, float)) else None


def _milliseconds_to_minutes(value: Any) -> float | None:
    return _rounded(value / 60_000) if isinstance(value, (int, float)) else None


def _speed_to_pace(value: Any) -> float | None:
    if not isinstance(value, (int, float)) or value <= 0:
        return None
    return round(1000 / value / 60, 2)


def summarize_activity(activity: dict[str, Any]) -> dict[str, Any]:
    activity_type = activity.get("activityType") or {}
    parent_type = activity.get("parentActivityType") or {}
    distance_m = activity.get("distance")
    moving_seconds = activity.get("movingDuration")
    duration_seconds = activity.get("duration")
    pace_seconds = moving_seconds if isinstance(moving_seconds, (int, float)) else duration_seconds

    pace = None
    if (
        isinstance(distance_m, (int, float))
        and distance_m > 0
        and isinstance(pace_seconds, (int, float))
    ):
        pace = round((pace_seconds / 60) / (distance_m / 1000), 2)

    return {
        "activity_id": activity.get("activityId"),
        "activity_name": activity.get("activityName"),
        "activity_type": activity_type.get("typeKey"),
        "parent_activity_type": parent_type.get("typeKey"),
        "start_time_local": activity.get("startTimeLocal"),
        "start_time_gmt": activity.get("startTimeGMT"),
        "distance_km": _rounded(distance_m / 1000) if isinstance(distance_m, (int, float)) else None,
        "duration_minutes": _seconds_to_minutes(duration_seconds),
        "moving_minutes": _seconds_to_minutes(moving_seconds),
        "elapsed_minutes": _seconds_to_minutes(activity.get("elapsedDuration")),
        "average_pace_min_per_km": pace,
        "average_speed_kmh": _rounded(activity.get("averageSpeed") * 3.6)
        if isinstance(activity.get("averageSpeed"), (int, float))
        else None,
        "max_speed_kmh": _rounded(activity.get("maxSpeed") * 3.6)
        if isinstance(activity.get("maxSpeed"), (int, float))
        else None,
        "elevation_gain_m": _rounded(activity.get("elevationGain")),
        "elevation_loss_m": _rounded(activity.get("elevationLoss")),
        "average_heart_rate": _rounded(activity.get("averageHR"), 1),
        "max_heart_rate": _rounded(activity.get("maxHR"), 1),
        "calories": _rounded(activity.get("calories"), 1),
        "aerobic_training_effect": _rounded(activity.get("aerobicTrainingEffect"), 1),
        "anaerobic_training_effect": _rounded(activity.get("anaerobicTrainingEffect"), 1),
        "training_effect_label": activity.get("trainingEffectLabel"),
        "activity_training_load": _rounded(activity.get("activityTrainingLoad"), 1),
        "vo2_max": _rounded(activity.get("vO2MaxValue"), 1),
        "average_running_cadence_spm": _rounded(
            activity.get("averageRunningCadenceInStepsPerMinute"), 1
        ),
        "max_running_cadence_spm": _rounded(
            activity.get("maxRunningCadenceInStepsPerMinute"), 1
        ),
        "average_power_w": _rounded(activity.get("avgPower"), 1),
        "normalized_power_w": _rounded(activity.get("normPower"), 1),
        "max_power_w": _rounded(activity.get("maxPower"), 1),
        "grade_adjusted_pace_min_per_km": _speed_to_pace(
            activity.get("avgGradeAdjustedSpeed")
        ),
        "average_respiration_rate": _rounded(activity.get("avgRespirationRate"), 1),
        "max_respiration_rate": _rounded(activity.get("maxRespirationRate"), 1),
        "average_stride_length_cm": _rounded(activity.get("avgStrideLength"), 1),
        "average_ground_contact_time_ms": _rounded(activity.get("avgGroundContactTime"), 1),
        "average_ground_contact_balance_pct": _rounded(
            activity.get("avgGroundContactBalance"), 1
        ),
        "average_vertical_oscillation_cm": _rounded(activity.get("avgVerticalOscillation"), 1),
        "average_vertical_ratio_pct": _rounded(activity.get("avgVerticalRatio"), 1),
        "min_temperature_c": _rounded(activity.get("minTemperature"), 1),
        "max_temperature_c": _rounded(activity.get("maxTemperature"), 1),
        "estimated_water_loss_ml": _rounded(activity.get("waterEstimated"), 0),
        "body_battery_change": _rounded(activity.get("differenceBodyBattery"), 1),
        "steps": activity.get("steps"),
        "moderate_intensity_minutes": activity.get("moderateIntensityMinutes"),
        "vigorous_intensity_minutes": activity.get("vigorousIntensityMinutes"),
        "fastest_1k_seconds": _rounded(activity.get("fastestSplit_1000"), 1),
        "fastest_mile_seconds": _rounded(activity.get("fastestSplit_1609"), 1),
        "hr_zone_1_minutes": _seconds_to_minutes(activity.get("hrTimeInZone_1")),
        "hr_zone_2_minutes": _seconds_to_minutes(activity.get("hrTimeInZone_2")),
        "hr_zone_3_minutes": _seconds_to_minutes(activity.get("hrTimeInZone_3")),
        "hr_zone_4_minutes": _seconds_to_minutes(activity.get("hrTimeInZone_4")),
        "hr_zone_5_minutes": _seconds_to_minutes(activity.get("hrTimeInZone_5")),
        "is_personal_record": activity.get("isPR"),
        "lap_count": activity.get("lapCount"),
        "device_id": activity.get("deviceId"),
        "manufacturer": activity.get("manufacturer"),
    }


def _load_json_list(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    payload = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(payload, list):
        raise ValueError(f"Expected a JSON list in {path}")
    return [item for item in payload if isinstance(item, dict)]


def _write_json(path: Path, records: list[dict[str, Any]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(records, indent=2, ensure_ascii=False), encoding="utf-8")


def _write_json_value(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2, ensure_ascii=False), encoding="utf-8")


def _canonical_json_bytes(value: Any) -> bytes:
    return json.dumps(
        value, sort_keys=True, separators=(",", ":"), ensure_ascii=False
    ).encode("utf-8")


def canonical_json_bytes(value: Any) -> bytes:
    """Return the stable compact representation used in upload hashes."""
    return _canonical_json_bytes(value)


def _write_raw_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(_canonical_json_bytes(value))


def _write_activity_csv(path: Path, records: list[dict[str, Any]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="", encoding="utf-8-sig") as handle:
        writer = csv.DictWriter(handle, fieldnames=ACTIVITY_FIELDS, extrasaction="ignore")
        writer.writeheader()
        writer.writerows(records)


def _write_csv(path: Path, records: list[dict[str, Any]], fields: tuple[str, ...]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="", encoding="utf-8-sig") as handle:
        writer = csv.DictWriter(handle, fieldnames=fields, extrasaction="ignore")
        writer.writeheader()
        writer.writerows(records)


def _without_location(value: Any) -> Any:
    """Recursively remove coordinates and map geometry from agent-facing data."""
    if isinstance(value, list):
        return [_without_location(item) for item in value]
    if not isinstance(value, dict):
        return value
    cleaned: dict[str, Any] = {}
    for key, item in value.items():
        normalized = key.lower()
        if "latitude" in normalized or "longitude" in normalized or "polyline" in normalized:
            continue
        cleaned[key] = _without_location(item)
    return cleaned


def _without_activity_location(value: Any) -> Any:
    """Remove indexed coordinate columns as well as named coordinate fields."""
    if isinstance(value, list):
        return [_without_activity_location(item) for item in value]
    if not isinstance(value, dict):
        return value
    cleaned: dict[str, Any] = {}
    for key, item in value.items():
        normalized = key.lower()
        if "latitude" in normalized or "longitude" in normalized or "polyline" in normalized:
            continue
        cleaned[key] = _without_activity_location(item)
    descriptors = cleaned.get("metricDescriptors")
    rows = cleaned.get("activityDetailMetrics")
    if not isinstance(descriptors, list) or not isinstance(rows, list):
        return cleaned

    coordinate_indices: set[int] = set()
    kept_descriptors: list[dict[str, Any]] = []
    for position, descriptor in enumerate(descriptors):
        if not isinstance(descriptor, dict):
            continue
        metric_key = str(descriptor.get("key") or "").lower()
        old_index = descriptor.get("metricsIndex")
        index = old_index if isinstance(old_index, int) else position
        if "latitude" in metric_key or "longitude" in metric_key or "polyline" in metric_key:
            coordinate_indices.add(index)
            continue
        updated = dict(descriptor)
        updated["metricsIndex"] = len(kept_descriptors)
        kept_descriptors.append(updated)

    if not coordinate_indices:
        return cleaned
    cleaned["metricDescriptors"] = kept_descriptors
    for row in rows:
        if not isinstance(row, dict) or not isinstance(row.get("metrics"), list):
            continue
        row["metrics"] = [
            metric for index, metric in enumerate(row["metrics"])
            if index not in coordinate_indices
        ]
    cleaned["metricsCount"] = len(kept_descriptors)
    return cleaned


def _optional_api(call: Any, default: Any = None) -> Any:
    try:
        return call()
    except GarminConnectConnectionError:
        return default


def _in_date_range(record: dict[str, Any], start: date, end: date) -> bool:
    start_text = record.get("start_time_local")
    if not isinstance(start_text, str) or len(start_text) < 10:
        return False
    try:
        activity_date = date.fromisoformat(start_text[:10])
    except ValueError:
        return False
    return start <= activity_date <= end


def sync_activities(
    api: Garmin,
    start: date,
    end: date,
    output_dir: Path,
    download_originals: bool,
) -> tuple[list[dict[str, Any]], int]:
    payloads = api.get_activities_by_date(start.isoformat(), end.isoformat(), sortorder="asc")
    fetched = [summarize_activity(payload) for payload in payloads]

    raw_dir = output_dir / "raw" / "activities"
    for payload in payloads:
        activity_id = payload.get("activityId") if isinstance(payload, dict) else None
        if activity_id is not None:
            _write_raw_json(raw_dir / f"{activity_id}.json", payload)

    json_path = output_dir / "activities.json"
    existing = _load_json_list(json_path)
    # The requested range is authoritative: replacing it also removes activities
    # that were deleted from Garmin since an earlier sync.
    preserved = [record for record in existing if not _in_date_range(record, start, end)]
    merged_by_id = {
        str(record.get("activity_id")): record
        for record in [*preserved, *fetched]
        if record.get("activity_id") is not None
    }
    merged = sorted(
        merged_by_id.values(),
        key=lambda record: str(record.get("start_time_local") or ""),
    )
    _write_json(json_path, merged)
    _write_activity_csv(output_dir / "activities.csv", merged)

    downloaded = 0
    if download_originals:
        originals_dir = output_dir / "activity_originals"
        originals_dir.mkdir(parents=True, exist_ok=True)
        for record in fetched:
            activity_id = record.get("activity_id")
            target = originals_dir / f"{activity_id}.zip"
            if activity_id is None or target.exists():
                continue
            content = api.download_activity(
                str(activity_id), Garmin.ActivityDownloadFormat.ORIGINAL
            )
            target.write_bytes(content)
            downloaded += 1

    return fetched, downloaded


def sync_activity_details(
    api: Garmin, activities: list[dict[str, Any]], output_dir: Path, raw_dir: Path | None = None
) -> int:
    details_dir = output_dir / "activity_details"
    details_dir.mkdir(parents=True, exist_ok=True)
    for cached_path in details_dir.glob("*.json"):
        cached = json.loads(cached_path.read_text(encoding="utf-8"))
        _write_json_value(cached_path, _without_activity_location(cached))
    written = 0
    for activity in activities:
        activity_id = activity.get("activity_id")
        if activity_id is None:
            continue
        target = details_dir / f"{activity_id}.json"
        if target.exists():
            continue
        activity_type = str(activity.get("activity_type") or "")
        details = {
            "activity_id": activity_id,
            "splits": _optional_api(lambda: api.get_activity_splits(str(activity_id))),
            "split_summaries": _optional_api(
                lambda: api.get_activity_split_summaries(str(activity_id))
            ),
            "heart_rate_zones": _optional_api(
                lambda: api.get_activity_hr_in_timezones(str(activity_id))
            ),
            "power_zones": _optional_api(
                lambda: api.get_activity_power_in_timezones(str(activity_id))
            ),
            "metric_time_series": _optional_api(
                lambda: api.get_activity_details(str(activity_id), maxchart=2000, maxpoly=2000)
            ),
        }
        if "strength" in activity_type:
            details["exercise_sets"] = _optional_api(
                lambda: api.get_activity_exercise_sets(str(activity_id))
            )
        if raw_dir is not None:
            _write_raw_json(raw_dir / f"{activity_id}.json", details)
        _write_json_value(target, _without_activity_location(details))
        written += 1
    return written


def summarize_body_battery_events(
    day_text: str, events: list[dict[str, Any]]
) -> dict[str, Any]:
    summaries: list[dict[str, Any]] = []
    for item in events:
        event = item.get("event") if isinstance(item, dict) else None
        if not isinstance(event, dict):
            continue
        summaries.append(
            {
                "event_type": event.get("eventType"),
                "start_time_gmt": event.get("eventStartTimeGmt"),
                "timezone_offset_ms": event.get("timezoneOffset"),
                "duration_minutes": _milliseconds_to_minutes(
                    event.get("durationInMilliseconds")
                ),
                "body_battery_impact": event.get("bodyBatteryImpact"),
                "average_stress": item.get("averageStress"),
                "activity_id": item.get("activityId"),
                "activity_name": item.get("activityName"),
                "activity_type": item.get("activityType"),
            }
        )
    return {"date": day_text, "events": summaries}


def summarize_daily_recovery(
    day_text: str,
    summary: dict[str, Any],
    stress: dict[str, Any],
    heart_rate: dict[str, Any],
    intensity: dict[str, Any],
    body_battery: dict[str, Any],
) -> dict[str, Any]:
    return {
        "date": day_text,
        "steps": summary.get("totalSteps"),
        "step_goal": summary.get("dailyStepGoal"),
        "distance_km": _rounded((summary.get("totalDistanceMeters") or 0) / 1000),
        "active_minutes": _seconds_to_minutes(summary.get("activeSeconds")),
        "highly_active_minutes": _seconds_to_minutes(summary.get("highlyActiveSeconds")),
        "sedentary_hours": _rounded((summary.get("sedentarySeconds") or 0) / 3600),
        "total_calories": summary.get("totalKilocalories"),
        "active_calories": summary.get("activeKilocalories"),
        "resting_heart_rate": heart_rate.get("restingHeartRate")
        or summary.get("restingHeartRate"),
        "minimum_heart_rate": heart_rate.get("minHeartRate") or summary.get("minHeartRate"),
        "maximum_heart_rate": heart_rate.get("maxHeartRate") or summary.get("maxHeartRate"),
        "average_stress": stress.get("avgStressLevel") or summary.get("averageStressLevel"),
        "maximum_stress": stress.get("maxStressLevel") or summary.get("maxStressLevel"),
        "stress_qualifier": summary.get("stressQualifier"),
        "high_stress_minutes": _milliseconds_to_minutes(summary.get("highStressDuration")),
        "medium_stress_minutes": _milliseconds_to_minutes(summary.get("mediumStressDuration")),
        "low_stress_minutes": _milliseconds_to_minutes(summary.get("lowStressDuration")),
        "rest_stress_minutes": _milliseconds_to_minutes(summary.get("restStressDuration")),
        "body_battery_at_wake": summary.get("bodyBatteryAtWakeTime"),
        "body_battery_highest": summary.get("bodyBatteryHighestValue"),
        "body_battery_lowest": summary.get("bodyBatteryLowestValue"),
        "body_battery_most_recent": summary.get("bodyBatteryMostRecentValue"),
        "body_battery_charged": summary.get("bodyBatteryChargedValue")
        or body_battery.get("charged"),
        "body_battery_drained": summary.get("bodyBatteryDrainedValue")
        or body_battery.get("drained"),
        "moderate_intensity_minutes": intensity.get("moderateMinutes")
        or summary.get("moderateIntensityMinutes"),
        "vigorous_intensity_minutes": intensity.get("vigorousMinutes")
        or summary.get("vigorousIntensityMinutes"),
        "weekly_intensity_minutes": intensity.get("weeklyTotal"),
        "floors_ascended": summary.get("floorsAscended"),
        "average_spo2": summary.get("averageSpo2"),
        "lowest_spo2": summary.get("lowestSpo2"),
        "average_waking_respiration": summary.get("avgWakingRespirationValue"),
    }


def sync_daily_recovery(api: Garmin, start: date, end: date, output_dir: Path) -> int:
    body_battery_payload = _optional_api(
        lambda: api.get_body_battery(start.isoformat(), end.isoformat()), []
    )
    body_battery_by_date = {
        item.get("date"): item
        for item in body_battery_payload
        if isinstance(item, dict) and item.get("date")
    }
    fetched: list[dict[str, Any]] = []
    fetched_events: list[dict[str, Any]] = []
    for offset in range((end - start).days + 1):
        day_text = (start + timedelta(days=offset)).isoformat()
        summary = _optional_api(lambda d=day_text: api.get_user_summary(d), {})
        stress = _optional_api(lambda d=day_text: api.get_stress_data(d), {})
        heart_rate = _optional_api(lambda d=day_text: api.get_heart_rates(d), {})
        intensity = _optional_api(lambda d=day_text: api.get_intensity_minutes_data(d), {})
        events = _optional_api(lambda d=day_text: api.get_body_battery_events(d), [])
        _write_raw_json(
            output_dir / "raw" / "daily_recovery" / f"{day_text}.json",
            {
                "summary": summary or {},
                "stress": stress or {},
                "heart_rate": heart_rate or {},
                "intensity": intensity or {},
                "body_battery": body_battery_by_date.get(day_text, {}),
                "events": events or [],
            },
        )
        fetched.append(
            summarize_daily_recovery(
                day_text,
                summary or {},
                stress or {},
                heart_rate or {},
                intensity or {},
                body_battery_by_date.get(day_text, {}),
            )
        )
        fetched_events.append(summarize_body_battery_events(day_text, events or []))

    path = output_dir / "daily_recovery.json"
    existing = _load_json_list(path)
    fetched_dates = {record["date"] for record in fetched}
    preserved = [record for record in existing if record.get("date") not in fetched_dates]
    merged = sorted([*preserved, *fetched], key=lambda record: str(record.get("date") or ""))
    _write_json(path, merged)
    _write_csv(output_dir / "daily_recovery.csv", merged, DAILY_RECOVERY_FIELDS)
    events_path = output_dir / "body_battery_events.json"
    existing_events = _load_json_list(events_path)
    preserved_events = [
        record for record in existing_events if record.get("date") not in fetched_dates
    ]
    merged_events = sorted(
        [*preserved_events, *fetched_events],
        key=lambda record: str(record.get("date") or ""),
    )
    _write_json(events_path, merged_events)
    return len(fetched)


def sync_training_and_body(api: Garmin, end: date, output_dir: Path) -> None:
    day_text = end.isoformat()
    training = {
        "captured_date": day_text,
        "training_status": _optional_api(lambda: api.get_training_status(day_text)),
        "max_metrics": _optional_api(lambda: api.get_max_metrics(day_text)),
        "race_predictions": _optional_api(api.get_race_predictions),
        "personal_records": _optional_api(api.get_personal_record),
        "lactate_threshold": _optional_api(api.get_lactate_threshold),
    }
    body_start = (end - timedelta(days=3650)).isoformat()
    body = {
        "captured_date": day_text,
        "body_composition": _optional_api(
            lambda: api.get_body_composition(body_start, day_text), {}
        ),
        "blood_pressure": _optional_api(
            lambda: api.get_blood_pressure(body_start, day_text), {}
        ),
    }
    _write_raw_json(output_dir / "raw" / "training_snapshot" / f"{day_text}.json", training)
    _write_raw_json(output_dir / "raw" / "body_measurements" / f"{day_text}.json", body)
    _write_json_value(output_dir / "training_snapshot.json", training)
    _write_json_value(output_dir / "body_measurements.json", body)


def sync_sleep(
    api: Garmin, start: date, end: date, output_dir: Path, raw_dir: Path | None = None
) -> int:
    sleep_path = output_dir / "sleep.json"
    existing = _load_json_list(sleep_path)
    fetched = [
        fetch_day(api, start + timedelta(days=offset), raw_dir=raw_dir)
        for offset in range((end - start).days + 1)
    ]
    fetched_dates = {record.get("date") for record in fetched}
    preserved = [record for record in existing if record.get("date") not in fetched_dates]
    merged = sorted([*preserved, *fetched], key=lambda record: str(record.get("date") or ""))
    _write_json(sleep_path, merged)
    sleep_fields = tuple(dict.fromkeys(key for record in merged for key in record.keys()))
    _write_csv(output_dir / "sleep.csv", merged, sleep_fields)
    return len(fetched)


def _raw_ingest_records(output_dir: Path) -> list[dict[str, Any]]:
    specs = (
        ("sleep", "sleep"),
        ("activities", "activities"),
        ("activity_details", "activity_details"),
        ("daily_recovery", "daily_recovery"),
        ("body_battery_events", "daily_recovery"),
        ("training_snapshot", "training_snapshot"),
        ("body_measurements", "body_measurements"),
    )
    records: list[dict[str, Any]] = []
    seen: set[tuple[str, str]] = set()
    activity_dates: dict[str, str] = {}
    for dataset, directory in specs:
        for path in sorted((output_dir / "raw" / directory).glob("*.json")):
            payload = json.loads(path.read_text(encoding="utf-8"))
            key = path.stem
            if dataset == "activities":
                key = str(payload.get("activityId") or key)
                record_date = str(payload.get("startTimeLocal") or "")[:10]
                if record_date:
                    activity_dates[key] = record_date
            elif dataset == "activity_details":
                key = str(payload.get("activity_id") or key)
                record_date = activity_dates.get(key, "")
            else:
                record_date = key
            if (dataset, key) in seen:
                continue
            seen.add((dataset, key))
            record = {
                "dataset": dataset,
                "record_key": key,
                "record_date": record_date or None,
                "payload_raw": payload,
            }
            records.append(record)
    return records


def raw_ingest_records(output_dir: Path) -> list[dict[str, Any]]:
    """Return the raw records in the shared ingestion contract."""
    return _raw_ingest_records(output_dir)


FITNESSD_MAX_BATCH_RECORDS = 100


def _multipart_body(
    output_dir: Path,
    records: list[dict[str, Any]] | None = None,
    archive_paths: list[Path] | None = None,
) -> tuple[bytes, str]:
    boundary = "----fitsync-" + uuid.uuid4().hex
    if records is None:
        records = _raw_ingest_records(output_dir)
    if archive_paths is None:
        archive_paths = sorted((output_dir / "activity_originals").glob("*.zip"))
    archives: list[dict[str, str]] = []
    file_parts: list[tuple[str, Path]] = []
    for path in archive_paths:
        upload_id = uuid.uuid4().hex
        archives.append(
            {
                "upload_id": upload_id,
                "original_filename": path.name,
                "content_type": "application/zip",
                "activity_record_key": path.stem,
            }
        )
        file_parts.append((upload_id, path))

    parts: list[bytes] = []
    metadata = _canonical_json_bytes({"records": records, "archives": archives})
    parts.append(
        (
            f"--{boundary}\r\n"
            "Content-Disposition: form-data; name=\"records\"\r\n"
            "Content-Type: application/json\r\n\r\n"
        ).encode()
        + metadata
        + b"\r\n"
    )
    for upload_id, path in file_parts:
        parts.append(
            (
                f"--{boundary}\r\n"
                f"Content-Disposition: form-data; name=\"archive_{upload_id}\"; filename=\"{path.name}\"\r\n"
                "Content-Type: application/zip\r\n\r\n"
            ).encode()
            + path.read_bytes()
            + b"\r\n"
        )
    parts.append(f"--{boundary}--\r\n".encode())
    return b"".join(parts), f"multipart/form-data; boundary={boundary}"


def _fitnessd_batches(
    records: list[dict[str, Any]], archive_paths: list[Path]
) -> list[tuple[list[dict[str, Any]], list[Path]]]:
    """Split records into server-sized batches and keep archives with activities."""
    if not records:
        raise FitnessdSyncError("No raw Garmin JSON exports found to upload to fitnessd")

    batches = [
        records[offset : offset + FITNESSD_MAX_BATCH_RECORDS]
        for offset in range(0, len(records), FITNESSD_MAX_BATCH_RECORDS)
    ]
    activity_batch: dict[str, int] = {}
    for batch_index, batch in enumerate(batches):
        for record in batch:
            if record.get("dataset") == "activities":
                activity_batch[str(record.get("record_key"))] = batch_index

    archives_by_batch: list[list[Path]] = [[] for _ in batches]
    for path in archive_paths:
        batch_index = activity_batch.get(path.stem, 0)
        archives_by_batch[batch_index].append(path)
    return list(zip(batches, archives_by_batch))


def upload_raw_to_fitnessd(
    output_dir: Path,
    environ: dict[str, str] | None = None,
    *,
    urlopen_fn: Any = urlopen,
    sleep_fn: Any = time.sleep,
) -> dict[str, Any]:
    env = environ if environ is not None else os.environ
    base_url = env.get("FITNESS_API_URL")
    token = env.get("FITNESS_INGEST_TOKEN")
    token_file = env.get("FITNESS_INGEST_TOKEN_FILE")
    if token and token_file:
        raise FitnessdSyncError("Set only one of FITNESS_INGEST_TOKEN and FITNESS_INGEST_TOKEN_FILE")
    if not token and token_file:
        try:
            token = Path(token_file).read_text(encoding="utf-8").strip()
        except OSError as exc:
            raise FitnessdSyncError("Could not read FITNESS_INGEST_TOKEN_FILE") from exc
    if not base_url or not token:
        raise FitnessdSyncError("Set FITNESS_API_URL and FITNESS_INGEST_TOKEN for fitnessd upload")
    records = _raw_ingest_records(output_dir)
    archive_paths = sorted((output_dir / "activity_originals").glob("*.zip"))
    total_records = 0
    total_archives = 0
    for batch_records, batch_archives in _fitnessd_batches(records, archive_paths):
        body, content_type = _multipart_body(output_dir, batch_records, batch_archives)
        request = Request(
            base_url.rstrip("/") + "/api/v1/ingest/garmin/batch",
            data=body,
            method="POST",
            headers={
                "Authorization": f"Bearer {token}",
                "Content-Type": content_type,
                "Accept": "application/json",
            },
        )
        last_error: Exception | None = None
        for attempt in range(4):
            try:
                with urlopen_fn(request, timeout=30) as response:
                    result = json.loads(response.read().decode("utf-8"))
                if not isinstance(result, dict):
                    raise FitnessdSyncError("fitnessd returned invalid JSON")
                total_records += int(result.get("records", 0))
                total_archives += int(result.get("archives", 0))
                break
            except HTTPError as exc:
                last_error = exc
                if exc.code not in {429, 500, 502, 503, 504}:
                    raise FitnessdSyncError(f"fitnessd upload returned HTTP {exc.code}") from exc
            except (URLError, TimeoutError, OSError, ValueError, json.JSONDecodeError) as exc:
                last_error = exc
            if attempt < 3:
                sleep_fn(min(8.0, 2.0**attempt))
        else:
            raise FitnessdSyncError("fitnessd upload failed after retries") from last_error
    return {"records": total_records, "archives": total_archives}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Incrementally sync Garmin sleep and activities to JSON/CSV."
    )
    parser.add_argument(
        "--end-date",
        type=date.fromisoformat,
        default=date.today(),
        help="Last date to sync (YYYY-MM-DD; default: today).",
    )
    parser.add_argument(
        "--days",
        type=int,
        default=7,
        help="Number of consecutive dates ending on --end-date (default: 7).",
    )
    parser.add_argument(
        "--output-dir",
        type=Path,
        default=Path("data") / "garmin",
        help="Output directory (default: data/garmin).",
    )
    parser.add_argument("--activities-only", action="store_true", help="Skip sleep sync.")
    parser.add_argument("--sleep-only", action="store_true", help="Skip activity sync.")
    parser.add_argument(
        "--download-originals",
        action="store_true",
        help="Also download each new activity's original ZIP/FIT payload.",
    )
    parser.add_argument(
        "--extended",
        action="store_true",
        help="Also sync daily recovery, training/body snapshots, and cached activity details.",
    )
    parser.add_argument(
        "--fitnessd-only",
        action="store_true",
        help="Upload existing raw Garmin exports to fitnessd without contacting Garmin.",
    )
    parser.add_argument(
        "--fitnessd",
        action="store_true",
        help="Upload cached raw Garmin responses to fitnessd using FITNESS_API_URL and FITNESS_INGEST_TOKEN.",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.days < 1 or args.days > 3660:
        print("--days must be between 1 and 3660", file=sys.stderr)
        return 2
    if args.activities_only and args.sleep_only:
        print("Choose at most one of --activities-only and --sleep-only", file=sys.stderr)
        return 2
    if args.fitnessd_only:
        try:
            result = upload_raw_to_fitnessd(args.output_dir)
            print(
                f"fitnessd: uploaded {result.get('records', 0)} raw record(s) and "
                f"{result.get('archives', 0)} archive(s)"
            )
            return 0
        except FitnessdSyncError as exc:
            print(f"fitnessd sync failed: {exc}", file=sys.stderr)
            return 1

    start = args.end_date - timedelta(days=args.days - 1)
    try:
        api = login()
        fetched_activities: list[dict[str, Any]] = []
        if not args.sleep_only:
            fetched_activities, download_count = sync_activities(
                api, start, args.end_date, args.output_dir, args.download_originals
            )
            print(
                f"Activities: synced {len(fetched_activities)}; "
                f"originals downloaded: {download_count}"
            )
        if not args.activities_only:
            sleep_count = sync_sleep(
                api, start, args.end_date, args.output_dir, args.output_dir / "raw" / "sleep"
            )
            print(f"Sleep: synced {sleep_count} day(s)")
        if args.extended:
            detail_count = sync_activity_details(
                api, fetched_activities, args.output_dir, args.output_dir / "raw" / "activity_details"
            )
            recovery_count = 0
            if not args.activities_only:
                recovery_count = sync_daily_recovery(api, start, args.end_date, args.output_dir)
            sync_training_and_body(api, args.end_date, args.output_dir)
            extended_message = f"Extended: {detail_count} new activity detail file(s)"
            if not args.activities_only:
                extended_message += f"; {recovery_count} recovery day(s)"
            print(extended_message + "; training/body snapshots updated")
        if args.fitnessd:
            result = upload_raw_to_fitnessd(args.output_dir)
            print(
                f"fitnessd: uploaded {result.get('records', 0)} raw record(s) and "
                f"{result.get('archives', 0)} archive(s)"
            )
        print(f"Output: {args.output_dir.resolve()}")
        return 0
    except GarminConnectAuthenticationError as exc:
        print(f"Garmin authentication failed: {exc}", file=sys.stderr)
    except GarminConnectTooManyRequestsError as exc:
        print(f"Garmin rate-limited the login or request: {exc}", file=sys.stderr)
    except GarminConnectConnectionError as exc:
        print(f"Garmin request failed: {exc}", file=sys.stderr)
    except FitnessdSyncError as exc:
        print(f"Fitnessd sync failed: {exc}", file=sys.stderr)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"Local sync failed: {exc}", file=sys.stderr)
    except KeyboardInterrupt:
        print("\nCancelled.", file=sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
