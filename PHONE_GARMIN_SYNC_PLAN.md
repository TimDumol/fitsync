# Phone-hosted Garmin sync implementation plan

## 1. Purpose

Add an Android-only Garmin importer to the existing FitSync mobile app. A user
must be able to connect a Garmin account, press **Sync Garmin now**, and upload
Garmin data directly from the phone to the existing Fitnessd server. The
laptop must not participate in the sync.

The final data path is:

```text
Garmin Connect
    -> HTTPS from the Android phone's current network
FitSync Android app
    -> HTTPS with the signed-in user's Pocket ID access token
Fitnessd on api.aries.timdumol.com
    -> existing SQLite Garmin tables and archive storage
```

Garmin sees the public egress address of the phone's active connection. On
Wi-Fi this is normally the router's public address; on cellular it is normally
the carrier NAT address; with a VPN it is the VPN exit address. Do not claim
that Garmin sees the phone's private LAN address.

This document is normative for this feature. Implement the phases in order.
Do not substitute Pyodide, a WebView Python runtime, or a server-side Garmin
worker without first updating this plan.

## 2. Fixed design decisions

### 2.1 Runtime

Use Chaquopy to embed CPython in the Android build. Use Python 3.12 initially,
because the repository already requires Python 3.12 or newer and Chaquopy 17
supports it. Pin the exact Chaquopy version in Gradle; do not use a dynamic
version.

The Expo app is generated with Continuous Native Generation. Native Garmin
code and Gradle configuration must live in a checked-in local Expo module and,
if necessary, a checked-in Expo config plugin. Never rely on edits made only
inside `apps/mobile/android`, because `scripts/build-mobile.sh` may generate
and remove that directory on every build.

### 2.2 Garmin client

Reuse the pinned `garminconnect` Python implementation. Do not rewrite the
Garmin authentication and endpoint client in TypeScript or Kotlin in the first
release.

Do not package `curl-cffi` on Android. It is a native dependency without a
normal Android wheel and is optional at runtime in the pinned Garmin client.
Vendor the exact `garminconnect` Python package source used by the repository,
including its upstream license, and let it use its plain `requests` login
strategy. Also omit the optional `ua-generator`; the client has a static user
agent fallback.

Keep the desktop dependency in `pyproject.toml` unchanged unless an update is
needed for an independently verified bug. The vendored Android copy and the
desktop pin must identify the same upstream version. Add a short README beside
the vendored copy recording the version, source URL, license, and SHA-256 of
the downloaded wheel or source archive.

### 2.3 Scope of the first release

The first release includes:

- connect with Garmin email and password;
- interactive MFA continuation;
- encrypted reusable Garmin session tokens;
- manual sync for 1, 3, or 7 days;
- activities, sleep, daily recovery, body-battery events, activity details,
  training snapshot, and body measurements;
- upload to Fitnessd with progress and a final summary;
- a server-derived last-sync status;
- disconnect and erase the locally stored Garmin session.

The first release does not include:

- exact scheduled execution;
- iOS support;
- activity ZIP/FIT archive downloads;
- storage of the Garmin password;
- a long-lived Fitnessd ingest secret in the APK;
- a WebView or Pyodide fallback;
- concurrent Garmin syncs;
- automatic account matching between the Garmin profile and a Fitnessd
  athlete.

Background sync is a later phase and must not delay the manual-sync release.

### 2.4 Authentication boundaries

There are two unrelated authentication sessions:

1. Pocket ID authenticates the mobile user to Fitnessd. Continue using the
   access and refresh tokens already managed by the mobile app.
2. Garmin tokens authenticate the embedded importer to Garmin Connect. Store
   one Garmin session per Fitnessd athlete ID on the device.

Never send Garmin email, password, MFA code, or Garmin session tokens to
Fitnessd. Never put the static `FITNESS_INGEST_*_TOKEN` values in source,
Expo public environment variables, app assets, Gradle resources, logs, or the
APK.

## 3. Current repository behavior to preserve

The existing importer in `garmin_sync.py` already implements the authoritative
dataset extraction and upload format. Preserve these behaviors:

