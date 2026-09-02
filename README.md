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
./scripts/deploy.sh --ask-vault-pass
```

Use `./scripts/deploy.sh --check --diff` for a check-mode preview. The entrypoint
refuses to invoke Ansible unless `./scripts/test.sh` and `./scripts/build.sh`
both pass. It uploads only the files needed for the Fitnessd image; it never
uploads Git metadata, local databases, Garmin data, editor files, caches, or
plaintext secrets. The Neon import directory is created empty and read-only;
place an operator-reviewed snapshot there separately before Step 11.

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

The OAuth client's redirect URI must exactly match the callback URL shown by
ChatGPT. Run `./scripts/sync-pocket-id-identities.sh` before connecting
ChatGPT. Once the two subjects are bound, no pending auth request or approval
step is needed. The `profile` scope is optional for this pre-provisioned flow;
it is only useful if the defensive unknown-identity fallback is exercised.

For the mobile app, create a separate Pocket ID OIDC client. It is a public
client, so no client secret is placed in the APK:

```text
Name:             FitSync Mobile
Client ID:        fitsync-mobile (or set EXPO_PUBLIC_OAUTH_CLIENT_ID to the generated ID)
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

The Expo mobile app is under `apps/mobile/`. Build an installable Android
release APK with:

```bash
./scripts/build-mobile.sh
```

The script installs the locked npm dependencies, runs the mobile typecheck and
tests, generates the native Android project, and runs Gradle's
`assembleRelease`. The APK is written to
`apps/mobile/dist/fitsync-mobile.apk`; `dist/` is gitignored. Generated native
project files are removed after the build.

Release builds default to the production API and Pocket ID endpoints. Override
`EXPO_PUBLIC_API_BASE_URL`, `EXPO_PUBLIC_POCKET_ID_ISSUER`,
`EXPO_PUBLIC_OAUTH_CLIENT_ID`, or `EXPO_PUBLIC_OAUTH_RESOURCE` in the build
environment for another deployment. The OAuth resource defaults to
`<EXPO_PUBLIC_API_BASE_URL>/mcp`.

On this system, the script defaults to these toolchain locations when the
variables are unset:

```text
JAVA_HOME=/tmp/prestosupuesto-toolchain/jdk-21.0.12.1+1
ANDROID_HOME=/home/timdumol/Android/Sdk
```

Override them for another machine, for example:

```bash
JAVA_HOME=/path/to/jdk ANDROID_HOME=/path/to/android-sdk \
  ./scripts/build-mobile.sh
```

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

## Upload curated exports to Neon

The sync can use a Neon API key to discover a project and request a temporary
PostgreSQL connection URI. Use a **project-scoped** API key where possible.
The key and returned database password are never written to the repository.

Install the locked project dependencies:

```powershell
uv sync
```

Enter the API key without echoing it or placing it in PowerShell history:

```powershell
$secureKey = Read-Host "Neon project API key" -AsSecureString
$env:NEON_API_KEY = [System.Net.NetworkCredential]::new("", $secureKey).Password
```

If the API key can access exactly one project, project discovery is automatic.
For a project-scoped API key, set its project ID; the sync then bypasses
account-wide project discovery. Database and owner-role discovery use the
project's primary branch and prefer the default `neondb` database.

```powershell
$env:NEON_PROJECT_ID = "your-project-id"
.\.venv\Scripts\python.exe .\garmin_sync.py --neon-only
```

For the normal combined refresh:

```powershell
.\.venv\Scripts\python.exe .\garmin_sync.py --days 3 --extended --neon
```

Optional overrides are `NEON_PROJECT_NAME`, `NEON_DATABASE_NAME`, and
`NEON_ROLE_NAME`. `NEON_DATABASE_URL` may be used instead of the management API
key when a scoped PostgreSQL connection string is preferred.

Only the curated files under `data\garmin` are uploaded. Garmin login tokens,
raw activity files, and `.private` contents are excluded. Neon stores the
records in private `fitness.records` JSONB storage and creates analysis views
under `fitness_agent`, plus a no-login `fitness_agent_reader` group role.

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

### Windows one-time setup and scheduling

The helper scripts use Windows DPAPI to protect the API key for the current
Windows user on this computer:

```powershell
Set-Location E:\codex\fitness
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\configure_neon.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\run_fitness_sync.ps1 -Days 3
```

To retry only the upload of already-downloaded Garmin data, without fetching
Garmin again:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\run_fitness_sync.ps1 -NeonOnly
```

After the manual sync succeeds, register a daily task (change the time if
desired):

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install_fitness_sync_task.ps1 -At "06:30" -Days 3
```

The task runs as the current Windows user, starts later if the computer was
asleep, and appends output to `.private\logs\fitness-sync.log`. Garmin may
occasionally require an interactive login/MFA refresh; run the sync manually
when the log reports an authentication failure.
