import type { GroupEdit, NodeEdit, ParsedProxyNode, ProxyEditorGroup, ProxyEditorNode, ProxyEditorPayload, ProxyEdits } from "../client";
import { tr } from "../i18n";

export const BUILTIN_PROXIES = new Set(["DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE", "PASS-RULE", "GLOBAL"]);

export function isBuiltinProxy(name: string): boolean {
  return BUILTIN_PROXIES.has(name);
}

export interface ProtocolMeta {
  id: string;
  label: string;
  defaultPort: number;
  supportsTls: boolean;
  supportsTransport: boolean;
}

export const PROTOCOLS: ProtocolMeta[] = [
  { id: "vless", label: "VLESS", defaultPort: 443, supportsTls: true, supportsTransport: true },
  { id: "vmess", label: "VMess", defaultPort: 443, supportsTls: true, supportsTransport: true },
  { id: "trojan", label: "Trojan", defaultPort: 443, supportsTls: true, supportsTransport: true },
  { id: "ss", label: "Shadowsocks", defaultPort: 8388, supportsTls: false, supportsTransport: false },
  { id: "hysteria2", label: "Hysteria2", defaultPort: 443, supportsTls: true, supportsTransport: false },
  { id: "tuic", label: "TUIC", defaultPort: 443, supportsTls: true, supportsTransport: false },
  { id: "wireguard", label: "WireGuard", defaultPort: 51820, supportsTls: false, supportsTransport: false },
  { id: "socks5", label: "SOCKS5", defaultPort: 1080, supportsTls: true, supportsTransport: false },
  { id: "http", label: "HTTP", defaultPort: 8080, supportsTls: true, supportsTransport: false },
];

export const SS_CIPHERS = [
  "aes-128-gcm",
  "aes-256-gcm",
  "chacha20-ietf-poly1305",
  "2022-blake3-aes-128-gcm",
  "2022-blake3-aes-256-gcm",
  "2022-blake3-chacha20-poly1305",
];

export const VMESS_CIPHERS = ["auto", "aes-128-gcm", "chacha20-poly1305", "zero", "none"];
export const NETWORK_TYPES = ["tcp", "ws", "grpc", "h2", "httpupgrade"];
export const FINGERPRINTS = ["", "chrome", "firefox", "safari", "ios", "edge", "random"];

export function protocolLabel(type?: string): string {
  const key = String(type ?? "").toLowerCase();
  const found = PROTOCOLS.find(p => p.id === key);
  if (found) return found.label;
  if (key === "shadowsocks") return "Shadowsocks";
  if (key === "hy2") return "Hysteria2";
  if (key === "wg") return "WireGuard";
  return type || "Proxy";
}

export function normalizeProtocolId(type?: string): string {
  const key = String(type ?? "").toLowerCase();
  if (key === "shadowsocks") return "ss";
  if (key === "hy2") return "hysteria2";
  if (key === "wg") return "wireguard";
  return key || "vless";
}

export function mergeDiff(base: unknown, value: unknown): unknown {
  if (JSON.stringify(base) === JSON.stringify(value)) return undefined;
  if (value && typeof value === "object" && !Array.isArray(value)) {
    const baseObj = (base && typeof base === "object" && !Array.isArray(base)) ? base as Record<string, unknown> : {};
    const valObj = value as Record<string, unknown>;
    const out: Record<string, unknown> = {};
    for (const key of new Set([...Object.keys(baseObj), ...Object.keys(valObj)])) {
      if (!(key in valObj)) out[key] = null;
      else {
        const diff = mergeDiff(baseObj[key], valObj[key]);
        if (diff !== undefined) out[key] = diff;
      }
    }
    return Object.keys(out).length ? out : undefined;
  }
  return value;
}

export function cloneEdits(edits?: ProxyEdits | null): ProxyEdits {
  if (!edits) return { nodes: [], groups: [] };
  const cloned = JSON.parse(JSON.stringify(edits)) as ProxyEdits;
  return {
    nodes: Array.isArray(cloned.nodes) ? cloned.nodes : [],
    groups: Array.isArray(cloned.groups) ? cloned.groups : [],
  };
}