- raw dataset names and record-key derivation;
- canonical compact JSON encoding with sorted keys;
- overlapping date syncs replacing changed Garmin records;
- a maximum of 100 records per server batch;
- upload retry only for network errors, HTTP 429, and HTTP 5xx responses;
- no retries for other HTTP 4xx responses;
- GPS removal from curated exports must not remove GPS from raw ingestion;
- Fitnessd calculates the payload hash and performs idempotent upsert;
- activity archives remain associated with their activity record when archive
  support is added later.

The server currently exposes token-authenticated
`POST /api/v1/ingest/garmin/batch`. Keep it operational for the Windows sync
task during this work. Add a separate OIDC-authenticated mobile route rather
than changing the meaning of the legacy route.

The mobile app's normal offline workout push/pull sync is separate from Garmin
ingestion. Do not add Garmin payloads to the mobile SQLite outbox.

## 4. Target repository layout

Use this layout unless an existing Expo convention requires a small naming
change:

```text
fitsync_garmin/
  __init__.py
  auth.py
  datasets.py
  uploader.py
  runner.py

apps/mobile/modules/garmin-sync/
  expo-module.config.json
  index.ts
  src/
    GarminSync.types.ts
    GarminSyncModule.ts
  android/
    build.gradle
    src/main/AndroidManifest.xml
    src/main/java/com/fitsync/garmin/
      GarminSyncModule.kt
      GarminSyncRunner.kt
      GarminTokenVault.kt
    src/main/python/
      phone_garmin_bridge.py
      garminconnect/
      GARMINCONNECT_VENDOR_README.md
      LICENSE.garminconnect

apps/mobile/src/garmin/
  service.ts
  tokenProvider.ts
  types.ts
  useGarminSync.ts
  GarminConnectForm.tsx
  GarminMfaForm.tsx
  GarminSyncPanel.tsx
  *.test.ts(x)

internal/ingest/
  ingest.go
  oidc.go
  *_test.go
```

Place shared importer code in the top-level `fitsync_garmin` package so the
desktop CLI and Android bridge exercise the same extraction and batching code.
Configure Chaquopy to include this directory as a Python source directory, or
copy it deterministically during the build. Do not maintain a second fork of
the extraction code under the mobile app.

## 5. Phase 0: compatibility spike

Complete this phase before implementing server or production UI changes.

### 5.1 Minimal spike

Create the local Expo module, start Chaquopy lazily, and expose one temporary
development-only method which:

1. reports the Python version;
2. imports `requests` and the vendored `garminconnect`;
3. confirms `garminconnect.client.HAS_CFFI` is false;
4. accepts Garmin email/password without persisting or logging either value;
5. constructs `Garmin(email, password, return_on_mfa=True)`;
6. calls `status, _ = login()`;
7. returns either `connected` or `needs_mfa`;
8. if MFA is required, retains the live Python Garmin object in memory and
   allows a second call to `resume_login({}, code)`; the pinned version
   requires the first dictionary argument for compatibility but ignores it,
   because the required MFA state is held on the live client;
9. calls one read-only endpoint, such as the current user's profile or one day
   of sleep data;
10. serializes the resulting session with `garmin.client.dumps()`, then creates
    `restored = Garmin()` and calls `restored.login(token_json)` to prove
    password-free reuse and initialize the restored client's profile/settings.

Use the exact public API of the vendored version. Do not simulate MFA by
performing a second password login.

### 5.2 Device matrix

Run the spike in a release APK on a physical arm64 Android device:

- once on Wi-Fi;
- once on mobile data;
- once after force-stopping and reopening the app using the serialized token;
- with MFA if the account enables it;
- with an intentionally wrong password to verify safe error handling.

Do not repeatedly perform fresh logins merely to test retries. Garmin can rate
limit by IP. Saved-token login must be the routine path.

### 5.3 Gate

Proceed only if plain `requests` can complete initial login, MFA when needed,
one authenticated request, token serialization, and saved-token reuse.

If the spike fails because of packaging, fix the checked-in module or choose a
supported Chaquopy/Python combination. If it fails because Garmin rejects the
plain-requests login strategy, stop. Do not add unsafe WebView settings, build
an unplanned native HTTP impersonation layer, or silently move the worker to
the server. Record the failure and ask for a design decision.

Remove the development-only spike method or put it behind the existing E2E
fixture flag before merging later phases.

## 6. Phase 1: refactor the Python sync core

### 6.1 Preserve the command line

