# Garmin Connect vendor

This directory contains the source copied from the `garminconnect` 0.3.4
distribution used by FitSync's Python environment.

- Source: https://github.com/cyberjunky/python-garminconnect
- License: MIT (see `LICENSE.garminconnect`)
- Distribution: `garminconnect==0.3.4`
- Wheel SHA-256: `1f360434c220e42f292fb79d4719f457d9e182ac32f4488b23845abb24fccbac`

The pure-Python package and the Android ARM64 `curl_cffi` transport are bundled.
The mobile login requires `curl_cffi` and deliberately does not fall back to
plain `requests`, because Garmin may treat that transport as bot-like.
