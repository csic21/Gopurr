const lang=app.texts().includes('Overview')?'en':'zh';
await app.setViewport(840,560);
await app.press('id:desktop-nav-mesh');
await app.press('id:mesh-connect-peer-1');
await app.screenshot(`native/ui/screenshots/desktop-mesh-quick-unprepared-${lang}-light-840x560.png`);
await app.press(`id:mesh-quick-content >> text:${lang==='en'?'Prepare access':'准备访问'}`);
await app.press(`text:${lang==='en'?'Enable and prepare':'启用并准备'}`);
for(const size of [{width:1060,height:720},{width:840,height:560}]) {
  await app.setViewport(size.width,size.height);
  for(const scheme of ['light','dark']) {
    await app.setColorScheme(scheme);
    for(const kind of ['ssh','rdp','vnc']) {
      await app.press(`id:desktop-tab-${kind}`);
      await app.screenshot(`native/ui/screenshots/desktop-mesh-quick-${kind}-${lang}-${scheme}-${size.width}x${size.height}.png`);
    }
    await app.hover('id:mesh-quick-probe');
    await app.press('id:mesh-quick-probe');
    await app.screenshot(`native/ui/screenshots/desktop-mesh-quick-checked-${lang}-${scheme}-${size.width}x${size.height}.png`);
    await app.press('id:mesh-quick-launch');
    await app.screenshot(`native/ui/screenshots/desktop-mesh-quick-error-${lang}-${scheme}-${size.width}x${size.height}.png`);
  }
}
