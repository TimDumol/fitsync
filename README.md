# Garmin sleep probe

## Self-hosted Fitnessd service

The Go service is built with `./scripts/build.sh` and tested with
`./scripts/test.sh`. The production-shaped Compose configuration publishes
only Caddy; Fitnessd and Pocket ID are reachable through the Compose network.
Create the three files referenced by `docker-compose.yml` under `secrets/`
outside version control before starting Compose. Keep the bind-mounted data
directories owned by the corresponding container users.

Fitnessd fetches OIDC discovery from `POCKETID_ISSUER` at startup and uses the
advertised `jwks_uri`. The official provisioning flow queries Pocket ID's admin
API with the local `.pocket-id-api-key`, then binds the returned immutable
subjects to the two known athletes:

```bash
./scripts/sync-pocket-id-identities.sh
```

The script requires exactly one Pocket ID user named `tim` and one named
`jopi`, verifies each returned subject is a UUID, and invokes Fitnessd's local
transactional bind command. The API key is read over direct HTTPS and is never
copied to the server. Verify the result with `fitnessd identities list`.

Pending identities remain a defensive fallback for an unexpected or newly
created subject. They can be inspected with `fitnessd identities list-pending`,
but `identities approve` is not part of normal provisioning.

The approved `(issuer, sub)` binding is authoritative; usernames are display
metadata only. Deployment and initial Pocket ID user provisioning are manual
operations and are not performed by the repository test/build scripts.

## Production deployment

Production runs Fitnessd and Pocket ID under `/opt/fitsync` on
`aries.timdumol.com`. The existing Aprendiendo deployment at
`/opt/aprendiendo-mcp/app` remains the only Caddy edge proxy; deploy its
coordinated Caddy/network change first. That change creates the external
`caddy-edge` network and imports `/opt/caddy/sites-enabled/*`.

The controller needs Ansible, an SSH client, and the target's Docker Compose
plugin. Copy `deploy/ansible/inventory/hosts.example.yml` to
`deploy/ansible/inventory/hosts.yml`, adjust the passwordless-SSH deployment
user if needed, and optionally create an Ansible-Vault-encrypted
`deploy/ansible/group_vars/aries/secrets.yml`. Empty or missing secret values
are generated on the server only when their `0400` files do not already exist.

Run the guarded deployment with:

```bash
./scripts/deploy.sh
```

If `deploy/ansible/group_vars/aries/secrets.yml` exists and is Ansible-Vault
encrypted, add `--ask-vault-pass`. The deploy script skips that prompt when the
file is absent because missing secret values are generated on the server.

Use `./scripts/deploy.sh --check --diff` for a check-mode preview. The entrypoint
refuses to invoke Ansible unless `./scripts/test.sh` and `./scripts/build.sh`
both pass. It uploads only the files needed for the Fitnessd image; it never
uploads Git metadata, local databases, Garmin data, editor files, caches, or
plaintext secrets.

The deployment starts Pocket ID and waits for public OIDC discovery before
starting Fitnessd, because Fitnessd fetches that discovery document during
startup.

## Pocket ID and ChatGPT MCP OAuth

The deployed Pocket ID `v2.12.0` does not advertise an OAuth dynamic client
registration endpoint (`registration_endpoint`) or client metadata documents.
Create the OAuth client manually in Pocket ID; there is no Registration URL to
enter in ChatGPT.

The production API resource is configured as follows:

```text
Name:             Fitness MCP
Resource:         https://api.aries.timdumol.com/mcp
Permissions:      fitness:read, fitness:write
Client access:    ChatGPT MCP, user-delegated permissions
```

To reproduce this in Pocket ID, open `Settings > APIs`, add the resource and
both permission keys, then open the `ChatGPT MCP` client and grant it
user-delegated access to those permissions. Keep machine-to-machine client
permissions empty. An administrative API key can perform the same operation
through `/api/apis`, `/api/apis/{id}/permissions`, and
`/api/api-access/{client-id}`; keep that key outside Git and never put it in
the OAuth or MCP configuration.

