for(const size of [{width:1060,height:720},{width:840,height:560}]) {
 await app.setViewport(size.width,size.height);
 for(const scheme of ['light','dark']) {
  await app.setColorScheme(scheme);
  await app.press('id:desktop-nav-overview');
  await app.hover('id:desktop-nav-proxies');
  await app.screenshot(`native/ui/screenshots/windows-nav-hover-${scheme}-${size.width}x${size.height}.png`);
  await app.hover('id:overview-route-action');
  await app.screenshot(`native/ui/screenshots/windows-action-hover-${scheme}-${size.width}x${size.height}.png`);
 }
}
