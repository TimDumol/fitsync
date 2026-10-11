# Garmin Connect vendor

This directory contains the source copied from the `garminconnect` 0.3.4
distribution used by FitSync's Python environment.

- Source: https://github.com/cyberjunky/python-garminconnect
- License: MIT (see `LICENSE.garminconnect`)
- Distribution: `garminconnect==0.3.4`
- Wheel SHA-256: `1f360434c220e42f292fb79d4719f457d9e182ac32f4488b23845abb24fccbac`

Local changes retain the required mobile transport checks and backport the
0.3.5 fix for [GHSA-wjhr-76vg-2hvc](https://github.com/advisories/GHSA-wjhr-76vg-2hvc):
token directories use mode 0700, token files use mode 0600, and token writes
reject symlinks where `O_NOFOLLOW` is available. Existing file permissions are
tightened before writing. Desktop Python uses the upstream 0.3.5 package.

The pure-Python package and the Android ARM64 `curl_cffi` transport are bundled.
The mobile login requires `curl_cffi` and deliberately does not fall back to
plain `requests`, because Garmin may treat that transport as bot-like.
