import { tr } from "./i18n";

export const BUILTIN_PROXIES = new Set(["DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE"]);

export function isBuiltinNode(name: string): boolean {
  return BUILTIN_PROXIES.has(name);
}

export const PROTOCOLS = [
  { value: "vless", title: "VLESS" },
  { value: "vmess", title: "VMess" },
  { value: "trojan", title: "Trojan" },
  { value: "ss", title: "Shadowsocks (SS)" },
  { value: "hysteria2", title: "Hysteria2" },
  { value: "tuic", title: "TUIC" },
  { value: "wireguard", title: "WireGuard" },
  { value: "socks5", title: "SOCKS5" },
  { value: "http", title: "HTTP" },
] as const;

export const SS_CIPHERS = [
  "aes-128-gcm",
  "aes-256-gcm",
  "chacha20-ietf-poly1305",
  "2022-blake3-aes-128-gcm",
  "2022-blake3-aes-256-gcm",
  "2022-blake3-chacha20-poly1305",
  "none",
];

export const VMESS_CIPHERS = ["auto", "aes-128-gcm", "chacha20-poly1305", "none"];

export const TRANSPORTS = [
  { value: "tcp", title: "TCP" },
  { value: "ws", title: "WebSocket (WS)" },
  { value: "grpc", title: "gRPC" },
  { value: "h2", title: "HTTP/2 (H2)" },
  { value: "httpupgrade", title: "HTTPUpgrade" },
];

export const FINGERPRINTS = [
  { value: "", title: "Default" },
  { value: "chrome", title: "Chrome" },
  { value: "firefox", title: "Firefox" },
  { value: "safari", title: "Safari" },
  { value: "ios", title: "iOS" },
  { value: "android", title: "Android" },
  { value: "edge", title: "Edge" },
  { value: "random", title: "Random" },
];

export function mergeDiff(base: any, value: any): any {
  if (JSON.stringify(base) === JSON.stringify(value)) return undefined;
  if (value && typeof value === "object" && !Array.isArray(value)) {
    const out: Record<string, any> = {};
    for (const key of new Set([...Object.keys(base ?? {}), ...Object.keys(value)])) {
      if (!(key in value)) out[key] = null;
      else {
        const d = mergeDiff(base?.[key], value[key]);
        if (d !== undefined) out[key] = d;
      }
    }
    return Object.keys(out).length ? out : undefined;
  }
  return value;
}

export interface StructuredNodeForm {
  type: string;
  name: string;
  server: string;
  port: string;
  // Auth
  uuid: string;
  password: string;
  username: string;
  cipher: string;
  alterId: string;
  flow: string;
  privateKey: string;
  publicKey: string;
  ip: string;
  ipv6: string;
  obfs: string;
  obfsPassword: string;
  congestionController: string;
  // Transport & Security
  tls: boolean;
  sni: string;
  fingerprint: string;
  alpn: string;
  skipCertVerify: boolean;
  realityPublicKey: string;
  realityShortId: string;
  network: string;
  wsPath: string;
  wsHost: string;
  grpcServiceName: string;
  h2Path: string;
  h2Host: string;
  // Extra YAML override for fields not represented above
  advancedYaml: string;
}

const STRUCTURED_OPTION_KEYS = new Set([
  "name", "type", "server", "port",
  "uuid", "password", "username", "cipher", "alterId", "flow",
  "private-key", "public-key", "ip", "ipv6", "obfs", "obfs-password", "congestion-controller",
  "tls", "servername", "sni", "client-fingerprint", "alpn", "skip-cert-verify",
  "reality-opts", "network", "ws-opts", "grpc-opts", "h2-opts", "http-opts",
]);

function parseScalar(raw: string): any {
  const s = raw.trim();
  if (s === "" || s === "null" || s === "~") return null;
  if (s === "true") return true;
  if (s === "false") return false;
  if (/^-?\d+(\.\d+)?$/.test(s)) return Number(s);
  if ((s.startsWith('"') && s.endsWith('"')) || (s.startsWith("'") && s.endsWith("'"))) {
    return s.slice(1, -1);
  }
  if ((s.startsWith("[") && s.endsWith("]")) || (s.startsWith("{") && s.endsWith("}"))) {
    try {
      return JSON.parse(s);
    } catch {
      if (s.startsWith("[") && s.endsWith("]")) {
        return s.slice(1, -1).split(",").map((item) => parseScalar(item)).filter((v) => v !== null && v !== "");
      }
    }
  }
  return s;
}

