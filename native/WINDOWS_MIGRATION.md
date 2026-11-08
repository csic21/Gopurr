# Windows NSH migration acceptance

Status: the actual NSH Windows frontend and Go DLL compile, and Windows C ABI tests pass (run 42476721715). Installer/runtime/update acceptance remains in progress on `codex/windows-nsh`; **not accepted or selected as the production default**. A macOS test result cannot establish Windows runtime compatibility.

The Win32 frontend (`native/windows`) embeds NSH 0.4.0 at the exact upstream commit `e626a8dd8f8d8b277b9507cfd5ac74d0e67020f3`. It runs the shared `native/ui/Desktop.tsx` eight-page desktop and calls the existing Go desktop backend through `GopurrBridge.dll`. The host, bridge and backend ship together in the same installation directory so that the existing updater replaces the entire payload.

## Preserved contracts

- Executable: `Gopurr.exe`; install directory: `%LOCALAPPDATA%\Programs\Gopurr`; application identifier: `io.github.gopurr.app`.
- Data: `%APPDATA%\Gopurr`; subscription paths, cache paths, profiles, overrides, preferences, remote access and mesh identity stay in place. The first NSH start creates the private, atomic `migration-nsh-v1` snapshot before reading the model. Invalid JSON or linked data stops migration rather than overwriting old data.
- Existing Ed25519 public key from `mygo.json`; both `update-windows-amd64.json` and `update-native-windows-amd64.json` refer to the same complete archive with identical signatures. The production key must be supplied through the explicitly named environment variable; no fallback key file or new key is accepted.
- Existing service and system proxy backend; administrative operations continue through the Go helper, including its protected service executable. No NSH process installs itself as the service binary.
- Tray background operation, first-close confirmation, silent launch, global shortcuts, protocol import and light-mode release remain host/backend responsibilities. Unsaved drafts prevent releasing the view.
- Restart launches the new `Gopurr.exe`, waits for the previous Go helper to exit, then loads the new DLL and takes the data lock. It never starts the backend as the desktop frontend.

The NSH Windows runtime requires Windows 10 1809 or later (including its Unix-socket support). A changed minimum Windows version must be visible in release notes before any production switch.

## Build and validate on Windows

```powershell
./scripts/build-native-windows.ps1
./scripts/package-native-windows.ps1
# Explicit production signing of the validated payload:
./scripts/package-native-windows.ps1 -Release
# Opt-in release builder; the ordinary release still selects mygo on Windows:
go run ./scripts/release -native-windows -platform windows/amd64 -release
```

The `Windows NSH acceptance` workflow also builds and installs the per-user installer on a disposable Windows runner, renders shared Windows fixtures and the actual custom host, runs the real core/service smoke checks, checks a production-signed old-client upgrade, and tests `mygo 0.0.0 → NSH 0.0.1 → NSH 0.0.2` with temporary keys, including invalid-signature refusal. These checks are prepared, not yet executed.

The build checks the pinned official SDK archive SHA-256 and uses Rust 1.99.1 with a checked-in Cargo lock. The locked `oxc_sourcemap` dependency requires Rust 1.95 or newer. Full shared business/UI Go regression runs in the regular CI workflow; the dedicated Windows acceptance job avoids duplicating that screenshot suite. Go and Rust caches live inside `.cache`. No command above publishes a GitHub release or changes a production feed.

## Required before switching production

- [ ] Real Windows x64 host compiles and starts, including bridge DLL dependencies on a clean Windows installation.
- [x] Private session ACL rejection/authentication and durable old-data snapshot tests pass on Windows (run 42473625744).
- [ ] All eight pages, collapsed navigation, editors and dialogs are inspected at 1060×720 and 840×560 in both themes, including the actual custom host window.
- [ ] First close, tray wake, silent start, light release/reopen and dirty-draft preservation pass.
- [ ] Real core traffic, Wintun/service management and proxy recovery pass on the ephemeral Windows runner.
- [ ] A disposable old mygo installation checks, downloads, verifies and installs the NSH production-signed archive while retaining seeded old data.
- [ ] An NSH installation updates again, exits both frontend and helper, restarts the new frontend and loads the retained data.
- [ ] Tampered archive, wrong key and incomplete native payload are refused without changing the old installation.
- [ ] Per-user installer, protocol handlers, autostart, registry identity and uninstall data retention pass.
- [ ] Only after all preceding evidence is recorded, change the default Windows release builder and require these checks in the official Release workflow.

Remote validation needs the development branch pushed to `csic21/Gopurr`. The user authorized the push and [draft PR #1](https://github.com/csic21/Gopurr/pull/1) runs the acceptance workflow. The first Windows run passed private session ACL/authentication and migration snapshots; remaining runtime/build checks are pending. Local verification alone does not clear the remaining Windows acceptance checkboxes above.
