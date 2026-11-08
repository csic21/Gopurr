import { expect, open, test } from "nsh/test";
import { fixtureTransport } from "./src/fixture";

function host(empty = false) {
  const dispatch = fixtureTransport(empty);
  return { handlers: { "android.notifications": { granted: false, enabled: false, canRequest: true, traffic: true }, "engine.dispatch": async (data: unknown) => {
    const request = data as { method: string; params: Record<string, unknown> };
    return { result: await dispatch(request.method, request.params) };
  } } };
}

test("mobile polling stops while hidden and resumes with fresh lists", async () => {
  const app = await open("./App.tsx", { host: host(), locale: "en-US", clock: {} });
  await app.waitFor("text:Local proxy running");
  await app.wait(100);
  const count = () => app.host.calls().filter(c => c.name === "engine.dispatch").length;
  await app.setLifecycle("background");
  const hidden = count();
  await app.clock!.runFor(30000);
  expect(count()).toBe(hidden);
  await app.setLifecycle("active");
  await app.wait(100);
  expect(count()).toBe(hidden + 3);
  await app.setLifecycle("inactive");
  const inactive = count();
  await app.clock!.runFor(12000);
  expect(count()).toBe(inactive);
});

test("mobile polls status less often away from Overview and refreshes only visible groups", async () => {
  const app = await open("./App.tsx", { host: host(), locale: "en-US", clock: {} });
  await app.waitFor("text:Local proxy running");
  await app.wait(100);
  const count = (method: string) => app.host.calls().filter(c => (c.data as { method?: string })?.method === method).length;
  let before = count("engine.status");
  await app.clock!.runFor(1600);
  await app.clock!.runFor(1600);
  expect(count("engine.status") - before).toBe(2);
  await app.press("id:nav-settings");
  await app.wait(100);
  before = count("engine.status");
  const profiles = count("profiles.list");
  const groups = count("proxies.groups");
  await app.clock!.runFor(6100);
  await app.clock!.runFor(6100);
  expect(count("engine.status") - before).toBe(2);
  expect(count("profiles.list")).toBe(profiles);
  expect(count("proxies.groups")).toBe(groups);
  await app.press("id:nav-proxies");
  await app.wait(100);
  const proxyProfiles = count("profiles.list");
  const proxyGroups = count("proxies.groups");
  await app.clock!.runFor(9100);
  await app.clock!.runFor(9100);
  expect(count("proxies.groups") - proxyGroups).toBe(2);
  expect(count("profiles.list")).toBe(proxyProfiles);
});

test("states local proxy and device routing independently", async () => {
  const app = await open("./App.tsx", { host: host(), width: 840, height: 560, locale: "en-US", platform: "macos" });
  await app.waitFor("text:Local proxy running");
  expect(app.texts()).toContain("Starting the local proxy does not route device traffic automatically.");
  expect(app.enabled("id:toggle-engine")).toBe(true);
  await app.press("id:toggle-engine");
  await app.waitFor("text:Disconnected");
});

test("first import is selected without starting; Overview connects and another profile explicitly activates", async () => {
  const app = await open("./App.tsx", { host: host(true), locale: "en-US" });
  await app.press("id:nav-profiles");
  expect(app.enabled("id:import-profile")).toBe(false);
  await app.type("id:profile-name", "Everyday");
  await app.type("id:profile-url", "https://example.invalid/profile.yaml");
  await app.press("id:import-profile");
  await app.waitFor("text:Selected");
  expect(app.host.calls().some((c) => (c.data as { method?: string })?.method === "profiles.activate")).toBe(false);
  expect(app.host.calls().some((c) => (c.data as { method?: string })?.method === "engine.start")).toBe(false);
  expect(app.enabled("id:activate-imported")).toBe(false);
  await app.press("id:nav-overview");
  await app.waitFor("text:Disconnected");
  await app.waitUntil(() => app.enabled("id:toggle-engine"));
  await app.press("id:toggle-engine");
  await app.waitFor("text:Local proxy running");
  await app.press("id:nav-profiles");
  await app.press("id:add-profile");
  await app.type("id:profile-name", "Second profile");
  await app.type("id:profile-url", "https://example.invalid/second.yaml");
  await app.press("id:import-profile");
  await app.waitFor("text:Second profile");
  await app.waitUntil(() => app.enabled("id:activate-imported-2"));
  await app.press("id:activate-imported-2");
  await app.waitUntil(() => app.host.calls().some((c) => (c.data as { method?: string; params?: { id?: string } })?.method === "profiles.activate" && (c.data as { params?: { id?: string } }).params?.id === "imported-2"));
  await app.press("id:nav-overview");
  await app.waitFor("text:Second profile");
});