/** Lightweight YAML object parser for the Advanced YAML Override box (maps, nested maps, lists, scalars). */
export function parseSimpleYaml(text: string): Record<string, any> {
  const trimmed = text.trim();
  if (!trimmed) return {};
  if (trimmed.startsWith("{")) {
    const parsed = JSON.parse(trimmed);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      throw new Error(tr("高级 YAML 覆写必须是键值对映射", "Advanced YAML override must be a key-value mapping"));
    }
    return parsed;
  }
  const lines = text.split(/\r?\n/);
  const root: Record<string, any> = {};
  const stack: { indent: number; target: any; key?: string }[] = [{ indent: -1, target: root }];

  for (let i = 0; i < lines.length; i++) {
    const rawLine = lines[i];
    const withoutComment = rawLine.replace(/\s+#.*$/, "");
    if (!withoutComment.trim() || withoutComment.trim().startsWith("#")) continue;
    const indent = rawLine.search(/\S/);
    const content = withoutComment.trim();

    while (stack.length > 1 && indent <= stack[stack.length - 1].indent) {
      stack.pop();
    }
    const parent = stack[stack.length - 1];

    if (content.startsWith("- ")) {
      const val = parseScalar(content.slice(2));
      if (Array.isArray(parent.target)) {
        parent.target.push(val);
      } else if (parent.key && parent.target && typeof parent.target === "object") {
        if (!Array.isArray(parent.target[parent.key])) parent.target[parent.key] = [];
        parent.target[parent.key].push(val);
      } else {
        throw new Error(tr(`第 ${i + 1} 行 YAML 列表缺少父键`, `YAML list on line ${i + 1} has no parent key`));
      }
      continue;
    }

    const colon = content.indexOf(":");
    if (colon <= 0) {
      throw new Error(tr(`第 ${i + 1} 行 YAML 格式无效：缺少冒号`, `Invalid YAML on line ${i + 1}: missing colon`));
    }
    const key = content.slice(0, colon).trim().replace(/^['"]|['"]$/g, "");
    const rest = content.slice(colon + 1).trim();
    if (!parent.target || typeof parent.target !== "object" || Array.isArray(parent.target)) {
      throw new Error(tr(`第 ${i + 1} 行 YAML 缩进无效`, `Invalid YAML indentation on line ${i + 1}`));
    }
    if (rest === "") {
      // Peek next non-empty line to see if it's a list or nested object
      let nextIsList = false;
      for (let j = i + 1; j < lines.length; j++) {
        const nextLine = lines[j].trim();
        if (!nextLine || nextLine.startsWith("#")) continue;
        nextIsList = nextLine.startsWith("- ");
        break;
      }
      const child = nextIsList ? [] : {};
      parent.target[key] = child;
      stack.push({ indent, target: child, key });
    } else {
      parent.target[key] = parseScalar(rest);
    }
  }
  return root;
}

function formatScalar(val: any): string {
  if (val === null || val === undefined) return "null";
  if (typeof val === "boolean" || typeof val === "number") return String(val);
  const s = String(val);
  if (/[:#\n\[\]{},]|^\s|\s$/.test(s) || s === "" || s === "true" || s === "false" || s === "null") {
    return JSON.stringify(s);
  }
  return s;
}

export function stringifySimpleYaml(obj: Record<string, any>, indent = 0): string {
  const pad = " ".repeat(indent);
  const lines: string[] = [];
  for (const [key, val] of Object.entries(obj)) {
    if (val === undefined) continue;
    if (Array.isArray(val)) {
      if (!val.length) {
        lines.push(`${pad}${key}: []`);
      } else {
        lines.push(`${pad}${key}:`);
        for (const item of val) {
          if (item && typeof item === "object") {
            lines.push(`${pad}  - ${JSON.stringify(item)}`);
          } else {
            lines.push(`${pad}  - ${formatScalar(item)}`);
          }
        }
      }
    } else if (val && typeof val === "object") {
      const entries = Object.entries(val).filter(([, v]) => v !== undefined);
      if (!entries.length) {
        lines.push(`${pad}${key}: {}`);
      } else {
        lines.push(`${pad}${key}:`);
        lines.push(stringifySimpleYaml(val, indent + 2));
      }
    } else {
      lines.push(`${pad}${key}: ${formatScalar(val)}`);
    }
  }
  return lines.join("\n");
}

export function extractNodeForm(row: any): StructuredNodeForm {
  const opts: Record<string, any> = { ...(row?.options ?? {}) };
  const rawType = String(opts.type ?? row?.type ?? "vless").toLowerCase();
  const type = rawType === "shadowsocks" ? "ss" : rawType === "hy2" ? "hysteria2" : rawType;
  const reality = opts["reality-opts"] && typeof opts["reality-opts"] === "object" ? opts["reality-opts"] : {};
  const wsOpts = opts["ws-opts"] && typeof opts["ws-opts"] === "object" ? opts["ws-opts"] : {};
  const wsHeaders = wsOpts.headers && typeof wsOpts.headers === "object" ? wsOpts.headers : {};
  const grpcOpts = opts["grpc-opts"] && typeof opts["grpc-opts"] === "object" ? opts["grpc-opts"] : {};
  const h2Opts = opts["h2-opts"] && typeof opts["h2-opts"] === "object" ? opts["h2-opts"] : {};
  const alpnList = Array.isArray(opts.alpn) ? opts.alpn.join(", ") : typeof opts.alpn === "string" ? opts.alpn : "";
  const extra: Record<string, any> = {};
  for (const [k, v] of Object.entries(opts)) {
    if (!STRUCTURED_OPTION_KEYS.has(k)) extra[k] = v;
  }
  const hasTls = Boolean(opts.tls || type === "trojan" || type === "hysteria2" || type === "tuic");
  const sniKey = type === "trojan" || type === "hysteria2" || type === "tuic" ? (opts.sni ?? opts.servername ?? "") : (opts.servername ?? opts.sni ?? "");
  return {
    type,
    name: String(row?.name ?? ""),
    server: String(row?.server ?? opts.server ?? ""),
    port: row?.port ? String(row.port) : opts.port ? String(opts.port) : "443",
    uuid: String(opts.uuid ?? ""),
    password: String(opts.password ?? ""),
    username: String(opts.username ?? ""),
    cipher: String(opts.cipher ?? (type === "vmess" ? "auto" : "aes-128-gcm")),
    alterId: opts.alterId !== undefined ? String(opts.alterId) : "0",
    flow: String(opts.flow ?? ""),
    privateKey: String(opts["private-key"] ?? ""),
    publicKey: String(opts["public-key"] ?? ""),
    ip: String(opts.ip ?? ""),
    ipv6: String(opts.ipv6 ?? ""),
    obfs: String(opts.obfs ?? ""),
    obfsPassword: String(opts["obfs-password"] ?? ""),
    congestionController: String(opts["congestion-controller"] ?? ""),
    tls: hasTls,
    sni: String(sniKey),
    fingerprint: String(opts["client-fingerprint"] ?? ""),
    alpn: alpnList,
    skipCertVerify: Boolean(opts["skip-cert-verify"]),
    realityPublicKey: String(reality["public-key"] ?? ""),
    realityShortId: String(reality["short-id"] ?? ""),
    network: String(opts.network ?? "tcp").toLowerCase(),
    wsPath: String(wsOpts.path ?? ""),
    wsHost: String(wsHeaders.Host ?? wsHeaders.host ?? ""),
    grpcServiceName: String(grpcOpts["grpc-service-name"] ?? ""),
    h2Path: String(h2Opts.path ?? ""),
    h2Host: Array.isArray(h2Opts.host) ? h2Opts.host.join(", ") : String(h2Opts.host ?? ""),
    advancedYaml: Object.keys(extra).length ? stringifySimpleYaml(extra) : "",
  };
}

export function buildNodeValue(form: StructuredNodeForm): { name: string; type: string; server: string; port: number; options: Record<string, any> } {
  const server = form.server.trim();
  const port = Number(form.port.trim());
  if (!server) throw new Error(tr("请填写服务器地址", "Server address is required"));
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(tr("端口必须是 1 到 65535 之间的整数", "Port must be an integer between 1 and 65535"));
  }
  const type = form.type.trim().toLowerCase() || "vless";
  const name = form.name.trim() || `${type.toUpperCase()}-${server}`;
  const extra = parseSimpleYaml(form.advancedYaml);
  const options: Record<string, any> = { ...extra, type };

  // Protocol auth
  if (type === "vless") {
    if (form.uuid.trim()) options.uuid = form.uuid.trim();
    if (form.flow.trim()) options.flow = form.flow.trim();
  } else if (type === "vmess") {
    if (form.uuid.trim()) options.uuid = form.uuid.trim();
    options.cipher = form.cipher.trim() || "auto";
    options.alterId = Number(form.alterId.trim() || "0") || 0;
  } else if (type === "trojan") {
    if (form.password.trim()) options.password = form.password.trim();
  } else if (type === "ss") {
    options.cipher = form.cipher.trim() || "aes-128-gcm";
    if (form.password.trim()) options.password = form.password.trim();
  } else if (type === "hysteria2") {
    if (form.password.trim()) options.password = form.password.trim();
    if (form.obfs.trim()) options.obfs = form.obfs.trim();
    if (form.obfsPassword.trim()) options["obfs-password"] = form.obfsPassword.trim();
  } else if (type === "tuic") {
    if (form.uuid.trim()) options.uuid = form.uuid.trim();
    if (form.password.trim()) options.password = form.password.trim();
    if (form.congestionController.trim()) options["congestion-controller"] = form.congestionController.trim();
  } else if (type === "wireguard") {
    if (form.privateKey.trim()) options["private-key"] = form.privateKey.trim();
    if (form.publicKey.trim()) options["public-key"] = form.publicKey.trim();
    if (form.ip.trim()) options.ip = form.ip.trim();
    if (form.ipv6.trim()) options.ipv6 = form.ipv6.trim();
  } else if (type === "socks5" || type === "http") {
    if (form.username.trim()) options.username = form.username.trim();
    if (form.password.trim()) options.password = form.password.trim();
  } else {
    if (form.password.trim()) options.password = form.password.trim();
    if (form.uuid.trim()) options.uuid = form.uuid.trim();
  }

  // Transport & Security
  const implicitTls = type === "trojan" || type === "hysteria2" || type === "tuic";
  if (form.tls && !implicitTls) options.tls = true;
  if (!form.tls && !implicitTls && "tls" in options) delete options.tls;
  if (form.sni.trim()) {
    if (implicitTls) options.sni = form.sni.trim();
    else options.servername = form.sni.trim();
  }
  if (form.fingerprint.trim()) options["client-fingerprint"] = form.fingerprint.trim();
  if (form.alpn.trim()) {
    const alpn = form.alpn.split(",").map((s) => s.trim()).filter(Boolean);
    if (alpn.length) options.alpn = alpn;
  }
  if (form.skipCertVerify) options["skip-cert-verify"] = true;
  if (form.realityPublicKey.trim() || form.realityShortId.trim()) {
    const r: Record<string, any> = {};
    if (form.realityPublicKey.trim()) r["public-key"] = form.realityPublicKey.trim();
    if (form.realityShortId.trim()) r["short-id"] = form.realityShortId.trim();
    options["reality-opts"] = r;
  }
  const net = form.network.trim().toLowerCase();
  if (net && net !== "tcp" && type !== "wireguard" && type !== "hysteria2" && type !== "tuic") {
    options.network = net;
    if (net === "ws" || net === "httpupgrade") {
      const ws: Record<string, any> = {};
      if (form.wsPath.trim()) ws.path = form.wsPath.trim();
      if (form.wsHost.trim()) ws.headers = { Host: form.wsHost.trim() };
      if (Object.keys(ws).length) options["ws-opts"] = ws;
    } else if (net === "grpc") {
      if (form.grpcServiceName.trim()) options["grpc-opts"] = { "grpc-service-name": form.grpcServiceName.trim() };
    } else if (net === "h2") {
      const h2: Record<string, any> = {};
      if (form.h2Path.trim()) h2.path = form.h2Path.trim();
      if (form.h2Host.trim()) h2.host = form.h2Host.split(",").map((s) => s.trim()).filter(Boolean);
      if (Object.keys(h2).length) options["h2-opts"] = h2;
    }
  }

  return { name, type, server, port, options };
}

export function transportSummary(form: StructuredNodeForm): string {
  const parts: string[] = [];
  if (form.tls || ["trojan", "hysteria2", "tuic"].includes(form.type)) parts.push("TLS");
  if (form.realityPublicKey.trim()) parts.push("Reality");
  if (form.network && form.network !== "tcp") parts.push(form.network.toUpperCase());
  else if (!["wireguard", "hysteria2", "tuic"].includes(form.type)) parts.push("TCP");
  if (form.sni.trim()) parts.push(form.sni.trim());
  return parts.join(" · ") || tr("默认", "Default");
}

/** Client-side controlled concurrency pool for latency testing (decoupled from global p.busy). */
export async function runConcurrentPool<T>(
  items: T[],
  concurrency: number,
  worker: (item: T) => Promise<void>,
  isCancelled: () => boolean,
  onProgress?: (done: number, total: number) => void,
): Promise<void> {
  const total = items.length;
  if (!total) return;
  const limit = Math.max(1, Math.min(8, concurrency || 6, total));
  let cursor = 0;
  let done = 0;
  onProgress?.(0, total);
  const runners = Array.from({ length: limit }, async () => {
    while (!isCancelled()) {
      const idx = cursor++;
      if (idx >= total) break;
      try {
        await worker(items[idx]);
      } catch {
        // Individual node timeout/error shouldn't stop the rest of the batch.
      } finally {
        done++;
        if (!isCancelled()) onProgress?.(done, total);
      }
    }
  });
  await Promise.all(runners);
}

/** Relative update time helper for subscription cards. */
export function fmtRelativeTime(iso?: string): string {
  if (!iso) return tr("从未更新", "Never updated");
  const ts = Date.parse(iso);
  if (Number.isNaN(ts)) return iso;
  const diffMin = Math.floor((Date.now() - ts) / 60000);
  if (diffMin >= 0 && diffMin < 1) return tr("刚刚更新", "Updated just now");
  if (diffMin >= 1 && diffMin < 60) return tr(`${diffMin} 分钟前更新`, `Updated ${diffMin}m ago`);
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr >= 1 && diffHr < 24) return tr(`${diffHr} 小时前更新`, `Updated ${diffHr}h ago`);
  const diffDays = Math.floor(diffHr / 24);
  if (diffDays >= 1 && diffDays < 30) return tr(`${diffDays} 天前更新`, `Updated ${diffDays}d ago`);
  const d = new Date(ts);
  const mm = String(d.getMonth() + 1).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  return tr(`更新于 ${mm}-${dd}`, `Updated ${mm}-${dd}`);
}

/** Expiry countdown helper for subscription userinfo. */
export function fmtExpiryCountdown(expireSec?: number): { text: string; expired: boolean; soon: boolean } | null {
  if (!expireSec || expireSec <= 0) return null;
  const ms = expireSec > 1e12 ? expireSec : expireSec * 1000;
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return null;
  const mm = String(d.getMonth() + 1).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  const diffDays = Math.ceil((ms - Date.now()) / 86400000);
  if (diffDays < 0) return { text: tr(`已过期 (${mm}-${dd})`, `Expired (${mm}-${dd})`), expired: true, soon: true };
  if (diffDays === 0) return { text: tr("今日到期", "Expires today"), expired: false, soon: true };
  if (diffDays <= 7) return { text: tr(`剩余 ${diffDays} 天 · ${mm}-${dd} 到期`, `${diffDays}d left · expires ${mm}-${dd}`), expired: false, soon: true };
  return { text: tr(`剩余 ${diffDays} 天 · ${mm}-${dd} 到期`, `${diffDays}d left · ${mm}-${dd}`), expired: false, soon: false };
}

export function autoUpdateSummary(updateHours: number | null | undefined, globalHours: number | undefined): string {
  if (updateHours === 0) return tr("不自动更新", "Manual update");
  if (typeof updateHours === "number" && updateHours > 0) return tr(`每 ${updateHours}h 自动更新`, `Every ${updateHours}h`);
  if (globalHours && globalHours > 0) return tr(`跟随全局 (每 ${globalHours}h)`, `Global (${globalHours}h)`);
  return tr("跟随全局 (关闭)", "Global (off)");
}

/** Quick synchronous heuristic for input preview before backend parse responds. */
export function detectInputKind(raw: string): "empty" | "url" | "nodes" | "clash_yaml" | "unknown" {
  const s = raw.trim();
  if (!s) return "empty";
  if (/^(vless|vmess|trojan|ss|ssr|hysteria|hysteria2|hy2|tuic|wireguard|wg|socks|socks5|socks5h|anytls):\/\//i.test(s)) return "nodes";
  if (/^https?:\/\/\S+$/i.test(s) && !s.includes("\n")) {
    try {
      const u = new URL(s);
      if (u.username || u.password) return "nodes";
      return "url";
    } catch {
      return "unknown";
    }
  }
  if (/(^|\n)\s*(proxies|proxy-groups|rules)\s*:/i.test(s)) return "clash_yaml";
  const lines = s.split(/\r?\n/).map((v) => v.trim()).filter(Boolean);
  if (lines.length > 1 && lines.some((l) => /^(vless|vmess|trojan|ss|ssr|hysteria2?|hy2|tuic|wireguard|wg|socks5?h?|https?):\/\//i.test(l))) return "nodes";
  return "unknown";
}
