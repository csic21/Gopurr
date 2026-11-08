// Same transition geometry with English labels and the native titlebar inset.
import { mount } from "nsh";
import { DesktopApp } from "./src/desktop/app";
import { usePreviewTransport } from "./src/desktop/client";
import { desktopFixtureTransport } from "./src/desktop/fixture";
usePreviewTransport(desktopFixtureTransport("en"));
mount(()=>DesktopApp({motionProgress:0.5}));
