import { cleanup, Scroll, signal, Text, View } from "nsh";
import { BackHandler } from "nsh/mobile";
import { Button } from "nsh/base";
import { call } from "../client";
import type { ProfileMutation, ProxyEditorNode, ProxyEditorPayload } from "../client";
import { tr } from "../i18n";
import { colors } from "../theme";
import { haptic } from "./platform";
import {
  acceptStatus, loadProxyEditor, message, openSubpage, popSubpage, profiles,
  proxyEditorData, refreshLists, saveProxyEdits, selectedGroup, showToast, status,
} from "./store";
import type { Subpage } from "./store";
import {
  buildDeleteNodeEdits, buildResetNodeEdits, buildSaveNodeEdits,
  extractTransportSummary, FINGERPRINTS, findDefaultTargetGroups,
  NETWORK_TYPES, normalizeProtocolId, parseAdvancedYaml, PROTOCOLS,
  protocolLabel, serializeSimpleYaml, SS_CIPHERS, uniqueNodeName, VMESS_CIPHERS,
} from "./proxy-helpers";
import {
  Action, BottomSheet, confirmDialog, Field, Group, GroupRow, Icon, Label,
  MobileCheckbox, Notice, RADIUS, SectionLabel, SubpageHeader, Tag, ToggleSwitch,
} from "./ui";

export interface NodeEditorDraft {
  protocol: string;
  name: string;
  server: string;
  port: string;
  // Auth
  uuid: string;
  password: string;
  cipher: string;
  alterId: string;
  flow: string;
  encryption: string;
  udp: boolean;
  obfs: string;
  obfsPassword: string;
  upMbps: string;
  downMbps: string;
  congestionController: string;
  udpRelayMode: string;
  privateKey: string;
  publicKey: string;
  ip: string;
  presharedKey: string;
  mtu: string;
  username: string;
  // Transport & Security
  tls: boolean;
  sni: string;
  fingerprint: string;
  skipCertVerify: boolean;
  alpn: string;
  realityPublicKey: string;
  realityShortId: string;
  realitySupportX25519Mlkem768: boolean;
  network: string;
  wsPath: string;
  wsHost: string;
  grpcServiceName: string;
  // Groups
  targetGroups: string[];
  // Advanced YAML Override
  advancedYaml: string;
}

const MANAGED_OPTION_KEYS = new Set([
  "type", "uuid", "password", "cipher", "alterId", "flow", "encryption", "udp",
  "obfs", "obfs-password", "up", "down", "congestion-controller", "udp-relay-mode",
  "private-key", "public-key", "ip", "pre-shared-key", "mtu", "username",
  "tls", "servername", "sni", "client-fingerprint", "skip-cert-verify", "alpn",
  "reality-opts", "network", "ws-opts", "grpc-opts", "h2-opts",
]);

function emptyDraft(protocol = "vless", targetGroups: string[] = []): NodeEditorDraft {
  const meta = PROTOCOLS.find(p => p.id === protocol) ?? PROTOCOLS[0];
  return {
    protocol,
    name: "",
    server: "",
    port: String(meta.defaultPort),
    uuid: "",
    password: "",
    cipher: protocol === "ss" ? "aes-128-gcm" : "auto",
    alterId: "0",
    flow: "",
    encryption: "none",
    udp: true,
    obfs: "",
    obfsPassword: "",
    upMbps: "",
    downMbps: "",
    congestionController: "bbr",
    udpRelayMode: "native",
    privateKey: "",
    publicKey: "",
    ip: "",
    presharedKey: "",
    mtu: "",
    username: "",
    tls: protocol === "vless" || protocol === "trojan" || protocol === "hysteria2" || protocol === "tuic",
    sni: "",
    fingerprint: "chrome",
    skipCertVerify: false,
    alpn: "",
    realityPublicKey: "",
    realityShortId: "",
    realitySupportX25519Mlkem768: false,
    network: "tcp",
    wsPath: "/",
    wsHost: "",
    grpcServiceName: "",
    targetGroups,
    advancedYaml: "",
  };
}

