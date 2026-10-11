#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
export ANDROID_HOME="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-/home/timdumol/Android/Sdk}}"
export ANDROID_SDK_ROOT="$ANDROID_HOME"
fixtures="${FITSYNC_MOBILE_FIXTURES:-0}"
case "$fixtures" in 0|1) ;; *) echo 'FITSYNC_MOBILE_FIXTURES must be 0 or 1.' >&2; exit 2 ;; esac
if [[ "$fixtures" == 1 ]]; then
  export FITSYNC_APPLICATION_ID=com.fitsync.mobile.nativefixtures
fi
cd "$repo_root/apps/android"
# Serialize entrypoints which share Gradle configuration and generated schema files.
exec 9>"$repo_root/apps/android/.build.lock"
flock 9
if [[ ! -x ./gradlew ]]; then
  gradle_bin="$(command -v gradle || true)"
  if [[ -z "$gradle_bin" ]]; then
    for candidate in "$HOME"/.gradle/wrapper/dists/gradle-8.13-bin/*/gradle-8.13/bin/gradle; do
      if [[ -x "$candidate" ]]; then gradle_bin="$candidate"; break; fi
    done
  fi
  if [[ -z "$gradle_bin" ]]; then echo 'Gradle 8.13 is required to bootstrap the wrapper.' >&2; exit 1; fi
  wrapper_root="$(mktemp -d /tmp/fitsync-wrapper.XXXXXX)"
  trap 'rm -rf -- "$wrapper_root"' EXIT
  touch "$wrapper_root/settings.gradle"
  "$gradle_bin" --no-daemon -p "$wrapper_root" wrapper --gradle-version 8.13
  cp "$wrapper_root/gradlew" "$wrapper_root/gradlew.bat" .
  mkdir -p gradle/wrapper
  cp "$wrapper_root/gradle/wrapper/gradle-wrapper.jar" "$wrapper_root/gradle/wrapper/gradle-wrapper.properties" gradle/wrapper/
  chmod +x gradlew
fi
if [[ "$fixtures" == 1 ]]; then variant=fixtures; else variant=production; fi
# Keep task history separate when production and fixture source sets alternate.
options=(--no-daemon --project-cache-dir "$repo_root/apps/android/.gradle/$variant" --max-workers="${FITSYNC_GRADLE_MAX_WORKERS:-2}")
if [[ -n "${FITSYNC_ANDROID_VERSION_CODE:-}" ]]; then options+=("-PversionCode=$FITSYNC_ANDROID_VERSION_CODE"); fi
if [[ "$fixtures" == 1 ]]; then options+=(-Pfixtures=true); fi
if [[ "${1:-}" == --test ]]; then
  ./gradlew "${options[@]}" testReleaseUnitTest lintRelease
  exit
fi
./gradlew "${options[@]}" testReleaseUnitTest lintRelease assembleRelease
if [[ "$fixtures" == 1 ]]; then default_apk="$repo_root/apps/android/dist/fitsync-mobile-fixtures.apk"; else default_apk="$repo_root/apps/android/dist/fitsync-mobile.apk"; fi
apk="${FITSYNC_MOBILE_APK:-$default_apk}"
mkdir -p -- "$(dirname -- "$apk")"
cp "app/build/$variant/outputs/apk/release/app-release.apk" "$apk"
printf '%s\n' "$fixtures" > "$apk.fixtures"
if [[ "$fixtures" == 1 ]]; then default_package=com.fitsync.mobile.nativefixtures; else default_package=com.fitsync.mobile; fi
printf '%s\n' "${FITSYNC_APPLICATION_ID:-$default_package}" > "$apk.package"
printf 'Built native APK at %s (fixtures: %s)\n' "$apk" "$fixtures"
