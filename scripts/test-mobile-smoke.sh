#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
mobile_root="$repo_root/apps/mobile"

if ! command -v npm >/dev/null 2>&1; then
  printf '%s\n' 'npm is required to run the mobile smoke tests.' >&2
  exit 1
fi

cd "$mobile_root"
if [[ "${FITSYNC_MOBILE_INSTALL:-0}" == 1 || ! -x node_modules/.bin/vitest || ! -x node_modules/.bin/tsc ]]; then
  npm ci --include=dev
fi
npm run typecheck
npm test

printf '%s\n' 'Mobile phone-free smoke tests passed.'
