for(const size of [{width:840,height:560},{width:1060,height:720}]) {
 await app.setViewport(size.width,size.height);
 for(const scheme of ['light','dark']) {
  await app.setColorScheme(scheme);
  for(const page of ['conns','logs','proxies','rules','subs']) {
   await app.press(`id:desktop-nav-${page}`);
   await app.screenshot(`native/ui/screenshots/desktop-empty-${page}-${scheme}-${size.width}x${size.height}.png`);
  }
 }
}
