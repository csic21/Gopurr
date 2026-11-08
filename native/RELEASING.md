# Native releases

## Android APK releases

`.github/workflows/android.yml` is shared by ordinary CI and the existing
Release workflow. CI builds development APKs for `arm64-v8a` and `x86_64`
without production secrets. Pushing a `vX.Y.Z` tag (including prereleases),
or dispatching Release with a tag, builds both signed release APKs:

- `gopurr-X.Y.Z-android-arm64-v8a.apk` for modern ARM phones/tablets.
- `gopurr-X.Y.Z-android-x86_64.apk` for x86_64 devices/emulators.

The tag must contain the Android sources and match `mygo.json`. Android APK
releases start at v0.6.22; v0.6.20 and earlier contain desktop packages only.
The APKs target Android 9/API 28 and later. Release waits for Android, macOS
and Windows to succeed, requires both APKs, and includes them in `SHA256SUMS`
before publishing. Already-public releases retain their assets.

### Signing configuration

Set these repository Actions secrets before the first release:

| Secret | Value |
| --- | --- |
| `ANDROID_KEYSTORE_BASE64` | Base64 of the Android release keystore, on one line |
| `ANDROID_KEY_ALIAS` | Private-key alias in that keystore |
| `ANDROID_STORE_PASSWORD` | Keystore password |
| `ANDROID_KEY_PASSWORD` | Private-key password (can equal store password) |

The repository's public signing certificate is
[`android/release-certificate.pem`](android/release-certificate.pem), SHA-256
`a6e1761ada485214bae924e252c3723bf1a1a4246611c02e5060eaa1010584e1`.
Release builds require that certificate, preventing an accidental secret
replacement from shipping APKs with a different signing identity.

Use a dedicated, backed-up Android signing key. Keep the same certificate for
all subsequent APKs so installed apps can upgrade. The desktop Ed25519 key
`MYGO_UPDATER_PRIVATE_KEY` is unrelated and must not be replaced or reused.
Release fails if signing configuration is absent; it never creates a temporary
production key or uses the debug keystore. PR/CI jobs receive no release key.
The release job decodes the keystore into a mode-0600 runner temporary file,
passes passwords through environment variables, and removes the key after
building (also on failure). Only the final APK is uploaded.

The generated release manifest has `android:debuggable="false"`; the source
manifest and development package remain debuggable for instrumentation.
The build checks the signed APK's application ID, version, debuggable flag,
native libraries, pinned signing certificate, 16 KB ZIP alignment and ELF load
alignment. Both ABIs have the same application ID and certificate. The package remains
`io.github.gopurr.nativeapp`; uninstall an existing debug-signed build before
the first release install (uninstalling deletes its data). Subsequent official
APKs install over earlier official APKs without changing signing identity.

`versionName` comes from `mygo.json`. CI release `versionCode` is
`1000000 + github.run_number` from the parent **Release** workflow, so stable
and prerelease runs share an increasing sequence and reruns keep their code.
Keep that workflow's run-number sequence when moving/renaming workflows;
if it resets, raise the baseline above the highest previously shipped code.
This pipeline distributes APKs on GitHub; it does not publish to Google Play.

For a local build, export `GOPURR_ANDROID_KEYSTORE` (keystore file path),
`GOPURR_ANDROID_KEY_ALIAS`, `GOPURR_ANDROID_STORE_PASSWORD`,
`GOPURR_ANDROID_KEY_PASSWORD` and `GOPURR_ANDROID_VERSION_CODE` (a positive
integer greater than previous shipped codes, at most 2100000000), then run:

```sh
make native-android-release NATIVE_ANDROID_VERSION=vX.Y.Z
make native-android-release NATIVE_ANDROID_VERSION=vX.Y.Z NATIVE_ANDROID_ABI=x86_64
```

Outputs live in `dist/android-<ABI>/`. The pinned NSH compiler requires macOS
Apple Silicon; the workflow selects JDK 17, SDK 36, Build Tools 36.1.0 and
NDK 29.0.14206865. Native Android long-running VPN and hardware/network
handover acceptance still require real-device testing; these build checks
do not replace that work.

## Native desktop updates

The macOS production entry point is now `scripts/release` / `make bundle`.
It builds `bin/darwin-arm64/Gopurr.app` with the established
`io.github.gopurr.app` identity, `Contents/MacOS/Gopurr` executable and
`Application Support/Gopurr` data directory. `make native-bundle` remains an
isolated preview. The Catalyst deployment target is iOSMac 15 / macOS 12;
production validation checks the plist floor so the preview's earlier macOS
14 plist cannot silently replace the legacy minimum.

