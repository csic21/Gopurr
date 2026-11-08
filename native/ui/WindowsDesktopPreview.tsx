import { mount } from "nsh";
import { DesktopApp } from "./src/desktop/app";
import { chromeInset, desktopPlatform, usePreviewTransport } from "./src/desktop/client";
import { desktopFixtureTransport } from "./src/desktop/fixture";
desktopPlatform.set("windows");
chromeInset.set(0);
usePreviewTransport(desktopFixtureTransport());
mount(DesktopApp);
