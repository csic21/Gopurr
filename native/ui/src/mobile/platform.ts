import { host } from "nsh";

export interface AppScope { mode: "all" | "include" | "exclude"; packages: string[]; excludeSelf: boolean }
export interface AndroidSession {
  phase: "idle" | "awaitingPermission" | "connecting" | "connected" | "disconnecting" | "failed";
  generation: number;
  busy: boolean;
  connected: boolean;
  reason?: string;
  error?: { code: string; message: string };
  scope: AppScope;
}
export interface InstalledApp { packageName: string; name: string; system: boolean }
export const androidHost = () => host.params.android === true;
export async function androidCall<T>(method: string, params: Record<string, unknown> = {}): Promise<T> {
  // The user may spend arbitrarily long in a system picker/permission dialog.
  // Activity teardown rejects abandoned replies; native work has its own limits.
  const prompt = method === "android.file.pick" || method === "android.notifications.request";
  return await host.call(method, params, { timeout: prompt ? 0 : 45000 }) as T;
}
