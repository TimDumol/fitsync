"""Non-interactive, JSON-compatible Garmin sync orchestration."""

from __future__ import annotations

import json
import os
import time
from datetime import date, timedelta
from pathlib import Path
from typing import Any, Callable
from urllib.parse import urlsplit

from garminconnect import Garmin, GarminConnectAuthenticationError, GarminConnectConnectionError, GarminConnectTooManyRequestsError

from .auth import GarminSyncError, _safe_token
from .datasets import sync_activities, sync_activity_details, sync_daily_recovery, sync_sleep, sync_training_and_body
from .uploader import upload_mobile

STAGES = ("authenticating", "fetching_activities", "fetching_sleep", "fetching_recovery", "fetching_activity_details", "fetching_training", "uploading", "cleaning_up", "complete")


def _validate_config(config: dict[str, Any]) -> tuple[date, date, Path]:
    if not isinstance(config, dict):
        raise GarminSyncError("INTERNAL", "Garmin sync configuration is invalid")
    required = {"athlete_id", "api_base_url", "days", "extended", "work_dir"}
    if set(config) - required or not required <= set(config):
        raise GarminSyncError("INTERNAL", "Garmin sync configuration is invalid")
    if not isinstance(config["athlete_id"], str):
        raise GarminSyncError("INTERNAL", "Garmin sync athlete is invalid")
    try:
        import uuid
        uuid.UUID(config["athlete_id"])
    except (ValueError, AttributeError):
        raise GarminSyncError("INTERNAL", "Garmin sync athlete is invalid") from None
    if not isinstance(config["api_base_url"], str):
        raise GarminSyncError("INTERNAL", "Garmin sync requires an HTTPS FitSync URL")
    try:
        api_url = urlsplit(config["api_base_url"])
    except ValueError:
        raise GarminSyncError("INTERNAL", "Garmin sync requires an HTTPS FitSync URL") from None
    test_http = os.environ.get("FITSYNC_TEST_MODE") == "1" and api_url.scheme == "http"
    if (api_url.scheme != "https" and not test_http) or not api_url.netloc or api_url.query or api_url.fragment:
        raise GarminSyncError("INTERNAL", "Garmin sync requires an HTTPS FitSync URL")
    if not isinstance(config["days"], int) or isinstance(config["days"], bool) or not 1 <= config["days"] <= 3660:
        raise GarminSyncError("INTERNAL", "Garmin sync days must be between 1 and 3660")
    if not isinstance(config["extended"], bool):
        raise GarminSyncError("INTERNAL", "Garmin sync extended option is invalid")
    if not isinstance(config["work_dir"], str) or not config["work_dir"]:
        raise GarminSyncError("INTERNAL", "Garmin sync work directory is invalid")
    work_dir = Path(config["work_dir"])
    if not work_dir.is_absolute():
        raise GarminSyncError("INTERNAL", "Garmin sync work directory must be absolute")
    end = date.today()
    start = end - timedelta(days=config["days"] - 1)
    return start, end, work_dir


def _progress(callback: Callable[[dict[str, Any]], None] | None, stage: str, completed: int, total: int, message: str) -> None:
    if callback is not None:
        callback({"stage": stage, "completed": completed, "total": total, "message": message})


def _map_sync_error(error: Exception) -> GarminSyncError:
    if isinstance(error, GarminSyncError):
        return error
    if isinstance(error, GarminConnectAuthenticationError):
        return GarminSyncError("GARMIN_AUTH_FAILED", "Garmin authentication expired", True)
    if isinstance(error, GarminConnectTooManyRequestsError):
        return GarminSyncError("GARMIN_RATE_LIMITED", "Garmin asked you to wait before trying again", True)
    if isinstance(error, GarminConnectConnectionError):
        return GarminSyncError("GARMIN_REQUEST_FAILED", "Garmin could not be reached", True)
    if isinstance(error, (TimeoutError, OSError, ConnectionError)):
        return GarminSyncError("OFFLINE", "No network connection is available", True)
    return GarminSyncError("INTERNAL", "Garmin sync could not be completed")


def sync(config: dict[str, Any], token_json: str, access_token: str, progress_callback: Callable[[dict[str, Any]], None] | None = None) -> dict[str, Any]:
    """Sync Garmin's authoritative raw datasets directly to Fitnessd."""

    start, end, work_dir = _validate_config(config)
    if not isinstance(token_json, str) or not token_json.strip() or not isinstance(access_token, str) or not access_token.strip():
        raise GarminSyncError("GARMIN_AUTH_FAILED", "Reconnect Garmin and FitSync before syncing")
    work_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    work_dir.chmod(0o700)
    runtime_config = dict(config)
    runtime_config.update({"from_date": start.isoformat(), "to_date": end.isoformat(), "started_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())})
    total_days = (end - start).days + 1
    try:
        # A pending manifest is uploaded by upload_mobile before the client is
        # restored. This is the password-free, no-refetch process-death path.
        if (work_dir / "manifest.json").exists():
            _progress(progress_callback, "uploading", 0, 1, "Retrying confirmed Garmin data")
            result = upload_mobile(runtime_config, access_token, progress_callback=progress_callback)
            result["refreshed_garmin_token_json"] = token_json
            _progress(progress_callback, "complete", 1, 1, "Garmin sync complete")
            return result

        _progress(progress_callback, "authenticating", 0, 1, "Authenticating with Garmin")
        client = Garmin()
        status, _ = client.login(token_json)
        if status == "needs_mfa":
            raise GarminSyncError("GARMIN_MFA_REQUIRED", "Garmin requires interactive verification")
        _progress(progress_callback, "fetching_activities", 0, 1, "Fetching Garmin activities")
        activities, _ = sync_activities(client, start, end, work_dir, False)
        _progress(progress_callback, "fetching_sleep", 1, total_days, "Fetching Garmin sleep")
        sync_sleep(client, start, end, work_dir, work_dir / "raw" / "sleep")
        _progress(progress_callback, "fetching_recovery", 0, total_days, "Fetching Garmin recovery")
        sync_daily_recovery(client, start, end, work_dir)
        if runtime_config["extended"]:
            _progress(progress_callback, "fetching_activity_details", 0, len(activities), "Fetching activity details")
            sync_activity_details(client, activities, work_dir, work_dir / "raw" / "activity_details")
            _progress(progress_callback, "fetching_training", 0, 1, "Fetching Garmin training data")
            sync_training_and_body(client, end, work_dir)
        _progress(progress_callback, "uploading", 0, 1, "Uploading Garmin data to FitSync")
        result = upload_mobile(runtime_config, access_token, progress_callback=progress_callback)
        result["refreshed_garmin_token_json"] = _safe_token(client)
        _progress(progress_callback, "complete", 1, 1, "Garmin sync complete")
        return result
    except Exception as exc:
        raise _map_sync_error(exc) from None