For a manually configured ChatGPT MCP connection, use:

```text
MCP server:       https://api.aries.timdumol.com/mcp
Authorization:    https://auth.aries.timdumol.com/authorize
Token:            https://auth.aries.timdumol.com/api/oidc/token
Registration:     leave blank; use the manually created client ID and secret
Scopes:           openid email offline_access fitness:read fitness:write
PKCE:             enabled
```

### Exercise identity in MCP writes

MCP clients may always write a free-form `exercise_name`, but must never invent
an `exercise_key`. Use the read-only `resolve_exercises` action to resolve a
batch before writing; exact reviewed aliases receive a canonical key and
non-exact candidates are suggestions only:

```json
{
  "exercises": [
    {"ref": "day1-a", "name": "Bulgarian split squat"},
    {"ref": "day1-b", "name": "Pull-up or chin-up"}
  ],
  "max_candidates": 3
}
```

Plan and workout writes preserve the supplied `exercise_name`. Exact aliases
are normalized automatically, deprecated keys are rewritten, and unknown
names remain valid with a null key. Bulk writes return a compact normalization
report with counts and only exceptional or suggested items.

The OAuth client's redirect URI must exactly match the callback URL shown by
ChatGPT. Run `./scripts/sync-pocket-id-identities.sh` before connecting
ChatGPT. Once the two subjects are bound, no pending auth request or approval
step is needed. The `profile` scope is optional for this pre-provisioned flow;
it is only useful if the defensive unknown-identity fallback is exercised.

For the mobile app, create a separate Pocket ID OIDC client. It is a public
client, so no client secret is placed in the APK:

```text
Name:             FitSync Mobile
Client ID:        fitsync-mobile (or set FITSYNC_OAUTH_CLIENT_ID to the generated ID)
Callback URL:     fitsync:///oauthredirect
Public Client:    enabled
PKCE:             enabled (S256)
API access:       Fitness MCP, user-delegated fitness:read and fitness:write
```

The callback URL must match exactly, including its slash count and path. The app requests
`fitness:read fitness:write openid offline_access` and exchanges the code with
PKCE; it does not use a client secret.

This is a small, read-only trial of the unofficial `python-garminconnect`
client. It fetches Garmin sleep, HRV, and resting-heart-rate data and prints a
compact JSON summary suitable for later loading into a sheet or database.

## Mobile Android APK

The Kotlin/Compose Android app lives under `apps/android/`. See
[Android instructions](apps/android/README.md) for configuration and signing.
Build an installable production ARM64 APK with:

```bash
./scripts/build-mobile.sh
```

The script runs JVM/Compose tests and Android lint, then builds the release APK
at `apps/android/dist/fitsync-mobile.apk`. It requires JDK 17+, Android SDK 35
and the checked-in Gradle wrapper. Garmin imports run through embedded Python.

Release builds default to the production API and Pocket ID endpoints. Override
`FITSYNC_API_BASE_URL`, `FITSYNC_POCKET_ID_ISSUER`, `FITSYNC_OAUTH_CLIENT_ID`,
or `FITSYNC_OAUTH_RESOURCE` for another deployment. The OAuth resource defaults
to `<FITSYNC_API_BASE_URL>/mcp`.

To install the APK on a connected Android device or emulator and launch the
app, run:

```bash
./scripts/install-mobile.sh
```

With one ready ADB target connected, the helper automatically selects it and
uses the release APK at `apps/android/dist/fitsync-mobile.apk`. Set a device or
APK override only when multiple targets are connected or a different APK is
needed.

