await app.setViewport(840,560);
for(const scheme of ['light','dark']) {
 await app.setColorScheme(scheme);
 await app.press('id:desktop-nav-rules');
 for(const tab of ['extra','yaml']) {
  await app.press(`id:desktop-tab-${tab}`);
  await app.wait(200);
  await app.screenshot(`native/ui/screenshots/desktop-rules-${tab}-${scheme}-840x560.png`);
 }
}
