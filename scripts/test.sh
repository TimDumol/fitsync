#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
cd "$repo_root"

go_cache="${GOCACHE:-/tmp/fitsync-go-cache}"
go_mod_cache="${GOMODCACHE:-/tmp/fitsync-go-modcache}"
uv_cache="${UV_CACHE_DIR:-/tmp/fitsync-uv-cache}"

usage() {
  printf '%s\n' \
    'Usage: ./scripts/test.sh [--all|--go|--vet|--python]' \
    '' \
    'Runs the complete local suite by default. Narrow the scope when iterating.'
}

run_go_tests() {
  env GOCACHE="$go_cache" GOMODCACHE="$go_mod_cache" go test ./...
}

run_go_vet() {
  env GOCACHE="$go_cache" GOMODCACHE="$go_mod_cache" go vet ./...
}

run_python_tests() {
  env UV_CACHE_DIR="$uv_cache" uv run python -m unittest
}

mode="${1:---all}"
case "$mode" in
  --all)
    run_go_tests
    run_go_vet
    run_python_tests
    ;;
  --go)
    run_go_tests
    ;;
  --vet)
    run_go_vet
    ;;
  --python)
    run_python_tests
    ;;
  --help|-h)
    usage
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
