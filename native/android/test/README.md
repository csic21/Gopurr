# Android native VPN smoke test

This separate development instrumentation APK exercises the actual target
application's JNI library, profile store, foreground `VpnService`, Android TUN
and mihomo packet forwarding. It is never included in the main APK.

Build the development Android application first, and start an isolated emulator
with the matching ABI. The test does not start or shut down emulators:

```sh
bash scripts/test-native-android.sh emulator-5580
```

The Android-independent session regressions also run on the host JVM:

```sh
bash scripts/test-native-android-host.sh
```

They cover late permission results, replacing a permission presenter, repeated
connect commands, superseded disconnects, timeouts, and TUN ownership across
task generations and replacement service instances.

Set `GOPURR_TEST_BUILD_ONLY=1` to compile and sign the separate test APK without
connecting to any emulator. For an x86_64 emulator, also set
`GOPURR_TEST_ANDROID_ABI=x86_64` after building that main APK.

Both the shell entrypoint and runner refuse non-`emulator-*` serials. The runner
also requires `ro.kernel.qemu=1` before installing anything or changing appops.
It installs only `io.github.gopurr.nativeapp` and its `.test` APK, clears that
development app's emulator data, grants VPN consent through emulator appops,
and imports a fixed `Native smoke` DIRECT/REJECT configuration.

The application excludes its own UID from the VPN, and Android's privileged
shell can also bypass it. A separate `ProbeActivity` in the test package sends
HTTP under its own ordinary application UID, verified by the instrumentation.
Requests go
to `10.0.2.2`, the emulator's host-loopback mapping, where the runner holds a
temporary Python HTTP fixture. It uses no public server or real subscription.
DIRECT must return the exact fixture marker, REJECT must never reach the
fixture, and forwarding must recover after node switching and VPN reconnect.
It also checks TUN state, real traffic counters and captured TUN startup logs.
Before the first connection and after stopping, JNI node queries must return a
saved-profile preview without starting the core. The disconnected node page is
also captured as `android-proxies-offline-live.png` for native visual review.
An overlapping disconnect/connect regression checks that a stale disconnect
cannot remove the new connection's foreground-service protection.
The instrumentation also verifies that a pending subscription download does
not block session/engine status or priority disconnect; cancelled VPN consent
cannot reconnect later; empty include scopes and including Gopurr itself are
rejected; application scope cannot change while connected; include scope
captures the independent probe UID, while exclude scope bypasses the VPN even
when its current proxy group is REJECT. Both Builder and the Go packet engine
use the same token-bound `engine.tunnelPlan`.
Selecting REJECT, disconnecting, and reconnecting must restore that selection
and continue blocking the independent UID's packets until DIRECT is selected.
The runner then force-stops the development UID without clearing its files and
starts a second instrumentation process, asserting distinct process IDs. Saved
direct mode and the profile's REJECT selection must survive; the new real VPN
must forward in direct mode, reject in rule mode, then recover after selecting
DIRECT. The host fixture independently checks these request paths.

Rapid same-service handover checks actual DIRECT/REJECT routing and foreground
service protection. It does not require a new Network ID: Android can preserve
its NetworkAgent during an in-place handover ([AOSP Vpn.establish](https://android.googlesource.com/platform/frameworks/base/+/master/services/core/java/com/android/server/connectivity/Vpn.java)).
The separate full service-stop/reconnect case still waits for a new network.

Results require both instrumentation success and the host's observed request
paths. The runner stops the development app and its VPN on exit. The fixture
profile remains in this isolated emulator for manual UI inspection.
The last instrumentation output and host-observed request paths are saved to
`bin/native/android/test/last-result.json`, including failures and TUN logs.
Successful live/disconnected overview, proxy nodes, profiles, settings, and application-scope
captures are copied from the development app's private cache to
`native/ui/screenshots/android-*.png` using `run-as`; no production debugging
endpoint is needed. The scope capture changes only an unsaved UI draft.

The VPN run also checks the lightweight `engine.traffic` counters, live versus
connection-only notification preferences, the separate transparent status icon,
and opening Connect from a notification after an existing Settings window goes
to the background. It verifies the inline native disconnect button, the absence
of a separate action row, and that the inline action really stops VPN.
Native light/dark captures are saved as `android-notification-live.png` and
`android-notification-dark-live.png`.
NSH virtual accessibility nodes are inspected by walking the tree, since the
framework's `findAccessibilityNodeInfosByText` does not find those nodes.

An APK build for an ABI is not a device test for that ABI. On an Apple Silicon
host with only ARM emulator images, the x86_64 APK can be compiled and signature
checked, while real VPN forwarding is verified on the ARM emulator only.
