// Run in a separate NSH invocation after android-update.steps.js.
for (const size of [{ width: 1060, height: 720 }, { width: 840, height: 560 }, { width: 390, height: 844 }]) {
  await app.setViewport(size.width, size.height);
  for (const scheme of ["light", "dark"]) {
    await app.setColorScheme(scheme);
    await app.press("id:nav-settings");
    await app.scroll("id:settings-scroll", 2000);
    await app.press("id:update-download");
    await app.waitFor("id:update-cancel");
    await app.screenshot(`native/ui/screenshots/android-update-downloading-${scheme}-${size.width}x${size.height}.png`);
    await app.press("id:update-cancel");
    await app.waitFor("id:update-download");
  }
}