Keep `garmin_sync.py` as a thin CLI with its current flags and console output.
Move implementation into `fitsync_garmin` without changing normal desktop
behavior. `garmin_sleep_probe.py` may import the shared auth/dataset helpers,
but its current CLI must continue to work.

The reusable runner must not call `input()`, `getpass()`, `print()`, or
`sys.exit()`. The CLI wrapper owns terminal interaction. The Android bridge
owns app interaction.

### 6.2 Python API

Expose a small, JSON-compatible API. Exact internal class names may differ,
but the behavior must match this contract:

```python
start_login(email: str, password: str) -> {
    "status": "connected" | "needs_mfa",
    "login_id": str | None,
    "token_json": str | None,
}

resume_login(login_id: str, mfa_code: str) -> {
    "status": "connected",
    "token_json": str,
}

sync(config: dict, token_json: str, access_token: str,
     progress_callback) -> {
    "status": "success",
    "records_sent": int,
    "archives_sent": 0,
    "batches_sent": int,
    "started_at": str,
    "completed_at": str,
    "latest_record_date": str | None,
    "refreshed_garmin_token_json": str,
}
```

`config` contains only:

```json
{
  "athlete_id": "UUID",
  "api_base_url": "https://api.aries.timdumol.com",
  "days": 3,
  "extended": true,
  "work_dir": "/absolute/app-private/path"
}
```

Validate all fields. Allow `days` values 1, 3, or 7 from the production UI.
Tests may use other values within the existing 1-3660 bound. Require HTTPS for
the API URL outside tests. Never accept an output path supplied by remote data.

### 6.3 MFA state

Generate an opaque random `login_id` for an in-memory pending Garmin client.
Allow at most one pending login per process. Expire it after ten minutes.
Clear it after success, cancellation, expiry, or error. The `login_id` is not a
Garmin secret and must not contain credentials.

If Android kills the process during MFA, return to the connect form and ask the
user to start login again. Do not persist the password or incomplete MFA
session.

### 6.4 Working files

Use one app-private work directory per athlete. Raw Garmin payloads may exist
there only as a retry buffer:

- create files with owner-only access where supported;
- never use shared/external Android storage;
- exclude the directory from Android backup;
- retain files after an upload failure so the user can retry;
- delete a batch after Fitnessd confirms it;
- remove abandoned temporary files older than seven days at startup;
- do not log paths whose names contain health identifiers;
- do not write Garmin session tokens into this directory.

Mobile batches should default to no more than 20 records even though the
server permits 100. Also cap encoded metadata for a mobile batch below the
server's 10 MiB limit. Split before constructing the full multipart body so a
large activity-detail response cannot cause an avoidable memory spike.

### 6.5 Progress and errors

Emit progress with stable stages:

```text
authenticating
fetching_activities
fetching_sleep
fetching_recovery
fetching_activity_details
fetching_training
uploading
cleaning_up
complete
```

Each event contains `stage`, `completed`, `total`, and a short display-safe
message. Never include response bodies, credentials, tokens, activity names,
health values, or raw exception representations.

Map errors to stable codes:

```text
GARMIN_AUTH_FAILED
GARMIN_MFA_REQUIRED
GARMIN_MFA_EXPIRED
GARMIN_RATE_LIMITED
GARMIN_REQUEST_FAILED
FITNESSD_UNAUTHORIZED
FITNESSD_FORBIDDEN
FITNESSD_RATE_LIMITED
FITNESSD_REQUEST_FAILED
OFFLINE
CANCELLED
INTERNAL
```

Include a safe message and a boolean `retryable`. Preserve detailed exceptions
only in local debug builds, with secrets and response bodies redacted.

## 7. Phase 2: OIDC-authenticated Garmin ingestion

### 7.1 Route

Add:

```text
POST /api/v2/athletes/{athlete_id}/ingest/garmin/batch
Authorization: Bearer <Pocket ID access token>
Content-Type: multipart/form-data
Required scope: fitness:write
```

Use the same multipart request and success response as the legacy route:

```json
{
  "athlete": "tim",
  "records": 12,
  "archives": 0
}
```

The path athlete ID is the authority for the requested target, but it is valid
only after OIDC authentication and the existing household target
authorization succeed. Never accept an athlete handle or ID in multipart JSON
as authorization evidence.

### 7.2 Refactor the handler

