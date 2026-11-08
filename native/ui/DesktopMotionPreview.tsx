// Explicit visual fixture: the real navigation geometry at its midpoint.
import { mount } from "nsh";
import { DesktopApp } from "./src/desktop/app";
import { usePreviewTransport } from "./src/desktop/client";
import { desktopFixtureTransport } from "./src/desktop/fixture";
usePreviewTransport(desktopFixtureTransport());
mount(()=>DesktopApp({motionProgress:0.5}));
