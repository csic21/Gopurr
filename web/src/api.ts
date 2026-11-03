import type {
  Connection,
  DelayResult,
  LogEntry,
  LogLevel,
  Mode,
  ProxyGroup,
  Session,
  Status,
} from "./types";

let csrf = "";
let unauthorizedHandler: (() => void) | null = null;

export class ApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

export function setCsrf(value: string) {
  csrf = value;
}

export function onUnauthorized(handler: (() => void) | null) {
  unauthorizedHandler = handler;
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  notifyUnauthorized?: boolean;
}

async function parseBody(response: Response): Promise<unknown> {
  if (response.status === 204) return undefined;
  const text = await response.text();
  if (!text) return undefined;
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (options.body !== undefined) headers["Content-Type"] = "application/json";
  if (options.method && options.method !== "GET" && csrf) headers["X-CSRF-Token"] = csrf;
  const response = await fetch(path, {
    method: options.method ?? "GET",
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    credentials: "same-origin",
    cache: "no-store",
  });
  if (response.status === 401) {
    if (options.notifyUnauthorized !== false) unauthorizedHandler?.();
    throw new ApiError("unauthorized", 401);
  }
  const data = await parseBody(response);
  if (!response.ok) {
    const message =
      data && typeof data === "object" && "error" in data && typeof data.error === "string"
        ? data.error
        : `HTTP ${response.status}`;
    throw new ApiError(message, response.status);
  }
  return data as T;
}

async function loginRequest(token: string): Promise<Session> {
  const response = await fetch("/api/login", {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify({ token }),
    credentials: "same-origin",
    cache: "no-store",
  });
  const data = await parseBody(response);
  if (response.status === 401) throw new ApiError("invalid token", 401);
  if (response.status === 429) throw new ApiError("too many attempts", 429);
  if (!response.ok) {
    const message =
      data && typeof data === "object" && "error" in data && typeof data.error === "string"
        ? data.error
        : `HTTP ${response.status}`;
    throw new ApiError(message, response.status);
  }
  return data as Session;
}

export async function loadI18n(): Promise<Record<string, string>> {
  try {
    const response = await fetch("/api/i18n", { cache: "no-store" });
    if (!response.ok) return {};
    return (await response.json()) as Record<string, string>;
  } catch {
    return {};
  }
}

export async function getSession(): Promise<Session | null> {
  try {
    return await request<Session>("/api/session", { notifyUnauthorized: false });
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) return null;
    throw error;
  }
}

export const api = {
  login: loginRequest,
  session: () => request<Session>("/api/session", { notifyUnauthorized: false }),
  logout: () => request<void>("/api/logout", { method: "POST", body: {} }),
  status: () => request<Status>("/api/status"),
  groups: () => request<{ groups: ProxyGroup[] }>("/api/groups"),
  select: (group: string, member: string) =>
    request<{ ok: boolean }>("/api/select", { method: "POST", body: { group, member } }),
  mode: (mode: Mode) => request<{ ok: boolean }>("/api/mode", { method: "POST", body: { mode } }),
  systemProxy: (on: boolean) =>
    request<{ ok: boolean }>("/api/system-proxy", { method: "POST", body: { on } }),
  tun: (on: boolean) => request<{ ok: boolean }>("/api/tun", { method: "POST", body: { on } }),
  connections: () => request<{ connections: Connection[] }>("/api/connections?limit=500"),
  closeConnection: (id: string) =>
    request<void>(`/api/connections/${encodeURIComponent(id)}`, { method: "DELETE" }),
  closeAllConnections: () => request<void>("/api/connections", { method: "DELETE" }),
  logs: (level: LogLevel) =>
    request<{ logs: LogEntry[] }>(`/api/logs?level=${level}&limit=300`),
  clearLogs: () => request<void>("/api/logs", { method: "DELETE" }),
  delay: (group: string, names: string[]) =>
    request<{ results: Record<string, DelayResult> }>("/api/delay", {
      method: "POST",
      body: { group, names },
    }),
};
