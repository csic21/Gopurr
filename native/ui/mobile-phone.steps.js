// Separate batch keeps native screenshots below the step runner 30s budget.
await app.setViewport(390, 844);
for (const scheme of ["light", "dark"]) {
  await app.setColorScheme(scheme);
  for (const page of ["overview", "profiles", "proxies", "settings"]) {
    await app.press(`id:nav-${page}`);
    await app.screenshot(`native/ui/screenshots/${page}-${scheme}-390x844.png`);
  }
}
