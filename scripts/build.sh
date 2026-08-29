#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
cd "$repo_root"

go_cache="${GOCACHE:-/tmp/fitsync-go-cache}"
go_mod_cache="${GOMODCACHE:-/tmp/fitsync-go-modcache}"
binary_path="${FITNESSD_BUILD_OUTPUT:-/tmp/fitsync-fitnessd}"
image="${FITNESSD_IMAGE:-fitsync-fitnessd:local}"

usage() {
  printf '%s\n' \
    'Usage: ./scripts/build.sh [--binary|--docker]' \
    '' \
    'Builds the Go binary by default. Use --docker to build the container image.'
}

mode="${1:---binary}"
case "$mode" in
  --binary)
    mkdir -p "$(dirname -- "$binary_path")"
    env GOCACHE="$go_cache" GOMODCACHE="$go_mod_cache" \
      go build -trimpath -ldflags='-s -w' -o "$binary_path" ./cmd/fitnessd
    printf 'built %s\n' "$binary_path"
    ;;
  --docker)
    docker build --quiet --tag "$image" .
    printf 'built docker image %s\n' "$image"
    ;;
  --help|-h)
    usage
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