test("latency testing does not switch a node; selection is explicit", async () => {
  const app = await open("./App.tsx", { host: host(), locale: "en-US" });
  await app.press("id:nav-proxies");
  await app.waitFor("id:delay-示例节点 / Example proxy");
  await app.press("id:delay-示例节点 / Example proxy");
  await app.waitFor("text:12 ms");
  expect(app.host.calls().some((c) => (c.data as { method?: string })?.method === "proxies.select")).toBe(false);
  await app.press("id:node-示例节点 / Example proxy");
  expect(app.host.calls().some((c) => (c.data as { method?: string })?.method === "proxies.select")).toBe(true);
  // DIRECT's selected helper changes back to its raw type only after the UI
  // has actually moved selection to the other node.
  await app.waitFor("text:Direct");
  await app.type("id:node-search", "no-match");
  await app.waitFor("text:No matching nodes");
  await app.press("text:Reset filter");
  await app.waitFor("id:node-DIRECT");
});

test("saved nodes remain searchable after disconnect without starting or testing", async () => {
  const app = await open("./App.tsx", { host: host(), locale: "en-US", clock: {} });
  await app.waitFor("text:Local proxy running");
  await app.press("id:toggle-engine");
  await app.waitFor("text:Disconnected");
  await app.press("id:nav-proxies");
  await app.waitFor("id:node-示例节点 / Example proxy");
  expect(app.texts()).toContain("Disconnected · Browse saved nodes. Connect to switch or test.");
  expect(app.texts()).toContain("For next connection");
  expect(app.enabled("id:node-示例节点 / Example proxy")).toBe(false);
  expect(app.enabled("id:delay-示例节点 / Example proxy")).toBe(false);
  expect(app.enabled("id:test-group")).toBe(false);
  await app.type("id:node-search", "no-match");
  await app.waitFor("text:No matching nodes");
  await app.press("text:Reset filter");
  await app.waitFor("id:node-DIRECT");
  const groupCalls = () => app.host.calls().filter(c => (c.data as { method?: string })?.method === "proxies.groups").length;
  const before = groupCalls();
  await app.clock!.runFor(20000);
  expect(groupCalls()).toBe(before);
  const methods = app.host.calls().map(c => (c.data as { method?: string })?.method);
  expect(methods).not.toContain("engine.start");
  expect(methods).not.toContain("proxies.delay");
  expect(methods).not.toContain("proxies.select");
});

test("first import shows offline nodes, and missing provider cache is explained", async () => {
  const bridge = host(true), original = bridge.handlers["engine.dispatch"];
  bridge.handlers["engine.dispatch"] = async (data: unknown) => {
    const reply = await original(data);
    if ((data as { method: string }).method === "proxies.groups") {
      return { result: { ...(reply.result as object), incomplete: true } } as never;
    }
    return reply;
  };
  const app = await open("./App.tsx", { host: bridge, locale: "en-US" });
  await app.press("id:nav-proxies");
  await app.waitFor("text:No profile selected");
  await app.press("text:Open profiles");
  await app.type("id:profile-name", "Offline");
  await app.type("id:profile-url", "https://example.invalid/offline.yaml");
  await app.press("id:import-profile");
  await app.waitFor("text:Selected");
  await app.press("id:nav-proxies");
  await app.waitFor("id:node-示例节点 / Example proxy");
  expect(app.texts()).toContain("Some provider nodes have no local preview. Connect to load the full list.");
  expect(app.host.calls().some(c => ["engine.start", "android.connect", "profiles.activate"].includes((c.data as { method?: string })?.method ?? c.name))).toBe(false);
});

