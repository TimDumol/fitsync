#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
mobile_root="$repo_root/apps/android"
application_id="${FITSYNC_MOBILE_PACKAGE:-${FITSYNC_APPLICATION_ID:-com.fitsync.mobile}}"
cd "$repo_root"

usage() {
  printf '%s\n' \
    'Usage: ./scripts/install-mobile.sh [--phone] [--fixtures]' \
    '' \
    'Builds the release APK when it is missing, installs it on one connected' \
    'Android device or emulator, and launches the FitSync mobile app.' \
    '' \
    'Options:' \
    '  --phone                 Ignore emulators when selecting a target' \
    '  --fixtures              Build and install with test fixture data' \
    '' \
    'Environment:' \
    '  FITSYNC_MOBILE_APK       APK path (default: apps/android/dist/fitsync-mobile.apk)' \
    '  FITSYNC_MOBILE_BUILD=1   Always build the APK before installing' \
    '  FITSYNC_MOBILE_PACKAGE   Android application ID (default: com.fitsync.mobile)' \
    '  FITSYNC_ANDROID_DEVICE   ADB serial when more than one target is connected' \
    '  ANDROID_SERIAL           Standard ADB serial override' \
    '  .phonerc                 Local fallback containing FITSYNC_ANDROID_DEVICE'
}

phone_only=0
fixtures=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --phone)
      phone_only=1
      ;;
    --fixtures)
      fixtures=1
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
  shift
done

custom_apk=0
if [[ -n "${FITSYNC_MOBILE_APK:-}" ]]; then
  custom_apk=1
  apk_path="$FITSYNC_MOBILE_APK"
elif [[ "$fixtures" == 1 ]]; then
  apk_path="$mobile_root/dist/fitsync-mobile-fixtures.apk"
else
  apk_path="$mobile_root/dist/fitsync-mobile.apk"
fi
fixture_marker="$apk_path.fixtures"
if [[ "$fixtures" == 1 ]]; then
  application_id=com.fitsync.mobile.nativefixtures
fi

android_sdk="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-/home/timdumol/Android/Sdk}}"
adb_bin="${FITSYNC_ADB:-}"
if [[ -z "$adb_bin" && -n "$android_sdk" && -x "$android_sdk/platform-tools/adb" ]]; then
  adb_bin="$android_sdk/platform-tools/adb"
fi
if [[ -z "$adb_bin" ]]; then
  adb_bin="$(command -v adb || true)"
fi
if [[ -z "$adb_bin" ]]; then
  printf '%s\n' 'adb is required. Install Android platform-tools or set FITSYNC_ADB.' >&2
  exit 1
fi

is_emulator() {
  local serial="$1"
  local qemu

  if [[ "$serial" == emulator-* ]]; then
    return 0
  fi
  qemu="$("$adb_bin" -s "$serial" shell getprop ro.kernel.qemu 2>/dev/null | tr -d '\r' || true)"
  [[ "$qemu" == 1 ]]
}

"$adb_bin" start-server >/dev/null
configured_device=""
phonerc_path="$repo_root/.phonerc"
if [[ -z "${FITSYNC_ANDROID_DEVICE:-}" && -z "${ANDROID_SERIAL:-}" && -f "$phonerc_path" ]]; then
  configured_device="$(sed -nE \
    's/^[[:space:]]*FITSYNC_ANDROID_DEVICE[[:space:]]*=[[:space:]]*([^[:space:]#]+).*/\1/p' \
    "$phonerc_path" | tail -n 1)"
fi
target_device="${FITSYNC_ANDROID_DEVICE:-${ANDROID_SERIAL:-}}"
mapfile -t connected_devices < <("$adb_bin" devices | awk 'NR > 1 && $2 == "device" { print $1 }')
if [[ "$phone_only" == 1 ]]; then
  phone_devices=()
  for device in "${connected_devices[@]}"; do
    if ! is_emulator "$device"; then
      phone_devices+=("$device")
    fi
  done
  connected_devices=("${phone_devices[@]}")
fi

if [[ -z "$target_device" && -n "$configured_device" ]]; then
  for device in "${connected_devices[@]}"; do
    if [[ "$device" == "$configured_device" ]]; then
      target_device="$configured_device"
      break
    fi
  done
fi

if [[ -z "$target_device" ]]; then
  case "${#connected_devices[@]}" in
    0)
      if [[ "$phone_only" == 1 ]]; then
        printf '%s\n' 'No physical Android device is connected.' >&2
        printf '%s\n' 'Connect a phone or set FITSYNC_ANDROID_DEVICE without --phone.' >&2
      else
        printf '%s\n' 'No Android device or emulator is connected.' >&2
        printf '%s\n' 'Connect a target, start an emulator, or set FITSYNC_ANDROID_DEVICE.' >&2
      fi
      exit 1
      ;;
    1)
      target_device="${connected_devices[0]}"
      ;;
    *)
      printf '%s\n' 'More than one Android target is connected; set FITSYNC_ANDROID_DEVICE.' >&2
      printf 'Connected targets: %s\n' "${connected_devices[*]}" >&2
      exit 1
      ;;
  esac
fi

device_state="$("$adb_bin" -s "$target_device" get-state 2>/dev/null || true)"
if [[ "$device_state" != device ]]; then
  printf '%s\n' "Android target '$target_device' is not ready (state: ${device_state:-unknown})." >&2
  exit 1
fi

target_is_emulator=0
if is_emulator "$target_device"; then
  target_is_emulator=1
fi
if [[ "$phone_only" == 1 && "$target_is_emulator" == 1 ]]; then
  printf '%s\n' "Android target '$target_device' is an emulator, but --phone was specified." >&2
  exit 1
fi

installed_fixture_mode=""
if [[ -f "$fixture_marker" ]]; then
  installed_fixture_mode="$(<"$fixture_marker")"
fi
apk_package="$(cat "$apk_path.package" 2>/dev/null || printf '%s' com.fitsync.mobile)"
if [[ ! -f "$apk_path" || "${FITSYNC_MOBILE_BUILD:-0}" == 1 || ("$custom_apk" == 0 && ("$installed_fixture_mode" != "$fixtures" || "$apk_package" != "$application_id")) ]]; then
  FITSYNC_APPLICATION_ID="$application_id" FITSYNC_MOBILE_FIXTURES="$fixtures" FITSYNC_MOBILE_APK="$apk_path" "$repo_root/scripts/build-mobile.sh"
  installed_fixture_mode="$fixtures"
fi
if [[ ! -f "$apk_path" ]]; then
  printf '%s\n' "Mobile APK not found at $apk_path." >&2
  exit 1
fi

if [[ "$installed_fixture_mode" == 1 && ("$application_id" != com.fitsync.mobile.nativefixtures || "$(cat "$apk_path.package" 2>/dev/null)" != com.fitsync.mobile.nativefixtures) ]]; then
  printf '%s\n' 'Refusing to install fixtures over a non-test app. Rebuild with --fixtures.' >&2
  exit 1
fi
fixture_label="${installed_fixture_mode:-unknown}"
printf 'Installing %s on %s (fixtures: %s)...\n' "$apk_path" "$target_device" "$fixture_label"
install_options=(-r)
if [[ "$target_is_emulator" == 0 ]]; then
  install_options=(--no-streaming "${install_options[@]}")
fi
"$adb_bin" -s "$target_device" install "${install_options[@]}" "$apk_path"
printf 'Launching %s...\n' "$application_id"
"$adb_bin" -s "$target_device" shell monkey -p "$application_id" -c android.intent.category.LAUNCHER 1 >/dev/null
printf 'FitSync mobile app launched on %s.\n' "$target_device"
