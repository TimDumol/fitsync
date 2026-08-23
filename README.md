# FitSync

FitSync is a local-first strength workout logger for Android and the web. The
same React application can be installed as a Progressive Web App or packaged as
an Android APK with Capacitor. Routines, active workouts, history, and settings
remain available offline and can sync between signed-in devices through Neon.

## Included in the first version

- Editable strength routines
- Weight, reps, and RPE/RIR entry for each set
- Warm-up and working sets
- Automatic rest timer
- Recoverable in-progress workouts
- Completed workout history and volume summary
- Neon Auth email/password accounts
- Neon Data API sync protected by per-user Row-Level Security
- Last-write-wins conflict resolution with queued offline changes
- Installable offline PWA
- Capacitor Android project
- GitHub Actions builds with downloadable APK releases

## Run in a browser

```powershell
npm install
npm run dev
```

Open the local URL printed by Vite. To exercise the offline service worker,
build and preview the production version:

```powershell
npm run build
npm run preview
```

Without Neon environment variables, the app intentionally runs in local-only
mode.

## Configure Neon Auth and sync

Use a Neon branch with both **Neon Auth** (Better Auth) and the **Data API**
enabled. Configure the Data API to use `neon_auth`; do not enable anonymous
access for FitSync.

1. Apply [neon/001_fitsync.sql](neon/001_fitsync.sql) to the branch database
   using the owner connection string. Neon Auth/Data API must be enabled first
   because the migration uses the `authenticated` role and `auth.user_id()`.
2. Copy `.env.example` to `.env.local` and paste the Auth and Data API URLs from
   the Neon Console.
3. In Neon Auth's allowed origins, add the local development URL, the deployed
   PWA origin, and the Capacitor Android origin used by the built app (normally
   `https://localhost`).
4. Build again so Vite embeds the public endpoint URLs:

```powershell
Copy-Item .env.example .env.local
npm run build
```

The Android workflow defaults to this project's production Auth and Data API
URLs so GitHub-built APKs include sync. Repository or organization **Actions
variables** named `VITE_NEON_AUTH_URL` and `VITE_NEON_DATA_API_URL` can override
those defaults for another branch or environment.

The client contains no database password or Neon API key. Neon Auth supplies a
short-lived user token to the Data API, and PostgreSQL RLS restricts every row
to the matching `auth.user_id()`.

The sync record is a per-user JSON document for each routine, workout, active
draft, and settings object. Changes save to IndexedDB first. When online and
signed in, FitSync merges local and remote records by `updatedAt`, uploads the
winner, and keeps deletion tombstones so another device cannot resurrect a
finished draft.

## Build or open the Android app locally

Prerequisites:

- Node.js 22 or later
- JDK 21
- Android Studio with an Android SDK installed

Install dependencies and synchronize the web bundle into Android:

```powershell
npm ci
npm run android:sync
npm run android:open
```

Android Studio can then run the app on a connected phone or emulator. To create
a debug APK, choose **Build → Build APK(s)**, or run from PowerShell:

```powershell
Set-Location .\android
.\gradlew.bat assembleDebug
```

The APK is written to:

```text
android/app/build/outputs/apk/debug/app-debug.apk
```

## Download an APK from GitHub

Versioned APKs are attached directly to the repository's
[Releases](https://github.com/TimDumol/fitsync/releases) page.

1. Open the latest release.
2. Expand **Assets** if necessary.
3. Download the `fitsync-vX.Y.Z-debug.apk` file and copy it to the Android
   phone.
4. Open the APK on the phone and allow installation from that source when
   Android prompts.

Maintainers can publish a release by pushing a version tag:

```powershell
git tag v0.2.0
git push origin v0.2.0
```

Alternatively, run **Android APK release** from the Actions tab and enter a
version tag such as `v0.2.0`. The workflow creates the tag and release at the
selected commit. Regular pushes and pull requests still run **Android debug
APK** and retain the ordinary Actions artifact for build verification.

Debug APKs are intended for personal testing. A Play Store release requires a
private signing key and a signed Android App Bundle (`.aab`).

## Data and privacy

Signed-out data remains only in browser or Android app storage. Clearing browser
data or uninstalling the app removes that local copy. When signed in, the same
data is also stored in `public.fitsync_items` in Neon and is isolated by user ID.
Signing out does not erase either copy.
