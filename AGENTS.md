# RTK output optimization

Use RTK for supported shell commands whose output may be verbose. On this machine,
prioritize `rtk go test`, `rtk format`, `rtk lint`, `rtk ls`, `rtk git`,
`rtk rg`, `rtk grep`, `rtk find`, `rtk pnpm`, and `rtk cargo`.

Run `curl` normally; this machine's RTK history shows an immediate exact-output
recovery after a compressed curl response.

Run unsupported shell utilities normally; do not force an unknown command through
`rtk`. When exact or machine-readable output is required, use the original command
or `rtk proxy <command>`.

# UI quality

Read `DESIGN.md` before changing the interface. UI quality is a primary acceptance
criterion for this project.

- Use the shared palette, typography and spacing. Keep navigation and action icons
  in the existing Lucide family; use labeled native buttons for actions.
- Treat `internal/brand/icon-master.png` as the rendered app/sidebar source,
  `logo.svg` as its flat companion and `tray.svg` as the menu-bar source.
  Regenerate runtime PNGs and the icon preview when those sources change.
- Render and inspect the affected pages at 1060×720 and 840×560. For shared theme
  or navigation changes, review all seven pages, editors and dialogs.
- Check key controls, long text, hover, selected and disabled states. Keep system
  proxy and TUN controls visible in the minimum overview window.
- Verify actual screenshots before declaring UI work complete; passing tests and
  nonblank renders alone do not establish visual quality.
- Keep render fixtures and the app bundle current when delivering UI changes.

Use the workspace caches configured in `Makefile` for Go commands. UI tests use
temporary local HTTP servers and Unix sockets; they do not require real profiles
or changes to the user's system proxy.
