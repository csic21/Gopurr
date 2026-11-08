// Run after render.steps.js, in a separate NSH invocation (30s step-runner budget).
await app.setViewport(390, 844);
await app.setColorScheme("light");
await app.press("id:nav-profiles");
await app.waitFor("id:add-profile");
await app.press("id:add-profile");
await app.waitFor("id:profile-name");
await app.screenshot("native/ui/screenshots/import-light-390x844.png");
await app.press("id:nav-settings");
await app.press("text:English");
await app.press("id:nav-overview");
await app.setViewport(840, 560);
await app.screenshot("native/ui/screenshots/overview-en-light-840x560.png");
await app.setViewport(390, 844);
await app.screenshot("native/ui/screenshots/overview-en-light-390x844.png");
await app.press("id:nav-profiles");
await app.screenshot("native/ui/screenshots/import-en-light-390x844.png");
await app.press("text:Close");
await app.press("id:edit-fixture-profile");
for (const size of [{ width: 1060, height: 720 }, { width: 840, height: 560 }, { width: 390, height: 844 }]) {
  await app.setViewport(size.width, size.height);
  await app.screenshot(`native/ui/screenshots/profile-editor-en-light-${size.width}x${size.height}.png`);
}
await app.press("text:Discard");
await app.press("id:nav-settings");
await app.press("id:open-logs");
await app.waitFor("text:Example diagnostic log — no live traffic.");
for (const size of [{ width: 1060, height: 720 }, { width: 840, height: 560 }, { width: 390, height: 844 }]) {
  await app.setViewport(size.width, size.height);
  await app.screenshot(`native/ui/screenshots/logs-en-light-${size.width}x${size.height}.png`);
}
