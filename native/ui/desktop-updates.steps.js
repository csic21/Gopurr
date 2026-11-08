const width=840,height=560;
await app.setViewport(width,height);
for(const scheme of ['light','dark']) {
 await app.setColorScheme(scheme);
 await app.press('id:desktop-tab-maintenance');
 await app.press('text:检查更新');
 await app.waitFor('text:正在检查更新');
 await app.screenshot(`native/ui/screenshots/desktop-update-checking-${scheme}-${width}x${height}.png`);
 await app.wait(180);
 await app.screenshot(`native/ui/screenshots/desktop-update-checking-frame2-${scheme}-${width}x${height}.png`);
 await app.waitFor('text:发现新版本');
 await app.wait(300);
 await app.screenshot(`native/ui/screenshots/desktop-update-available-${scheme}-${width}x${height}.png`);
 await app.press('text:下载并安装');
 await app.waitFor('text:正在下载更新');
 await app.screenshot(`native/ui/screenshots/desktop-update-downloading-${scheme}-${width}x${height}.png`);
 await app.waitFor('text:更新已准备好');
 await app.wait(300);
 await app.screenshot(`native/ui/screenshots/desktop-update-ready-${scheme}-${width}x${height}.png`);
 await app.press('text:稍后重启');
}
