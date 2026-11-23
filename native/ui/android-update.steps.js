// Run from the repository root:
// nsh test native/ui/AndroidPreview.tsx --target macos < native/ui/android-update.steps.js
await app.press("id:nav-settings");
await app.waitFor("id:open-update");
await app.press("id:open-update");
await app.waitFor("id:update-download");
for (const size of [{ width: 1060, height: 720 }, { width: 840, height: 560 }, { width: 390, height: 844 }]) {
  await app.setViewport(size.width, size.height);
  for (const scheme of ["light", "dark"]) {
    await app.setColorScheme(scheme);
    await app.waitFor("id:update-download");
    await app.screenshot(`native/ui/screenshots/android-update-available-${scheme}-${size.width}x${size.height}.png`);
  }
}
