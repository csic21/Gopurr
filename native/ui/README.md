# Gopurr NSH interface

`AndroidPreview.tsx` adds fixture Android notification settings for native visual review. Run `.cache/nsh/nsh/bin/nsh test native/ui/AndroidPreview.tsx --target macos < native/ui/android-notifications.steps.js` to refresh the light/dark notification settings captures at 1060×720, 840×560 and 390×844. The real `App.tsx` never installs preview transports.

The Android update panel has its own fixture path because the real host only
offers it in signed release builds. Refresh the available/downloading states
at phone and tablet widths with:

```sh
.cache/nsh/nsh/bin/nsh test native/ui/AndroidPreview.tsx --target macos < native/ui/android-update.steps.js
.cache/nsh/nsh/bin/nsh test native/ui/AndroidPreview.tsx --target macos < native/ui/android-update-progress.steps.js
```

The UI targets **NSH v0.4.0**. `Desktop.tsx` is the full eight-page desktop
entry; it uses `desktop.dispatch` and the existing desktop business model in a
private helper process. `App.tsx` is the four-page mobile entry. It calls the native host's `engine.dispatch` handler and never
falls back to a demo or remote desktop connection. The host returns the SDK JSON
envelope (`result` or `error`); `src/client.ts` unwraps it.

Run commands from the repository root with the pinned CLI:

```sh
python3 native/ui/sync-assets.py
.cache/nsh/nsh/bin/nsh check native/ui
.cache/nsh/nsh/bin/nsh test native/ui/App.test.tsx
.cache/nsh/nsh/bin/nsh build native/ui/App.tsx --release --out native/ui/.nsh/release
```

The application needs the native host to operate. Running `nsh run App.tsx` by
itself shows an engine-unavailable error; it cannot start a VPN by itself.

`Preview.tsx` is a separate entry for visual review. Its deterministic fixtures
have an always-visible preview label and contain no real credentials or proxy
traffic. The screenshot batches capture the four pages in both themes at
1060×720, 840×560 and 390×844, plus import, editing and diagnostic pages in
English. Each runs separately within the native step runner's 30-second budget:

```sh
mkdir -p native/ui/screenshots
.cache/nsh/nsh/bin/nsh test native/ui/Preview.tsx --target macos < native/ui/render.steps.js
.cache/nsh/nsh/bin/nsh test native/ui/Preview.tsx --target macos < native/ui/mobile-compact.steps.js
.cache/nsh/nsh/bin/nsh test native/ui/Preview.tsx --target macos < native/ui/mobile-phone.steps.js
.cache/nsh/nsh/bin/nsh test native/ui/Preview.tsx --target macos < native/ui/mobile-details.steps.js
.cache/nsh/nsh/bin/nsh test native/ui/Preview.tsx --target macos < native/ui/offline-nodes.steps.js
```

These screenshots are actual **AppKit renders at phone and desktop widths**;
phone-width AppKit images do not establish Android or iOS VPN acceptance.

`sync-assets.py` generates colors from `internal/app/theme.go` and copies the
existing Lucide assets (license in `assets/icons/LICENSE`) and the app mark
from `internal/brand/icon-master.png`. The desktop sidebar uses its exact
`internal/brand/sidebar.png` runtime derivative, matching the original window
at both 32-point and compact 24-point sizes. Run it before builds; those files remain
the shared source of palette and branding.

The native preview uses its own application ID and storage so installing it
does not replace the existing desktop client's subscriptions or update setup.
UI preferences use NSH's private `Storage`; imported credentials remain in the
Go engine's private data directory, not UI storage.

Mobile business state and task coordination live in `src/mobile/store.ts`;
`platform.ts` describes Android session, document and application-scope calls.
The host publishes connection intent and progress independently of engine
queries. A connection command never waits for a traffic refresh before making
Cancel available. Latency values come from the engine's current group snapshot,
so a same-name node in a new profile does not inherit a UI-only cached result.
Page motion uses ordinary view opacity/translation with a finite animation;
mobile pages do not require a ShaderView/Vulkan transition surface.

The desktop entry includes subscription/aggregate and proxy editors, connections,
mesh, rules and template library, logs, and seven settings categories. It keeps
unsaved drafts and filters across navigation, supports reduced motion, and uses
320 ms navigation transitions and 400 ms traffic history interpolation. It does
not substitute fixture data when the host is unavailable.

`DesktopPreview.tsx` and `DesktopMotionPreview.tsx` are separate visual fixtures.
Run the real native AppKit renderer (one instance at a time):

```sh
.cache/nsh/nsh/bin/nsh test native/ui/DesktopPreview.tsx --target macos < native/ui/desktop-render.steps.js
.cache/nsh/nsh/bin/nsh test native/ui/DesktopPreview.tsx --target macos < native/ui/desktop-dialogs.steps.js
.cache/nsh/nsh/bin/nsh test native/ui/DesktopPreview.tsx --target macos < native/ui/desktop-editors.steps.js
```

`Desktop.test.tsx` and `DesktopEditors.test.tsx` exercise draft preservation,
failed saves, explicit authorization of mesh ports, and animation reversal.
`make native-desktop-smoke` verifies the shipped desktop helper with a real core,
loopback traffic, rollback and backup validation in an isolated directory.

`DesktopMesh.test.tsx` covers connection tabs, draft parameters, preparation
cancellation and client errors. Run `nsh test native/ui/DesktopMeshPreview.tsx
--target macos < native/ui/desktop-mesh.steps.js` (and
`DesktopMeshEnglishPreview.tsx`) to refresh both sizes and themes for SSH, RDP,
VNC, long device names, unprepared access and connection feedback. These
fixtures never change system settings or open real clients.

The Catalyst host pins NSH's desktop font scale to 1.0: macOS's 13pt body
must not be normalized against iOS's 17pt default. The host owns its status
item; its fresh menu and commands come from the existing desktop model.
`DesktopUpdatePreview.tsx` and `desktop-updates.steps.js` capture checking
animation frames, release notes, download progress and ready-to-restart states.
For the large matrix, replace the script's 840×560 viewport with 1060×720.

`DesktopSelect.test.tsx` replays fresh snapshot objects every second and checks
that open menus retain keyboard selection on Catalyst's iOS platform, macOS and
Windows. `SelectInitializationPreview.tsx` delays frame initialization to verify
that an early direction key is retained, including when reopening a selection.
The shared desktop selector uses an anchored native `Popover` and
`Command` list instead of NSH's mobile Select sheet. Refresh its two-size,
light/dark captures with:

```sh
.cache/nsh/nsh/bin/nsh test native/ui/DesktopSelectPreview.tsx --target macos < native/ui/desktop-select.steps.js
.cache/nsh/nsh/bin/nsh test native/ui/DesktopSelectPreview.tsx --target macos < native/ui/desktop-select-dialog.steps.js
```

`Traffic.test.tsx` covers sliding and growing histories, skipped samples, reset
histories, interrupted motion, stable scaling and curves without false peaks.
`DesktopTrafficPreview.tsx` is an isolated visual replay that settles at zero;
`desktop-traffic.steps.js` records native intermediate frames in
`.cache/traffic-frames/` and a zero-rate screenshot. Production data is never
replaced with this fixture.
