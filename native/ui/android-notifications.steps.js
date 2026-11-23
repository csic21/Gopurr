await app.press("id:nav-settings");
await app.waitFor("id:open-notifications");
await app.press("id:open-notifications");
await app.waitFor("id:notification-settings");
for (const size of [{ width: 1060, height: 720 }, { width: 840, height: 560 }, { width: 390, height: 844 }]) {
  await app.setViewport(size.width, size.height);
  for (const scheme of ["light", "dark"]) {
    await app.setColorScheme(scheme);
    await app.waitFor("id:notification-settings");
    await app.screenshot(`native/ui/screenshots/android-notification-settings-${scheme}-${size.width}x${size.height}.png`);
  }
}