Refactor `internal/ingest.Handler` into two layers:

1. authentication resolves an authorized athlete ID;
2. shared ingestion parses, validates, and commits a request for that already
   authorized athlete.

The legacy route continues to hash its static bearer token and resolve the
configured athlete exactly as it does now. The new route validates the OIDC
access token, requires `fitness:write`, calls `AuthorizeTarget`, and then calls
the shared ingestion layer. Do not duplicate multipart parsing or commit SQL.

Keep all existing limits:

- request: 100 MiB;
- JSON metadata part: 10 MiB;
- one archive: 50 MiB;
- records per batch: 1-100;
- valid dates, upload IDs, JSON, and archive references;
- atomic database and archive behavior;
- server-calculated SHA-256 and idempotent upsert.

Return `401` for invalid/expired access tokens, `403` for an unauthorized
athlete or insufficient scope, `413` for size limits, `400` for invalid input,
and `500` for an internal transaction failure. Do not reveal whether a
forbidden athlete exists.

Document the new route in `api/openapi.yaml` even if multipart dispatch remains
a small manually registered handler. Regenerate checked-in OpenAPI Go and
TypeScript artifacts using the repository's existing generation mechanism.

### 7.3 Status route

Add:

```text
GET /api/v2/athletes/{athlete_id}/garmin/sync-status
Required scope: fitness:read
```

Response:

```json
{
  "last_ingested_at": "2026-09-02T08:30:00Z",
  "latest_record_date": "2026-09-02",
  "datasets": {
    "activities": "2026-09-02",
    "sleep": "2026-09-02",
    "daily_recovery": "2026-09-02"
  }
}
```

Use `null` for missing timestamps/dates. The dataset map contains only known
dataset names with records. Derive values from committed server rows, not from
client-supplied sync metadata.

## 8. Phase 3: production Android module

### 8.1 Module responsibilities

`GarminSyncModule.kt` is the Expo bridge. `GarminSyncRunner.kt` serializes
operations and invokes Python away from the UI thread. `GarminTokenVault.kt`
encrypts Garmin token JSON.

Expose these TypeScript-facing methods:

```typescript
type ConnectionState = 'disconnected' | 'connected' | 'needs_reconnect';

getConnectionState(athleteID: string): Promise<ConnectionState>;
startLogin(athleteID: string, email: string, password: string):
  Promise<{ status: 'connected' } | { status: 'needs_mfa'; loginID: string }>;
resumeLogin(athleteID: string, loginID: string, code: string):
  Promise<{ status: 'connected' }>;
cancelLogin(loginID: string): Promise<void>;
syncNow(options: NativeSyncOptions): Promise<NativeSyncResult>;
disconnect(athleteID: string): Promise<void>;
```

Emit a `garminSyncProgress` event using the stable Python progress contract.
Do not return Garmin token JSON to JavaScript.

Reject a second login or sync while an operation is running. A sync for one
athlete must not accidentally read the token for another athlete. Validate
athlete IDs as UUIDs before using them as vault aliases or directory names.

### 8.2 Token vault

Use Android Keystore AES-GCM with a non-exportable key. Store ciphertext,
nonce, schema version, and athlete ID in app-private storage. Authenticate the
athlete ID as associated data so ciphertext cannot be renamed between athlete
files.

On successful login or token refresh:

1. receive token JSON from Python in Kotlin memory;
2. validate that it is non-empty JSON;
3. encrypt it immediately;
4. atomically replace the previous ciphertext;
5. discard plaintext references.

On decryption or token-load failure, erase the unusable ciphertext and report
`needs_reconnect`. Do not fall back to plaintext storage. Configure Android
backup rules to exclude the vault and Garmin work directories. Disconnect must
delete the athlete ciphertext and pending work files; it does not delete data
already uploaded to Fitnessd.

### 8.3 Access token handling

Immediately before `syncNow`, the TypeScript layer asks the existing auth
provider for a fresh Pocket ID access token. Pass it to Kotlin and then Python
only for the duration of the upload. Do not persist it in the Garmin vault or
working files. If refresh fails, do not contact Garmin; report the existing app
authentication error first.

If Fitnessd returns `401`, refresh the Pocket ID access token once at the
TypeScript layer and retry only the unconfirmed batch. Do not re-fetch Garmin
data. A `403` is not retryable.

