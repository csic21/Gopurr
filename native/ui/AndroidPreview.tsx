import { mount } from "nsh";
import { GopurrApp } from "./src/app";
import { fixtureTransport } from "./src/fixture";
import { usePreviewTransport } from "./src/client";
import { usePreviewAndroidTransport } from "./src/mobile/platform";

usePreviewTransport(fixtureTransport());
let notifications = { granted: false, enabled: false, canRequest: true, traffic: true };
let update = {
  supported: true,
  phase: "available",
  currentVersion: "0.6.29",
  currentVersionCode: 1000060,
  version: "0.6.30",
  notes: "手机端增加应用内更新：检查新版本、下载 APK 并交给系统安装程序。",
  downloaded: 0,
  total: 32 * 1024 * 1024,
  error: "",
  detail: "",
  canInstall: true,
};
usePreviewAndroidTransport(async (method, params) => {
  if (method === "android.session") return { phase: "idle", generation: 0, busy: false, connected: false, scope: { mode: "all", packages: [], excludeSelf: true } };
  if (method === "android.notifications") return notifications;
  if (method === "android.notifications.configure") return notifications = { ...notifications, traffic: params.traffic as boolean };
  if (method === "android.notifications.request") return notifications = { ...notifications, granted: true, enabled: true, canRequest: false };
  if (method === "android.appearance" || method === "android.notifications.settings") return {};
  if (method === "android.update") return update;
  if (method === "android.update.check") return update;
  if (method === "android.update.download") return update = { ...update, phase: "downloading", downloaded: 8 * 1024 * 1024 };
  if (method === "android.update.cancel") return update = { ...update, phase: "available", downloaded: 0 };
  if (method === "android.update.install") return update = { ...update, phase: "installing", detail: "请在系统安装程序中确认。" };
  throw new Error(`Unsupported Android preview operation: ${method}`);
});
mount(() => <GopurrApp preview />);
