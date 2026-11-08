import { mount } from "nsh";
import { GopurrApp } from "./src/app";
import { fixtureTransport } from "./src/fixture";
import { usePreviewTransport } from "./src/client";
import { usePreviewAndroidTransport } from "./src/mobile/platform";

usePreviewTransport(fixtureTransport());
let notifications = { granted: false, enabled: false, canRequest: true, traffic: true };
usePreviewAndroidTransport(async (method, params) => {
  if (method === "android.session") return { phase: "idle", generation: 0, busy: false, connected: false, scope: { mode: "all", packages: [], excludeSelf: true } };
  if (method === "android.notifications") return notifications;
  if (method === "android.notifications.configure") return notifications = { ...notifications, traffic: params.traffic as boolean };
  if (method === "android.notifications.request") return notifications = { ...notifications, granted: true, enabled: true, canRequest: false };
  if (method === "android.appearance" || method === "android.notifications.settings") return {};
  throw new Error(`Unsupported Android preview operation: ${method}`);
});
mount(() => <GopurrApp preview />);
