#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
mobile_root="$repo_root/apps/mobile"

if ! command -v npm >/dev/null 2>&1; then
  printf '%s\n' 'npm is required to build the mobile app.' >&2
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
  printf '%s\n' 'a JDK (java and javac) is required to build the Android APK.' >&2
  exit 1
fi

android_sdk="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-/home/timdumol/Android/Sdk}}"
if [[ -z "$android_sdk" || ! -x "$android_sdk/platform-tools/adb" ]]; then
  printf '%s\n' 'ANDROID_HOME or ANDROID_SDK_ROOT must point to an Android SDK.' >&2
  exit 1
fi

cd "$mobile_root"
export ANDROID_HOME="$android_sdk"
export ANDROID_SDK_ROOT="$android_sdk"
export EXPO_PUBLIC_API_BASE_URL="${EXPO_PUBLIC_API_BASE_URL:-https://api.aries.timdumol.com}"
export EXPO_PUBLIC_POCKET_ID_ISSUER="${EXPO_PUBLIC_POCKET_ID_ISSUER:-https://auth.aries.timdumol.com}"
export EXPO_PUBLIC_OAUTH_CLIENT_ID="${EXPO_PUBLIC_OAUTH_CLIENT_ID:-fitsync-mobile}"
apk_output="$mobile_root/dist/fitsync-mobile.apk"
mkdir -p "$mobile_root/dist"
rm -f -- "$apk_output"

# Install the exact dependency tree recorded in the lockfile.
npm ci --include=dev

npm run typecheck
npm test

# Expo generates the native Android project from app.json. Keep that generated
# project temporary and retain only the installable APK in dist/.
native_project_created=false
if [[ ! -d android ]]; then
  native_project_created=true
fi
cleanup() {
  if [[ "$native_project_created" == true ]]; then
    rm -rf -- "$mobile_root/android"
  fi
}
trap cleanup EXIT

npx expo prebuild --platform android --no-install
export NODE_ENV=production
(cd android && ./gradlew --no-daemon assembleRelease)

cp android/app/build/outputs/apk/release/app-release.apk "$apk_output"
printf 'built APK at %s\n' "$apk_output"
