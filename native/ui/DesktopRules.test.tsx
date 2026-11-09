import { expect, open, test } from "nsh/test";
import { desktopFixtureTransport } from "./src/desktop/fixture";

test("desktop application picker keeps its dialog open for the rule editor", async () => {
  const fixture = desktopFixtureTransport();
  const base: any = await fixture("desktop.snapshot");
  base.appRules = [];
  const calls: { method: string; params: any }[] = [];
  const app = await open("./Desktop.tsx", {
    width: 840,
    height: 560,
    platform: "macos",
    locale: "zh-CN",
    host: {
      params: { page: "rules" },
      handlers: {
        "desktop.window.dirty": () => {},
        "desktop.dispatch": async (data: unknown) => {
          const request = data as { method: string; params: any };
          calls.push(request);
          if (request.method === "desktop.snapshot") return { result: base };
          if (request.method === "desktop.applications.scan")
            return {
              result: [
                {
                  name: "Browser",
                  path: "/Applications/Browser.app",
                  executable: "/Applications/Browser.app/Contents/MacOS/Browser",
                },
              ],
            };
          if (request.method === "desktop.rules.save") {
            base.appRules = request.params.appRules;
            return { result: {} };
          }
          return { result: (await fixture(request.method, request.params)) ?? {} };
        },
      },
    },
  });
  await app.press("id:desktop-tab-apps");
  await app.press("text:添加应用");
  await app.waitFor("text:选择应用");
  await app.press("text:选择");
  await app.waitFor("text:编辑应用分流");
  expect(app.texts()).toContain("Browser");
  await app.press("text:加入草稿");
  await app.waitFor("id:desktop-nav-rules");
  expect(app.texts()).toContain("Browser");
  await app.press("text:保存并应用");
  await app.waitUntil(() => calls.some((call) => call.method === "desktop.rules.save"));
  expect(calls.find((call) => call.method === "desktop.rules.save")!.params.appRules[0].application.path).toBe(
    "/Applications/Browser.app",
  );
});
