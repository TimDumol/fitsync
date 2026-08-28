# Garmin sleep probe

This is a small, read-only trial of the unofficial `python-garminconnect`
client. It fetches Garmin sleep, HRV, and resting-heart-rate data and prints a
compact JSON summary suitable for later loading into a sheet or database.

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
