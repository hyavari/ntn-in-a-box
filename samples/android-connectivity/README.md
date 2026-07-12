# Android Connectivity Sample

Minimal Android app for NTN-in-a-Box Mobile DX (Phase A).

- Polls a URL every 2s
- On failure: enqueues a message and applies exponential backoff
- On success: drains the queue
- UI status: **online** / **offline / retrying** (connectivity-only — no companion library)

`minSdk 26`. No Google Play services.

## Build / install

Requires JDK 17+ and Android SDK. Easiest: open this folder in Android Studio
and Run. Or:

```bash
cd samples/android-connectivity
# If gradlew is missing a wrapper jar, generate it once:
#   gradle wrapper --gradle-version 8.9
./gradlew :app:assembleDebug
./gradlew :app:installDebug
```

## Run under ntnbox (Linux / WSL2)

```bash
# From repo root — prints the full cheat-sheet
./scripts/demo-android.sh sos_burst

# Wrap emulator (replace @MyAVD)
sudo ./ntnbox run --addr :8080 \
  --profile testdata/profiles/sos_burst.yaml \
  -- emulator @MyAVD
```

Then install/launch the app. During coverage gaps the queue grows; when the
pass returns it flushes.

Optional — reach the host API from the emulator:

```bash
adb reverse tcp:8080 tcp:8080
# GET http://127.0.0.1:8080/devices/sandbox-0/condition
```

## Custom URL

Default target is `https://example.com`. Override via intent extra
`target_url` or (if set in the process environment) `TARGET_URL`.

## Docs

Full walkthrough: [TUTORIAL.md Step 11](../../TUTORIAL.md#step-11-test-with-an-android-emulator).
