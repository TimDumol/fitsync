#!/usr/bin/env bash
set -euo pipefail
export FITSYNC_MOBILE_FIXTURES=1
exec "$(dirname -- "${BASH_SOURCE[0]}")/build-mobile.sh" --test