export function draftFromNode(node: ProxyEditorNode, editor: ProxyEditorPayload, duplicate = false): NodeEditorDraft {
  const protocol = normalizeProtocolId(node.type || String(node.options?.type ?? "vless"));
  const opts = node.options ?? {};
  const base = emptyDraft(protocol);
  const wsOpts = (opts["ws-opts"] && typeof opts["ws-opts"] === "object") ? opts["ws-opts"] as Record<string, unknown> : {};
  const wsHeaders = (wsOpts.headers && typeof wsOpts.headers === "object") ? wsOpts.headers as Record<string, unknown> : {};
  const grpcOpts = (opts["grpc-opts"] && typeof opts["grpc-opts"] === "object") ? opts["grpc-opts"] as Record<string, unknown> : {};
  const h2Opts = (opts["h2-opts"] && typeof opts["h2-opts"] === "object") ? opts["h2-opts"] as Record<string, unknown> : {};
  const realityOpts = (opts["reality-opts"] && typeof opts["reality-opts"] === "object") ? opts["reality-opts"] as Record<string, unknown> : {};
  const alpnList = Array.isArray(opts.alpn) ? opts.alpn.map(String).join(", ") : typeof opts.alpn === "string" ? opts.alpn : "";

  const extra: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(opts)) {
    if (!MANAGED_OPTION_KEYS.has(k)) extra[k] = v;
  }

  const realityExtra: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(realityOpts)) {
    if (k !== "public-key" && k !== "short-id" && k !== "support-x25519mlkem768") realityExtra[k] = v;
  }
  if (Object.keys(realityExtra).length) extra["reality-opts"] = realityExtra;

  const wsExtra: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(wsOpts)) {
    if (k === "path") continue;
    if (k === "headers" && v && typeof v === "object" && !Array.isArray(v)) {
      const extraHeaders: Record<string, unknown> = {};
      for (const [hk, hv] of Object.entries(v as Record<string, unknown>)) {
        if (hk !== "Host" && hk !== "host") extraHeaders[hk] = hv;
      }
      if (Object.keys(extraHeaders).length) wsExtra.headers = extraHeaders;
    } else {
      wsExtra[k] = v;
    }
  }
  if (Object.keys(wsExtra).length) extra["ws-opts"] = wsExtra;

  const grpcExtra: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(grpcOpts)) {
    if (k !== "grpc-service-name") grpcExtra[k] = v;
  }
  if (Object.keys(grpcExtra).length) extra["grpc-opts"] = grpcExtra;

  const h2Extra: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(h2Opts)) {
    if (k !== "path" && k !== "host") h2Extra[k] = v;
  }
  if (Object.keys(h2Extra).length) extra["h2-opts"] = h2Extra;

  const memberGroups = editor.groups
    .filter(g => !g.deleted && !g.missing && g.members.includes(node.name))
    .map(g => g.name);

  const existingNames = new Set(editor.nodes.map(n => n.name));
  const displayName = duplicate ? uniqueNodeName(`${node.name} Copy`, existingNames) : node.name;
  const network = String(opts.network ?? "tcp");
  const h2HostStr = Array.isArray(h2Opts.host) ? h2Opts.host.map(String).join(", ") : String(h2Opts.host ?? "");

  return {
    ...base,
    protocol,
    name: displayName,
    server: node.server ?? "",
    port: node.port ? String(node.port) : String(base.port),
    uuid: String(opts.uuid ?? ""),
    password: String(opts.password ?? ""),
    cipher: String(opts.cipher ?? (protocol === "ss" ? "aes-128-gcm" : "auto")),
    alterId: opts.alterId !== undefined ? String(opts.alterId) : "0",
    flow: String(opts.flow ?? ""),
    encryption: String(opts.encryption ?? "none"),
    udp: opts.udp !== false,
    obfs: String(opts.obfs ?? ""),
    obfsPassword: String(opts["obfs-password"] ?? ""),
    upMbps: String(opts.up ?? ""),
    downMbps: String(opts.down ?? ""),
    congestionController: String(opts["congestion-controller"] ?? "bbr"),
    udpRelayMode: String(opts["udp-relay-mode"] ?? "native"),
    privateKey: String(opts["private-key"] ?? ""),
    publicKey: String(opts["public-key"] ?? ""),
    ip: String(opts.ip ?? ""),
    presharedKey: String(opts["pre-shared-key"] ?? ""),
    mtu: opts.mtu !== undefined ? String(opts.mtu) : "",
    username: String(opts.username ?? ""),
    tls: opts.tls === true || protocol === "trojan" || protocol === "hysteria2" || protocol === "tuic",
    sni: String(opts.servername ?? opts.sni ?? ""),
    fingerprint: String(opts["client-fingerprint"] ?? ""),
    skipCertVerify: opts["skip-cert-verify"] === true,
    alpn: alpnList,
    realityPublicKey: String(realityOpts["public-key"] ?? ""),
    realityShortId: String(realityOpts["short-id"] ?? ""),
    realitySupportX25519Mlkem768: realityOpts["support-x25519mlkem768"] === true,
    network,
    wsPath: String(wsOpts.path ?? (network === "h2" ? h2Opts.path ?? "" : "")),
    wsHost: String(wsHeaders.Host ?? wsHeaders.host ?? (network === "h2" ? h2HostStr : "")),
    grpcServiceName: String(grpcOpts["grpc-service-name"] ?? ""),
    targetGroups: memberGroups.length ? memberGroups : findDefaultTargetGroups(editor, selectedGroup()?.name),
    advancedYaml: serializeSimpleYaml(extra),
  };
}

