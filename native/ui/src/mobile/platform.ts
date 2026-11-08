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
export interface NotificationSettings { granted: boolean; enabled: boolean; canRequest: boolean; traffic: boolean }
type PreviewTransport = (method: string, params: Record<string, unknown>) => Promise<unknown>;
let previewTransport: PreviewTransport | undefined;
// Only AndroidPreview.tsx installs this fixture; the real app requires its host.
export function usePreviewAndroidTransport(transport: PreviewTransport): void { previewTransport = transport; }
export const androidHost = () => !!previewTransport || host.params.android === true;
export async function androidCall<T>(method: string, params: Record<string, unknown> = {}): Promise<T> {
  if (previewTransport) return await previewTransport(method, params) as T;
  // The user may spend arbitrarily long in a system picker/permission dialog.
  // Activity teardown rejects abandoned replies; native work has its own limits.
  const prompt = method === "android.file.pick" || method === "android.notifications.request";
  return await host.call(method, params, { timeout: prompt ? 0 : 45000 }) as T;
}
