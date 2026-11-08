import { cleanup, effect, host, lifecycle, signal } from "nsh";
import { bridgeAvailable, call } from "../client";
import type { Group, Profile, Status } from "../client";
import { tr } from "../i18n";
import { androidCall, androidHost } from "./platform";
import type { AndroidSession } from "./platform";

export type Page = "overview" | "profiles" | "proxies" | "settings";
export const page = signal<Page>(["overview", "profiles", "proxies", "settings"].includes(String(host.params.page)) ? host.params.page as Page : "overview");
export const status = signal<Status | null>(null);
export const session = signal<AndroidSession | null>(null);
export const profiles = signal<Profile[]>([]);
export const groups = signal<Group[]>([]);
export const groupsIncomplete = signal(false);
export const tasks = signal<Record<string, boolean>>({});
export const error = signal("");
export const loaded = signal(false);
export const groupName = signal("");
export const query = signal("");
let statusRequest: Promise<void> | undefined;
let listsRequest: Promise<void> | undefined;
let groupsRequest: Promise<void> | undefined;
let connectionGeneration = 0;

export const activeProfile = () => profiles().find(p => p.id === status()?.activeProfile);
export const visibleGroups = () => groups().filter(g => !g.hidden && (status()?.mode === "global" ? g.name === "GLOBAL" : g.name !== "GLOBAL"));
export const selectedGroup = () => visibleGroups().find(g => g.name === groupName()) ?? visibleGroups()[0];
export const members = () => selectedGroup()?.members.filter(m => m.name.toLowerCase().includes(query().trim().toLowerCase())) ?? [];
export const connected = () => session()?.connected ?? (status()?.vpnSupported ? !!status()?.tun : !!status()?.running);
export const connecting = () => session() ? ["awaitingPermission", "connecting"].includes(session()!.phase) : !!tasks().connection;
export const mutationBusy = () => !!status()?.busy || Object.entries(tasks()).some(([k, v]) => v && !k.startsWith("delay") && k !== "connection" && k !== "diagnostics");
export const busy = () => Object.keys(tasks()).find(k => tasks()[k]) ?? "";
export function message(value: unknown): string { return value instanceof Error ? value.message : String(value); }

function acceptStatus(next: Status) {
  if (next.apiVersion !== 1) throw new Error(tr("内核接口版本不兼容，请使用配套应用。", "The engine API is incompatible. Use the matching app build."));
  status.set(next); loaded.set(true);
}

export async function refreshStatus(): Promise<void> {
  if (statusRequest) return statusRequest;
  statusRequest = (async () => { acceptStatus(await call<Status>("engine.status")); })();
  try { await statusRequest; } finally { statusRequest = undefined; }
}

export async function refreshLists(): Promise<void> {
  if (listsRequest) return listsRequest;
  listsRequest = (async () => {
    const [list] = await Promise.all([
      call<{ active: string; profiles: Profile[] }>("profiles.list"),
      refreshGroups(),
    ]);
    profiles.set(list.profiles);
  })();
  try { await listsRequest; } finally { listsRequest = undefined; }
}

async function refreshGroups(): Promise<void> {
  if (groupsRequest) return groupsRequest;
  groupsRequest = (async () => {
    try {
      const result = await call<{ groups: Group[]; incomplete?: boolean }>("proxies.groups");
      groups.set(result.groups); groupsIncomplete.set(!!result.incomplete);
    } catch (value) { groups.set([]); groupsIncomplete.set(false); throw value; }
  })();
  try { await groupsRequest; } finally { groupsRequest = undefined; }
}

export async function refresh(report = true): Promise<void> {
  try {
    await refreshStatus();
    if (lifecycle() !== "active") return;
    await refreshLists();
    if (report) error.set("");
  }
  catch (value) { if (report || !error()) error.set(message(value)); }
}

export async function action(key: string, fn: () => Promise<void>, reload = true): Promise<boolean> {
  if (tasks()[key]) return false;
  tasks.set({ ...tasks(), [key]: true }); error.set("");
  try { await fn(); if (reload) await refresh(false); return true; }
  catch (value) { error.set(message(value)); return false; }
  finally { const next = { ...tasks() }; delete next[key]; tasks.set(next); }
}

export function activate(id: string): void {
  void action("activate", async () => { acceptStatus(await call<Status>("profiles.activate", { id })); });
}

function acceptSession(next: AndroidSession) {
  if (session() && next.generation < session()!.generation) return;
  const previous = session(); session.set(next);
  if (next.error) error.set(next.error.message);
  if (previous?.phase !== next.phase && !next.busy && lifecycle() === "active") void refresh(false);
}

export function toggleEngine(): void {
  // Disconnect remains available while importing, testing or awaiting consent.
  if (session()?.phase === "disconnecting") return;
  if (androidHost()) {
    const stop = connected() || connecting();
    // The host command only acknowledges intent. Its state/traffic refresh may
    // wait behind core startup; it must never hold the Cancel button busy.
    void action("connection", async () => { acceptSession(await androidCall<AndroidSession>(stop ? "android.disconnect" : "android.connect")); }, false);
    return;
  }
  if (tasks().connection) return;
  const generation = ++connectionGeneration;
  void action("connection", async () => {
    const next = await call<Status>(connected() ? "engine.stop" : "engine.start");
    if (generation === connectionGeneration) acceptStatus(next);
  });
}

export async function changeMode(mode: "rule" | "global" | "direct") {
  await action("mode", async () => { acceptStatus(await call<Status>("engine.mode", { mode })); });
}

export async function testMember(name: string) {
  await action(`delay-${name}`, async () => {
    await call("proxies.delay", { name });
  });
}

export function startMobileStore() {
  if (androidHost()) {
    cleanup(host.on("android.session", value => acceptSession(value as unknown as AndroidSession)));
    void androidCall<AndroidSession>("android.session").then(acceptSession).catch(e => error.set(message(e)));
  }
  effect(() => {
    // Remove timers when hidden, rather than repeatedly waking JS to skip work.
    // Returning to the app refreshes immediately, including background changes.
    if (lifecycle() !== "active" || !bridgeAvailable()) return;
    const currentPage = page();
    const interval = currentPage === "overview" ? 1500 : 6000;
    let disposed = false;
    let statusTimer: ReturnType<typeof setTimeout> | undefined;
    let groupsTimer: ReturnType<typeof setTimeout> | undefined;
    void refresh(false);
    async function pollStatus() {
      try { await refreshStatus(); }
      catch (value) { if (!error()) error.set(message(value)); }
      finally { if (!disposed) statusTimer = setTimeout(pollStatus, interval); }
    }
    async function pollGroups() {
      try { if (status()?.running && !mutationBusy()) await refreshGroups(); }
      catch { /* A failed automatic refresh retains the last usable list. */ }
      finally { if (!disposed) groupsTimer = setTimeout(pollGroups, 9000); }
    }
    statusTimer = setTimeout(pollStatus, interval);
    if (currentPage === "proxies") groupsTimer = setTimeout(pollGroups, 9000);
    // Schedule after completion so a slow bridge cannot accumulate poll work.
    return () => { disposed = true; clearTimeout(statusTimer); clearTimeout(groupsTimer); };
  });
  cleanup(() => { connectionGeneration++; });
}
