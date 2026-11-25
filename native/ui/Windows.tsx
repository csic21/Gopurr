import { host, mount, signal } from "nsh";
import { Window } from "nsh/desktop";
import { DesktopApp } from "./src/desktop/app";
import { closeRequested, nativeCall, state } from "./src/desktop/client";

// NSH 0.7 opens a desktop app's own windows. The Win32 host keeps the message
// loop and tray, but it stays invisible: this is the app's only visible window.
const open = signal(host.params.showWindow !== false);
let main: import("nsh/desktop").WindowRef | undefined;
host.on("desktop.event", (payload) => {
  const event = payload as { kind?: string };
  if (event?.kind === "show") {
    open.set(true);
    queueMicrotask(() => main?.focus());
  } else if (event?.kind === "hide") {
    main?.hide();
  }
});

mount(() => (
  <Window
    id="main"
    title="Gopurr"
    open={open()}
    ref={(window) => {
      main = window;
    }}
    defaultSize={{ width: 1060, height: 720 }}
    minSize={{ width: 840, height: 560 }}
    closeOnPress={false}
    onClosePress={() => {
      if (state().prefs.closeToTrayAcknowledged) {
        void nativeCall("desktop.window.close");
      } else {
        closeRequested.set(true);
      }
    }}
  >
    <DesktopApp />
  </Window>
));
