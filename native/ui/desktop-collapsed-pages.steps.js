const lang=app.texts().includes('Overview')?'en':'zh';
await app.press('id:desktop-toggle-sidebar');
for(const size of [{width:1060,height:720},{width:840,height:560}]) {
 await app.setViewport(size.width,size.height);
 for(const scheme of ['light','dark']) {
  await app.setColorScheme(scheme);
  for(const page of ['overview','subs','proxies','conns','mesh','rules','logs','settings']) {
   await app.press(`id:desktop-nav-${page}`);
   await app.screenshot(`native/ui/screenshots/desktop-top-${page}-${lang}-${scheme}-${size.width}x${size.height}.png`);
  }
 }
}
