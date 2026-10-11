"""Native Android adapter which forwards real runner progress to Kotlin."""

import json

from phone_garmin_bridge import (
    _invoke,
    cancel_login,
    resume_login,
    start_login,
)
from fitsync_garmin.runner import sync as run_sync


def sync(config_json, token_json, access_token, progress):
    def report(event):
        progress.emit(json.dumps(event, separators=(",", ":")))

    return _invoke(
        lambda: run_sync(json.loads(config_json), token_json, access_token, report),
        "Garmin sync could not be completed",
    )
