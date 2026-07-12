# Android Connectivity Sample

Android app for NTN-in-a-Box emulator testing.

- Polls a URL every 2s with retry + offline queue
- Uses the [`android/ntnbox`](../../android/ntnbox/) companion for coverage SSE
  + condition countdown (`sandbox-0` @ `http://127.0.0.1:8080`)
- On `window_opened`, drains the offline queue
- `minSdk 26`

## Build / install

Requires JDK 17+ and Android SDK. **Easiest:** open this folder in Android Studio
and Run (Studio generates/syncs the Gradle wrapper). Or:

```bash
cd samples/android-connectivity
gradle wrapper --gradle-version 8.9   # once, if ./gradlew is missing
./gradlew :app:assembleDebug
./gradlew :app:installDebug
```

The sample pulls the companion via Gradle `includeBuild("../../android")`.

## Run under ntnbox (Linux / WSL2)

```bash
./scripts/demo-android.sh sos_burst

sudo ./ntnbox run --addr :8080 \
  --profile testdata/profiles/sos_burst.yaml \
  -- emulator @MyAVD

adb reverse tcp:8080 tcp:8080
```

Then launch the app. Watch HTTP online/offline and the ntnbox countdown line.

## Custom URL

Default target is `https://example.com`. Override with an intent extra:

```bash
adb shell am start -n com.ntnbox.samples.connectivity/.MainActivity \
  --es target_url https://example.org
```

## Docs

- [TUTORIAL.md Step 11](../../TUTORIAL.md#step-11-test-with-an-android-emulator)
- [android/ntnbox/README.md](../../android/ntnbox/README.md)
