import { expect, open, test } from "nsh/test";
import { fixtureTransport } from "./src/fixture";

function bridge(granted = false) {
  const dispatch = fixtureTransport();
  let settings = { granted, enabled: granted, canRequest: !granted, traffic: true };
  return { params: { android: true }, handlers: {
    "engine.dispatch": async (value: unknown) => { const request = value as { method: string; params: Record<string, unknown> }; return { result: await dispatch(request.method, request.params) }; },
    "android.session": { phase: "idle", generation: 0, busy: false, connected: false, scope: { mode: "all", packages: [], excludeSelf: true } },
    "android.appearance": {},
    "android.notifications": () => settings,
    "android.notifications.request": () => settings = { ...settings, granted: true, enabled: true, canRequest: false },
    "android.notifications.configure": (value: unknown) => settings = { ...settings, traffic: (value as { traffic: boolean }).traffic },
    "android.notifications.settings": {},
  } };
}

test("Android notifications expose permission, live traffic preferences and system settings", async () => {
  const app = await open("./App.tsx", { host: bridge(), locale: "en-US", width: 390, height: 844 });
  await app.press("id:nav-settings");
  await app.waitFor("text:Notifications disabled");
  await app.press("id:notification-permission");
  await app.waitFor("text:Notifications enabled");
  await app.press("id:notification-state-only");
  expect(app.host.calls().some(c => c.name === "android.notifications.configure" && (c.data as { traffic?: boolean }).traffic === false)).toBe(true);
  await app.press("id:notification-traffic");
  expect(app.host.calls().some(c => c.name === "android.notifications.configure" && (c.data as { traffic?: boolean }).traffic === true)).toBe(true);
  await app.press("id:notification-settings");
  expect(app.host.calls().some(c => c.name === "android.notifications.settings")).toBe(true);
});

test("notification taps open Connect in an existing Android window", async () => {
  const app = await open("./App.tsx", { host: bridge(true), locale: "en-US" });
  await app.press("id:nav-settings");
  await app.press("id:open-logs");
  await app.waitFor("text:Diagnostic logs");
  await app.host.send("android.navigate", { page: "overview" });
  await app.waitFor("id:toggle-engine");
  expect(app.texts()).toContain("Your profile and network connection.");
});

