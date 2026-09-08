"""Bounded Fitnessd upload and crash-safe mobile retry manifests."""

from __future__ import annotations

import hashlib
import json
import os
import shutil
import tempfile
import time
import uuid
from pathlib import Path
from typing import Any, Callable
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

from .auth import GarminSyncError
from .datasets import canonical_json_bytes, raw_ingest_records

MOBILE_MAX_RECORDS = 20
MOBILE_MAX_METADATA_BYTES = (10 << 20) - (64 << 10)
UPLOAD_TIMEOUT_SECONDS = 30


def _atomic_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.parent.chmod(0o700)
    fd, name = tempfile.mkstemp(prefix=".fitsync-", suffix=".tmp", dir=path.parent, text=True)
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            json.dump(value, handle, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def cleanup_stale(work_dir: Path, now: float | None = None) -> None:
    """Remove abandoned temporary files, retaining a retry manifest."""

    if not work_dir.exists():
        return
    cutoff = (time.time() if now is None else now) - 7 * 24 * 60 * 60
    for path in work_dir.rglob("*"):
        # Batch JSON and raw exports are retry state, not abandoned temporary
        # files. Never remove an unconfirmed batch merely because a sync was
        # offline for more than a week.
        if path.is_file() and path.name != "manifest.json" and (path.name.startswith(".fitsync-") or path.suffix == ".tmp"):
            try:
                if path.stat().st_mtime < cutoff:
                    path.unlink()
            except OSError:
                continue


def _cleanup_uploaded_data(work_dir: Path) -> None:
    """Remove Garmin exports after every batch has been confirmed."""

    generated = (
        "raw",
        "batches",
        "activity_details",
        "activity_originals",
        "activities.json",
        "activities.csv",
        "sleep.json",
        "sleep.csv",
        "daily_recovery.json",
        "daily_recovery.csv",
        "body_battery_events.json",
        "training_snapshot.json",
        "body_measurements.json",
    )
    for name in generated:
        path = work_dir / name
        if path.is_dir():
            shutil.rmtree(path, ignore_errors=True)
        else:
            try:
                path.unlink(missing_ok=True)
            except OSError:
                continue


def _batch_id(records: list[dict[str, Any]], index: int) -> str:
    digest = hashlib.sha256(canonical_json_bytes(records)).hexdigest()[:24]
    return f"batch-{index:04d}-{digest}"


def split_mobile_records(records: list[dict[str, Any]]) -> list[list[dict[str, Any]]]:
    """Split before multipart construction to bound metadata memory."""

    batches: list[list[dict[str, Any]]] = []
    current: list[dict[str, Any]] = []
    for record in records:
        candidate = [*current, record]
        size = len(canonical_json_bytes({"records": candidate, "archives": []}))
        if current and (len(candidate) > MOBILE_MAX_RECORDS or size > MOBILE_MAX_METADATA_BYTES):
            batches.append(current)
            current = [record]
        else:
            current = candidate
        if len(current) > MOBILE_MAX_RECORDS:
            batches.append(current[:-1])
            current = [record]
    if current:
        batches.append(current)
    if any(len(canonical_json_bytes({"records": batch, "archives": []})) > MOBILE_MAX_METADATA_BYTES for batch in batches):
        raise GarminSyncError("INTERNAL", "A Garmin response is too large for a mobile upload batch")
    if any(len(batch) > MOBILE_MAX_RECORDS for batch in batches):
        raise GarminSyncError("INTERNAL", "Garmin data could not be split into safe upload batches")
    return batches


def _multipart_body(records: list[dict[str, Any]], archives: list[Path] | None = None) -> tuple[bytes, str]:
    archives = archives or []
    boundary = "----fitsync-" + uuid.uuid4().hex
    metadata: list[dict[str, str]] = []
    files: list[tuple[str, Path]] = []
    for path in archives:
        upload_id = uuid.uuid4().hex
        metadata.append({
            "upload_id": upload_id,
            "original_filename": path.name,
            "content_type": "application/zip",
            "activity_record_key": path.stem,
        })
        files.append((upload_id, path))
    body = bytearray()
    body.extend((f"--{boundary}\r\nContent-Disposition: form-data; name=\"records\"\r\n"
                 "Content-Type: application/json\r\n\r\n").encode())
    body.extend(canonical_json_bytes({"records": records, "archives": metadata}))
    body.extend(b"\r\n")
    for upload_id, path in files:
        body.extend((f"--{boundary}\r\nContent-Disposition: form-data; name=\"archive_{upload_id}\"; "
                     f"filename=\"{path.name}\"\r\nContent-Type: application/zip\r\n\r\n").encode())
        body.extend(path.read_bytes())
        body.extend(b"\r\n")
    body.extend(f"--{boundary}--\r\n".encode())
    return bytes(body), f"multipart/form-data; boundary={boundary}"


def _safe_upload_error(status: int | None = None) -> GarminSyncError:
    if status == 401:
        return GarminSyncError("FITNESSD_UNAUTHORIZED", "FitSync sign-in expired", True)
    if status == 403:
        return GarminSyncError("FITNESSD_FORBIDDEN", "You cannot upload data for this athlete")
    if status == 429:
        return GarminSyncError("FITNESSD_RATE_LIMITED", "FitSync asked you to wait before trying again", True)
    return GarminSyncError("FITNESSD_REQUEST_FAILED", "FitSync could not accept the Garmin data", True)


def _upload_one(
    api_base_url: str,
    athlete_id: str,
    records: list[dict[str, Any]],
    archives: list[Path],
    access_token: str,
    *,
    urlopen_fn: Callable[..., Any] = urlopen,
    sleep_fn: Callable[[float], None] = time.sleep,
) -> dict[str, int]:
    body, content_type = _multipart_body(records, archives)
    request = Request(
        api_base_url.rstrip("/") + f"/api/v2/athletes/{athlete_id}/ingest/garmin/batch",
        data=body,
        method="POST",
        headers={"Authorization": f"Bearer {access_token}", "Content-Type": content_type, "Accept": "application/json"},
    )
    last_error: Exception | None = None
    for attempt in range(4):
        try:
            with urlopen_fn(request, timeout=UPLOAD_TIMEOUT_SECONDS) as response:
                result = json.loads(response.read().decode("utf-8"))
            if not isinstance(result, dict):
                raise ValueError("invalid response")
            return {"records": int(result.get("records", 0)), "archives": int(result.get("archives", 0))}
        except HTTPError as exc:
            last_error = exc
            if exc.code not in {429, 500, 502, 503, 504}:
                raise _safe_upload_error(exc.code) from None
        except (URLError, TimeoutError, OSError, ValueError, json.JSONDecodeError) as exc:
            last_error = exc
        if attempt < 3:
            sleep_fn(min(8.0, 2.0**attempt))
    raise _safe_upload_error() from last_error


def _manifest_result(manifest: dict[str, Any], completed_at: str, counts: dict[str, int]) -> dict[str, Any]:
    return {
        "status": "success",
        "records_sent": counts["records"],
        "archives_sent": counts["archives"],
        "batches_sent": len(manifest["batches"]),
        "started_at": manifest["started_at"],
        "completed_at": completed_at,
        "latest_record_date": manifest.get("latest_record_date"),
    }


def upload_mobile(
    config: dict[str, Any],
    access_token: str,
    *,
    progress_callback: Callable[[dict[str, Any]], None] | None = None,
    urlopen_fn: Callable[..., Any] = urlopen,
    sleep_fn: Callable[[float], None] = time.sleep,
) -> dict[str, Any]:
    """Upload raw records with a manifest that survives process death."""

    work_dir = Path(config["work_dir"])
    work_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    work_dir.chmod(0o700)
    cleanup_stale(work_dir)
    manifest_path = work_dir / "manifest.json"
    batches_dir = work_dir / "batches"
    manifest: dict[str, Any]
    if manifest_path.exists():
        try:
            manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
            if manifest.get("athlete_id") != config["athlete_id"]:
                raise ValueError
        except (OSError, ValueError, json.JSONDecodeError):
            manifest_path.unlink(missing_ok=True)
            manifest = {}
    else:
        manifest = {}
    if not manifest:
        records = raw_ingest_records(work_dir)
        if not records:
            raise GarminSyncError("INTERNAL", "No Garmin data was available to upload")
        batches = split_mobile_records(records)
        batches_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
        for index, records_batch in enumerate(batches):
            batch_id = _batch_id(records_batch, index)
            _atomic_json(batches_dir / f"{batch_id}.json", {"records": records_batch, "archives": []})
        manifest = {
            "schema_version": 1,
            "athlete_id": config["athlete_id"],
            "from_date": config.get("from_date"),
            "to_date": config.get("to_date"),
            "started_at": config.get("started_at") or time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "latest_record_date": max((r.get("record_date") for r in records if r.get("record_date")), default=None),
            "batches": [{"id": _batch_id(item, index), "file": f"batches/{_batch_id(item, index)}.json"} for index, item in enumerate(batches)],
            "confirmed_batch_ids": [],
            "records_sent": 0,
            "archives_sent": 0,
        }
        _atomic_json(manifest_path, manifest)

    confirmed = set(manifest.get("confirmed_batch_ids", []))
    counts = {"records": int(manifest.get("records_sent", 0)), "archives": int(manifest.get("archives_sent", 0))}
    total = len(manifest["batches"])
    for index, item in enumerate(manifest["batches"]):
        if item["id"] in confirmed:
            continue
        if progress_callback:
            progress_callback({"stage": "uploading", "completed": index, "total": total, "message": "Uploading Garmin data"})
        batch_file = work_dir / item["file"]
        try:
            payload = json.loads(batch_file.read_text(encoding="utf-8"))
            result = _upload_one(config["api_base_url"], config["athlete_id"], payload["records"], [], access_token, urlopen_fn=urlopen_fn, sleep_fn=sleep_fn)
        except GarminSyncError:
            raise
        except (OSError, ValueError, json.JSONDecodeError) as exc:
            raise GarminSyncError("INTERNAL", "A pending Garmin batch could not be read") from exc
        counts["records"] += result["records"]
        counts["archives"] += result["archives"]
        confirmed.add(item["id"])
        manifest["confirmed_batch_ids"] = sorted(confirmed)
        manifest["records_sent"] = counts["records"]
        manifest["archives_sent"] = counts["archives"]
        _atomic_json(manifest_path, manifest)
        batch_file.unlink(missing_ok=True)
    manifest_path.unlink(missing_ok=True)
    try:
        batches_dir.rmdir()
    except OSError:
        pass
    _cleanup_uploaded_data(work_dir)
    if progress_callback:
        progress_callback({"stage": "cleaning_up", "completed": total, "total": total, "message": "Cleaning up confirmed Garmin batches"})
    return _manifest_result(manifest, time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), counts)
