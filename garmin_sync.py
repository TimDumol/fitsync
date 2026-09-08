"""Desktop CLI compatibility wrapper for the shared Garmin sync core."""

from fitsync_garmin.datasets import *
from fitsync_garmin.datasets import (
    _canonical_json_bytes,
    _raw_ingest_records,
    _without_activity_location,
    main,
    summarize_activity,
)


if __name__ == "__main__":
    raise SystemExit(main())
