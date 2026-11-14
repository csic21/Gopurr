import { expect, test } from "nsh/test";
import { mergeDesktopSnapshot } from "./src/desktop/snapshot";

function snapshot(revision: number) {
  return {
    apiVersion: 2,
    status: { kernel: true, traffic: { up: revision, down: revision * 2 }, connections: revision },
    history: [{ up: revision, down: revision * 2 }],
    prefs: { appearance: "dark", language: "en" },
    profiles: [{ id: "daily", name: "Daily", updated: "2026-11-14" }],
    groups: [{ name: "Route", now: "One", members: [{ name: "One" }] }],
  };
}

test("identical snapshots keep the previous object identity", () => {
  const previous = snapshot(1);
  const next = snapshot(1);
  expect(mergeDesktopSnapshot(previous, next, "settings")).toBe(previous);
});

test("static pages ignore live traffic while retaining changed settings", () => {
  const previous = snapshot(1);
  const next = snapshot(2);
  next.prefs.language = "zh-Hans";
  const merged = mergeDesktopSnapshot(previous, next, "settings");
  expect(merged.status.traffic.up).toBe(1);
  expect(merged.status.connections).toBe(1);
  expect(merged.history).toBe(previous.history);
  expect(merged.prefs.language).toBe("zh-Hans");
  expect(merged.prefs).not.toBe(previous.prefs);
});

test("page-specific fields do not survive a page switch as undefined keys", () => {
  const previous = snapshot(1);
  const next: any = { apiVersion: 2, status: previous.status, prefs: previous.prefs };
  const merged = mergeDesktopSnapshot(previous, next, "settings");
  expect(Object.keys(merged).sort()).toEqual(["apiVersion", "prefs", "status"]);
  expect("groups" in merged).toBe(false);
  expect("profiles" in merged).toBe(false);
});

test("unchanged subtrees survive a changed sibling", () => {
  const previous = snapshot(1);
  const next = snapshot(2);
  const merged = mergeDesktopSnapshot(previous, next, "overview");
  expect(merged.status.traffic.up).toBe(2);
  expect(merged.history).not.toBe(previous.history);
  expect(merged.profiles).toBe(previous.profiles);
  expect(merged.prefs).toBe(previous.prefs);
});
