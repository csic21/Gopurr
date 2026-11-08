export type Page = "overview" | "subs" | "proxies" | "conns" | "mesh" | "rules" | "logs" | "settings";
export interface DesktopSnapshot extends Record<string, any> {
  apiVersion: number; status: Record<string, any>; prefs: Record<string, any>; network: Record<string, any>; features: Record<string, any>;
  profiles: any[]; groups: any[]; history: {up:number;down:number}[]; service: Record<string, any>; capabilities: Record<string, any>;
}
export interface PageProps {
  snapshot: () => DesktopSnapshot;
  call: <T = any>(method: string, params?: any) => Promise<T>;
  refresh: () => Promise<void>;
  busy: () => string;
  run: (key: string, fn: () => Promise<any>) => Promise<void>;
  navigate: (page: Page) => void;
}