export function compactEdits(edits: ProxyEdits): ProxyEdits | null {
  const nodes = (edits.nodes ?? []).filter(Boolean);
  const groups = (edits.groups ?? []).filter(Boolean);
  if (!nodes.length && !groups.length) return null;
  return { ...(nodes.length ? { nodes } : {}), ...(groups.length ? { groups } : {}) };
}

export function extractTransportSummary(opts: Record<string, unknown>, protocol: string): string {
  const p = normalizeProtocolId(protocol);
  if (p === "wireguard") return tr("UDP 隧道", "UDP tunnel");
  if (p === "ss") {
    const udp = opts.udp !== false ? "UDP" : tr("仅 TCP", "TCP only");
    return opts.plugin ? `${String(opts.plugin)} · ${udp}` : udp;
  }
  const parts: string[] = [];
  const tls = opts.tls === true || p === "trojan" || p === "hysteria2" || p === "tuic";
  if (tls) parts.push("TLS");
  if (opts["reality-opts"] && typeof opts["reality-opts"] === "object") parts.push("Reality");
  const net = String(opts.network ?? "tcp").toUpperCase();
  if (p !== "hysteria2" && p !== "tuic") parts.push(net);
  if (opts["skip-cert-verify"] === true) parts.push(tr("跳过校验", "Insecure"));
  return parts.length ? parts.join(" · ") : tr("默认传输", "Default transport");
}