`GOPURR_NATIVE_DISTRIBUTION=1` selects the production identity; the release
driver also supplies the native update feed. Packaging requires `-production`,
which checks the original identity and data location, then emits both
`update-darwin-arm64.json` (existing clients) and
`update-native-darwin-arm64.json` (NSH clients). These files are identical and
point at `gopurr-VERSION-darwin-arm64.tar.gz`, containing the whole app. The
DMG retains `gopurr-VERSION-arm64.dmg`. Publishing requires both manifests.
Use a new tag above the latest public version; do not replace a public tag's
assets to perform the migration.

Before the desktop model loads existing data, it preserves a private atomic
`migration-nsh-v1/` snapshot. Paths and schemas remain unchanged. The legacy
single-instance lock protects the canonical home from the old GUI; the home
lock protects native backends, including explicit test homes. Damaged JSON
or linked snapshot inputs fail startup without changing the original files.
Previews do not automatically merge into the production home.

The release workflow requires `TestNativeDesktopMigrationRoundtrip` before
publication. An isolated identity, generated keys and loopback HTTP exercise
the old updater, the actual Swift/NSH host, rejected signatures and a second
whole-bundle update. `verify-update` separately installs the actual release
archive using the existing production public key. Neither verification
modifies an installed user application or system proxy. Evidence and
distribution boundaries are recorded in [MACOS_MIGRATION.md](MACOS_MIGRATION.md).

The NSH macOS app can retain the Ed25519 update trust in `mygo.json` and the
existing `MYGO_UPDATER_PRIVATE_KEY` GitHub Actions secret. This is independent
of Apple code signing and NSH's optional `.nshpkg` signing. Do not generate a
replacement update key: an installed build trusts its existing public key.

The preview packaging commands below do not publish a release or alter an
installed application. Isolated development builds keep updates disabled;
the production release driver enables the native feed explicitly.

## Build a release helper

The Swift NSH executable and the Go desktop helper ship in one app bundle.
The helper must be built with its real version, the existing public key and
the **native** feed. Obtain the exact flags without reading any private key:

```sh
go run ./scripts/package-native-update -print-link-flags
```

The repository's builder enables those settings only when an explicit native
feed is supplied. It also writes the required bundle build marker:

```sh
GOPURR_NATIVE_UPDATE_FEED=https://github.com/csic21/Gopurr/releases/latest/download/update-native-darwin-arm64.json \
  bash scripts/build-native-apple.sh catalyst
```

Omitting `GOPURR_NATIVE_UPDATE_FEED` builds the isolated development app with
updates disabled. This command builds locally and never reads the signing key.

For the preview identity the flags set (production uses `io.github.gopurr.app`):

- `github.com/egoist/mygo.production=1`
- `github.com/egoist/mygo.packageUpdateFeed`
- `github.com/egoist/mygo.packageUpdateKey`
- `github.com/egoist/mygo.packageName`
- `github.com/egoist/mygo.packageVersion`
- `github.com/egoist/mygo.packageIdentifier=io.github.gopurr.native`
- `github.com/egoist/mygo.packageURLSchemes=gopurr,clash`
- `gopurr/internal/buildinfo.Version`

Pass this output as **one** argument to `go build -ldflags "$native_update_flags"`
when explicitly building the Catalyst release helper. Do not set these update
flags in the default development build. Neither the helper nor the Swift app
ever receives the private signing key.

For this repository's current `tagPrefix: "v"`, the feed is:

```text
https://github.com/csic21/Gopurr/releases/latest/download/update-native-darwin-arm64.json
```

A separate tagged channel can use, for example:

```text
https://github.com/csic21/Gopurr/releases/download/native-v{version}/update-native-darwin-arm64.json
```

That is mygo's literal `{version}` placeholder, resolved through the GitHub
releases API. An alternate channel needs a deliberately maintained config with
`updates.tagPrefix: "native-v"`; pass it with `-config`. With `updates.url`, the
feed is `<updates.url>/update-native-darwin-arm64.json`. Keep native and legacy
manifests separate so a native app cannot download the old desktop UI.

## Validate and package

After building and signing the entire `.app`, validate without reading a key
or producing any artifacts:

```sh
go run ./scripts/package-native-update -app 'bin/native/Gopurr Native.app'
```