The script builds the APK automatically when it is missing. Set
`FITSYNC_MOBILE_BUILD=1` to force a rebuild. With more than one ADB target
connected, select one with `FITSYNC_ANDROID_DEVICE=serial` (or use the
standard `ANDROID_SERIAL` variable). Set `FITSYNC_MOBILE_APK` to install a
different APK. Pass `--phone` to ignore connected emulators and automatically
select the physical device. Physical-device installs use ADB's non-streaming
mode, which is more reliable over WSL2 USB connections. Mobile installs use
the real API and authentication by default, including phone installs. Pass
`--fixtures` to build and install the fixture-backed app explicitly; fixture
and normal APKs are cached separately so one cannot be mistaken for the other.

The build uses `JAVA_HOME` or the JDK on `PATH`. The Android SDK defaults to
`/home/timdumol/Android/Sdk` when `ANDROID_HOME` and `ANDROID_SDK_ROOT` are unset.

Override them for another machine, for example:

```bash
JAVA_HOME=/path/to/jdk ANDROID_HOME=/path/to/android-sdk \
  ./scripts/build-mobile.sh
```

### Mobile Maestro E2E tests

The mobile app has a deterministic Maestro suite under `.maestro/`. It builds
an isolated Android APK with fixture data, so it does not need Pocket ID
credentials or production health data. The fixture mode is enabled only by
the E2E script; normal mobile builds use the configured API and OAuth flow.

