# Windows NSH migration acceptance

Status: the actual Windows NSH lifecycle and update chain passed [run 42485072850](https://github.com/csic21/Gopurr/actions/runs/42485072850). Native screenshots were visually reviewed at both sizes and themes. **Production remains on mygo** while the additional in-app delivery of the graceful NSH uninstaller and the final complete CI run are being verified.

The Win32 frontend (`native/windows`) embeds NSH 0.4.0 at upstream commit `e626a8dd8f8d8b277b9507cfd5ac74d0e67020f3`. It runs the shared `native/ui/Desktop.tsx` eight-page desktop and calls the existing Go desktop backend through `GopurrBridge.dll`. The host, bridge, backend, UI assets and graceful NSH uninstaller ship in the same installation directory so the existing updater replaces the complete payload. NSIS emits the replacement uninstaller without installing files or writing registry entries on the builder.

## Preserved contracts

- Executable `Gopurr.exe`, default installation `%LOCALAPPDATA%\Programs\Gopurr`, application identifier `io.github.gopurr.app`, existing shortcuts and protocol schemes.
- Data `%APPDATA%\Gopurr`. Profiles, cached subscriptions, credentials, overrides, preferences, remote access and mesh identity remain in place. Before opening the model, the first NSH start creates a private atomic `migration-nsh-v1` snapshot. Invalid JSON or linked data stops migration without overwriting the original files. Directory identity checks preserve singleton locking for case aliases.
- The existing Ed25519 public key in `mygo.json`. `update-windows-amd64.json` and `update-native-windows-amd64.json` contain identical archive URLs, sizes and signatures. Production signing requires the explicitly named environment variable; no fallback key file or key rotation is accepted.
- Existing core, service, Wintun, proxy recovery, tray and shortcut implementations. Privileged operations use the Go helper and its protected service executable.
- First-close confirmation, tray activation, silent launch and light-mode view release. Unsaved drafts prevent releasing the view. RPC replies are complete JSON values; event polling retries with sequenced IDs and a bounded transport timeout, without waiting for connection teardown.
- Update restart starts the new `Gopurr.exe`, waits for the old helper to exit, then loads the new DLL and takes the data lock. It never relaunches the helper as the frontend.
- The complete update includes `Uninstall.exe`, replacing the legacy force-stop uninstaller. The NSH uninstaller requests graceful network/core shutdown and preserves user data, migration snapshots and the managed service.

The NSH Windows runtime requires Windows 10 1809 or later. This minimum must appear in release notes before production selection. Validation runs on disposable Windows x64 GitHub runners, independently of macOS.

## Build and validate on Windows

```powershell
./scripts/build-native-windows.ps1
./scripts/package-native-windows.ps1
# Explicit production signing of the complete validated payload:
./scripts/package-native-windows.ps1 -Release
# Acceptance remains opt-in until the production checklist is complete:
go run ./scripts/release -native-windows -platform windows/amd64 -release
```

The build verifies the pinned NSH SDK archive SHA-256, uses Rust 1.99.1 and the checked-in Cargo lock, and bundles Rust dependency notices. Go and Rust caches remain inside `.cache`. Runtime fixtures restrict PATH to the Windows system directory so developer toolchain DLLs cannot satisfy a missing runtime dependency.

## Acceptance evidence

[Run 42485072850](https://github.com/csic21/Gopurr/actions/runs/42485072850) passed the real Win32 host, bridge DLL, backend and core. Its production-key check installs the signed archive through the old mygo updater. A separate disposable-key integration fixture drives `mygo 0.0.0 → NSH 0.0.1 → NSH 0.0.2` through real checks, downloads, verification, replacement and process restart. It retains seeded credentials/profiles/preferences, verifies real HTTP through the migrated mixed proxy after both upgrades, refuses an invalid signature without changing the frontend, and tests login registration, silent launch and activation.

Native window navigation is verified through Windows UI Automation before every screenshot. The 32 actual-host images, Windows SDK page renders, collapsed navigation, editors and dialogs were reviewed at 1060×720 and 840×560, light and dark. The minimum overview keeps system proxy, TUN, DNS and sniffing controls visible. Screenshots alone do not replace the real core/update checks.

- [x] Windows x64 host build, C ABI, clean runtime PATH and actual startup.
- [x] Private ACL rejection, authenticated IPC, canonical-home locking and durable migration snapshots.
- [x] Eight pages, collapsed navigation, long text, selected/disabled controls, editors and dialogs visually reviewed in both sizes/themes.
- [x] First close, backend continuity, background activation, light-mode release/reopen, login entry and silent launch. Shared interaction tests also preserve hidden-page drafts and dirty transitions.
- [x] Real core traffic, Wintun/service operations and proxy recovery.
- [x] Production-key old-client signed update verification and seeded-data migration into the actual NSH frontend.
- [x] NSH-to-NSH update exits both processes, starts the new frontend and retains the old data.
- [x] Invalid signature leaves the installed frontend unchanged; wrong/malformed signing keys are rejected. The packager requires the full native payload before signing.
- [x] Per-user installer, protocol commands, registry identity, graceful fresh-install uninstall and data retention.
- [ ] Replacement uninstaller delivered by an in-app update gracefully uninstalls the running upgraded app and retains old data.
- [ ] Final complete macOS/Windows/Android CI, including NSH type checking.
- [ ] Only after all preceding evidence, select NSH as the Windows release default and gate the official Release workflow on this acceptance suite.

The user authorized source push and [draft PR #1](https://github.com/csic21/Gopurr/pull/1). No acceptance command publishes a release or changes the production feed.
