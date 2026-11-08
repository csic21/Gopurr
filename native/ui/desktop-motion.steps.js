const lang=app.texts().includes('Overview')?'en':'zh';
for(const size of [{width:840,height:560},{width:1060,height:720}]) {
 await app.setViewport(size.width,size.height);
 for(const scheme of ['light','dark']) {
  await app.setColorScheme(scheme);
  await app.screenshot(`native/ui/screenshots/desktop-titlebar-mid-${lang}-${scheme}-${size.width}x${size.height}.png`);
 }
}
