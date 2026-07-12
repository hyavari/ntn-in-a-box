# ntnbox Android companion

Client library for NTN-in-a-Box coverage, condition, lookahead, link-state, and messaging signals.

- SSE: `GET /events` — coverage, `linkstate`, `message`
- Poll: `GET /devices/{id}/condition` (countdown + link metrics)
- Poll: `GET /devices/{id}/lookahead` (absolute open/close, duration, elev)
- Messaging: `sendMessage` / `fetchInbox` (store-and-forward)
- Callbacks + optional Kotlin Flow wrappers
- `minSdk 26`

## Defaults

| Setting | Default |
|---------|---------|
| baseUrl | `http://127.0.0.1:8080` (use `adb reverse tcp:8080 tcp:8080`) |
| deviceId | `sandbox-0` |

Cleartext HTTP to localhost requires `android:usesCleartextTraffic="true"` (or a
network security config) on the app — the sample enables this.

## Depend from the sample (composite build)

`samples/android-connectivity` already uses:

```kotlin
// settings.gradle.kts
includeBuild("../../android") {
    dependencySubstitution {
        substitute(module("com.ntnbox:ntnbox")).using(project(":ntnbox"))
    }
}

// app/build.gradle.kts
implementation("com.ntnbox:ntnbox")
```

Open the sample (or `android/`) in Android Studio to sync. CLI: use `./gradlew`
if present; otherwise `gradle wrapper --gradle-version 8.9` once.

## Usage

```kotlin
val client = NtnBoxClient() // defaults after adb reverse
val mainExecutor = ContextCompat.getMainExecutor(context)

client.addListener(mainExecutor, object : NtnBoxListener {
    override fun onCoverageChanged(inCoverage: Boolean, kind: CoverageKind) { /* … */ }
    override fun onCondition(condition: NtnCondition) { /* countdown */ }
    override fun onLookahead(lookahead: NtnLookahead) { /* open/close / duration */ }
    override fun onLinkState(linkState: NtnLinkState) { /* throttled metrics */ }
    override fun onMessage(message: NtnMessage) { /* delivered / status */ }
    override fun onConnectionChanged(connected: Boolean) { /* SSE up/down */ }
})
client.start()
// …
client.sendMessage("cloud", "SOS") // call off the main thread
client.fetchInbox("cloud")
// …
client.stop()
```

Flow helpers (`coverageFlow()`, `conditionFlow()`, `lookaheadFlow()`,
`linkStateFlow()`, `connectionFlow()`) do **not** call `start()` — start the
client first.

End-to-end emulator walkthrough: [TUTORIAL.md Step 11](../../TUTORIAL.md#step-11-test-with-an-android-emulator).
Sample app: `samples/android-connectivity/`.