test("failed imports preserve the draft and never become successful", async () => {
  const bridge = host(true);
  const original = bridge.handlers["engine.dispatch"];
  bridge.handlers["engine.dispatch"] = async (data: unknown) => {
    if ((data as { method: string }).method === "profiles.import") return { error: { code: "invalid_config", message: "The profile was rejected." } } as never;
    return original(data);
  };
  const app = await open("./App.tsx", { host: bridge, locale: "en-US" });
  await app.press("id:nav-profiles");
  await app.type("id:profile-name", "Keep draft");
  await app.type("id:profile-url", "https://example.invalid/bad.yaml");
  await app.press("id:import-profile");
  await app.waitFor("text:The profile was rejected.");
  expect(app.value("id:profile-name")).toBe("Keep draft");
  expect(app.enabled("id:import-profile")).toBe(true);
});

test("language changes update page content and persisted preferences", async () => {
  const app = await open("./App.tsx", { host: host(), locale: "en-US" });
  await app.press("id:nav-settings");
  await app.press("text:简体中文");
  expect(app.texts()).toContain("外观");
  expect(app.storage().language).toBe("zh-Hans");
});

test("VPN support does not report a connection until the native tunnel starts", async () => {
  const dispatch = fixtureTransport();
  let tunnel = false;
  const app = await open("./App.tsx", { locale: "en-US", platform: "android", host: { handlers: {
    "engine.dispatch": async (data: unknown) => {
      const request = data as { method: string; params: Record<string, unknown> };
      const result = await dispatch(request.method, request.params) as Record<string, unknown>;
      if (request.method === "engine.start") tunnel = true;
      if (request.method === "engine.stop") tunnel = false;
      if (request.method === "engine.status" || request.method === "engine.start" || request.method === "engine.stop") {
        return { result: { ...result, vpnSupported: true, tun: tunnel, capabilities: { systemProxy: false, tun: true } } };
      }
      return { result };
    },
  } } });
  await app.waitFor("text:Connect VPN");
  expect(app.texts()).not.toContain("Device VPN connected");
  await app.press("id:toggle-engine");
  await app.waitFor("text:Device VPN connected");
  await app.press("id:toggle-engine");
  await app.waitFor("text:Connect VPN");
});

test("waiting for VPN consent stays busy and prevents another start", async () => {
  const dispatch = fixtureTransport();
  let grantPermission: (() => void) | undefined;
  const consent = new Promise<void>((resolve) => { grantPermission = resolve; });
  let tunnel = false;
  const app = await open("./App.tsx", { locale: "en-US", platform: "android", host: { handlers: {
    "engine.dispatch": async (data: unknown) => {
      const request = data as { method: string; params: Record<string, unknown> };
      if (request.method === "engine.start") { await consent; tunnel = true; }
      const result = await dispatch(request.method, request.params) as Record<string, unknown>;
      if (request.method === "engine.status" || request.method === "engine.start") {
        return { result: { ...result, vpnSupported: true, tun: tunnel, capabilities: { systemProxy: false, tun: true } } };
      }
      return { result };
    },
  } } });
  await app.waitFor("text:Connect VPN");
  const pendingPress = app.press("id:toggle-engine");
  await new Promise<void>((resolve) => setTimeout(resolve, 150));
  expect(app.enabled("id:toggle-engine")).toBe(false);
  expect(app.texts()).toContain("Working…");
  expect(app.host.calls().filter((c) => (c.data as { method?: string })?.method === "engine.start").length).toBe(1);
  grantPermission!();
  await pendingPress;
  await app.waitFor("text:Device VPN connected");
  await app.waitUntil(() => app.enabled("id:toggle-engine"));
  expect(app.enabled("id:toggle-engine")).toBe(true);
});


