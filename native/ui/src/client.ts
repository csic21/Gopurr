import { host } from "nsh";

export interface Status {
  apiVersion: number;
  running: boolean;
  activeProfile: string;
  mixedPort: number;
  mode: "rule" | "global" | "direct";
  systemProxy: boolean;
  tun: boolean;
  vpnSupported: boolean;
  capabilities: { systemProxy: boolean; tun: boolean };
  traffic: { up: number; down: number; upTotal: number; downTotal: number; connections: number; memory: number; at: number };
  error?: string;
  busy?: boolean;
  operation?: { method: string; profileId?: string; phase: string };
  reconnectRequired?: boolean;
}

export interface Profile { id: string; name: string; kind: string; source: string; updated: string; active: boolean; reconnectRequired?: boolean }
export interface Member { name: string; type: string; delay?: number }
export interface Group { name: string; type: string; now: string; hidden: boolean; members: Member[] }
interface Reply<T> { result?: T; error?: { code: string; message: string } }
export class EngineError extends Error {
  constructor(public readonly code: string, message: string) { super(message); this.name = "EngineError"; }
}
export type Transport = (method: string, params: Record<string, unknown>) => Promise<unknown>;

let previewTransport: Transport | undefined;

// Only Preview.tsx installs a fixture. App.tsx always reaches its native host.
export function usePreviewTransport(transport: Transport): void { previewTransport = transport; }
export function bridgeAvailable(): boolean { return !!previewTransport || host.available(); }

export async function call<T>(method: string, params: Record<string, unknown> = {}): Promise<T> {
  if (previewTransport) return await previewTransport(method, params) as T;
  if (!host.available()) throw new Error("The native engine is unavailable. Launch Gopurr from its native app.");
  // Android may be waiting for the user in its system VPN permission dialog.
  // The native service bounds actual connection work after consent is given.
  const reply = await host.call("engine.dispatch", { method, params }, { timeout: method === "engine.start" ? 0 : 45000 }) as Reply<T>;
  if (reply.error) throw new EngineError(reply.error.code, reply.error.message);
  if (!("result" in reply)) throw new Error("The native engine returned an invalid reply.");
  return reply.result as T;
}
