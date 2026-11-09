import { expect, open, test } from "nsh/test";
import { desktopFixtureTransport } from "./src/desktop/fixture";

test("desktop proxy list keeps its visible rows through fresh snapshots", async () => {
  const fixture = desktopFixtureTransport();
  const base: any = await fixture("desktop.snapshot");
  const nodes = Array.from({ length: 120 }, (_, index) => ({
    name: `节点 ${String(index).padStart(3, "0")}`,
    type: "Vless",
    delay: 100,
    delayKnown: true,
  }));
  base.groups = [{ name: "节点选择", type: "Selector", now: "节点 060", hidden: false, members: nodes }];
  let revision = 0;
  const app = await open("./Desktop.tsx", {
    width: 840,
    height: 560,
    platform: "macos",
    locale: "zh-CN",
    host: {
      params: { page: "proxies" },
      handlers: {
        "desktop.window.dirty": () => {},
        "desktop.dispatch": async (data: unknown) => {
          const request = data as { method: string; params: any };
          if (request.method === "desktop.snapshot") {
            revision++;
            const next = JSON.parse(JSON.stringify(base));
            for (const node of next.groups[0].members) node.delay = 100 + revision * 7;
            return { result: next };
          }
          return { result: (await fixture(request.method, request.params)) ?? {} };
        },
      },
    },
  });
  await app.waitFor("id:proxy-节点 000");
  await app.scroll("id:proxies-list", 500);
  await app.waitFor("id:proxy-节点 009");
  const names = Array.from({ length: 120 }, (_, index) => `节点 ${String(index).padStart(3, "0")}`);
  const visible = () => names.filter((name) => app.shows(`id:proxy-${name}`));
  const before = visible();
  const beforeTexts = app.texts();
  await app.wait(2400);
  const after = visible();
  expect(before.length).toBeGreaterThan(0);
  expect(after).toEqual(before);
  expect(app.texts()).not.toEqual(beforeTexts);
});
