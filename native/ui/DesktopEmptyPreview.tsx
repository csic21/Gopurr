import { mount } from "nsh";
import { DesktopApp } from "./src/desktop/app";
import { usePreviewTransport } from "./src/desktop/client";
import { desktopFixtureTransport } from "./src/desktop/fixture";

// A deterministic empty workspace for reviewing zero-data states. It shares the
// normal fixture and never reaches a real host, core or stored credential.
const base = desktopFixtureTransport();
usePreviewTransport(async (method, params) => {
  const value = await base(method, params);
  if (method === "desktop.snapshot") {
    return { ...value, connections: [], logs: [], groups: [], profiles: [], activeProfile: "", effectiveRules: [], history: [] };
  }
  return value;
});
mount(DesktopApp);
