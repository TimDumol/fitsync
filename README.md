# FitSync

FitSync is a local-first strength workout logger for Android and the web. The
same React application can be installed as a Progressive Web App or packaged as
an Android APK with Capacitor.

This initial version deliberately has **no Neon or other cloud connection**.
Routines, active workouts, and workout history are stored in IndexedDB on the
device. Active workouts are saved after every edit so an accidental app close
does not discard the session.

## Included in the first version

- Editable strength routines
- Weight, reps, and RPE/RIR entry for each set
- Warm-up and working sets
- Automatic rest timer
- Recoverable in-progress workouts
- Completed workout history and volume summary
- Installable offline PWA
- Capacitor Android project
- GitHub Actions build that publishes a debug APK artifact

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

## Download the APK built by GitHub

Every push and pull request runs the **Android debug APK** workflow.

1. Open the repository's **Actions** tab.
2. Open the latest successful **Android debug APK** run.
3. Download the `fitsync-debug-apk` artifact.
4. Extract `app-debug.apk` and copy it to the Android phone.
5. Open the APK on the phone and allow installation from that source when
   Android prompts.

Debug APKs are intended for personal testing. A Play Store release requires a
private signing key and a signed Android App Bundle (`.aab`).

## Data and privacy

All data currently remains inside the browser or Android app's local storage.
Clearing browser data or uninstalling the Android app removes it. An export and
backup feature should be added before treating the app as the only copy of
long-term workout history.

The eventual sync boundary is intentionally isolated in `src/storage.ts`, so a
future Neon synchronization queue can be added without rewriting the workout
screens.
