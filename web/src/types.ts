export type Mode = "rule" | "global" | "direct";
export type Page = "overview" | "proxies" | "connections" | "logs";
export type LogLevel = "all" | "info" | "warning" | "error";
export type Theme = "system" | "light" | "dark";

export interface Traffic {
  up: number;
  down: number;
  upTotal: number;
  downTotal: number;
  at: number;
  stale: boolean;
}

export interface Status {
  version: string;
  kernel: boolean;
  mode: Mode;
  systemProxy: boolean;
  systemProxyAvailable: boolean;
  tun: boolean;
  tunAvailable: boolean;
  tunInstalled: boolean;
  busy: boolean;
  traffic: Traffic;
  connections: number;
  connectionsStale: boolean;
  rules: number;
  proxies: number;
  notice?: string;
  error?: string;
}

export interface Member {
  name: string;
  type: string;
  delay: number | null;
  delayKnown: boolean;
  delayError?: string;
}

export interface ProxyGroup {
  name: string;
  type: string;
  now: string;
  hidden: boolean;
  global: boolean;
  members: Member[];
}

export interface Connection {
  id: string;
  host: string;
  network: string;
  process: string;
  processPath: string;
  source: string;
  destination: string;
  rule: string;
  rulePayload: string;
  chain: string;
  chains: string[];
  up: number;
  down: number;
  start: string;
  sourceIP?: string;
  sourcePort?: string;
  destinationIP?: string;
  destinationPort?: string;
}

export interface LogEntry {
  at: string;
  level: string;
  text: string;
}

export interface DelayResult {
  delay: number;
  error?: string;
}

export interface Session {
  csrf: string;
  lang: string;
  version: string;
}

export interface TrafficSample {
  up: number;
  down: number;
}

export interface ToastMessage {
  text: string;
  bad?: boolean;
}