Implement that retry without adding another native method: before contacting
Garmin, `syncNow` checks for a pending manifest for the same athlete. If one
exists, it uploads its unconfirmed batches first. If all pending batches then
succeed, return that pending run's result and do not start a new Garmin fetch.
Thus the TypeScript layer can refresh the access token and call `syncNow` again
with the same options. A manifest belongs to exactly one athlete, records its
date range and confirmed batch IDs, contains no access token, and is deleted
only after every batch is confirmed.

### 8.4 Cancellation and lifecycle

The first release runs while the app is in the foreground. Keep the screen
awake only while a user-initiated sync is running, and allow cancellation
between individual Garmin calls and upload batches. Do not interrupt a server
request halfway through committing; mark cancellation and stop before the next
unit of work.

App backgrounding may allow the current operation to finish, but completion is
not guaranteed in this phase. Persist enough batch state to retry safely after
process death. Fitnessd idempotency is the final defense against duplicates.

## 9. Phase 4: mobile UI

Add a **Garmin import** section to the existing Settings screen. Keep its state
and terminology separate from the existing workout outbox `SyncStatus`.

### 9.1 Disconnected state

Show:

- explanation that Garmin credentials are used on the phone and are not sent
  to Fitnessd;
- Garmin email input;
- password input with secure text entry;
- **Connect Garmin** button;
- a link or short warning that this uses an unofficial Garmin interface.

Disable submission while fields are blank or an operation is active. Clear the
password field immediately after native `startLogin` resolves or rejects.

### 9.2 MFA state

Show a modal or dedicated panel with:

- MFA code input;
- **Verify** and **Cancel** actions;
- a ten-minute expiry message;
- no email or password echo.

Cancel invokes `cancelLogin`. Expiry returns to the disconnected state.

### 9.3 Connected state

Show:

- connected status for the currently selected athlete;
- server-derived last successful ingest time and latest record date;
- day selector with 1, 3, and 7; default 3;
- **Extended data** enabled by default;
- **Sync Garmin now**;
- current stage and progress;
- last result counts;
- **Disconnect Garmin** with confirmation.

Changing the selected athlete must reload that athlete's Garmin connection and
server status. Never reuse the previous athlete's displayed state or token.

After success:

1. refresh the Garmin status query;
2. invalidate activity and health queries;
3. run the ordinary Fitnessd pull if required for screens backed by local
   SQLite;
4. show a concise success result.

For errors, show the safe message, whether retry is useful, and a reconnect
action for authentication failures. A Garmin 429 message should tell the user
to wait; do not automatically launch another password login.

### 9.4 Fixture/E2E behavior

The existing `EXPO_PUBLIC_E2E=1` build must never initialize Chaquopy, contact
Garmin, or require Garmin credentials. Provide a fixture implementation of the
TypeScript service with disconnected, MFA, progress, success, and failure
states. Add Maestro coverage without network calls.

## 10. Phase 5: optional background sync

Implement only after manual sync is reliable on physical devices.

Use Expo BackgroundTask/TaskManager, backed by Android WorkManager. Register
one deferrable task with network availability as a constraint and a minimum
interval no shorter than allowed by Android. Treat the interval as inexact.
Do not promise execution at a specific clock time.

The task must:

1. exit successfully when signed out, disconnected from Garmin, already
   syncing, or synced recently;
2. refresh the Pocket ID token through the existing auth code;
3. sync the default three-day overlap without archives;
4. use persisted checkpoints and small batches;
5. stop before Android's worker time limit and resume on the next opportunity;
6. notify only when user action is required, such as reconnect or MFA.

Do not store the Garmin password to make unattended reauthentication possible.
If Garmin tokens cease to refresh, background work reports `needs_reconnect`
and waits for an interactive login.

Provide a Settings toggle labeled **Background Garmin sync**. Default it off
for the first release containing the feature. Explain that Android controls
the execution time and that battery-vendor restrictions can delay it.

## 11. Testing requirements

### 11.1 Python unit tests

Extend the Python suite with fake Garmin and HTTP clients. Cover:

- connected and MFA login paths;
- MFA expiry, cancellation, and incorrect codes;
- token serialization and reload without a password;
- all dataset record keys and dates;
- canonical JSON stability;
- mobile batch count and byte limits;
- retry classification and preservation of unconfirmed batches;
- refreshed Garmin token return;
- redaction of credentials, tokens, raw payloads, and health values;
- cleanup of confirmed and stale temporary files;
- unchanged desktop CLI behavior.

