await app.setViewport(840,560);
for(const scheme of ['light','dark']) {
 await app.setColorScheme(scheme);
 await app.press('id:desktop-nav-settings');
 for(const tab of ['network','dns','sniffer']) {
  await app.press(`id:desktop-tab-${tab}`);
  await app.wait(120);
  await app.screenshot(`native/ui/screenshots/desktop-settings-${tab}-${scheme}-840x560.png`);
 }
}