The tool checks the bundle identity and version, an arm64 Mac Catalyst host,
the Go helper's embedded production/native feed/public key/version/identity, desktop NSH
capabilities and JS/bytecode resources, licenses, safe symlinks and the whole
bundle's `codesign --verify --deep --strict` result. It also requires
`Contents/Resources/native-build.json` with schema 1, `ui: "nsh-desktop"`,
`backend: "desktop-v2"`, matching `version`, `updateFeed`, `updatePublicKey`,
and `updateEnabled: true`. The ordinary development build records disabled
updates and is refused. It rejects the legacy
AppKit host, mobile UI and development helper without update trust settings.
The input must remain unchanged while packaging is running.

In an explicitly authorized release job, provide the existing GitHub secret
only as the named environment variable, then request signing explicitly:

```sh
go run ./scripts/package-native-update \
  -app 'bin/native/Gopurr Native.app' \
  -out dist/native-darwin-arm64 \
  -sign -key-env MYGO_UPDATER_PRIVATE_KEY
```

Do not put the secret value in command arguments or logs. The tool does not
read `.secrets`, a config `privateKey` path, or any fallback key location. A
64-byte base64 Ed25519 private key must match `updates.publicKey`; a mismatch
fails before output is created. Tests generate temporary key pairs only.

Optional `-notes path/to/release-notes.md` supplies manifest release notes.
The version comes solely from the selected `mygo.json`; that version must
already match the signed bundle and its helper. Existing artifacts are never
overwritten. Output must be outside the input bundle.

For version `0.6.20`, output is:

```text
dist/native-darwin-arm64/
  gopurr-native-0.6.20-darwin-arm64.tar.gz
  update-native-darwin-arm64.json
```

The archive root contains exactly one `.app`, preserving executable modes,
code signatures and internal relative symlinks:

```text
Gopurr Native.app/
  Contents/
    Info.plist
    MacOS/Gopurr Native
    Resources/gopurr-desktop
    Resources/nsh/app.json
    Resources/nsh/capabilities.json
    Resources/nsh/bundle.js
    Resources/nsh/bundle.nshc
    Resources/LICENSE
    Resources/THIRD_PARTY_NOTICES.md
    ... all remaining original bundle resources and signatures ...
```

This is the layout mygo's macOS updater expects. Its install target is the
app bundle three parent directories above the helper in `Contents/Resources`.
It downloads and stages the complete replacement bundle and swaps it after
the helper exits. The Swift host must quit before that exit, and the existing
relaunch integration must start the new NSH host rather than the helper.

The JSON manifest uses mygo's `version`, `notes`, RFC 3339 `date`, HTTPS `url`,
byte `size`, and base64 `signature`. The signature is Ed25519 over the SHA-256
digest of the **compressed archive bytes**. No deltas are emitted. Archives
must fit mygo's 1 GiB limit. The default GitHub archive URL uses
`releases/download/v<version>/<archive-name>`; only uploading both artifacts
to the intended release makes the feed usable. This tool never uploads them.

Apple distribution signing/notarization and a disposable install-and-relaunch
test are separate release checks; an ad hoc signature passing local validation
does not establish Developer ID or notarization. Keep the old production feed
unchanged until a deliberate migration release is ready.

## Tests

Use the workspace Go cache settings from `Makefile`:

```sh
GOPATH="$PWD/.cache/go" GOMODCACHE="$PWD/.cache/mod" \
GOCACHE="$PWD/.cache/build" GOTOOLCHAIN=go1.27.3 \
GOFLAGS=-tags=with_gvisor go test ./scripts/package-native-update
```

Tests authenticate complete temporary archives, reject modified archives,
wrong keys/feeds/versions, legacy/mobile bundles, missing helpers and escaping
links, preserve input bundles and existing output, and assert that validation
and printing linker flags do not read a key. They never modify an installed
application or access production signing material.

To exercise a real native host and a complete archive without using the real
key, opt in with the path of an existing NSH build:

```sh
GOPURR_NATIVE_PACKAGE_TEST_APP="$PWD/bin/native/Gopurr Native.app" \
  go test -v ./scripts/package-native-update -run '^TestRealNativeBundlePackaging$'
```

Use the same workspace Go cache environment above. This test copies the app
into a temporary directory, generates a temporary key pair, rebuilds only the
copy's helper with that temporary public key, updates only the copy's build
marker, signs the copy ad hoc, and produces/authenticates the complete archive.
It compares the original app's file contents, modes and links before and after.
The temporary app and artifacts are removed when the test finishes.
