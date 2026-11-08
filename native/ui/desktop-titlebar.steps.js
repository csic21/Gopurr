const lang=app.texts().includes("Overview")?"en":"zh";
for(const size of [{width:1060,height:720},{width:840,height:560}]) {
 await app.setViewport(size.width,size.height);
 for(const scheme of ['light','dark']) {
  await app.setColorScheme(scheme);
  await app.screenshot(`native/ui/screenshots/desktop-expanded-titlebar-${lang}-${scheme}-${size.width}x${size.height}.png`);
  await app.press('id:desktop-toggle-sidebar');
  await app.screenshot(`native/ui/screenshots/desktop-collapsed-titlebar-${lang}-${scheme}-${size.width}x${size.height}.png`);
  await app.press('id:desktop-toggle-sidebar');
 }
}
