// Run from the repository root:
// nsh test native/ui/Preview.tsx --target macos < native/ui/render.steps.js
// `app` is supplied by NSH's test step runner. These are native AppKit pixels.
for (const size of [{ width: 1060, height: 720 }]) {
  await app.setViewport(size.width, size.height);
  for (const scheme of ["light", "dark"]) {
    await app.setColorScheme(scheme);
    for (const page of ["overview", "profiles", "proxies", "settings"]) {
      await app.press(`id:nav-${page}`);
      await app.screenshot(`native/ui/screenshots/${page}-${scheme}-${size.width}x${size.height}.png`);
    }
  }
}