Install the [Maestro CLI](https://docs.maestro.dev/getting-started), make sure
the Android SDK has an AVD, then run:

```bash
./scripts/test-mobile-e2e.sh
```

The script prefers an existing emulator and otherwise starts the
`fitsync_api35` AVD headlessly. Override the AVD with
`FITSYNC_ANDROID_AVD=your_avd_name`; set `FITSYNC_E2E_KEEP_EMULATOR=0` to stop
an emulator started by the script when the run finishes. A physical device is
only used when explicitly selected with `FITSYNC_E2E_DEVICE=serial`.

On Linux, hardware acceleration requires the current user to have KVM access;
the script requires it and fails fast if it is unavailable. Add your user to
the `kvm` group and start a new login session before running the suite.

The script keeps the full build and Maestro output in a timestamped log under
`/tmp` and prints only a concise result by default. If a run fails, it prints
the tail of that log and the Maestro artifact directory; inspect the complete
log without rerunning, or stream output live with:

```bash
FITSYNC_E2E_VERBOSE=1 ./scripts/test-mobile-e2e.sh
```

The suite covers authentication gating and logout, tab navigation, plan and
planned-session review, skipping a session, workout editing/rest/finish, and
history/summary browsing. Run one flow while iterating with, for example:

```bash
./scripts/test-mobile-e2e.sh .maestro/flows/workout-lifecycle.yaml
```

For a faster phone-free check of the same fixture-backed core behavior, run:

```bash
./scripts/test-mobile-smoke.sh
```

This validates authentication data, plan review/skipping, workout editing and
completion, and history/summary data. It does not replace Maestro UI coverage;
it is intended for quick local iteration when no Android target is available.
The smoke suite runs JVM, Room/Robolectric and Compose tests plus Android lint.

## First run

From PowerShell in this directory:

```powershell
.\.venv\Scripts\python.exe .\garmin_sleep_probe.py --days 7 --output .\data\garmin_sleep_summary.json
```

The first run prompts interactively for your Garmin email and password, and
for an MFA code when Garmin requires one. The password is not written to disk.
Reusable Garmin tokens are stored under `.private\garmin`, which is excluded
by `.gitignore`.

Later runs load the saved tokens and normally do not prompt for credentials.

To inspect the complete responses while testing:

```powershell
.\.venv\Scripts\python.exe .\garmin_sleep_probe.py --days 7 --raw-dir .\data\raw\garmin\sleep
```

Raw health responses and token files are sensitive. Do not commit or share
them. Because this package uses unsupported Garmin Connect interfaces, expect
occasional authentication or endpoint breakage.

## Secret scanning

Install the project and development tools with `uv`, then enable the repository
hooks once per clone:

```powershell
uv sync --dev
cargo install foxguard --version 0.12.0 --locked
uv run pre-commit install
```

Foxguard is a system-language pre-commit hook, so its CLI must be available on
`PATH` in addition to the Python-based pre-commit package managed by `uv`.

Foxguard then scans staged content automatically on every commit. To scan all
tracked files manually, run:

```powershell
uv run pre-commit run foxguard --all-files
```

GitHub Actions runs the same Foxguard hook on pushes and pull requests. Treat
any finding as a real credential until verified; rotate a credential if it was
ever committed or pushed.

## Sync sleep and activities

After the first login, run the incremental combined sync:

```powershell
.\.venv\Scripts\python.exe .\garmin_sync.py --days 30
```

This updates:

- `data\garmin\sleep.json`
- `data\garmin\activities.json`
- `data\garmin\activities.csv` (sheet-friendly)

All Garmin activity types are included. Re-running an overlapping range safely
replaces that range and preserves older records. To download the original
FIT-containing ZIP for each newly encountered activity as well:

```powershell
.\.venv\Scripts\python.exe .\garmin_sync.py --days 30 --download-originals
```

For a lightweight daily refresh, use `--days 3`. Use `--activities-only` or
`--sleep-only` when only one side needs refreshing.

To include naps in total daily sleep and extract training status, race
predictions, daily recovery, richer activity fields, splits, HR/power zones,
metric time series without GPS, and available Garmin body measurements:

```powershell
.\.venv\Scripts\python.exe .\garmin_sync.py --days 30 --extended
```

Extended mode adds `sleep.csv`, `daily_recovery.json/csv`, summarized
`body_battery_events.json` (including recognized nap events),
`training_snapshot.json`, `body_measurements.json`, and one GPS-stripped JSON
file per activity under `activity_details`. Sleep rows retain main sleep,
nap duration, and `total_sleep_including_naps_hours` as separate fields so
missing main-sleep records are not mistaken for unusually short nights.

## Upload raw Garmin data to Fitnessd

Fitnessd is the new canonical SQLite-backed service at
`https://api.aries.timdumol.com`. The existing `garmin_sync.py` uploader sends
the cached raw Garmin responses to its authenticated ingest endpoint; the
server assigns them to `tim` from the per-athlete ingest token. Curated JSON
and CSV exports continue to be written locally for human use.

On the Windows machine, install the locked dependencies and securely obtain
the `tim` ingest token configured for Fitnessd on Aries. Store it with the
DPAPI-protected helper:

```powershell
uv sync
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\configure_fitnessd.ps1
```

Run a normal refresh manually first:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\run_fitnessd_sync.ps1 -Days 3
```

This fetches Garmin, updates the local exports, and uploads raw records in
server-sized batches. To include the original FIT-containing ZIP archives,
add `-DownloadOriginals`. To retry an upload from already-cached raw files
without contacting Garmin, use `-ExistingOnly`.

Register the daily Windows task after the manual run succeeds:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install_fitnessd_sync_task.ps1
```

By default, this runs at 09:00 and 11:00 and syncs the preceding day. Override
the schedule with, for example, `-At "08:00","20:00" -Days 3`. The task must
run as the same Windows user that configured the Fitnessd and
Garmin tokens, because the stored Fitnessd token is protected with Windows
DPAPI. Its log is `.private\logs\fitnessd-sync.log`. Garmin can still require
an interactive MFA refresh occasionally; run the sync manually if the task
reports an authentication failure.

Manual strength and circuit logs use structured `fitness.workout_sessions` and
`fitness.workout_entries` tables. Ordered entries can represent exercise sets,
rests, or notes, and a session can link to its matching Garmin activity without
double-counting the manual placeholder in `fitness_agent.activities`. Read-only
analysis is available through `fitness_agent.workout_sessions` and
`fitness_agent.workout_entries`.