export function buildOptionsFromDraft(d: NodeEditorDraft, advanced: Record<string, unknown>): Record<string, unknown> {
  const opts: Record<string, unknown> = { ...advanced, type: d.protocol };
  const p = d.protocol;

  if (p === "vless") {
    if (d.uuid.trim()) opts.uuid = d.uuid.trim();
    if (d.flow.trim()) opts.flow = d.flow.trim();
    if (d.encryption.trim() && d.encryption.trim() !== "none") opts.encryption = d.encryption.trim();
    opts.udp = d.udp;
  } else if (p === "vmess") {
    if (d.uuid.trim()) opts.uuid = d.uuid.trim();
    opts.alterId = Number.isFinite(Number(d.alterId)) ? Math.max(0, Math.floor(Number(d.alterId))) : 0;
    opts.cipher = d.cipher.trim() || "auto";
    opts.udp = d.udp;
  } else if (p === "trojan") {
    if (d.password) opts.password = d.password;
    opts.udp = d.udp;
  } else if (p === "ss") {
    opts.cipher = d.cipher.trim() || "aes-128-gcm";
    if (d.password) opts.password = d.password;
    opts.udp = d.udp;
  } else if (p === "hysteria2") {
    if (d.password) opts.password = d.password;
    if (d.obfs.trim()) opts.obfs = d.obfs.trim();
    if (d.obfsPassword) opts["obfs-password"] = d.obfsPassword;
    if (d.upMbps.trim()) opts.up = d.upMbps.trim();
    if (d.downMbps.trim()) opts.down = d.downMbps.trim();
  } else if (p === "tuic") {
    if (d.uuid.trim()) opts.uuid = d.uuid.trim();
    if (d.password) opts.password = d.password;
    if (d.congestionController.trim()) opts["congestion-controller"] = d.congestionController.trim();
    if (d.udpRelayMode.trim()) opts["udp-relay-mode"] = d.udpRelayMode.trim();
  } else if (p === "wireguard") {
    if (d.privateKey.trim()) opts["private-key"] = d.privateKey.trim();
    if (d.publicKey.trim()) opts["public-key"] = d.publicKey.trim();
    if (d.ip.trim()) opts.ip = d.ip.trim();
    if (d.presharedKey.trim()) opts["pre-shared-key"] = d.presharedKey.trim();
    if (d.mtu.trim() && Number.isFinite(Number(d.mtu))) opts.mtu = Math.floor(Number(d.mtu));
    opts.udp = true;
  } else if (p === "socks5" || p === "http") {
    if (d.username.trim()) opts.username = d.username.trim();
    if (d.password) opts.password = d.password;
    opts.udp = d.udp;
  }

  const meta = PROTOCOLS.find(item => item.id === p);
  if (meta?.supportsTls) {
    if (p === "trojan" || p === "hysteria2" || p === "tuic" || d.tls) {
      if (p !== "trojan" && p !== "hysteria2" && p !== "tuic") opts.tls = true;
      const sni = d.sni.trim();
      if (sni) {
        if (p === "trojan" || p === "hysteria2" || p === "tuic") opts.sni = sni;
        else opts.servername = sni;
      }
      if (d.fingerprint.trim()) opts["client-fingerprint"] = d.fingerprint.trim();
      if (d.skipCertVerify) opts["skip-cert-verify"] = true;
      if (d.alpn.trim()) opts.alpn = d.alpn.split(",").map(s => s.trim()).filter(Boolean);
      if (p === "vless") {
        const extraReality = (opts["reality-opts"] && typeof opts["reality-opts"] === "object" && !Array.isArray(opts["reality-opts"]))
          ? { ...(opts["reality-opts"] as Record<string, unknown>) }
          : {};
        if (d.realityPublicKey.trim() || d.realityShortId.trim() || d.realitySupportX25519Mlkem768 || Object.keys(extraReality).length) {
          const r: Record<string, unknown> = { ...extraReality };
          if (d.realityPublicKey.trim()) r["public-key"] = d.realityPublicKey.trim();
          if (d.realityShortId.trim()) r["short-id"] = d.realityShortId.trim();
          if (d.realitySupportX25519Mlkem768) r["support-x25519mlkem768"] = true;
          if (Object.keys(r).length) opts["reality-opts"] = r;
        }
      }
    }
  }

  if (meta?.supportsTransport && d.network && d.network !== "tcp") {
    opts.network = d.network;
    if (d.network === "ws" || d.network === "httpupgrade") {
      const extraWs = (opts["ws-opts"] && typeof opts["ws-opts"] === "object" && !Array.isArray(opts["ws-opts"]))
        ? { ...(opts["ws-opts"] as Record<string, unknown>) }
        : {};
      const extraHeaders = (extraWs.headers && typeof extraWs.headers === "object" && !Array.isArray(extraWs.headers))
        ? { ...(extraWs.headers as Record<string, unknown>) }
        : {};
      const wsOpts: Record<string, unknown> = { ...extraWs };
      if (d.wsPath.trim()) wsOpts.path = d.wsPath.trim();
      if (d.wsHost.trim()) wsOpts.headers = { ...extraHeaders, Host: d.wsHost.trim() };
      else if (Object.keys(extraHeaders).length) wsOpts.headers = extraHeaders;
      if (Object.keys(wsOpts).length) opts["ws-opts"] = wsOpts;
    } else if (d.network === "grpc") {
      const extraGrpc = (opts["grpc-opts"] && typeof opts["grpc-opts"] === "object" && !Array.isArray(opts["grpc-opts"]))
        ? { ...(opts["grpc-opts"] as Record<string, unknown>) }
        : {};
      const grpcOpts: Record<string, unknown> = { ...extraGrpc };
      if (d.grpcServiceName.trim()) grpcOpts["grpc-service-name"] = d.grpcServiceName.trim();
      if (Object.keys(grpcOpts).length) opts["grpc-opts"] = grpcOpts;
    } else if (d.network === "h2") {
      const extraH2 = (opts["h2-opts"] && typeof opts["h2-opts"] === "object" && !Array.isArray(opts["h2-opts"]))
        ? { ...(opts["h2-opts"] as Record<string, unknown>) }
        : {};
      const h2Opts: Record<string, unknown> = { ...extraH2 };
      if (d.wsPath.trim()) h2Opts.path = d.wsPath.trim();
      if (d.wsHost.trim()) h2Opts.host = d.wsHost.split(",").map(s => s.trim()).filter(Boolean);
      if (Object.keys(h2Opts).length) opts["h2-opts"] = h2Opts;
    }
  }

  return opts;
}

// Module-level draft state survives light/dark theme switches cleanly.
const draft = signal<NodeEditorDraft>(emptyDraft());
const initialSerialized = signal("");
const editingNode = signal<ProxyEditorNode | null>(null);
const editorProfileId = signal("");
const saving = signal(false);
const editorError = signal("");
const protocolSheetOpen = signal(false);
const cipherSheetOpen = signal(false);
const networkSheetOpen = signal(false);
const groupsSheetOpen = signal(false);
const transportExpanded = signal(false);
const advancedExpanded = signal(false);

export function nodeEditorIsDirty(): boolean {
  return JSON.stringify(draft()) !== initialSerialized();
}

export async function openNodeEditor(options: {
  profileId?: string;
  node?: ProxyEditorNode | null;
  duplicate?: boolean;
  currentGroup?: string;
  returnTo?: Subpage;
  inlineOnly?: boolean;
} = {}): Promise<void> {
  editorError.set("");
  transportExpanded.set(false);
  advancedExpanded.set(false);
  const targetProfileId = options.profileId || status()?.activeProfile || "";
  editorProfileId.set(targetProfileId);

  let editor: ProxyEditorPayload | null = proxyEditorData();
  if (targetProfileId && (!editor || (editor.profileId || editor.id) !== targetProfileId)) {
    try {
      editor = await loadProxyEditor(targetProfileId);
    } catch (e) {
      editorError.set(message(e));
    }
  }

  if (options.node && editor) {
    const nextDraft = draftFromNode(options.node, editor, !!options.duplicate);
    editingNode.set(options.duplicate ? null : options.node);
    draft.set(nextDraft);
    initialSerialized.set(JSON.stringify(nextDraft));
  } else {
    const defaultGroups = findDefaultTargetGroups(editor, options.currentGroup ?? selectedGroup()?.name);
    const nextDraft = emptyDraft("vless", defaultGroups);
    editingNode.set(null);
    draft.set(nextDraft);
    initialSerialized.set(JSON.stringify(nextDraft));
  }

  if (!options.inlineOnly) {
    openSubpage("node-editor", options.returnTo ?? "");
  }
}

function updateDraft(patch: Partial<NodeEditorDraft>) {
  draft.set({ ...draft(), ...patch });
  if (editorError()) editorError.set("");
}

