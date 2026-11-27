import { host } from "nsh";

export interface MeshSummary {
  managed: boolean;
  running: boolean;
  state: string;
  selfName?: string;
  selfIP?: string;
  tailnet?: string;
  peerCount: number;
  shieldsUp: boolean;
  authURL?: string;
  accessReady: boolean;
  error?: string;
}

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
  mesh?: MeshSummary;
}

export interface ProfileInfo {
  upload: number;
  download: number;
  total: number;
  expire: number;
}

export interface AggregateRef {
  id: string;
  name: string;
}

export interface AggregateSourceSummary {
  id: string;
  name: string;
  prefix?: string;
}

export interface AggregateSummary {
  sources: AggregateSourceSummary[];
  include?: string;
  exclude?: string;
  dedupe: boolean;
}

export interface Profile {
  id: string;
  name: string;
  kind: string;
  source: string;
  updated: string;
  active: boolean;
  reconnectRequired?: boolean;
  info?: ProfileInfo;
  userAgent?: string;
  updateHours?: number | null;
  useProxy?: boolean;
  isAggregate?: boolean;
  hasPath?: boolean;
  isPureLocal?: boolean;
  nodeCount?: number;
  localNodeCount?: number;
  editedNodeCount?: number;
  editedGroupCount?: number;
  extraRuleCount?: number;
  templateCount?: number;
  lastError?: string;
  dependentAggregates?: AggregateRef[];
  aggregate?: AggregateSummary;
}

export interface ProfileMutation {
  profile?: Profile;
  saved: boolean;
  applied: boolean;
  reconnectRequired?: boolean;
  status: Status;
}

export interface ProfileEditor {
  id: string;
  name: string;
  url: string;
  kind: string;
  info?: ProfileInfo;
  userAgent?: string;
  updateHours?: number | null;
  useProxy?: boolean;
  isAggregate?: boolean;
  hasPath?: boolean;
  isPureLocal?: boolean;
  nodeCount?: number;
  localNodeCount?: number;
  editedNodeCount?: number;
  editedGroupCount?: number;
  extraRuleCount?: number;
  templateCount?: number;
  lastError?: string;
  dependentAggregates?: AggregateRef[];
  aggregate?: AggregateSummary;
}

export interface ParseFailure {
  line: number;
  raw: string;
  reason: string;
}

export interface ParsedProxyNode {
  name: string;
  type: string;
  server: string;
  port: number;
  options: Record<string, unknown>;
}

export interface ParseResult {
  kind: "url" | "nodes" | "clash_yaml" | "empty" | "unknown";
  url?: string;
  yaml?: string;
  decoded?: boolean;
  ambiguousHttp?: boolean;
  nodes?: ParsedProxyNode[];
  failures?: ParseFailure[];
}

export interface NodeEdit {
  source: string;
  localId?: string;
  aliases?: string[];
  name?: string;
  server?: string;
  port?: number;
  options?: Record<string, unknown>;
  delete?: boolean;
}

export interface GroupEdit {
  source?: string;
  aliases?: string[];
  name?: string;
  type?: string;
  members?: string[];
  appendMembers?: string[];
  delete?: boolean;
}

export interface ProxyEdits {
  nodes?: NodeEdit[];
  groups?: GroupEdit[];
}

export interface ProxyBaseNode {
  name: string;
  type: string;
  server: string;
  port: number;
  options?: Record<string, unknown>;
}

export interface ProxyBaseGroup {
  name: string;
  type: string;
  members?: string[];
}

export interface ProxyEditorNode {
  source: string;
  localId?: string;
  name: string;
  type: string;
  server: string;
  port: number;
  options: Record<string, unknown>;
  edited: boolean;
  missing: boolean;
  base?: ProxyBaseNode | null;
}

export interface ProxyEditorGroup {
  source: string;
  name: string;
  type: string;
  members: string[];
  inheritedMembers: string[];
  appendMembers: string[];
  explicit: boolean;
  explicitMembers: boolean;
  edited: boolean;
  missing: boolean;
  deleted: boolean;
  use: string[];
  includeAll: boolean;
  base?: ProxyBaseGroup | null;
}

export interface ProviderNodeInfo {
  provider: string;
  name: string;
  type: string;
}

export interface ProxyEditorPayload {
  id: string;
  profileId: string;
  profileName: string;
  profileKind: string;
  isPureLocal: boolean;
  nodes: ProxyEditorNode[];
  groups: ProxyEditorGroup[];
  edits: ProxyEdits | null;
  options: string[];
  availableNames: string[];
  providerNodes: ProviderNodeInfo[];
}

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
