# FitSync Android

Kotlin/Compose app with Room storage for aggregate JSON, active drafts, rest timers and a transactional outbox. WorkManager retries workout changes. Pocket ID sign-in uses AppAuth with PKCE and encrypted token storage excluded from backup. Garmin imports run the shared `fitsync_garmin` engine through Chaquopy; Android bridge, vault and vendored client sources live under `app/src/production/`.

Build, test and install using repository entrypoints, outside the sandbox:

```bash
./scripts/build-mobile.sh
./scripts/test-mobile-smoke.sh
./scripts/install-mobile.sh
./scripts/test-mobile-e2e.sh
```

Production APKs target ARM64. `FITSYNC_MOBILE_FIXTURES=1` builds an isolated, Python-free APK that runs on x86_64 emulators. Fixtures always use `com.fitsync.mobile.nativefixtures`, even when `FITSYNC_APPLICATION_ID` is set; only fixture builds package synthetic data. The fixture installer refuses to target another package. Maestro clears only the separate fixture app, preserving the production app and its workout data. E2E defaults to an emulator; a phone is used only when explicitly selected with `FITSYNC_E2E_DEVICE`. Fixture builds reset their local database on cold startup. Production builds never reset it. APKs are written to `apps/android/dist/`; Maestro flows live in `.maestro/`.

Requirements: JDK 17+, Android SDK 35 and the checked-in Gradle 8.13 wrapper. Smoke/build verification includes JVM domain tests, Robolectric storage/sync tests, Compose interaction tests and Android lint. Maestro requires a connected device or an emulator with hardware acceleration. Garmin imports require an ARM64 phone and a Garmin account.

## Configuration

Use `FITSYNC_API_BASE_URL`, `FITSYNC_POCKET_ID_ISSUER`, `FITSYNC_OAUTH_CLIENT_ID` and `FITSYNC_OAUTH_RESOURCE`. `FITSYNC_BUILD_TIMESTAMP` overrides build metadata.

For a separate development installation, set `FITSYNC_APPLICATION_ID=com.fitsync.nativepreview`, `FITSYNC_OAUTH_REDIRECT_URI=fitsync-native-preview:///oauthredirect`, and the public Pocket ID client ID registered for that callback. Production defaults are `com.fitsync.mobile` and `fitsync:///oauthredirect`.

Distribution signing uses `FITSYNC_SIGNING_STORE`, `FITSYNC_SIGNING_STORE_PASSWORD`, `FITSYNC_SIGNING_ALIAS` and `FITSYNC_SIGNING_KEY_PASSWORD`. Without these, APKs use the local Android debug certificate. Set a higher release versionCode with `FITSYNC_ANDROID_VERSION_CODE`. Updates must use the same certificate as the installed APK; the installer never uninstalls an app to bypass a mismatch.

## Local data and account recovery

Existing `fitsync.db` data is imported transactionally into Room, preserving caches, drafts, pending writes, timers, cursors and athlete selection. Unsupported or malformed data stops import with an error and leaves the source intact. Imported data remains unavailable until the matching server-verified account signs in.

A rejected or expired session returns to sign-in without deleting drafts, timers, pending writes or Garmin sessions. Signing in with the same account restores access to them. Logout deliberately clears workout/account data and pending changes; a confirmation appears when pending writes exist.

Today and Plan display only an active plan. A successful empty active-plan response clears the selected plan while retaining cached drafts for publication and archived plans for storage.

Live Pocket ID callbacks, signing compatibility, and Garmin session reuse/upload require verification on the intended deployment/device. Avoid uninstalling or downgrading an installation with pending local data.
