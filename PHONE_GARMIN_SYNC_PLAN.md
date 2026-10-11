# Phone-hosted Garmin sync

The Android app imports Garmin data through Chaquopy on ARM64 phones. Desktop
CLI behavior remains in the shared `fitsync_garmin` package.

## Source ownership

- `apps/android/app/src/production/kotlin/com/fitsync/garmin/Platform.kt`: Kotlin service, progress callbacks, vault access and one forced API token-refresh retry.
- `GarminSyncRunner.kt` and `GarminTokenVault.kt` in that directory: serialized operations and athlete-bound Android Keystore storage.
- `apps/android/app/src/production/python/`: phone login/MFA bridge, Kotlin progress adapter and vendored `garminconnect` with its license and provenance.
- `fitsync_garmin/`: shared login, collection, batching, upload and cleanup engine.
- `apps/android/app/src/fixtures/`: synthetic Garmin behavior with no Python runtime or network credentials.

## Behavior and boundaries

Pocket ID tokens authorize FitSync uploads; Garmin sessions authorize Garmin
reads. Store Garmin sessions encrypted under the existing athlete-bound vault
alias in no-backup storage. Keep passwords, MFA codes and tokens out of logs.
MFA supports submit and cancellation. Settings supports 1/3/7-day imports,
extended-data selection, progress, record/batch totals, ingestion status and
confirmed disconnect. Keep the screen awake during foreground imports.

Retry an unauthorized FitSync upload once with a refreshed bearer token. A
rejected session returns to Pocket ID sign-in while preserving offline work.
Do not retry authorization failures or rate limits indiscriminately. Preserve
working batches until upload acknowledgement; disconnect removes local Garmin
session and pending Garmin files for the selected athlete.

Background Garmin scheduling is outside the current foreground import flow.
Workout synchronization uses WorkManager independently.

## Verification

Run repository scripts outside the sandbox:

```bash
./scripts/test.sh --python
./scripts/test-mobile-smoke.sh
./scripts/build-mobile.sh
./scripts/test-mobile-e2e.sh
```

Tests cover shared Python behavior, bridge progress/arguments, fixture MFA,
import and disconnect, and local storage/sync. Verify production Garmin login,
MFA, session reuse and upload on an ARM64 phone with a real account.
