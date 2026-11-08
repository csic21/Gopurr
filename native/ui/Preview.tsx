import { mount } from "nsh";
import { GopurrApp } from "./src/app";
import { usePreviewTransport } from "./src/client";
import { fixtureTransport } from "./src/fixture";

usePreviewTransport(fixtureTransport());
mount(() => <GopurrApp preview />);
