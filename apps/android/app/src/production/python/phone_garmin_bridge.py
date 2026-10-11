"""Small Chaquopy entrypoint; never prints secrets or raw Garmin responses."""

from __future__ import annotations

import json
import logging
import re
from collections.abc import Callable
from typing import Any

from garminconnect.client import require_curl_cffi
from fitsync_garmin.auth import GarminSyncError, cancel_login as _cancel_login, resume_login as _resume_login, start_login as _start_login
from fitsync_garmin.runner import sync as _sync


_SENSITIVE_VALUE = re.compile(
    r"(?i)((?:[\"']?)(?:password|passphrase|token|authorization|cookie|secret|credential|mfa)[\w-]*[\"']?\s*[:=]\s*[\"']?)([^\"'\s,;}]+)"
)


def _redact(value: str) -> str:
    return _SENSITIVE_VALUE.sub(r"\1[REDACTED]", value)


class _CaptureHandler(logging.Handler):
    def __init__(self) -> None:
        super().__init__()
        self.lines: list[str] = []

    def emit(self, record: logging.LogRecord) -> None:
        try:
            message = self.format(record)
            if record.exc_info:
                message = f"{message}\n{self.formatException(record.exc_info)}"
            self.lines.append(_redact(message))
        except Exception:
            self.lines.append("Python logging failed while formatting a diagnostic event")


def _bounded_logs(handler: _CaptureHandler) -> str:
    return "\n".join(handler.lines)[-20_000:]


def _ok(value: Any, logs: str = "") -> str:
    payload: dict[str, Any] = {"ok": True, "value": value}
    if logs:
        payload["logs"] = logs
    return json.dumps(payload, separators=(",", ":"))


def _error(error: GarminSyncError, logs: str = "") -> str:
    error_payload = error.as_dict()
    if isinstance(error_payload.get("details"), str):
        error_payload["details"] = _redact(error_payload["details"])
    payload: dict[str, Any] = {"ok": False, "error": error_payload}
    if logs:
        payload["logs"] = logs
    return json.dumps(payload, separators=(",", ":"))


def _invoke(callback: Callable[[], Any], fallback: str) -> str:
    handler = _CaptureHandler()
    handler.setFormatter(logging.Formatter("%(asctime)s %(levelname)s [%(name)s] %(message)s", "%H:%M:%S"))
    root = logging.getLogger()
    previous_level = root.level
    root.addHandler(handler)
    root.setLevel(logging.DEBUG)
    failure: GarminSyncError | None = None
    value: Any = None
    try:
        value = callback()
    except GarminSyncError as error:
        failure = error
    except Exception as error:
        failure = GarminSyncError("INTERNAL", fallback, details=f"{type(error).__name__}: {_redact(str(error))}"[:4000])
    finally:
        root.removeHandler(handler)
        root.setLevel(previous_level)
    logs = _bounded_logs(handler)
    return _error(failure, logs) if failure else _ok(value, logs)


def _require_mobile_transport() -> None:
    """Fail locally before credentials reach Garmin if curl_cffi is broken."""

    try:
        status = require_curl_cffi()
    except Exception as error:
        raise GarminSyncError(
            "GARMIN_TRANSPORT_UNAVAILABLE",
            "Garmin login stopped before any Garmin request because curl_cffi is unavailable",
            details=f"curl_cffi preflight failed: {type(error).__name__}: {_redact(str(error))}"[:4000],
        ) from None
    logging.getLogger(__name__).info(
        "curl_cffi preflight passed before Garmin login: module=%s",
        status.get("module", "unknown"),
    )


def start_login(email: str, password: str) -> str:
    def invoke() -> Any:
        _require_mobile_transport()
        return _start_login(email, password)

    return _invoke(invoke, "Garmin login could not be completed")


def resume_login(login_id: str, code: str) -> str:
    return _invoke(lambda: _resume_login(login_id, code), "Garmin verification could not be completed")


def cancel_login(login_id: str) -> str:
    return _invoke(lambda: _cancel_login(login_id), "Garmin verification could not be cancelled")


def sync(config_json: str, token_json: str, access_token: str) -> str:
    def invoke() -> Any:
        config = json.loads(config_json)
        return _sync(config, token_json, access_token)

    return _invoke(invoke, "Garmin sync could not be completed")
