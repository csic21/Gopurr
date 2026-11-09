for(const size of [{width:1060,height:720},{width:840,height:560}]) {
 await app.setViewport(size.width,size.height);
 for(const scheme of ['light','dark']) {
  await app.setColorScheme(scheme);
  await app.press('id:feature-policy-dns');
  await app.wait(2200);
  if(!app.shows('id:feature-policy-dns-options'))throw new Error('Polling closed the menu');
  await app.screenshot(`native/ui/screenshots/desktop-policy-menu-${scheme}-${size.width}x${size.height}.png`);
  await app.keyboard.press('escape');
 }
}
