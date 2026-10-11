#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
mobile_root="$repo_root/apps/android"
flow_target="${1:-$repo_root/.maestro}"
if [[ "$flow_target" != /* ]]; then flow_target="$PWD/$flow_target"; fi
apk_output="/tmp/fitsync-mobile-e2e.apk"
maestro_artifacts="/tmp/fitsync-maestro"
log_root="${FITSYNC_E2E_LOG_DIR:-/tmp}"
run_id="$(date -u +%Y%m%d-%H%M%S)"
log_file="$log_root/fitsync-mobile-e2e-$run_id.log"
verbose="${FITSYNC_E2E_VERBOSE:-0}"
emulator_pid=""
emulator_started=false
target_device=""
keep_emulator="${FITSYNC_E2E_KEEP_EMULATOR:-1}"
maestro_user_home="$(getent passwd "$(id -u)" 2>/dev/null | cut -d: -f6 || true)"
if [[ -n "$maestro_user_home" && -x "$maestro_user_home/.maestro/bin/maestro" ]]; then
  export PATH="$maestro_user_home/.maestro/bin:$PATH"
fi

mkdir -p -- "$log_root"
exec 3>&1
if [[ "$verbose" == 1 ]]; then
  exec > >(tee "$log_file") 2>&1
else
  exec >"$log_file" 2>&1
fi

finish() {
  exit_code=$?
  if [[ "$emulator_started" == true && "$keep_emulator" != 1 && -n "$emulator_pid" ]]; then
    kill "$emulator_pid" 2>/dev/null || true
    wait "$emulator_pid" 2>/dev/null || true
  fi
  if [[ "$exit_code" == 0 ]]; then
    printf 'Mobile Maestro E2E passed.\nFull log: %s\n' "$log_file" >&3
  else
    printf 'Mobile Maestro E2E failed (exit %s).\nFull log: %s\nMaestro artifacts: %s\nLast 80 log lines:\n' "$exit_code" "$log_file" "$maestro_artifacts" >&3
    tail -n 80 "$log_file" >&3
  fi
  exit "$exit_code"
}
trap finish EXIT
trap 'exit 130' INT TERM

maestro_bin="$(command -v maestro || true)"
if [[ -z "$maestro_bin" ]]; then
  printf '%s\n' 'Maestro CLI is required. Install it from https://docs.maestro.dev/getting-started.' >&2
  exit 1
fi

default_java_home="/tmp/prestosupuesto-toolchain/jdk-21.0.12.1+1"
java_home="${JAVA_HOME:-}"
if [[ -z "$java_home" ]] &&
  { ! command -v java >/dev/null 2>&1 || ! command -v javac >/dev/null 2>&1; } &&
  [[ -x "$default_java_home/bin/java" && -x "$default_java_home/bin/javac" ]]; then
  java_home="$default_java_home"
fi
if [[ -n "$java_home" && -x "$java_home/bin/java" && -x "$java_home/bin/javac" ]]; then
  export JAVA_HOME="$java_home"
  export PATH="$JAVA_HOME/bin:$PATH"
fi
if ! command -v java >/dev/null 2>&1 || ! command -v javac >/dev/null 2>&1; then
  printf '%s\n' 'a JDK (java and javac) is required to build the Android E2E app.' >&2
  exit 1
fi

android_sdk="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-/home/timdumol/Android/Sdk}}"
adb_bin="$android_sdk/platform-tools/adb"
emulator_bin="$android_sdk/emulator/emulator"
if [[ -z "$android_sdk" || ! -x "$adb_bin" ]]; then
  printf '%s\n' 'ANDROID_HOME or ANDROID_SDK_ROOT must point to an Android SDK.' >&2
  exit 1
fi
if [[ ! -x "$emulator_bin" ]]; then
  printf '%s\n' "Android emulator binary not found under $android_sdk." >&2
  exit 1
fi

cd "$mobile_root"
export ANDROID_HOME="$android_sdk"
export ANDROID_SDK_ROOT="$android_sdk"
export PATH="$android_sdk/platform-tools:$android_sdk/emulator:$PATH"
export MAESTRO_CLI_NO_ANALYTICS="${MAESTRO_CLI_NO_ANALYTICS:-1}"
export MAESTRO_CLI_ANALYSIS_NOTIFICATION_DISABLED="${MAESTRO_CLI_ANALYSIS_NOTIFICATION_DISABLED:-true}"

"$adb_bin" start-server >/dev/null
if [[ -n "${FITSYNC_E2E_DEVICE:-}" ]]; then
  target_device="$FITSYNC_E2E_DEVICE"
elif target_device="$($adb_bin devices | awk '$1 ~ /^emulator-/ && ($2 == "device" || $2 == "offline") { print $1; exit}')"; [[ -z "$target_device" ]]; then
  emulator_name="${FITSYNC_ANDROID_AVD:-fitsync_api35}"
  emulator_port="${FITSYNC_EMULATOR_PORT:-5554}"
  avd_found=false
  while IFS= read -r avd; do
    if [[ "$avd" == "$emulator_name" ]]; then
      avd_found=true
      break
    fi
  done < <("$emulator_bin" -list-avds)
  if [[ "$avd_found" != true ]]; then
    printf '%s\n' "Android AVD '$emulator_name' was not found. Set FITSYNC_ANDROID_AVD to an installed AVD." >&2
    exit 1
  fi
  if [[ "$(uname -s)" == Linux && (! -r /dev/kvm || ! -w /dev/kvm) ]]; then
    printf '%s\n' 'KVM hardware acceleration is required to start the Android emulator; grant this user access to /dev/kvm and retry.' >&2
    exit 1
  fi
  target_device="emulator-$emulator_port"
  emulator_args=(-avd "$emulator_name" -port "$emulator_port" -accel on -no-boot-anim -gpu swiftshader_indirect)
  if [[ "${FITSYNC_EMULATOR_HEADLESS:-1}" == 1 ]]; then
    emulator_args+=(-no-window)
  fi
  "$emulator_bin" "${emulator_args[@]}" &
  emulator_pid=$!
  emulator_started=true
fi

boot_deadline=$((SECONDS + 180))
while true; do
  device_state="$($adb_bin -s "$target_device" get-state 2>/dev/null || true)"
  boot_state="$($adb_bin -s "$target_device" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r' || true)"
  if [[ "$device_state" == device && "$boot_state" == 1 ]]; then
    break
  fi
  if (( SECONDS >= boot_deadline )); then
    printf '%s\n' "Android target '$target_device' did not finish booting within 180 seconds." >&2
    exit 1
  fi
  sleep 1
done

export FITSYNC_APPLICATION_ID=com.fitsync.mobile.nativefixtures
FITSYNC_MOBILE_FIXTURES=1 FITSYNC_MOBILE_APK="$apk_output" "$repo_root/scripts/build-mobile.sh"
"$adb_bin" -s "$target_device" install -r "$apk_output" >/dev/null
"$maestro_bin" --device "$target_device" test "$flow_target"
