for(const size of [{width:1060,height:720}]) {
 await app.setViewport(size.width,size.height);
 for(const scheme of ['light','dark']) {
  await app.setColorScheme(scheme);
  await app.press('id:desktop-nav-overview');
  await app.press('id:feature-settings-dns');
  await app.screenshot(`native/ui/screenshots/desktop-overview-dns-${scheme}-${size.width}x${size.height}.png`);
  await app.press('text:关闭');
  await app.press('id:desktop-nav-subs');
  await app.press('id:edit-profile-daily');
  await app.screenshot(`native/ui/screenshots/desktop-profile-editor-${scheme}-${size.width}x${size.height}.png`);
  await app.press('text:取消');
  await app.press('id:add-aggregate');
  await app.screenshot(`native/ui/screenshots/desktop-aggregate-${scheme}-${size.width}x${size.height}.png`);
  await app.press('text:取消');
  await app.press('id:desktop-nav-conns');
  await app.press('id:connection-conn-0');
  await app.screenshot(`native/ui/screenshots/desktop-connection-detail-${scheme}-${size.width}x${size.height}.png`);
  await app.press('id:connection-detail-close');
 }
}