test("profile edits, refresh staging and deletion stay explicit", async () => {
  const app = await open("./App.tsx", { host: host(true), locale: "en-US" });
  await app.press("id:nav-profiles");
  await app.type("id:profile-name", "Daily");
  await app.type("id:profile-url", "https://example.invalid/profile.yaml");
  await app.press("id:import-profile");
  await app.waitFor("text:Selected");
  await app.press("id:edit-imported");
  await app.waitFor("id:edit-profile-name");
  await app.type("id:edit-profile-name", "Renamed");
  await app.press("id:save-profile");
  await app.waitFor("text:DailyRenamed");
  await app.press("id:nav-overview");
  await app.press("id:toggle-engine");
  await app.waitFor("text:Local proxy running");
  await app.press("id:nav-profiles");
  await app.press("id:refresh-imported");
  await app.waitFor("text:Saved · reconnect to apply");
  expect(app.enabled("id:delete-imported")).toBe(false);
  await app.press("id:nav-overview");
  await app.waitFor("id:reconnect-required");
  await app.press("id:toggle-engine");
  await app.waitFor("text:Disconnected");
  await app.press("id:nav-profiles");
  await app.press("id:delete-imported");
  expect(app.host.calls().some(c => (c.data as { method?: string })?.method === "profiles.delete")).toBe(false);
  await app.press("id:confirm-delete-profile");
  await app.waitFor("id:import-profile");
  expect(app.host.calls().some(c => (c.data as { method?: string })?.method === "profiles.delete")).toBe(true);
});

test("Android consent can be cancelled and stale session events cannot reconnect", async () => {
  const bridge = host();
  let session = { phase: "idle", generation: 0, busy: false, connected: false, scope: { mode: "all", packages: [], excludeSelf: true } };
  const app = await open("./App.tsx", { locale: "en-US", platform: "android", host: {
    params: { android: true }, handlers: {
      ...bridge.handlers,
      "android.session": () => session,
      "android.connect": () => session = { ...session, phase: "awaitingPermission", generation: 1, busy: true },
      "android.disconnect": () => session = { ...session, phase: "idle", generation: 2, busy: false },
    },
  } });
  await app.waitFor("id:toggle-engine");
  await app.waitUntil(() => app.enabled("id:toggle-engine"));
  await app.press("id:toggle-engine");
  await app.waitFor("text:Waiting for VPN permission");
  await app.waitFor("text:Cancel connection");
  expect(app.enabled("id:toggle-engine")).toBe(true);
  await app.press("id:toggle-engine");
  await app.waitFor("text:Disconnected");
  await app.host.send("android.session", { ...session, phase: "connected", connected: true, generation: 1 });
  expect(app.texts()).not.toContain("Device VPN connected");
  expect(app.host.calls().filter(c => c.name === "android.connect").length).toBe(1);
  expect(app.host.calls().filter(c => c.name === "android.disconnect").length).toBe(1);
});

test("Android app include mode refuses an empty selection and saves package identity", async () => {
  const bridge = host();
  const app = await open("./App.tsx", { locale: "en-US", platform: "android", width: 390, height: 844, host: {
    params: { android: true }, handlers: {
      ...bridge.handlers,
      "android.session": { phase: "idle", generation: 0, busy: false, connected: false, scope: { mode: "all", packages: [], excludeSelf: true } },
      "android.apps": { apps: [{ packageName: "com.example.browser", name: "Example browser", system: false }] },
      "android.scope.get": { mode: "all", packages: [], excludeSelf: true },
      "android.scope.set": (value: unknown) => value,
    },
  } });
  await app.press("id:nav-settings");
  await app.press("id:open-app-scope");
  await app.waitFor("text:Selected only");
  await app.press("text:Selected only");
  expect(app.enabled("id:save-app-scope")).toBe(false);
  await app.press("id:scope-app-com.example.browser");
  expect(app.enabled("id:save-app-scope")).toBe(true);
  await app.press("id:save-app-scope");
  await app.waitFor("id:open-app-scope");
  const saved = app.host.calls().find(c => c.name === "android.scope.set");
  expect((saved?.data as { packages: string[] }).packages[0]).toBe("com.example.browser");
});


