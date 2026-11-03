import type { ProxyGroup } from "./types";

export function formatBytes(value: number): string {
  const n = Number(value) || 0;
  if (n < 1024) return `${Math.max(0, Math.round(n))} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i += 1;
  }
  return `${v >= 100 ? v.toFixed(0) : v >= 10 ? v.toFixed(1) : v.toFixed(2)} ${units[i]}`;
}

export const formatRate = (value: number) => `${formatBytes(value)}/s`;

export function formatTime(iso: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  const now = new Date();
  if (d.toDateString() === now.toDateString()) {
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });
  }
  return d.toLocaleString([], { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false });
}

export function latencyClass(delay: number | null): string {
  if (delay == null) return "";
  if (delay === 0) return "bad";
  if (delay < 200) return "good";
  if (delay < 500) return "mid";
  return "bad";
}

export function latencyText(delay: number | null, t: (key: string) => string): string {
  if (delay == null) return t("未测试");
  if (delay === 0) return t("不可用");
  return `${delay} ms`;
}

export function typeLabel(type: string, t: (key: string) => string): string {
  switch (String(type || "").toLowerCase()) {
    case "selector":
      return t("手动选择");
    case "urltest":
    case "url-test":
      return t("自动选择");
    case "fallback":
      return t("故障转移");
    case "loadbalance":
    case "load-balance":
      return t("负载均衡");
    case "relay":
      return t("链式代理");
    case "direct":
      return t("直连");
    default:
      return type || "—";
  }
}

export function resolveExit(groups: ProxyGroup[], group: ProxyGroup): string {
  const byName = new Map(groups.map((item) => [item.name, item]));
  let current: ProxyGroup | undefined = group;
  const seen = new Set<string>();
  while (current && !seen.has(current.name)) {
    seen.add(current.name);
    const kind = String(current.type || "").toLowerCase();
    if (!current.now || kind === "loadbalance" || kind === "load-balance" || kind === "relay") {
      return current.now;
    }
    const next = byName.get(current.now);
    if (!next) return current.now;
    current = next;
  }
  return current?.now ?? "";
}

export function isManual(group: ProxyGroup): boolean {
  return String(group.type || "").toLowerCase() === "selector";
}

export function visibleGroups(groups: ProxyGroup[], mode: string): ProxyGroup[] {
  if (mode === "direct") return [];
  return groups.filter((group) => {
    if (group.hidden) return false;
    if (mode === "global") return group.global;
    return !group.global;
  });
}

export function uniqueNodeCount(groups: ProxyGroup[]): number {
  const groupNames = new Set(groups.map((group) => group.name));
  const builtin = new Set(["DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE"]);
  const names = new Set<string>();
  for (const group of groups) {
    for (const member of group.members) {
      if (!groupNames.has(member.name) && !builtin.has(member.name)) names.add(member.name);
    }
  }
  return names.size;
}
