// Native pixels for the disconnected node browser, at all acceptance widths.
await app.press("id:nav-overview");
await app.waitFor("text:本地代理已运行");
await app.press("id:toggle-engine");
await app.waitFor("text:尚未连接");
await app.press("id:nav-proxies");
await app.waitFor("id:node-示例节点 / Example proxy");
for (const size of [{ width: 1060, height: 720 }, { width: 840, height: 560 }, { width: 390, height: 844 }]) {
  await app.setViewport(size.width, size.height);
  for (const scheme of ["light", "dark"]) {
    await app.setColorScheme(scheme);
    await app.screenshot(`native/ui/screenshots/proxies-offline-${scheme}-${size.width}x${size.height}.png`);
  }
}
await app.type("id:node-search", "no-match");
await app.screenshot("native/ui/screenshots/proxies-offline-search-empty-390x844.png");
