await app.setViewport(840,560);
for(const scheme of ['light','dark']) {
 await app.setColorScheme(scheme);
 await app.press('id:desktop-nav-overview');
 await app.press('id:feature-settings-dns');
 await app.press('id:dns-source');
 await app.wait(2200);
 await app.screenshot(`native/ui/screenshots/desktop-policy-editor-menu-${scheme}-840x560.png`);
 await app.keyboard.press('escape');
 if(!app.shows('id:dns-source'))throw new Error('Escape closed the editor with the menu');
 await app.press('text:关闭');
 await app.press('id:desktop-nav-logs');
 await app.press('label:日志等级');
 await app.screenshot(`native/ui/screenshots/desktop-policy-log-menu-${scheme}-840x560.png`);
 await app.keyboard.press('escape');
}