export function NodeEditorBody(props: {
  inline?: boolean;
  onSaved?: () => void;
  onClosed?: () => void;
}) {
  const editor = () => {
    const data = proxyEditorData();
    if (!data) return null;
    if (editorProfileId() && (data.profileId || data.id) !== editorProfileId()) return null;
    return data;
  };
  const node = () => editingNode();
  const isNew = () => !node();
  const isLocal = () => !node() || !node()!.source || !!editor()?.isPureLocal;
  const canReset = () => !!node() && !!node()!.source && !!node()!.edited && !editor()?.isPureLocal;
  const canDelete = () => !!node() && (!node()!.source || !!editor()?.isPureLocal);

  const protoMeta = () => PROTOCOLS.find(p => p.id === draft().protocol) ?? PROTOCOLS[0];
  const parsedYaml = () => parseAdvancedYaml(draft().advancedYaml);
  const transportSummary = () => {
    const d = draft();
    const yamlObj = parsedYaml().ok ? (parsedYaml() as { ok: true; value: Record<string, unknown> }).value : {};
    return extractTransportSummary(buildOptionsFromDraft(d, yamlObj), d.protocol);
  };

  const activeGroups = () => (editor()?.groups ?? []).filter(g => !g.deleted && !g.missing);

  const validate = (): string => {
    const d = draft();
    if (!d.server.trim()) return tr("请填写服务器地址", "Server address is required");
    const portNum = Number(d.port);
    if (!Number.isInteger(portNum) || portNum < 1 || portNum > 65535) {
      return tr("端口须为 1–65535 之间的整数", "Port must be an integer between 1 and 65535");
    }
    if ((d.protocol === "vless" || d.protocol === "vmess" || d.protocol === "tuic") && !d.uuid.trim()) {
      return tr("请填写 UUID", "UUID is required");
    }
    if ((d.protocol === "trojan" || d.protocol === "ss" || d.protocol === "hysteria2") && !d.password.trim()) {
      return tr("请填写密码", "Password is required");
    }
    if (d.protocol === "wireguard" && (!d.privateKey.trim() || !d.publicKey.trim())) {
      return tr("WireGuard 须填写私钥和公钥", "WireGuard requires both private key and public key");
    }
    const adv = parsedYaml();
    if (!adv.ok) return adv.error;
    return "";
  };

  const handleSave = async () => {
    const err = validate();
    if (err) {
      editorError.set(err);
      haptic("reject");
      return;
    }
    const d = draft();
    const portNum = Number(d.port);
    const adv = parsedYaml();
    if (!adv.ok) { editorError.set(adv.error); return; }
    const options = buildOptionsFromDraft(d, adv.value);
    const autoName = d.name.trim() || `${protocolLabel(d.protocol)}-${d.server.trim()}`;

    saving.set(true);
    editorError.set("");
    try {
      // Zero-profile flow: automatically create "本地节点" local profile!
      if (!editorProfileId() || profiles().length === 0) {
        const res = await call<ProfileMutation>("profiles.createLocal", {
          name: tr("本地节点", "Local Nodes"),
          nodes: [{ name: autoName, type: d.protocol, server: d.server.trim(), port: portNum, options }],
          activate: true,
        });
        if (res.status) acceptStatus(res.status);
        await refreshLists();
        if (res.profile?.id) await loadProxyEditor(res.profile.id).catch(() => {});
        haptic("confirm");
        showToast(tr("已创建「本地节点」并保存节点", "Created Local Nodes profile and saved node"));
        initialSerialized.set(JSON.stringify(draft()));
        if (props.onSaved) props.onSaved();
        else popSubpage();
        return;
      }

      let currentEditor = editor();
      if (!currentEditor) {
        currentEditor = await loadProxyEditor(editorProfileId());
      }
      if (!currentEditor) throw new Error(tr("无法加载配置编辑器", "Could not load proxy editor"));

      const existingNames = new Set(
        currentEditor.nodes
          .filter(n => n !== node())
          .map(n => n.name),
      );
      const finalName = isNew() ? uniqueNodeName(autoName, existingNames) : autoName;

      const edits = buildSaveNodeEdits(currentEditor, {
        editingNode: node(),
        name: finalName,
        server: d.server.trim(),
        port: portNum,
        options,
        targetGroups: d.targetGroups,
      });

      const result = await saveProxyEdits(currentEditor.profileId || currentEditor.id, edits);
      haptic("confirm");
      if (result.reconnectRequired) {
        showToast(tr("节点配置已更新，重新连接后生效", "Node saved. Reconnect to apply."));
      } else {
        showToast(tr("节点已保存", "Node saved"));
      }
      initialSerialized.set(JSON.stringify(draft()));
      if (props.onSaved) props.onSaved();
      else popSubpage();
    } catch (e) {
      haptic("reject");
      editorError.set(message(e));
    } finally {
      saving.set(false);
    }
  };

  const handleReset = async () => {
    const currentEditor = editor();
    const target = node();
    if (!currentEditor || !target) return;
    const ok = await confirmDialog({
      title: tr(`恢复「${target.name}」为订阅原值？`, `Reset "${target.name}" to subscription defaults?`),
      description: tr("将移除该节点的本地修改，下次刷新订阅将继续自动更新。", "Removes local overrides for this node."),
      confirmLabel: tr("恢复原值", "Reset"),
      cancelLabel: tr("取消", "Cancel"),
      destructive: true,
    });
    if (!ok) return;
    saving.set(true);
    editorError.set("");
    try {
      const edits = buildResetNodeEdits(currentEditor, target);
      const res = await saveProxyEdits(currentEditor.profileId || currentEditor.id, edits);
      haptic("confirm");
      showToast(res.reconnectRequired ? tr("已恢复订阅原值，重连后生效", "Reset to subscription. Reconnect to apply.") : tr("已恢复订阅原值", "Reset to subscription"));
      initialSerialized.set(JSON.stringify(draft()));
      if (props.onSaved) props.onSaved();
      else popSubpage();
    } catch (e) {
      haptic("reject");
      editorError.set(message(e));
    } finally {
      saving.set(false);
    }
  };

  const handleDelete = async () => {
    const currentEditor = editor();
    const target = node();
    if (!currentEditor || !target) return;
    const ok = await confirmDialog({
      title: tr(`删除自建节点「${target.name}」？`, `Delete local node "${target.name}"?`),
      description: tr("将同时从相关策略组中移除此节点引用，此操作无法撤销。", "Also removes references from policy groups. Cannot be undone."),
      confirmLabel: tr("删除节点", "Delete node"),
      cancelLabel: tr("取消", "Cancel"),
      destructive: true,
    });
    if (!ok) return;
    saving.set(true);
    editorError.set("");
    try {
      const edits = buildDeleteNodeEdits(currentEditor, target);
      const res = await saveProxyEdits(currentEditor.profileId || currentEditor.id, edits);
      haptic("confirm");
      showToast(res.reconnectRequired ? tr("节点已删除，重连后生效", "Node deleted. Reconnect to apply.") : tr("节点已删除", "Node deleted"));
      initialSerialized.set(JSON.stringify(draft()));
      if (props.onSaved) props.onSaved();
      else popSubpage();
    } catch (e) {
      haptic("reject");
      editorError.set(message(e));
    } finally {
      saving.set(false);
    }
  };

  const toggleGroup = (gName: string) => {
    const cur = draft().targetGroups;
    const next = cur.includes(gName) ? cur.filter(x => x !== gName) : [...cur, gName];
    updateDraft({ targetGroups: next });
  };

  return <View style={{ flex: 1, minHeight: 0 }}>
    <Scroll
      testID="node-editor-scroll"
      keyboardShouldPersistTaps="handled"
      keyboardDismissMode="on-drag"
      style={{ flex: 1 }}
      contentStyle={{ padding: 16, paddingBottom: 24, gap: 14 }}>

      <If when={!!node() && !!node()!.source && !editor()?.isPureLocal}>
        <Notice
          tone="info"
          text={tr("订阅节点 · 仅保存修改字段，未修改的凭据与传输参数将在更新订阅时自动保留。", "Subscription node · Only modified fields are saved; unchanged credentials follow subscription updates.")} />
      </If>

      <If when={profiles().length === 0}>
        <Notice
          tone="info"
          text={tr("当前暂无订阅，保存后将自动创建「本地节点」档案。", "No profiles yet. Saving will create a \"Local Nodes\" profile automatically.")} />
      </If>

      {/* Section 1: Basic Info */}
      <SectionLabel title={tr("基础信息", "Basic info")} />
      <Group>
        <GroupRow
          testID="node-editor-protocol"
          icon="server"
          title={tr("协议", "Protocol")}
          value={protocolLabel(draft().protocol)}
          valueTone="accent"
          chevron
          disabled={saving()}
          onPress={() => protocolSheetOpen.set(true)} />
      </Group>
      <View style={{ gap: 12 }}>
        <Field
          testID="node-editor-name"
          label={tr("节点名称（选填）", "Node name (optional)")}
          value={draft().name}
          onChange={v => updateDraft({ name: v })}
          placeholder={draft().server.trim() ? `${protocolLabel(draft().protocol)}-${draft().server.trim()}` : tr("留空自动生成", "Auto-generated if blank")}
          disabled={saving()} />
        <View style={{ flexDirection: "row", gap: 10 }}>
          <View style={{ flex: 1, minWidth: 0 }}>
            <Field
              testID="node-editor-server"
              label={tr("服务器地址", "Server")}
              value={draft().server}
              onChange={v => updateDraft({ server: v })}
              placeholder="example.com"
              disabled={saving()} />
          </View>
          <View style={{ width: 112 }}>
            <Field
              testID="node-editor-port"
              label={tr("端口", "Port")}
              value={draft().port}
              onChange={v => updateDraft({ port: v.replace(/[^\d]/g, "") })}
              keyboardType="numeric"
              placeholder="443"
              disabled={saving()} />
          </View>
        </View>
      </View>

      {/* Section 2: Authentication (Protocol-specific) */}
      <SectionLabel title={tr("认证凭据", "Authentication")} />
      <View style={{ gap: 12 }}>
        <If when={draft().protocol === "vless" || draft().protocol === "vmess" || draft().protocol === "tuic"}>
          <Field
            testID="node-editor-uuid"
            label="UUID"
            secret
            value={draft().uuid}
            onChange={v => updateDraft({ uuid: v })}
            placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
            disabled={saving()} />
        </If>

        <If when={draft().protocol === "vless"}>
          <Field
            testID="node-editor-flow"
            label={tr("Flow（选填）", "Flow (optional)")}
            value={draft().flow}
            onChange={v => updateDraft({ flow: v })}
            placeholder="xtls-rprx-vision"
            disabled={saving()} />
        </If>

        <If when={draft().protocol === "vmess"}>
          <View style={{ flexDirection: "row", gap: 10 }}>
            <View style={{ flex: 1 }}>
              <Group>
                <GroupRow
                  testID="node-editor-cipher"
                  title={tr("加密方式", "Cipher")}
                  value={draft().cipher}
                  chevron
                  onPress={() => cipherSheetOpen.set(true)} />
              </Group>
            </View>
            <View style={{ width: 104 }}>
              <Field
                testID="node-editor-alterid"
                label="AlterId"
                keyboardType="numeric"
                value={draft().alterId}
                onChange={v => updateDraft({ alterId: v.replace(/[^\d]/g, "") })}
                placeholder="0"
                disabled={saving()} />
            </View>
          </View>
        </If>

        <If when={draft().protocol === "ss"}>
          <Group>
            <GroupRow
              testID="node-editor-cipher"
              title={tr("加密方式", "Cipher")}
              value={draft().cipher}
              chevron
              onPress={() => cipherSheetOpen.set(true)} />
          </Group>
        </If>

        <If when={draft().protocol === "trojan" || draft().protocol === "ss" || draft().protocol === "hysteria2" || draft().protocol === "tuic" || draft().protocol === "socks5" || draft().protocol === "http"}>
          <If when={draft().protocol === "socks5" || draft().protocol === "http"}>
            <Field
              testID="node-editor-username"
              label={tr("用户名（选填）", "Username (optional)")}
              value={draft().username}
              onChange={v => updateDraft({ username: v })}
              disabled={saving()} />
          </If>
          <Field
            testID="node-editor-password"
            label={draft().protocol === "socks5" || draft().protocol === "http" ? tr("密码（选填）", "Password (optional)") : tr("密码", "Password")}
            secret
            value={draft().password}
            onChange={v => updateDraft({ password: v })}
            placeholder={tr("输入节点密码", "Enter password")}
            disabled={saving()} />
        </If>

        <If when={draft().protocol === "hysteria2"}>
          <Field
            testID="node-editor-obfs"
            label={tr("混淆方式（选填）", "Obfs (optional)")}
            value={draft().obfs}
            onChange={v => updateDraft({ obfs: v })}
            placeholder="salamander"
            disabled={saving()} />
          <If when={!!draft().obfs.trim()}>
            <Field
              testID="node-editor-obfs-password"
              label={tr("混淆密码", "Obfs password")}
              secret
              value={draft().obfsPassword}
              onChange={v => updateDraft({ obfsPassword: v })}
              disabled={saving()} />
          </If>
          <View style={{ flexDirection: "row", gap: 10 }}>
            <View style={{ flex: 1 }}>
              <Field label={tr("上行带宽 (选填)", "Up Mbps (optional)")} value={draft().upMbps} onChange={v => updateDraft({ upMbps: v })} placeholder="50 Mbps" disabled={saving()} />
            </View>
            <View style={{ flex: 1 }}>
              <Field label={tr("下行带宽 (选填)", "Down Mbps (optional)")} value={draft().downMbps} onChange={v => updateDraft({ downMbps: v })} placeholder="200 Mbps" disabled={saving()} />
            </View>
          </View>
        </If>

        <If when={draft().protocol === "tuic"}>
          <View style={{ flexDirection: "row", gap: 10 }}>
            <View style={{ flex: 1 }}>
              <Field label={tr("拥塞控制", "Congestion")} value={draft().congestionController} onChange={v => updateDraft({ congestionController: v })} placeholder="bbr" disabled={saving()} />
            </View>
            <View style={{ flex: 1 }}>
              <Field label={tr("UDP 中继", "UDP relay")} value={draft().udpRelayMode} onChange={v => updateDraft({ udpRelayMode: v })} placeholder="native" disabled={saving()} />
            </View>
          </View>
        </If>

        <If when={draft().protocol === "wireguard"}>
          <Field testID="node-editor-wg-priv" label={tr("私钥 (Private Key)", "Private key")} secret value={draft().privateKey} onChange={v => updateDraft({ privateKey: v })} disabled={saving()} />
          <Field testID="node-editor-wg-pub" label={tr("对端公钥 (Public Key)", "Peer public key")} value={draft().publicKey} onChange={v => updateDraft({ publicKey: v })} disabled={saving()} />
          <View style={{ flexDirection: "row", gap: 10 }}>
            <View style={{ flex: 1 }}>
              <Field label={tr("本地 IP", "Local IP")} value={draft().ip} onChange={v => updateDraft({ ip: v })} placeholder="172.16.0.2/32" disabled={saving()} />
            </View>
            <View style={{ width: 104 }}>
              <Field label="MTU" keyboardType="numeric" value={draft().mtu} onChange={v => updateDraft({ mtu: v.replace(/[^\d]/g, "") })} placeholder="1280" disabled={saving()} />
            </View>
          </View>
          <Field label={tr("预共享密钥（选填）", "Pre-shared key (optional)")} secret value={draft().presharedKey} onChange={v => updateDraft({ presharedKey: v })} disabled={saving()} />
        </If>
      </View>

      {/* Section 3: Transport & Security (Collapsible with summary subtitle) */}
      <SectionLabel title={tr("传输与安全", "Transport & security")} />
      <Group>
        <GroupRow
          testID="node-editor-transport-toggle"
          icon="shield"
          title={tr("传输与 TLS 设置", "Transport & TLS")}
          subtitle={transportSummary()}
          trailing={() => <Icon name={transportExpanded() ? "chevronDown" : "chevronRight"} size={16} color={colors().muted} />}
          onPress={() => { haptic("selection"); transportExpanded.set(!transportExpanded()); }} />
      </Group>

      <If when={transportExpanded()}>
        <View style={{ gap: 12 }}>
          <Group>
            <If when={protoMeta().supportsTls && draft().protocol !== "trojan" && draft().protocol !== "hysteria2" && draft().protocol !== "tuic"}>
              <GroupRow
                title={tr("启用 TLS", "Enable TLS")}
                trailing={() => <ToggleSwitch testID="node-editor-tls" checked={draft().tls} onChange={v => updateDraft({ tls: v })} label={tr("启用 TLS", "Enable TLS")} disabled={saving()} />} />
            </If>
            <If when={protoMeta().supportsTls}>
              <GroupRow
                title={tr("跳过证书校验", "Skip cert verify")}
                subtitle={tr("仅在自签名证书时开启", "Only enable for self-signed certificates")}
                trailing={() => <ToggleSwitch testID="node-editor-skip-cert" checked={draft().skipCertVerify} onChange={v => updateDraft({ skipCertVerify: v })} label={tr("跳过证书校验", "Skip cert verify")} disabled={saving()} />} />
            </If>
            <If when={draft().protocol !== "wireguard" && draft().protocol !== "hysteria2" && draft().protocol !== "tuic"}>
              <GroupRow
                title={tr("启用 UDP 转发", "Enable UDP")}
                trailing={() => <ToggleSwitch checked={draft().udp} onChange={v => updateDraft({ udp: v })} label={tr("启用 UDP 转发", "Enable UDP")} disabled={saving()} />} />
            </If>
          </Group>

          <If when={protoMeta().supportsTls && (draft().tls || draft().protocol === "trojan" || draft().protocol === "hysteria2" || draft().protocol === "tuic")}>
            <Field label="SNI / ServerName" value={draft().sni} onChange={v => updateDraft({ sni: v })} placeholder={draft().server || "example.com"} disabled={saving()} />
            <Field label="ALPN" value={draft().alpn} onChange={v => updateDraft({ alpn: v })} placeholder="h2, http/1.1" disabled={saving()} />
            <View style={{ gap: 6 }}>
              <Label text={tr("客户端指纹 (Fingerprint)", "Client fingerprint")} bold size={12} muted />
              <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 6 }}>
                <Each of={FINGERPRINTS} key={fp => fp || "none"}>{fp => {
                  const active = () => draft().fingerprint === fp();
                  return <Button
                    onPress={() => { haptic("selection"); updateDraft({ fingerprint: fp() }); }}
                    style={{
                      minHeight: 36, paddingHorizontal: 12, borderRadius: RADIUS.pill,
                      alignItems: "center", justifyContent: "center",
                      backgroundColor: active() ? colors().selected : colors().surface,
                      borderWidth: 1, borderColor: active() ? colors().selectedBorder : colors().border,
                    }}>
                    <Text style={{ fontSize: 12, fontWeight: active() ? "600" : "500", color: active() ? colors().accent : colors().muted }}>{fp() || tr("默认", "Default")}</Text>
                  </Button>;
                }}</Each>
              </View>
            </View>
            <If when={draft().protocol === "vless"}>
              <Field testID="node-editor-reality-pubkey" label={tr("Reality Public Key（选填）", "Reality public key (optional)")} value={draft().realityPublicKey} onChange={v => updateDraft({ realityPublicKey: v })} disabled={saving()} />
              <Field testID="node-editor-reality-shortid" label={tr("Reality Short ID（选填）", "Reality short ID (optional)")} value={draft().realityShortId} onChange={v => updateDraft({ realityShortId: v })} disabled={saving()} />
              <Group>
                <GroupRow
                  title={tr("ML-KEM-768 后量子密钥交换", "ML-KEM-768 post-quantum")}
                  subtitle="support-x25519mlkem768"
                  trailing={() => <ToggleSwitch testID="node-editor-reality-mlkem" checked={draft().realitySupportX25519Mlkem768} onChange={v => updateDraft({ realitySupportX25519Mlkem768: v })} label={tr("ML-KEM-768 后量子密钥交换", "ML-KEM-768 post-quantum")} disabled={saving()} />} />
              </Group>
            </If>
          </If>

          <If when={protoMeta().supportsTransport}>
            <Group>
              <GroupRow
                testID="node-editor-network"
                title={tr("传输协议", "Network")}
                value={draft().network.toUpperCase()}
                chevron
                onPress={() => networkSheetOpen.set(true)} />
            </Group>
            <If when={draft().network === "ws" || draft().network === "httpupgrade" || draft().network === "h2"}>
              <Field label={tr("路径 (Path)", "Path")} value={draft().wsPath} onChange={v => updateDraft({ wsPath: v })} placeholder="/" disabled={saving()} />
              <Field label={tr("伪装域名 (Host)", "Host header")} value={draft().wsHost} onChange={v => updateDraft({ wsHost: v })} placeholder="cdn.example.com" disabled={saving()} />
            </If>
            <If when={draft().network === "grpc"}>
              <Field label="gRPC Service Name" value={draft().grpcServiceName} onChange={v => updateDraft({ grpcServiceName: v })} placeholder="grpc" disabled={saving()} />
            </If>
          </If>
        </View>
      </If>

      {/* Section 4: Policy Groups */}
      <If when={activeGroups().length > 0}>
        <SectionLabel title={tr("所属策略组", "Policy groups")} trailing={String(draft().targetGroups.length)} />
        <Group>
          <GroupRow
            testID="node-editor-groups"
            icon="layers"
            title={tr("选择策略组", "Assign policy groups")}
            subtitle={draft().targetGroups.length ? draft().targetGroups.join(" · ") : tr("未加入任何策略组（仅可在「全部节点」中查看）", "Not in any group (visible under All Nodes)")}
            chevron
            disabled={saving()}
            onPress={() => groupsSheetOpen.set(true)} />
        </Group>
      </If>

      {/* Section 5: Advanced YAML Override (Collapsed) */}
      <SectionLabel title={tr("高级覆写", "Advanced")} />
      <Group>
        <GroupRow
          testID="node-editor-advanced-toggle"
          icon="sliders"
          title={tr("高级 YAML 覆写", "Advanced YAML override")}
          subtitle={draft().advancedYaml.trim() ? tr("已填写自定义参数", "Custom options set") : tr("可选 · 例如 dialer-proxy、tfo、smux 等", "Optional · e.g. dialer-proxy, tfo, smux")}
          trailing={() => <Icon name={advancedExpanded() ? "chevronDown" : "chevronRight"} size={16} color={colors().muted} />}
          onPress={() => { haptic("selection"); advancedExpanded.set(!advancedExpanded()); }} />
      </Group>
      <If when={advancedExpanded()}>
        <Field
          testID="node-editor-advanced-yaml"
          multiline
          height={120}
          value={draft().advancedYaml}
          onChange={v => updateDraft({ advancedYaml: v })}
          placeholder={"dialer-proxy: Landing\ntfo: true"}
          error={!parsedYaml().ok ? (parsedYaml() as { ok: false; error: string }).error : undefined}
          helper={tr("仅填写额外协议键值；不要重复填写 name / server / port。", "Extra protocol keys only; do not duplicate name, server, or port.")}
          disabled={saving()} />
      </If>

      <If when={!!editorError()}><Notice testID="node-editor-error" text={editorError()} tone="danger" /></If>

      <If when={canReset() || canDelete()}>
        <View style={{ flexDirection: "row", gap: 10, paddingTop: 4 }}>
          <If when={canReset()}>
            <View style={{ flex: 1 }}>
              <Action testID="node-editor-reset" full icon="rotateCcw" label={tr("恢复订阅原值", "Reset to sub")} disabled={saving()} onPress={() => void handleReset()} />
            </View>
          </If>
          <If when={canDelete()}>
            <View style={{ flex: 1 }}>
              <Action testID="node-editor-delete" full danger icon="trash" label={tr("删除节点", "Delete node")} disabled={saving()} onPress={() => void handleDelete()} />
            </View>
          </If>
        </View>
      </If>
    </Scroll>

    {/* Sticky Bottom Save Bar (keyboard & safe-area aware) */}
    <View style={{
      paddingHorizontal: 16, paddingVertical: 12,
      marginBottom: props.inline ? 0 : "env(keyboard-inset-height)",
      backgroundColor: colors().surface,
      borderTopWidth: 1, borderColor: colors().border,
      flexDirection: "row", gap: 10,
    }}>
      <If when={!!props.inline && !!props.onClosed}>
        <View style={{ flex: 1 }}>
          <Action full label={tr("取消", "Cancel")} disabled={saving()} onPress={() => props.onClosed?.()} />
        </View>
      </If>
      <View style={{ flex: 2 }}>
        <Action
          testID="node-editor-save"
          primary
          full
          label={saving() ? tr("正在保存…", "Saving…") : isNew() ? tr("保存节点", "Save node") : tr("保存修改", "Save changes")}
          disabled={saving()}
          onPress={() => void handleSave()} />
      </View>
    </View>

    {/* Protocol Picker BottomSheet */}
    <BottomSheet
      testID="protocol-picker-sheet"
      open={protocolSheetOpen()}
      onClose={() => protocolSheetOpen.set(false)}
      title={tr("选择节点协议", "Select protocol")}
      scrollable>
      <Group>
        <Each of={PROTOCOLS} key={p => p.id}>{p => <GroupRow
          testID={`protocol-opt-${p().id}`}
          title={p().label}
          subtitle={p().id.toUpperCase()}
          trailing={() => <If when={draft().protocol === p().id}><Tag text={tr("当前", "Current")} tone="accent" /></If>}
          onPress={() => {
            haptic("selection");
            const nextProto = p().id;
            const prevMeta = PROTOCOLS.find(item => item.id === draft().protocol);
            const nextPort = !draft().port || draft().port === String(prevMeta?.defaultPort) ? String(p().defaultPort) : draft().port;
            updateDraft({
              protocol: nextProto,
              port: nextPort,
              cipher: nextProto === "ss" ? "aes-128-gcm" : "auto",
              tls: nextProto === "vless" || nextProto === "trojan" || nextProto === "hysteria2" || nextProto === "tuic",
            });
            protocolSheetOpen.set(false);
          }} />}</Each>
      </Group>
    </BottomSheet>

    {/* Cipher Picker BottomSheet */}
    <BottomSheet
      open={cipherSheetOpen()}
      onClose={() => cipherSheetOpen.set(false)}
      title={tr("选择加密方式", "Select cipher")}
      scrollable>
      <Group>
        <Each of={draft().protocol === "ss" ? SS_CIPHERS : VMESS_CIPHERS} key={c => c}>{c => <GroupRow
          title={c()}
          trailing={() => <If when={draft().cipher === c()}><Tag text={tr("当前", "Current")} tone="accent" /></If>}
          onPress={() => { haptic("selection"); updateDraft({ cipher: c() }); cipherSheetOpen.set(false); }} />}</Each>
      </Group>
    </BottomSheet>

    {/* Network Picker BottomSheet */}
    <BottomSheet
      open={networkSheetOpen()}
      onClose={() => networkSheetOpen.set(false)}
      title={tr("选择传输协议", "Select transport")}>
      <Group>
        <Each of={NETWORK_TYPES} key={n => n}>{n => <GroupRow
          title={n().toUpperCase()}
          trailing={() => <If when={draft().network === n()}><Tag text={tr("当前", "Current")} tone="accent" /></If>}
          onPress={() => { haptic("selection"); updateDraft({ network: n() }); networkSheetOpen.set(false); }} />}</Each>
      </Group>
    </BottomSheet>

    {/* Policy Groups Multi-Select BottomSheet */}
    <BottomSheet
      testID="node-groups-sheet"
      open={groupsSheetOpen()}
      onClose={() => groupsSheetOpen.set(false)}
      title={tr("所属策略组", "Policy groups")}
      subtitle={isLocal() ? tr("自建节点将通过追加方式加入订阅策略组，更新订阅不会删除。", "Local nodes are appended safely and survive subscription updates.") : tr("勾选此节点所属的策略组。", "Select policy groups containing this node.")}
      scrollable
      footer={() => <Action primary full label={tr("完成", "Done")} disabled={false} onPress={() => groupsSheetOpen.set(false)} />}>
      <Group>
        <Each of={activeGroups()} key={g => g.name}>{g => {
          const checked = () => draft().targetGroups.includes(g().name);
          return <GroupRow
            testID={`node-group-opt-${g().name}`}
            title={g().name}
            subtitle={`${g().members.length} ${tr("个成员", "members")}`}
            trailing={() => <MobileCheckbox checked={checked()} onChange={() => toggleGroup(g().name)} />}
            onPress={() => toggleGroup(g().name)} />;
        }}</Each>
      </Group>
    </BottomSheet>
  </View>;
}

export function NodeEditorPage() {
  const handleBack = () => {
    if (saving()) return;
    if (!nodeEditorIsDirty()) { popSubpage(); return; }
    void confirmDialog({
      title: tr("放弃未保存的更改？", "Discard unsaved changes?"),
      description: tr("当前节点编辑尚未保存，返回将丢失已填写的内容。", "Your node changes have not been saved."),
      confirmLabel: tr("放弃更改", "Discard"),
      cancelLabel: tr("继续编辑", "Keep editing"),
      destructive: true,
    }).then(discard => { if (discard) popSubpage(); });
  };

  cleanup(BackHandler.on("press", () => {
    if (saving()) return true;
    handleBack();
    return true;
  }));

  const title = () => editingNode() ? tr("编辑节点", "Edit node") : tr("新增节点", "New node");
  const subtitle = () => editingNode()?.name || proxyEditorData()?.profileName || "";

  return <View testID="node-editor-page" style={{ flex: 1, backgroundColor: colors().bg }}>
    <SubpageHeader
      title={title()}
      subtitle={subtitle()}
      backTestID="node-editor-back"
      backDisabled={saving()}
      onBack={handleBack} />
    <NodeEditorBody />
  </View>;
}