No automated test may contact Garmin or production Fitnessd.

### 11.2 Go tests

Cover:

- valid OIDC `fitness:write` mobile ingestion;
- missing/expired token;
- missing scope;
- authorized household target and unauthorized target;
- body athlete-ID spoofing has no effect;
- all existing multipart size/path/JSON validation;
- duplicate upload no-op and changed payload replacement;
- rollback behavior;
- continued legacy static-token ingestion;
- sync-status values, null behavior, and target authorization.

### 11.3 TypeScript tests

Cover the Garmin service and hook state machine:

- disconnected -> connecting -> connected;
- disconnected -> connecting -> MFA -> connected;
- cancellation and expiry;
- fresh Pocket ID token acquisition before native sync;
- one access-token refresh on Fitnessd `401`;
- no retry on `403` or Garmin `429`;
- selected-athlete isolation;
- progress and success rendering;
- disconnect cleanup;
- fixture mode never loads the native module.

### 11.4 Android tests

Add native unit or instrumentation tests for:

- vault encrypt/decrypt and athlete-bound associated data;
- atomic token replacement;
- corrupt ciphertext cleanup;
- operation mutex;
- Python-to-Kotlin result/error conversion;
- absence of plaintext tokens in files and logs;
- generated release APK startup on arm64.

### 11.5 Maestro tests

Using fixtures only, cover:

- connect form validation;
- MFA UI and cancellation;
- manual sync progress and result;
- recoverable and reconnect-required errors;
- athlete switching;
- disconnect confirmation.

## 12. Manual acceptance checklist

Use a real, non-emulator Android phone and a test window which will not cause
harmful Garmin rate limiting.

1. Install a release APK built by the repository entrypoint.
2. Sign into Pocket ID and select the intended athlete.
3. Connect Garmin with correct credentials and complete MFA if requested.
4. Confirm the password field is cleared and no credential appears in Logcat.
5. Run a three-day extended sync on Wi-Fi.
6. Confirm Fitnessd contains expected raw records with GPS retained where the
   Garmin payload contains it.
7. Run the same sync again and confirm no duplicate records.
8. Restart and then force-stop/reopen the app; sync without entering a
   password.
9. Repeat a small sync on cellular data and confirm success.
10. Interrupt connectivity during upload, restore it, retry, and confirm only
    unconfirmed batches are resent.
11. Switch athletes and confirm connection/status isolation.
12. Disconnect Garmin, confirm local tokens and pending payloads are removed,
    and confirm server data remains.
13. Inspect the release APK and generated Android backup rules for accidental
    secrets or raw test data.

## 13. Verification commands

Repository instructions require these entrypoints to run outside the sandbox.
Use focused checks while iterating, then the complete checks before declaring
the feature done:

```bash
./scripts/test.sh --python
./scripts/test.sh --go
./scripts/test.sh --vet
./scripts/test.sh
./scripts/build.sh
./scripts/build-mobile.sh
./scripts/test-mobile-e2e.sh
```

The final handoff must report which commands passed, which physical-device
acceptance checks passed, and any checks not run. A successful typecheck alone
is not sufficient.

## 14. Delivery sequence and completion criteria

Deliver in these independently reviewable increments:

1. Chaquopy/plain-requests compatibility spike on a physical phone.
2. Shared Python core with unchanged desktop behavior.
3. OIDC ingest and sync-status server routes.
4. Production Android bridge and encrypted vault.
5. Settings UI and fixture tests.
6. Physical-device hardening and documentation.
7. Optional background sync in a later change.

Manual phone sync is complete only when:

- the release APK performs Garmin login/MFA and saved-token reuse;
- a signed-in user can sync the selected authorized athlete without a static
  ingest token in the APK;
- the requests originate from the phone's active network;
- all first-release datasets upload idempotently;
- credentials and tokens are absent from logs, backups, and plaintext files;
- errors are actionable and safe to display;
- desktop Garmin sync, legacy ingestion, mobile workout sync, and existing MCP
  behavior remain compatible;
- repository verification and the physical-device checklist pass.

Background scheduling is explicitly not part of the manual-sync completion
gate.