// Lightweight YAML / JSON parser & serializer for the collapsible Advanced YAML Override section.
export function serializeSimpleYaml(obj: Record<string, unknown>, indent = 0): string {
  const keys = Object.keys(obj);
  if (!keys.length) return "";
  const pad = "  ".repeat(indent);
  const lines: string[] = [];
  for (const key of keys) {
    const val = obj[key];
    if (val === undefined) continue;
    if (val === null) {
      lines.push(`${pad}${key}: null`);
    } else if (typeof val === "boolean" || typeof val === "number") {
      lines.push(`${pad}${key}: ${String(val)}`);
    } else if (typeof val === "string") {
      const needsQuote = val === "" || /[:#\n\r\t]|^\s|\s$|^(true|false|null|~|\d+(\.\d+)?)$/i.test(val);
      lines.push(`${pad}${key}: ${needsQuote ? JSON.stringify(val) : val}`);
    } else if (Array.isArray(val)) {
      if (!val.length) {
        lines.push(`${pad}${key}: []`);
      } else if (val.every(item => typeof item !== "object" || item === null)) {
        lines.push(`${pad}${key}: [${val.map(v => typeof v === "string" ? JSON.stringify(v) : String(v)).join(", ")}]`);
      } else {
        lines.push(`${pad}${key}: ${JSON.stringify(val)}`);
      }
    } else if (typeof val === "object") {
      const nested = val as Record<string, unknown>;
      if (!Object.keys(nested).length) {
        lines.push(`${pad}${key}: {}`);
      } else {
        lines.push(`${pad}${key}:`);
        lines.push(serializeSimpleYaml(nested, indent + 1));
      }
    }
  }
  return lines.join("\n");
}

function parseYamlScalar(raw: string): unknown {
  const s = raw.trim();
  if (s === "" || s === "null" || s === "~") return null;
  if (s === "true") return true;
  if (s === "false") return false;
  if (/^-?\d+(\.\d+)?$/.test(s)) return Number(s);
  if ((s.startsWith('"') && s.endsWith('"')) || (s.startsWith("'") && s.endsWith("'"))) {
    if (s.startsWith('"')) {
      try { return JSON.parse(s); } catch { return s.slice(1, -1); }
    }
    return s.slice(1, -1);
  }
  if ((s.startsWith("[") && s.endsWith("]")) || (s.startsWith("{") && s.endsWith("}"))) {
    try { return JSON.parse(s); } catch {
      if (s.startsWith("[") && s.endsWith("]")) {
        const inner = s.slice(1, -1).trim();
        if (!inner) return [];
        return inner.split(",").map(part => parseYamlScalar(part));
      }
    }
  }
  return s;
}

export function parseAdvancedYaml(text: string): { ok: true; value: Record<string, unknown> } | { ok: false; error: string } {
  const trimmed = text.trim();
  if (!trimmed) return { ok: true, value: {} };
  if (trimmed.startsWith("{")) {
    try {
      const parsed = JSON.parse(trimmed);
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
        return { ok: false, error: tr("高级覆写必须是键值映射对象", "Advanced override must be a key-value map") };
      }
      return { ok: true, value: parsed as Record<string, unknown> };
    } catch (e) {
      return { ok: false, error: e instanceof Error ? e.message : tr("JSON/YAML 格式无效", "Invalid JSON/YAML syntax") };
    }
  }
  const lines = text.split(/\r?\n/);
  const root: Record<string, unknown> = {};
  const stack: { indent: number; target: Record<string, unknown> }[] = [{ indent: -1, target: root }];
  for (let i = 0; i < lines.length; i++) {
    const rawLine = lines[i];
    const clean = rawLine.replace(/#.*$/, "");
    if (!clean.trim()) continue;
    const indent = rawLine.search(/\S/);
    const trimmedLine = clean.trim();
    if (trimmedLine.startsWith("- ")) {
      return { ok: false, error: tr(`第 ${i + 1} 行：请使用内联数组语法 key: [a, b]`, `Line ${i + 1}: use inline array syntax key: [a, b]`) };
    }
    const colon = trimmedLine.indexOf(":");
    if (colon <= 0) {
      return { ok: false, error: tr(`第 ${i + 1} 行缺少冒号分隔符`, `Line ${i + 1}: missing ':' separator`) };
    }
    const key = trimmedLine.slice(0, colon).trim().replace(/^["']|["']$/g, "");
    const rest = trimmedLine.slice(colon + 1).trim();
    if (!key) {
      return { ok: false, error: tr(`第 ${i + 1} 行键名不能为空`, `Line ${i + 1}: key cannot be empty`) };
    }
    if (key === "name" || key === "server" || key === "port") {
      return { ok: false, error: tr(`请在基础表单中填写 ${key}，不要放入高级覆写`, `Edit "${key}" in Basic info instead of Advanced YAML`) };
    }
    while (stack.length > 1 && stack[stack.length - 1].indent >= indent) {
      stack.pop();
    }
    const current = stack[stack.length - 1].target;
    if (rest === "") {
      const child: Record<string, unknown> = {};
      current[key] = child;
      stack.push({ indent, target: child });
    } else {
      current[key] = parseYamlScalar(rest);
    }
  }
  return { ok: true, value: root };
}

export function generateLocalId(): string {
  return `local-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
}

export function uniqueNodeName(desired: string, existingNames: Set<string>): string {
  const base = desired.trim() || "Node";
  if (!existingNames.has(base)) return base;
  for (let i = 2; i < 1000; i++) {
    const candidate = `${base} (${i})`;
    if (!existingNames.has(candidate)) return candidate;
  }
  return `${base}-${Date.now()}`;
}

export function findDefaultTargetGroups(editor: ProxyEditorPayload | null, currentGroupName?: string): string[] {
  if (!editor) return [];
  const activeGroups = editor.groups.filter(g => !g.deleted && !g.missing);
  if (currentGroupName) {
    const found = activeGroups.find(g => g.name === currentGroupName && g.type.toLowerCase() === "select");
    if (found) return [found.name];
  }
  const firstSelect = activeGroups.find(g => g.type.toLowerCase() === "select");
  if (firstSelect) return [firstSelect.name];
  return activeGroups[0] ? [activeGroups[0].name] : [];
}

function upsertGroupMembership(
  edits: ProxyEdits,
  group: ProxyEditorGroup,
  nodeName: string,
  include: boolean,
  isPureLocal: boolean,
): void {
  const list = edits.groups ?? [];
  const idx = list.findIndex(ge => (group.source ? ge.source === group.source : !ge.source && ge.name === group.name));
  const existing: GroupEdit | undefined = idx >= 0 ? list[idx] : undefined;

  const explicit = group.explicitMembers || group.explicit || !group.source || isPureLocal || existing?.members !== undefined;
  if (explicit) {
    const currentMembers = existing?.members ? [...existing.members] : [...group.members];
    const has = currentMembers.includes(nodeName);
    if (include === has) return;
    const nextMembers = include ? [...currentMembers, nodeName] : currentMembers.filter(m => m !== nodeName);
    const nextEdit: GroupEdit = existing ? { ...existing, members: nextMembers } : { source: group.source, ...(group.source ? {} : { name: group.name, type: group.type }), members: nextMembers };
    if (idx >= 0) list[idx] = nextEdit;
    else list.push(nextEdit);
    edits.groups = list;
    return;
  }

  // Mode A: AppendMembers (preserves subscription updates)
  const inheritedSet = new Set(group.inheritedMembers);
  const currentAppends = existing?.appendMembers ? [...existing.appendMembers] : [...group.appendMembers];
  if (include) {
    if (inheritedSet.has(nodeName) || currentAppends.includes(nodeName)) return;
    const nextAppends = [...currentAppends, nodeName];
    const nextEdit: GroupEdit = existing ? { ...existing, appendMembers: nextAppends } : { source: group.source, appendMembers: nextAppends };
    if (idx >= 0) list[idx] = nextEdit;
    else list.push(nextEdit);
    edits.groups = list;
  } else {
    if (currentAppends.includes(nodeName)) {
      const nextAppends = currentAppends.filter(m => m !== nodeName);
      if (existing) {
        const nextEdit: GroupEdit = { ...existing, appendMembers: nextAppends.length ? nextAppends : undefined };
        const isEmptyEdit = !nextEdit.name && !nextEdit.type && !nextEdit.members && !(nextEdit.appendMembers?.length) && !nextEdit.delete;
        if (isEmptyEdit) list.splice(idx, 1);
        else list[idx] = nextEdit;
        edits.groups = list;
      }
    } else if (inheritedSet.has(nodeName)) {
      // Removing an inherited member requires converting this group to explicit custom members
      const nextMembers = group.members.filter(m => m !== nodeName);
      const nextEdit: GroupEdit = existing
        ? { ...existing, members: nextMembers, appendMembers: undefined }
        : { source: group.source, members: nextMembers };
      if (idx >= 0) list[idx] = nextEdit;
      else list.push(nextEdit);
      edits.groups = list;
    }
  }
}

export function buildSaveNodeEdits(
  editor: ProxyEditorPayload,
  params: {
    editingNode: ProxyEditorNode | null;
    name: string;
    server: string;
    port: number;
    options: Record<string, unknown>;
    targetGroups: string[];
  },
): ProxyEdits | null {
  const edits = cloneEdits(editor.edits);
  const editing = params.editingNode;
  const finalName = params.name.trim();
  const finalServer = params.server.trim();
  const finalPort = params.port;

  if (!editing) {
    // Creating a new local node
    const localId = generateLocalId();
    const entry: NodeEdit = {
      source: "",
      localId,
      name: finalName,
      server: finalServer,
      port: finalPort,
      options: params.options,
    };
    edits.nodes = [...(edits.nodes ?? []), entry];
  } else if (!editing.source) {
    // Updating an existing local node
    const localId = editing.localId || generateLocalId();
    const prev = (edits.nodes ?? []).find(n => n.localId === localId);
    const aliases = editing.name !== finalName
      ? [...new Set([...(prev?.aliases ?? []), editing.name])].filter(a => a !== finalName)
      : prev?.aliases;
    const entry: NodeEdit = {
      source: "",
      localId,
      ...(aliases?.length ? { aliases } : {}),
      name: finalName,
      server: finalServer,
      port: finalPort,
      options: params.options,
    };
    edits.nodes = [...(edits.nodes ?? []).filter(n => n.localId !== localId), entry];
  } else {
    // Editing an existing subscription or pure-local base node
    const base = editing.base;
    const prev = (edits.nodes ?? []).find(n => n.source === editing.source);
    const aliases = editing.name !== editing.source && editing.name !== finalName
      ? [...new Set([...(prev?.aliases ?? []), editing.name])].filter(a => a !== finalName && a !== editing.source)
      : prev?.aliases;
    const entry: NodeEdit = {
      source: editing.source,
      ...(aliases?.length ? { aliases } : {}),
    };
    if (!base || base.name !== finalName) entry.name = finalName;
    if (!base || base.server !== finalServer) entry.server = finalServer;
    if (!base || base.port !== finalPort) entry.port = finalPort;
    const optDiff = mergeDiff(base?.options ?? {}, params.options);
    if (optDiff && typeof optDiff === "object" && !Array.isArray(optDiff)) {
      entry.options = optDiff as Record<string, unknown>;
    }
    const hasChanges = entry.name !== undefined || entry.server !== undefined || entry.port !== undefined || entry.options !== undefined;
    edits.nodes = (edits.nodes ?? []).filter(n => n.source !== editing.source);
    if (hasChanges) edits.nodes.push(entry);
  }

  // Update group memberships for target groups
  const desiredGroups = new Set(params.targetGroups);
  for (const group of editor.groups) {
    if (group.deleted || group.missing) continue;
    const shouldInclude = desiredGroups.has(group.name);
    const prevName = editing?.name;
    const currentlyIncluded = prevName ? group.members.includes(prevName) : false;
    if (!editing && !shouldInclude) continue;
    if (editing && currentlyIncluded === shouldInclude) continue;
    upsertGroupMembership(edits, group, finalName, shouldInclude, editor.isPureLocal);
  }

  return compactEdits(edits);
}

export function buildBatchAddNodesEdits(
  editor: ProxyEditorPayload,
  nodes: ParsedProxyNode[],
  targetGroups: string[],
): ProxyEdits | null {
  const edits = cloneEdits(editor.edits);
  const existingNames = new Set(editor.nodes.map(n => n.name));
  for (const g of editor.groups) existingNames.add(g.name);
  const addedNames: string[] = [];

  for (const node of nodes) {
    const name = uniqueNodeName(node.name || `${protocolLabel(node.type)}-${node.server}`, existingNames);
    existingNames.add(name);
    addedNames.push(name);
    const opts: Record<string, unknown> = { ...(node.options ?? {}), type: node.type };
    delete opts.name;
    delete opts.server;
    delete opts.port;
    edits.nodes = [...(edits.nodes ?? []), {
      source: "",
      localId: generateLocalId(),
      name,
      server: node.server,
      port: node.port,
      options: opts,
    }];
  }

  const desiredGroups = new Set(targetGroups);
  for (const group of editor.groups) {
    if (group.deleted || group.missing || !desiredGroups.has(group.name)) continue;
    for (const nodeName of addedNames) {
      upsertGroupMembership(edits, group, nodeName, true, editor.isPureLocal);
    }
  }

  return compactEdits(edits);
}

export function buildDeleteNodeEdits(editor: ProxyEditorPayload, node: ProxyEditorNode): ProxyEdits | null {
  const edits = cloneEdits(editor.edits);
  if (!node.source) {
    edits.nodes = (edits.nodes ?? []).filter(n => n.localId !== node.localId);
  } else if (editor.isPureLocal) {
    edits.nodes = [
      ...(edits.nodes ?? []).filter(n => n.source !== node.source),
      { source: node.source, delete: true },
    ];
  }
  // Clean group references for immediate consistency (backend also prunes transactionally)
  const targetNames = new Set([node.name, node.source].filter(Boolean));
  for (const group of editor.groups) {
    if (group.deleted || group.missing) continue;
    if (group.members.some(m => targetNames.has(m)) || group.appendMembers.some(m => targetNames.has(m))) {
      for (const n of targetNames) {
        upsertGroupMembership(edits, group, n, false, editor.isPureLocal);
      }
    }
  }
  return compactEdits(edits);
}

export function buildResetNodeEdits(editor: ProxyEditorPayload, node: ProxyEditorNode): ProxyEdits | null {
  const edits = cloneEdits(editor.edits);
  edits.nodes = (edits.nodes ?? []).filter(n => (node.source ? n.source !== node.source : n.localId !== node.localId));
  return compactEdits(edits);
}

export function buildAssignGroupsEdits(editor: ProxyEditorPayload, nodeNames: string[], groupNames: string[], mode: "set" | "add"): ProxyEdits | null {
  const edits = cloneEdits(editor.edits);
  const selectedGroups = new Set(groupNames);
  for (const group of editor.groups) {
    if (group.deleted || group.missing) continue;
    for (const nodeName of nodeNames) {
      if (mode === "add") {
        if (selectedGroups.has(group.name)) {
          upsertGroupMembership(edits, group, nodeName, true, editor.isPureLocal);
        }
      } else {
        const shouldInclude = selectedGroups.has(group.name);
        const currentlyIncluded = group.members.includes(nodeName);
        if (shouldInclude !== currentlyIncluded) {
          upsertGroupMembership(edits, group, nodeName, shouldInclude, editor.isPureLocal);
        }
      }
    }
  }
  return compactEdits(edits);
}

export function buildSaveGroupEdits(
  editor: ProxyEditorPayload,
  params: {
    editingGroup: ProxyEditorGroup | null;
    name: string;
    type: string;
    explicitMode: boolean;
    members: string[];
    appendMembers: string[];
  },
): ProxyEdits | null {
  const edits = cloneEdits(editor.edits);
  const editing = params.editingGroup;
  const finalName = params.name.trim();
  const list = edits.groups ?? [];

  if (!editing) {
    list.push({
      source: "",
      name: finalName,
      type: params.type,
      members: params.members,
    });
    edits.groups = list;
    return compactEdits(edits);
  }

  const prev = list.find(g => (editing.source ? g.source === editing.source : !g.source && g.name === editing.name));
  const filtered = list.filter(g => !(editing.source ? g.source === editing.source : !g.source && g.name === editing.name));

  if (!editing.source) {
    filtered.push({
      source: "",
      name: finalName,
      type: params.type,
      members: params.members,
    });
    edits.groups = filtered;
    return compactEdits(edits);
  }

  const base = editing.base;
  const aliases = editing.name !== editing.source && editing.name !== finalName
    ? [...new Set([...(prev?.aliases ?? []), editing.name])].filter(a => a !== finalName && a !== editing.source)
    : prev?.aliases;

  const entry: GroupEdit = {
    source: editing.source,
    ...(aliases?.length ? { aliases } : {}),
  };
  if (!base || base.name !== finalName) entry.name = finalName;
  if (!base || base.type !== params.type) entry.type = params.type;

  if (params.explicitMode) {
    entry.members = params.members;
  } else if (params.appendMembers.length > 0) {
    entry.appendMembers = params.appendMembers;
  }

  const hasChanges = entry.name !== undefined || entry.type !== undefined || entry.members !== undefined || (entry.appendMembers?.length ?? 0) > 0;
  if (hasChanges) filtered.push(entry);
  edits.groups = filtered;
  return compactEdits(edits);
}

export function buildDeleteGroupEdits(editor: ProxyEditorPayload, group: ProxyEditorGroup): ProxyEdits | null {
  const edits = cloneEdits(editor.edits);
  const list = (edits.groups ?? []).filter(g => !(group.source ? g.source === group.source : !g.source && g.name === group.name));
  if (group.source) {
    list.push({ source: group.source, delete: true });
  }
  edits.groups = list;
  return compactEdits(edits);
}

export function buildResetGroupEdits(editor: ProxyEditorPayload, group: ProxyEditorGroup): ProxyEdits | null {
  const edits = cloneEdits(editor.edits);
  edits.groups = (edits.groups ?? []).filter(g => !(group.source ? g.source === group.source : !g.source && g.name === group.name));
  return compactEdits(edits);
}

// Cycle detection: returns true if adding `candidate` into `groupName` creates a cycle.
export function wouldCreateGroupCycle(
  groups: ProxyEditorGroup[],
  groupName: string,
  candidate: string,
): boolean {
  if (!groupName || !candidate) return false;
  if (candidate === groupName) return true;
  const byName = new Map<string, string[]>();
  for (const g of groups) {
    if (!g.deleted && !g.missing) byName.set(g.name, g.members);
  }
  const visited = new Set<string>();
  const queue = [candidate];
  while (queue.length) {
    const cur = queue.shift()!;
    if (cur === groupName) return true;
    if (visited.has(cur)) continue;
    visited.add(cur);
    const next = byName.get(cur);
    if (next) {
      for (const m of next) queue.push(m);
    }
  }
  return false;
}
