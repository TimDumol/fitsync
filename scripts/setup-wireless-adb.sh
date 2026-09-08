#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"

usage() {
  printf '%s\n' \
    'Usage: ./scripts/setup-wireless-adb.sh [--install] [--fixtures]' \
    '' \
    'Bootstraps ADB over Wi-Fi from one connected physical Android phone.' \
    'This uses legacy unencrypted ADB; use it only on a trusted LAN.' \
    'The phone must be connected by USB for the one-time bootstrap.' \
    '' \
    'Options:' \
    '  --install               Install the mobile APK after connecting' \
    '  --fixtures              Install the fixture APK (implies --install)' \
    '' \
    'Environment:' \
    '  FITSYNC_ADB             ADB executable override' \
    '  FITSYNC_ANDROID_DEVICE USB serial when more than one phone is connected' \
    '  FITSYNC_WIFI_IP         Phone Wi-Fi IPv4 address override' \
    '  FITSYNC_ADB_WIFI_PORT   TCP port (default: 5555)'
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

install_after_setup=0
fixtures=0
wifi_port="${FITSYNC_ADB_WIFI_PORT:-5555}"
target_device="${FITSYNC_ANDROID_DEVICE:-${ANDROID_SERIAL:-}}"
wifi_ip="${FITSYNC_WIFI_IP:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --install)
      install_after_setup=1
      ;;
    --fixtures)
      fixtures=1
      install_after_setup=1
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

android_sdk="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-/home/timdumol/Android/Sdk}}"
adb_bin="${FITSYNC_ADB:-}"
if [[ -z "$adb_bin" && -n "$android_sdk" && -x "$android_sdk/platform-tools/adb" ]]; then
  adb_bin="$android_sdk/platform-tools/adb"
fi
if [[ -z "$adb_bin" ]]; then
  adb_bin="$(command -v adb || true)"
fi
[[ -n "$adb_bin" ]] || die 'adb is required. Install Android platform-tools or set FITSYNC_ADB.'

if ! [[ "$wifi_port" =~ ^[0-9]+$ ]] || (( wifi_port < 1 || wifi_port > 65535 )); then
  die "invalid FITSYNC_ADB_WIFI_PORT: $wifi_port"
fi

is_emulator() {
  local serial="$1"
  local qemu

  [[ "$serial" == emulator-* ]] && return 0
  qemu="$("$adb_bin" -s "$serial" shell getprop ro.kernel.qemu 2>/dev/null | tr -d '\r' || true)"
  [[ "$qemu" == 1 ]]
}

"$adb_bin" start-server >/dev/null

if [[ -z "$target_device" ]]; then
  mapfile -t usb_devices < <(
    "$adb_bin" devices | awk '
      NR > 1 && $2 == "device" && $1 !~ /^emulator-/ && $1 !~ /:/ { print $1 }
    '
  )
  case "${#usb_devices[@]}" in
    0)
      die 'no physical USB Android phone is connected'
      ;;
    1)
      target_device="${usb_devices[0]}"
      ;;
    *)
      die "more than one physical phone is connected; set FITSYNC_ANDROID_DEVICE (found: ${usb_devices[*]})"
      ;;
  esac
fi

[[ "$target_device" != *:* ]] || die "target '$target_device' is already a network ADB target; provide the USB serial for bootstrap"
is_emulator "$target_device" && die "target '$target_device' is an emulator; connect a physical phone by USB"

device_state="$("$adb_bin" -s "$target_device" get-state 2>/dev/null || true)"
[[ "$device_state" == device ]] || die "USB target '$target_device' is not ready (state: ${device_state:-unknown})"

if [[ -z "$wifi_ip" ]]; then
  wifi_ip="$("$adb_bin" -s "$target_device" shell ip -4 -o addr show wlan0 2>/dev/null \
    | tr -d '\r' \
    | awk '/inet / { split($4, address, "/"); print address[1]; exit }' || true)"
fi
if [[ -z "$wifi_ip" ]]; then
  wifi_ip="$("$adb_bin" -s "$target_device" shell ip -4 route 2>/dev/null \
    | tr -d '\r' \
    | awk '/wlan0/ && /src/ { for (i = 1; i <= NF; i++) if ($i == "src") { print $(i + 1); exit } }' || true)"
fi

if ! [[ "$wifi_ip" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
  die "could not determine a Wi-Fi IPv4 address; set FITSYNC_WIFI_IP (detected: ${wifi_ip:-none})"
fi

wireless_device="$wifi_ip:$wifi_port"
printf 'Switching %s to legacy ADB-over-Wi-Fi at %s...\n' "$target_device" "$wireless_device"
"$adb_bin" -s "$target_device" tcpip "$wifi_port"

deadline=$((SECONDS + 30))
connected=0
while (( SECONDS < deadline )); do
  "$adb_bin" connect "$wireless_device" >/dev/null 2>&1 || true
  if [[ "$("$adb_bin" -s "$wireless_device" get-state 2>/dev/null || true)" == device ]]; then
    connected=1
    break
  fi
  sleep 1
done

(( connected == 1 )) || die "could not connect to $wireless_device; check that the phone and workstation are on the same LAN"

phonerc_path="$repo_root/.phonerc"
printf 'FITSYNC_ANDROID_DEVICE=%s\n' "$wireless_device" >"$phonerc_path"

printf 'Wireless ADB connected: %s\n' "$wireless_device"
printf 'Saved device target to %s\n' "$phonerc_path"
"$adb_bin" devices -l

if (( install_after_setup == 1 )); then
  install_args=()
  if (( fixtures == 1 )); then
    install_args+=(--fixtures)
  fi
  FITSYNC_ANDROID_DEVICE="$wireless_device" "$repo_root/scripts/install-mobile.sh" "${install_args[@]}"
else
  printf '\nRun the mobile installer with:\n'
  printf 'FITSYNC_ANDROID_DEVICE=%s ./scripts/install-mobile.sh\n' "$wireless_device"
fi
