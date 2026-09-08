"""Credential-free-at-rest Garmin login state.

The only password-bearing object in this module is the short-lived client
created by :func:`start_login`.  It is intentionally process memory only.
"""

from __future__ import annotations

import json
import re
import secrets
import threading
import time
from dataclasses import dataclass
from typing import Any, Callable

from garminconnect import (
    Garmin,
    GarminConnectAuthenticationError,
    GarminConnectConnectionError,
    GarminConnectTooManyRequestsError,
)

LOGIN_LIFETIME_SECONDS = 10 * 60


class GarminSyncError(RuntimeError):
    """Safe error that can cross the Python/native boundary."""

    def __init__(self, code: str, message: str, retryable: bool = False, details: str | None = None) -> None:
        super().__init__(message)
        self.code = code
        self.message = message
        self.retryable = retryable
        self.details = details

    def as_dict(self) -> dict[str, Any]:
        return {
            "code": self.code,
            "message": self.message,
            "retryable": self.retryable,
            **({"details": self.details} if self.details else {}),
        }


@dataclass
class _PendingLogin:
    login_id: str
    client: Garmin
    expires_at: float


_lock = threading.RLock()
_pending: _PendingLogin | None = None


def _debug_detail(error: Exception) -> str:
    """Return useful diagnostics without allowing secrets into app logs."""

    detail = f"{type(error).__name__}: {error}"
    return re.sub(
        r"(?i)(password|passphrase|token|authorization|cookie|secret|credential)\s*[:=]\s*[^\s,;}]+",
        r"\1=[REDACTED]",
        detail,
    )[:4000]


def _safe_token(client: Any) -> str:
    try:
        token_client = getattr(client, "client", client)
        value = token_client.dumps()
    except Exception as exc:
        raise GarminSyncError("INTERNAL", "Garmin session could not be saved", details=_debug_detail(exc)) from exc
    if not isinstance(value, str) or not value.strip():
        raise GarminSyncError("INTERNAL", "Garmin session could not be saved")
    try:
        parsed = json.loads(value)
    except (TypeError, ValueError) as exc:
        raise GarminSyncError("INTERNAL", "Garmin session could not be saved", details=_debug_detail(exc)) from exc
    if not isinstance(parsed, dict) or not any(parsed.values()):
        raise GarminSyncError("INTERNAL", "Garmin session could not be saved")
    return value


def _map_login_error(error: Exception) -> GarminSyncError:
    if isinstance(error, GarminConnectAuthenticationError):
        return GarminSyncError("GARMIN_AUTH_FAILED", "Garmin authentication failed", details=_debug_detail(error))
    if isinstance(error, GarminConnectTooManyRequestsError):
        return GarminSyncError("GARMIN_RATE_LIMITED", "Garmin asked you to wait before trying again", True, _debug_detail(error))
    if isinstance(error, GarminConnectConnectionError):
        return GarminSyncError("GARMIN_REQUEST_FAILED", "Garmin could not be reached", True, _debug_detail(error))
    return GarminSyncError("INTERNAL", "Garmin login could not be completed", details=_debug_detail(error))


def _expire_pending(now: float | None = None) -> _PendingLogin | None:
    global _pending
    now = time.monotonic() if now is None else now
    if _pending is not None and _pending.expires_at <= now:
        _pending = None
        return None
    return _pending


def start_login(email: str, password: str, *, garmin_factory: Callable[..., Garmin] = Garmin) -> dict[str, Any]:
    """Start an interactive Garmin login without persisting credentials."""

    if not isinstance(email, str) or not email.strip() or not isinstance(password, str) or not password:
        raise GarminSyncError("GARMIN_AUTH_FAILED", "Enter your Garmin email and password")
    global _pending
    with _lock:
        _expire_pending()
        if _pending is not None:
            raise GarminSyncError("INTERNAL", "Another Garmin login is already in progress")
        try:
            client = garmin_factory(email.strip(), password, return_on_mfa=True)
            status, _ = client.login()
        except GarminSyncError:
            _pending = None
            raise
        except Exception as exc:
            _pending = None
            raise _map_login_error(exc) from None
        if status == "needs_mfa":
            login_id = secrets.token_urlsafe(24)
            _pending = _PendingLogin(login_id, client, time.monotonic() + LOGIN_LIFETIME_SECONDS)
            return {"status": "needs_mfa", "login_id": login_id, "token_json": None}
        _pending = None
        return {"status": "connected", "login_id": None, "token_json": _safe_token(client)}


def resume_login(login_id: str, mfa_code: str) -> dict[str, Any]:
    """Complete the one live MFA flow, passing the compatibility state dict."""

    if not isinstance(login_id, str) or not login_id or not isinstance(mfa_code, str) or not mfa_code.strip():
        raise GarminSyncError("GARMIN_AUTH_FAILED", "Enter the Garmin verification code")
    global _pending
    with _lock:
        pending = _expire_pending()
        if pending is None or pending.login_id != login_id:
            _pending = None
            raise GarminSyncError("GARMIN_MFA_EXPIRED", "The Garmin verification session expired", True)
        try:
            pending.client.resume_login({}, mfa_code.strip())
            token_json = _safe_token(pending.client)
        except GarminSyncError:
            _pending = None
            raise
        except Exception as exc:
            _pending = None
            mapped = _map_login_error(exc)
            if mapped.code == "INTERNAL":
                mapped = GarminSyncError("GARMIN_AUTH_FAILED", "The Garmin verification code was not accepted", details=mapped.details)
            raise mapped from None
        _pending = None
        return {"status": "connected", "token_json": token_json}


def cancel_login(login_id: str) -> None:
    global _pending
    with _lock:
        if _pending is not None and _pending.login_id == login_id:
            _pending = None