test("global mode exposes the global selector without changing selection implicitly", async () => {
  const app = await open("./App.tsx", { host: host(), locale: "en-US" });
  await app.press("id:mode-global");
  await app.press("id:nav-proxies");
  await app.waitFor("text:GLOBAL");
  expect(app.host.calls().some(c => (c.data as { method?: string })?.method === "proxies.select")).toBe(false);
  await app.press("id:node-示例节点 / Example proxy");
  const selected = app.host.calls().find(c => (c.data as { method?: string })?.method === "proxies.select");
  expect((selected?.data as { params: { group: string } }).params.group).toBe("GLOBAL");
});


test("Android cancel stays available while engine status is waiting for startup", async () => {
  const dispatch = fixtureTransport();
  let blocked = false, waitingForStatus = false;
  let releaseStatus: (() => void) | undefined;
  const statusBarrier = new Promise<void>(resolve => { releaseStatus = resolve; });
  let session = { phase: "idle", generation: 0, busy: false, connected: false, scope: { mode: "all", packages: [], excludeSelf: true } };
  const app = await open("./App.tsx", { locale: "en-US", platform: "android", host: {
    params: { android: true }, handlers: {
      "engine.dispatch": async (data: unknown) => {
        const request = data as { method: string; params: Record<string, unknown> };
        if (blocked && request.method === "engine.status") { waitingForStatus = true; await statusBarrier; }
        return { result: await dispatch(request.method, request.params) };
      },
      "android.session": () => session,
      "android.connect": () => { blocked = true; return session = { ...session, phase: "connecting", generation: 1, busy: true }; },
      "android.disconnect": () => session = { ...session, phase: "idle", generation: 2, busy: false },
    },
  } });
  await app.waitUntil(() => app.enabled("id:toggle-engine"));
  await app.press("id:toggle-engine");
  await app.waitFor("text:Cancel connection");
  const refreshing = app.press("text:Refresh");
  await app.waitUntil(() => waitingForStatus);
  expect(app.enabled("id:toggle-engine")).toBe(true);
  const cancelling = app.press("id:toggle-engine");
  await app.waitUntil(() => app.host.calls().some(c => c.name === "android.disconnect"));
  releaseStatus!();
  await refreshing;
  await cancelling;
  await app.waitFor("text:Disconnected");
  expect(app.host.calls().filter(c => c.name === "android.disconnect").length).toBe(1);
});

test("same-name nodes do not retain a latency result from another profile", async () => {
  const app = await open("./App.tsx", { host: host(), locale: "en-US" });
  await app.press("id:nav-proxies");
  await app.press("id:delay-示例节点 / Example proxy");
  await app.waitFor("text:12 ms");
  await app.press("id:nav-profiles");
  await app.press("id:add-profile");
  await app.type("id:profile-name", "Another source");
  await app.type("id:profile-url", "https://example.invalid/another.yaml");
  await app.press("id:import-profile");
  await app.waitFor("text:Another source");
  await app.press("id:activate-imported-2");
  await app.waitUntil(() => app.host.calls().some(c => (c.data as { method?: string })?.method === "profiles.activate"));
  await app.press("id:nav-proxies");
  await app.waitFor("id:delay-示例节点 / Example proxy");
  expect(app.texts()).not.toContain("12 ms");
});


test("Android window chrome follows the effective appearance", async () => {
  const bridge = host();
  const app = await open("./App.tsx", { locale: "en-US", platform: "android", colorScheme: "light", host: {
    params: { android: true }, handlers: {
      ...bridge.handlers,
      "android.session": { phase: "idle", generation: 0, busy: false, connected: false, scope: { mode: "all", packages: [], excludeSelf: true } },
      "android.appearance": { ok: true },
    },
  } });
  await app.waitUntil(() => app.host.calls().some(c => c.name === "android.appearance" && (c.data as { scheme: string }).scheme === "light"));
  await app.press("id:nav-settings");
  await app.press("text:Dark");
  await app.waitUntil(() => app.host.calls().some(c => c.name === "android.appearance" && (c.data as { scheme: string }).scheme === "dark"));
  expect(app.storage().appearance).toBe("dark");
});
