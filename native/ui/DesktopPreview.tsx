import { mount } from "nsh";
import { DesktopApp } from "./src/desktop/app";
import { usePreviewTransport } from "./src/desktop/client";
import { desktopFixtureTransport } from "./src/desktop/fixture";
usePreviewTransport(desktopFixtureTransport());
mount(DesktopApp);
