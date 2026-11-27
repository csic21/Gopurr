import { colorScheme, effect, List, Scroll, signal, Text, View, windowSize } from "nsh";
import { Button } from "nsh/base";
import { colors } from "./theme";
import { tr } from "./i18n";
import {
  Action, CheckRow, Collapsible, Empty, Feedback, Field, Group, GroupRow, Icon, IconButton,
  Label, Modal, QuotaBar, RADIUS, Row, SearchField, SecretField, Select, Tabs, Tag, Toggle,
  confirmDialog, elevation, groupTypeLabel, tint,
} from "./ui";
import {
  FINGERPRINTS, PROTOCOLS, SS_CIPHERS, TRANSPORTS, VMESS_CIPHERS,
  buildNodeValue, extractNodeForm, mergeDiff, transportSummary,
  type StructuredNodeForm,
} from "./proxy-helpers";
import type { PageProps } from "./types";

const MOTION = { transitionProperty: "background-color, border-color, color", transitionDuration: 140, transitionTimingFunction: "ease-out" } as const;

export interface ProxyEditorModel {
  id: string;
  profileId?: string;
  profileName?: string;
  profileKind?: string;
  isPureLocal?: boolean;
  nodes: any[];
  groups: any[];
  edits: { nodes?: any[]; groups?: any[] } | null;
  options?: string[];
  availableNames?: string[];
  providerNodes?: { provider: string; name: string; type: string }[];
}

export function cloneEdits(edits: any): { nodes: any[]; groups: any[] } {
  return {
    nodes: JSON.parse(JSON.stringify(edits?.nodes ?? [])),
    groups: JSON.parse(JSON.stringify(edits?.groups ?? [])),
  };
}

/** Determine which groups currently include a node name (from editor model). */
export function groupsContainingNode(model: ProxyEditorModel | null, nodeName: string): string[] {
  if (!model || !nodeName) return [];
  const out: string[] = [];
  for (const g of model.groups ?? []) {
    if (g.deleted) continue;
    const members: string[] = g.members ?? [];
    if (members.includes(nodeName)) out.push(g.name);
  }
  return out;
}

/** Update group membership in edits cleanly using AppendMembers when following subscription. */
export function updateGroupMembershipsInEdits(
  model: ProxyEditorModel,
  edits: { nodes: any[]; groups: any[] },
  nodeName: string,
  oldNodeName: string,
  targetGroupNames: Set<string>,
): void {
  const effectiveOld = oldNodeName || nodeName;
  for (const g of model.groups ?? []) {
    if (g.deleted) continue;
    const gKey = g.source || g.name;
    const want = targetGroupNames.has(g.name);
    const currentMembers: string[] = g.members ?? [];
    const inherited: string[] = g.inheritedMembers ?? (g.source ? (g.base?.members ?? []) : []);
    const currentlyHas = currentMembers.includes(effectiveOld) || currentMembers.includes(nodeName);
    if (want === currentlyHas && (effectiveOld === nodeName || !currentlyHas)) continue;

    let idx = edits.groups.findIndex((e: any) => (g.source ? e.source === g.source : e.name === g.name));
    let entry: any = idx >= 0 ? { ...edits.groups[idx] } : { source: g.source ?? "" };
    if (!g.source) {
      entry.name = entry.name ?? g.name;
      entry.type = entry.type ?? g.type;
    }

    const isExplicit = Boolean(g.explicitMembers || g.explicit || entry.members != null || !g.source);
    if (want) {
      if (isExplicit) {
        const list = [...(entry.members ?? currentMembers)];
        const oldIdx = list.indexOf(effectiveOld);
        if (oldIdx >= 0) list[oldIdx] = nodeName;
        else if (!list.includes(nodeName)) list.push(nodeName);
        entry.members = list;
        delete entry.appendMembers;
      } else {
        // Subscription group following upstream: use AppendMembers!
        const app: string[] = [...(entry.appendMembers ?? g.appendMembers ?? [])];
        if (!inherited.includes(nodeName) && !app.includes(nodeName)) {
          app.push(nodeName);
        }
        entry.appendMembers = app;
      }
    } else {
      if (isExplicit) {
        const list = [...(entry.members ?? currentMembers)].filter((m) => m !== effectiveOld && m !== nodeName);
        if (list.length > 0) entry.members = list;
      } else {
        const app: string[] = [...(entry.appendMembers ?? g.appendMembers ?? [])].filter((m) => m !== effectiveOld && m !== nodeName);
        if (inherited.includes(effectiveOld) || inherited.includes(nodeName)) {
          // Removing an inherited member requires explicit list
          const list = currentMembers.filter((m) => m !== effectiveOld && m !== nodeName);
          if (list.length > 0) {
            entry.members = list;
            delete entry.appendMembers;
          }
        } else {
          if (app.length) entry.appendMembers = app;
          else delete entry.appendMembers;
        }
      }
    }

    const hasMeaningfulChange = Boolean(
      !g.source || entry.name != null || entry.type != null || entry.members != null || (entry.appendMembers && entry.appendMembers.length > 0) || entry.delete,
    );
    if (idx >= 0) {
      if (hasMeaningfulChange) edits.groups[idx] = entry;
      else edits.groups.splice(idx, 1);
    } else if (hasMeaningfulChange) {
      edits.groups.push(entry);
    }
  }
}

/** Build updated ProxyEdits after saving a node form + group membership. */
export function buildNodeSaveEdits(
  model: ProxyEditorModel,
  row: any,
  form: StructuredNodeForm,
  selectedGroups: Set<string>,
): { edits: { nodes: any[]; groups: any[] }; savedName: string } {
  const value = buildNodeValue(form);
  const edits = cloneEdits(model.edits);
  const isNew = !row || (!row.source && !row.localId);
  const isLocal = !row?.source;
  const localId = row?.localId || (isLocal ? `local-${Date.now()}-${Math.random().toString(36).slice(2, 6)}` : "");
  const base = row?.base ?? {};
  const prevIdx = edits.nodes.findIndex((v: any) => (localId ? v.localId === localId : v.source === row.source));
  const previous = prevIdx >= 0 ? edits.nodes[prevIdx] : null;
  const aliases = [
    ...new Set([
      ...(previous?.aliases ?? []),
      ...(row?.source && row.name !== row.source && row.name !== value.name ? [row.name] : []),
    ]),
  ];

  const entry: any = {
    source: row?.source ?? "",
    ...(localId ? { localId } : {}),
    ...(aliases.length ? { aliases } : {}),
  };

  if (isLocal) {
    entry.name = value.name;
    entry.server = value.server;
    entry.port = value.port;
    entry.options = value.options;
  } else {
    for (const key of ["name", "server", "port", "options"] as const) {
      const diff = mergeDiff(base[key], (value as any)[key]);
      if (diff !== undefined) entry[key] = diff;
    }
  }

  const unchanged = !isLocal && entry.name === undefined && entry.server === undefined && entry.port === undefined && entry.options === undefined;
  if (prevIdx >= 0) {
    if (unchanged) edits.nodes.splice(prevIdx, 1);
    else edits.nodes[prevIdx] = entry;
  } else if (!unchanged || isNew) {
    edits.nodes.push(entry);
  }

  updateGroupMembershipsInEdits(model, edits, value.name, row?.name ?? "", selectedGroups);
  return { edits, savedName: value.name };
}

/** Build updated ProxyEdits for appending one or more parsed/local nodes into target groups. */
export function buildAppendNodesEdits(
  model: ProxyEditorModel,
  nodes: { name: string; type?: string; server: string; port: number; options?: Record<string, any> }[],
  targetGroups: string[],
): { edits: { nodes: any[]; groups: any[] }; addedNames: string[] } {
  const edits = cloneEdits(model.edits);
  const existingNames = new Set<string>((model.nodes ?? []).map((n: any) => n.name));
  const addedNames: string[] = [];

  for (let i = 0; i < nodes.length; i++) {
    const n = nodes[i];
    let candidate = (n.name || `${n.type || "proxy"}-${n.server}`).trim();
    if (existingNames.has(candidate)) {
      let suffix = 2;
      while (existingNames.has(`${candidate} (${suffix})`)) suffix++;
      candidate = `${candidate} (${suffix})`;
    }
    existingNames.add(candidate);
    addedNames.push(candidate);
    const opts = { ...(n.options ?? {}), type: (n.options?.type ?? n.type ?? "vless") };
    edits.nodes.push({
      source: "",
      localId: `local-${Date.now()}-${i}-${Math.random().toString(36).slice(2, 6)}`,
      name: candidate,
      server: n.server,
      port: n.port,
      options: opts,
    });
  }

  const targetSet = new Set(targetGroups);
  for (const name of addedNames) {
    updateGroupMembershipsInEdits(model, edits, name, "", targetSet);
  }
  return { edits, addedNames };
}

/** Build updated ProxyEdits for deleting a local/pure-local node, scrubbing group references. */
export function buildNodeDeleteEdits(
  model: ProxyEditorModel,
  row: any,
): { edits: { nodes: any[]; groups: any[] }; error?: string } {
  const edits = cloneEdits(model.edits);
  const nodeName = row.name;

  // Pre-check empty group constraint
  for (const g of model.groups ?? []) {
    if (g.deleted) continue;
    const members: string[] = g.members ?? [];
    if (members.includes(nodeName) && members.filter((m) => m !== nodeName).length === 0) {
      return {
        edits,
        error: tr(
          `无法删除“${nodeName}”：它是策略组“${g.name}”的唯一成员，请先向该策略组添加其他成员。`,
          `Cannot delete “${nodeName}”: it is the only member of group “${g.name}”. Add another member first.`,
        ),
      };
    }
  }

  if (row.source && model.isPureLocal) {
    edits.nodes = edits.nodes.filter((v: any) => v.source !== row.source);
    edits.nodes.push({ source: row.source, delete: true });
  } else {
    edits.nodes = edits.nodes.filter((v: any) => (row.localId ? v.localId !== row.localId : v.source !== row.source));
  }

  // Clean references from group edits
  for (let i = edits.groups.length - 1; i >= 0; i--) {
    const g = edits.groups[i];
    if (Array.isArray(g.members)) g.members = g.members.filter((m: string) => m !== nodeName);
    if (Array.isArray(g.appendMembers)) {
      g.appendMembers = g.appendMembers.filter((m: string) => m !== nodeName);
      if (!g.appendMembers.length) delete g.appendMembers;
    }
  }
  return { edits };
}

/** Build updated ProxyEdits for restoring a modified subscription node (or removing an orphan edit). */
export function buildNodeRestoreEdits(model: ProxyEditorModel, row: any): { nodes: any[]; groups: any[] } {
  const edits = cloneEdits(model.edits);
  edits.nodes = edits.nodes.filter((v: any) => (row.localId ? v.localId !== row.localId : v.source !== row.source));
  return edits;
}

export interface GroupFormState {
  name: string;
  type: string;
  explicitMembers: boolean;
  inheritedMembers: string[];
  appendMembers: string[];
  members: string[];
}

export function extractGroupForm(row: any): GroupFormState {
  const isLocal = !row?.source;
  const inherited: string[] = isLocal ? [] : [...(row?.inheritedMembers ?? row?.base?.members ?? [])];
  const append: string[] = [...(row?.appendMembers ?? [])];
  const explicit = Boolean(isLocal || row?.explicitMembers || row?.explicit);
  const members: string[] = [...(row?.members ?? (isLocal ? ["DIRECT"] : inherited))];
  return {
    name: String(row?.name ?? ""),
    type: String(row?.type ?? "select").toLowerCase(),
    explicitMembers: explicit,
    inheritedMembers: inherited,
    appendMembers: append,
    members,
  };
}

export function buildGroupSaveEdits(
  model: ProxyEditorModel,
  row: any,
  form: GroupFormState,
): { edits: { nodes: any[]; groups: any[] }; savedName: string } {
  const trimmedName = form.name.trim();
  if (!trimmedName) throw new Error(tr("请填写策略组名称", "Policy group name is required"));
  const effectiveMembers = form.explicitMembers ? form.members : [...form.inheritedMembers, ...form.appendMembers];
  if (effectiveMembers.length === 0) {
    throw new Error(tr("策略组至少需要一个成员", "A policy group must contain at least one member"));
  }
  const edits = cloneEdits(model.edits);
  const isLocal = !row?.source;
  const base = row?.base ?? {};
  const prevIdx = edits.groups.findIndex((v: any) => (row?.source ? v.source === row.source : v.name === row?.name));
  const previous = prevIdx >= 0 ? edits.groups[prevIdx] : null;
  const aliases = [
    ...new Set([
      ...(previous?.aliases ?? []),
      ...(row?.source && row.name !== row.source && row.name !== trimmedName ? [row.name] : []),
    ]),
  ];

  const entry: any = {
    source: row?.source ?? "",
    ...(aliases.length ? { aliases } : {}),
  };

  if (isLocal) {
    entry.name = trimmedName;
    entry.type = form.type || "select";
    entry.members = [...form.members];
  } else {
    if (trimmedName !== base.name) entry.name = trimmedName;
    if ((form.type || "select") !== String(base.type ?? "select").toLowerCase()) entry.type = form.type || "select";
    if (form.explicitMembers) {
      if (JSON.stringify(form.members) !== JSON.stringify(base.members ?? [])) {
        entry.members = [...form.members];
      }
    } else if (form.appendMembers.length > 0) {
      entry.appendMembers = [...form.appendMembers];
    }
  }

  const unchanged = !isLocal && entry.name === undefined && entry.type === undefined && entry.members === undefined && entry.appendMembers === undefined;
  if (prevIdx >= 0) {
    if (unchanged) edits.groups.splice(prevIdx, 1);
    else edits.groups[prevIdx] = entry;
  } else if (!unchanged) {
    edits.groups.push(entry);
  }
  return { edits, savedName: trimmedName };
}

export function buildGroupDeleteEdits(model: ProxyEditorModel, row: any): { nodes: any[]; groups: any[] } {
  const edits = cloneEdits(model.edits);
  edits.groups = edits.groups.filter((v: any) => (row.source ? v.source !== row.source : v.name !== row.name));
  if (row.source) {
    edits.groups.push({ source: row.source, delete: true });
  }
  return edits;
}

export function buildGroupRestoreEdits(model: ProxyEditorModel, row: any): { nodes: any[]; groups: any[] } {
  const edits = cloneEdits(model.edits);
  edits.groups = edits.groups.filter((v: any) => (row.source ? v.source !== row.source : v.name !== row.name));
  return edits;
}

/** Structured Node Editor fields (zero raw JSON!). */
export function NodeEditorFields(p: {
  form: () => StructuredNodeForm;
  setForm: (next: StructuredNodeForm) => void;
  groups?: any[];
  selectedGroups?: () => Set<string>;
  onToggleGroup?: (groupName: string, checked: boolean) => void;
  showSubscriptionHint?: boolean;
  testIDPrefix?: string;
}) {
  const transportOpen = signal(false);
  const groupsOpen = signal(false);
  const advancedOpen = signal(false);
  const update = <K extends keyof StructuredNodeForm>(key: K, value: StructuredNodeForm[K]) => {
    p.setForm({ ...p.form(), [key]: value });
  };
  const prefix = p.testIDPrefix ?? "node-edit";

  const protocolItems = () => {
    const base = PROTOCOLS.map((x) => ({ value: x.value as string, title: x.title as string }));
    const cur = p.form().type;
    if (cur && !base.some((b) => b.value === cur)) base.push({ value: cur, title: cur.toUpperCase() });
    return base;
  };
  const type = () => p.form().type;
  const implicitTls = () => ["trojan", "hysteria2", "tuic"].includes(type());
  const supportsTransport = () => ["vless", "vmess", "trojan", "ss"].includes(type());
  const manualGroups = () => (p.groups ?? []).filter((g: any) => !g.deleted);

  return <View style={{ gap: 11 }}>
    <If when={!!p.showSubscriptionHint}>
      <View style={{ paddingHorizontal: 11, paddingVertical: 8, borderRadius: 8, backgroundColor: tint(colors().accent, colorScheme() === "dark" ? "14" : "0d"), borderWidth: 1, borderColor: tint(colors().accent, colorScheme() === "dark" ? "28" : "20") }}>
        <Text style={{ fontSize: 11.5, lineHeight: 16, color: colors().text }}>
          {tr("自建节点与修改保存在本地叠加层，更新订阅时保留，不会覆盖订阅规则。", "Local nodes and edits stay in a local overlay and survive subscription updates without overwriting subscription rules.")}
        </Text>
      </View>
    </If>

    <View style={{ gap: 6 }}>
      <Label text={tr("协议", "Protocol")} muted size={12} weight={600} />
      <Select testID={`${prefix}-protocol`} label={tr("协议", "Protocol")} value={type()} onChange={(v) => update("type", v)} items={protocolItems()} />
    </View>

    <Field testID={`${prefix}-name`} label={tr("名称", "Name")} value={p.form().name} onChange={(v) => update("name", v)} placeholder={tr("留空自动按协议和地址命名", "Auto-generated if blank")} />

    <Row gap={10}>
      <View style={{ flex: 1, minWidth: 0 }}>
        <Field testID={`${prefix}-server`} label={tr("服务器", "Server")} value={p.form().server} onChange={(v) => update("server", v)} placeholder="example.com" />
      </View>
      <View style={{ width: 92, flexShrink: 0 }}>
        <Field testID={`${prefix}-port`} label={tr("端口", "Port")} value={p.form().port} onChange={(v) => update("port", v)} placeholder="443" />
      </View>
    </Row>

    {/* Protocol-specific auth fields */}
    <If when={type() === "vless"}>
      <SecretField testID={`${prefix}-uuid`} label="UUID" value={p.form().uuid} onChange={(v) => update("uuid", v)} placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" />
      <Field testID={`${prefix}-flow`} label={tr("流控 (Flow，可选)", "Flow (optional)")} value={p.form().flow} onChange={(v) => update("flow", v)} placeholder="xtls-rprx-vision" />
    </If>

    <If when={type() === "vmess"}>
      <SecretField testID={`${prefix}-uuid`} label="UUID" value={p.form().uuid} onChange={(v) => update("uuid", v)} />
      <Row gap={10}>
        <View style={{ flex: 1, gap: 6 }}>
          <Label text={tr("加密方式", "Cipher")} muted size={12} weight={600} />
          <Select label={tr("加密方式", "Cipher")} value={p.form().cipher || "auto"} onChange={(v) => update("cipher", v)} items={VMESS_CIPHERS.map((c) => ({ value: c, title: c }))} />
        </View>
        <View style={{ width: 92 }}>
          <Field label="AlterId" value={p.form().alterId} onChange={(v) => update("alterId", v)} placeholder="0" />
        </View>
      </Row>
    </If>

    <If when={type() === "trojan"}>
      <SecretField testID={`${prefix}-password`} label={tr("密码", "Password")} value={p.form().password} onChange={(v) => update("password", v)} />
    </If>

    <If when={type() === "ss"}>
      <View style={{ gap: 6 }}>
        <Label text={tr("加密方式", "Cipher")} muted size={12} weight={600} />
        <Select label={tr("加密方式", "Cipher")} value={p.form().cipher || "aes-128-gcm"} onChange={(v) => update("cipher", v)} items={[...new Set([...SS_CIPHERS, p.form().cipher].filter(Boolean))].map((c) => ({ value: c, title: c }))} />
      </View>
      <SecretField testID={`${prefix}-password`} label={tr("密码", "Password")} value={p.form().password} onChange={(v) => update("password", v)} />
    </If>

    <If when={type() === "hysteria2"}>
      <SecretField testID={`${prefix}-password`} label={tr("认证密码", "Auth password")} value={p.form().password} onChange={(v) => update("password", v)} />
      <Field label={tr("混淆类型 (Obfs，可选)", "Obfs (optional)")} value={p.form().obfs} onChange={(v) => update("obfs", v)} placeholder="salamander" />
      <If when={!!p.form().obfs.trim()}>
        <SecretField label={tr("混淆密码", "Obfs password")} value={p.form().obfsPassword} onChange={(v) => update("obfsPassword", v)} />
      </If>
    </If>

    <If when={type() === "tuic"}>
      <SecretField testID={`${prefix}-uuid`} label="UUID" value={p.form().uuid} onChange={(v) => update("uuid", v)} />
      <SecretField testID={`${prefix}-password`} label={tr("密码", "Password")} value={p.form().password} onChange={(v) => update("password", v)} />
      <Field label={tr("拥塞控制", "Congestion controller")} value={p.form().congestionController} onChange={(v) => update("congestionController", v)} placeholder="bbr" />
    </If>

    <If when={type() === "wireguard"}>
      <SecretField testID={`${prefix}-privkey`} label={tr("私钥 (Private key)", "Private key")} value={p.form().privateKey} onChange={(v) => update("privateKey", v)} />
      <Field label={tr("对端公钥 (Public key)", "Peer public key")} value={p.form().publicKey} onChange={(v) => update("publicKey", v)} />
      <Row gap={10}>
        <View style={{ flex: 1 }}><Field label="IP" value={p.form().ip} onChange={(v) => update("ip", v)} placeholder="10.0.0.2/32" /></View>
        <View style={{ flex: 1 }}><Field label="IPv6" value={p.form().ipv6} onChange={(v) => update("ipv6", v)} placeholder="可选 / Optional" /></View>
      </Row>
    </If>

    <If when={type() === "socks5" || type() === "http"}>
      <Field label={tr("用户名（可选）", "Username (optional)")} value={p.form().username} onChange={(v) => update("username", v)} />
      <SecretField testID={`${prefix}-password`} label={tr("密码（可选）", "Password (optional)")} value={p.form().password} onChange={(v) => update("password", v)} />
    </If>

    <If when={!["vless", "vmess", "trojan", "ss", "hysteria2", "tuic", "wireguard", "socks5", "http"].includes(type())}>
      <SecretField testID={`${prefix}-password`} label={tr("密码 / 凭据", "Password / secret")} value={p.form().password} onChange={(v) => update("password", v)} />
    </If>

    {/* Transport & Security (collapsible) */}
    <If when={type() !== "wireguard"}>
      <Collapsible testID={`${prefix}-transport-toggle`} title={tr("传输与安全", "Transport & security")} summary={transportSummary(p.form())} open={transportOpen()} onToggle={() => transportOpen.set(!transportOpen())}>
        <If when={!implicitTls()}>
          <Row justify="space-between"><Label text={tr("启用 TLS", "Enable TLS")} weight={500} size={12.5} /><Toggle showLabel={false} label="TLS" checked={p.form().tls} onChange={(v) => update("tls", v)} /></Row>
        </If>
        <If when={implicitTls() || p.form().tls}>
          <Field label={tr("SNI / 服务器名", "SNI / Server name")} value={p.form().sni} onChange={(v) => update("sni", v)} placeholder="example.com" />
          <Row gap={10}>
            <View style={{ flex: 1, gap: 6 }}>
              <Label text={tr("客户端指纹", "Fingerprint")} muted size={12} weight={600} />
              <Select label={tr("客户端指纹", "Fingerprint")} value={p.form().fingerprint} onChange={(v) => update("fingerprint", v)} items={FINGERPRINTS} />
            </View>
            <View style={{ flex: 1 }}>
              <Field label="ALPN" value={p.form().alpn} onChange={(v) => update("alpn", v)} placeholder="h2, http/1.1" />
            </View>
          </Row>
          <Row justify="space-between"><Label text={tr("跳过证书校验", "Skip certificate verify")} weight={500} size={12.5} /><Toggle showLabel={false} label={tr("跳过证书校验", "Skip certificate verify")} checked={p.form().skipCertVerify} onChange={(v) => update("skipCertVerify", v)} /></Row>
          <If when={type() === "vless"}>
            <Field label={tr("Reality 公钥 (Public key)", "Reality public key")} value={p.form().realityPublicKey} onChange={(v) => update("realityPublicKey", v)} placeholder={tr("仅 Reality 节点填写", "Reality only")} />
            <Field label="Reality Short ID" value={p.form().realityShortId} onChange={(v) => update("realityShortId", v)} placeholder={tr("可选", "Optional")} />
          </If>
        </If>
        <If when={supportsTransport()}>
          <View style={{ gap: 6 }}>
            <Label text={tr("传输协议", "Transport")} muted size={12} weight={600} />
            <Select label={tr("传输协议", "Transport")} value={p.form().network || "tcp"} onChange={(v) => update("network", v)} items={TRANSPORTS} />
          </View>
          <If when={p.form().network === "ws" || p.form().network === "httpupgrade"}>
            <Field label={tr("路径 (Path)", "Path")} value={p.form().wsPath} onChange={(v) => update("wsPath", v)} placeholder="/ws" />
            <Field label="Host" value={p.form().wsHost} onChange={(v) => update("wsHost", v)} placeholder="cdn.example.com" />
          </If>
          <If when={p.form().network === "grpc"}>
            <Field label="gRPC Service Name" value={p.form().grpcServiceName} onChange={(v) => update("grpcServiceName", v)} placeholder="grpc" />
          </If>
          <If when={p.form().network === "h2"}>
            <Field label="H2 Path" value={p.form().h2Path} onChange={(v) => update("h2Path", v)} placeholder="/" />
            <Field label="H2 Host" value={p.form().h2Host} onChange={(v) => update("h2Host", v)} placeholder="example.com" />
          </If>
        </If>
      </Collapsible>
    </If>

    {/* Policy Groups checklist */}
    <If when={!!p.onToggleGroup && manualGroups().length > 0}>
      <Collapsible testID={`${prefix}-groups-toggle`} title={tr("所属策略组", "Policy groups")} summary={tr(`${p.selectedGroups?.().size ?? 0} 个已选`, `${p.selectedGroups?.().size ?? 0} selected`)} open={groupsOpen()} onToggle={() => groupsOpen.set(!groupsOpen())}>
        <View style={{ gap: 2 }}>
          <Each of={manualGroups()} key={(g: any) => g.name}>{(g) => <CheckRow
            checked={!!p.selectedGroups?.().has(g().name)}
            onChange={(checked) => p.onToggleGroup?.(g().name, checked)}
            label={g().name}
            subtitle={groupTypeLabel(g().type ?? "select")}
          />}</Each>
        </View>
      </Collapsible>
    </If>

    {/* Advanced YAML Override */}
    <Collapsible testID={`${prefix}-advanced-toggle`} title={tr("高级 YAML 覆写", "Advanced YAML override")} summary={p.form().advancedYaml.trim() ? tr("已配置", "Configured") : tr("可选", "Optional")} open={advancedOpen()} onToggle={() => advancedOpen.set(!advancedOpen())}>
      <Field testID={`${prefix}-advanced-yaml`} multiline height={106} value={p.form().advancedYaml} onChange={(v) => update("advancedYaml", v)} placeholder={"udp: true\ndialer-proxy: JumpNode"} />
      <Label text={tr("按标准 Clash YAML 键值对填写，保存时合并到节点配置。", "Standard Clash YAML key-value pairs merged into this node on save.")} faint size={11} />
    </Collapsible>
  </View>;
}

/** Searchable & filterable member checklist (reused in both single-layer Modal and inline Tab). */
export function MemberPickerList(p: {
  available: { name: string; type?: string; local?: boolean }[];
  excludeNames: Set<string>;
  selected: () => Set<string>;
  onToggle: (name: string) => void;
  height?: number;
}) {
  const query = signal("");
  const proto = signal("");
  const protocols = () => {
    const s = new Set<string>();
    for (const item of p.available) {
      if (item.type) s.add(String(item.type).toLowerCase());
    }
    return [...s].sort();
  };
  const candidates = () => {
    const q = query().trim().toLowerCase();
    const pr = proto();
    return p.available.filter((item) => {
      if (p.excludeNames.has(item.name)) return false;
      if (pr && String(item.type ?? "").toLowerCase() !== pr) return false;
      if (q && !item.name.toLowerCase().includes(q) && !String(item.type ?? "").toLowerCase().includes(q)) return false;
      return true;
    });
  };

  return <View style={{ gap: 8 }}>
    <Row gap={8}>
      <View style={{ flex: 1, minWidth: 0 }}>
        <SearchField testID="member-picker-search" value={query()} onChange={(v) => query.set(v)} placeholder={tr("搜索可选节点或策略组", "Search available proxies or groups")} />
      </View>
      <If when={protocols().length > 1}>
        <Select label={tr("协议筛选", "Protocol filter")} value={proto()} onChange={(v) => proto.set(v)} items={[{ value: "", title: tr("全部协议", "All protocols") }, ...protocols().map((x) => ({ value: x, title: x.toUpperCase() }))]} />
      </If>
    </Row>
    <View style={{ height: p.height ?? 260, borderRadius: 10, borderWidth: 1, borderColor: colors().border, backgroundColor: colors().surface, overflow: "hidden" }}>
      <List each={candidates()} keyExtractor={(it: any) => it.name} style={{ flex: 1 }} empty={<Empty icon="search" title={tr("没有可添加的成员", "No matching members")} description={tr("调整搜索关键词或协议筛选。", "Try adjusting your search or protocol filter.")} />}>
        {(item) => <View style={{ borderBottomWidth: 1, borderColor: colors().grid }}>
          <CheckRow
            testID={`pick-member-${item().name}`}
            checked={p.selected().has(item().name)}
            onChange={() => p.onToggle(item().name)}
            label={item().name}
            subtitle={item().type ? String(item().type).toUpperCase() : undefined}
            trailing={item().local ? () => <Tag text={tr("自建", "Local")} tone="accent" /> : undefined}
          />
        </View>}
      </List>
    </View>
  </View>;
}

/** Dual-mode Policy Group Editor (AppendMembers vs Explicit Members, ZERO nested modals). */
export function GroupEditorFields(p: {
  row: any;
  form: () => GroupFormState;
  setForm: (next: GroupFormState) => void;
  available: { name: string; type?: string; local?: boolean }[];
  inModal: boolean;
  onRequestModalPicker?: () => void;
}) {
  const inheritedOpen = signal(false);
  const modalTab = signal("selected");
  const isSubscriptionGroup = () => !!p.row?.source;
  const selfName = () => p.form().name.trim() || p.row?.name || "";

  const move = (list: string[], idx: number, dir: number): string[] => {
    if (idx + dir < 0 || idx + dir >= list.length) return list;
    const next = [...list];
    [next[idx], next[idx + dir]] = [next[idx + dir], next[idx]];
    return next;
  };

  const switchToCustom = () => {
    const f = p.form();
    const merged = [...f.inheritedMembers];
    for (const m of f.appendMembers) if (!merged.includes(m)) merged.push(m);
    p.setForm({ ...f, explicitMembers: true, members: merged });
  };

  const resetToFollowSubscription = () => {
    const f = p.form();
    const inheritedSet = new Set(f.inheritedMembers);
    const extra = f.members.filter((m) => !inheritedSet.has(m));
    p.setForm({ ...f, explicitMembers: false, appendMembers: extra });
  };

  const activeList = () => p.form().explicitMembers ? p.form().members : p.form().appendMembers;
  const setActiveList = (next: string[]) => {
    const f = p.form();
    if (f.explicitMembers) p.setForm({ ...f, members: next });
    else p.setForm({ ...f, appendMembers: next });
  };

  const excludeInPicker = () => {
    const ex = new Set<string>([selfName()]);
    if (!p.form().explicitMembers) {
      for (const m of p.form().inheritedMembers) ex.add(m);
    }
    return ex;
  };

  const selectedSet = () => new Set(activeList());
  const togglePickerMember = (name: string) => {
    const list = activeList();
    if (list.includes(name)) setActiveList(list.filter((m) => m !== name));
    else setActiveList([...list, name]);
  };

  const reorderRow = (member: () => string, idx: () => number, listLen: () => number) => (
    <View style={{ flexDirection: "row", alignItems: "center", gap: 6, paddingLeft: 10, paddingRight: 4, minHeight: 36, borderBottomWidth: 1, borderColor: colors().grid, backgroundColor: colors().surface }}>
      <Text style={{ flex: 1, minWidth: 0, fontSize: 12.5, color: colors().text, numberOfLines: 1, textOverflow: "ellipsis" }} tooltip={member()}>{member()}</Text>
      <IconButton size={26} icon="arrowUp" label={tr("上移", "Move up")} disabled={idx() === 0} onPress={() => setActiveList(move(activeList(), idx(), -1))} />
      <IconButton size={26} icon="arrowDown" label={tr("下移", "Move down")} disabled={idx() >= listLen() - 1} onPress={() => setActiveList(move(activeList(), idx(), 1))} />
      <IconButton size={26} icon="x" label={tr("移除", "Remove")} onPress={() => setActiveList(activeList().filter((_, i) => i !== idx()))} />
    </View>
  );

  const openPicker = () => {
    if (p.inModal) modalTab.set("add");
    else p.onRequestModalPicker?.();
  };

  return <View style={{ gap: 11 }}>
    <Field testID="group-edit-name" label={tr("名称", "Name")} value={p.form().name} onChange={(v) => p.setForm({ ...p.form(), name: v })} />
    <View style={{ gap: 6 }}>
      <Label text={tr("类型", "Type")} muted size={12} weight={600} />
      <Select testID="group-edit-type" label={tr("类型", "Type")} value={p.form().type} onChange={(v) => p.setForm({ ...p.form(), type: v })} items={[
        { value: "select", title: tr("手动选择 (select)", "Manual (select)") },
        { value: "url-test", title: tr("自动选择 (url-test)", "Auto (url-test)") },
        { value: "fallback", title: tr("故障转移 (fallback)", "Fallback") },
        { value: "load-balance", title: tr("负载均衡 (load-balance)", "Load balance") },
      ]} />
    </View>
    <If when={p.form().type !== "select"}>
      <Label text={tr("测速参数使用默认配置（generate_204 · 300s）", "Latency check uses default settings (generate_204 · 300s)")} faint size={11} />
    </If>

    {/* Mode banner & switch */}
    <If when={isSubscriptionGroup()}>
      <View style={{ padding: 10, borderRadius: 10, borderWidth: 1, borderColor: colors().border, backgroundColor: colorScheme() === "dark" ? "#ffffff04" : tint(colors().bg, "80"), gap: 6 }}>
        <Row justify="space-between" gap={8}>
          <Row gap={6} style={{ flex: 1, minWidth: 0 }}>
            <Tag text={p.form().explicitMembers ? tr("自定义完整列表", "Custom full list") : tr("跟随订阅新增", "Follow subscription")} tone={p.form().explicitMembers ? "warn" : "accent"} />
          </Row>
          <If when={!p.form().explicitMembers}>
            <Action size="small" label={tr("转为自定义完整列表", "Switch to custom list")} onPress={switchToCustom} />
          </If>
          <If when={p.form().explicitMembers}>
            <Action size="small" quiet label={tr("恢复跟随订阅新增", "Follow subscription")} onPress={resetToFollowSubscription} />
          </If>
        </Row>
        <Label
          text={p.form().explicitMembers
            ? tr("完全由你控制成员顺序；订阅更新不会自动追加新节点。", "You control the full member order; subscription updates won't auto-append new nodes.")
            : tr("订阅更新时自动保留新节点，本地追加的节点固定保留在末尾。", "Subscription refreshes keep upstream nodes automatically while preserving your appended nodes.")}
          faint size={11} lines={2}
        />
      </View>
    </If>

    {/* Zero-nested-modal tab switch when inside a Modal */}
    <If when={p.inModal}>
      <Tabs
        testIDPrefix="group-modal-tab"
        value={modalTab()}
        onChange={(v) => modalTab.set(v)}
        items={[
          { id: "selected", label: p.form().explicitMembers ? tr(`已选成员 (${p.form().members.length})`, `Members (${p.form().members.length})`) : tr(`本地追加 (${p.form().appendMembers.length})`, `Appended (${p.form().appendMembers.length})`) },
          { id: "add", label: p.form().explicitMembers ? tr("添加成员", "Add members") : tr("追加成员", "Append members") },
        ]}
      />
    </If>

    <If when={!p.inModal || modalTab() === "selected"}>
      {/* Mode A: Collapsible readonly inherited members */}
      <If when={isSubscriptionGroup() && !p.form().explicitMembers}>
        <Collapsible
          title={tr(`继承自订阅 (${p.form().inheritedMembers.length})`, `Inherited from subscription (${p.form().inheritedMembers.length})`)}
          summary={tr("只读 · 随订阅更新", "Read-only")}
          open={inheritedOpen()}
          onToggle={() => inheritedOpen.set(!inheritedOpen())}
        >
          <View style={{ maxHeight: 130 }}>
            <Scroll style={{ maxHeight: 130 }}>
              <View style={{ gap: 4 }}>
                <Each of={p.form().inheritedMembers}>{(m) => <Label text={m()} muted size={12} lines={1} tooltip={m()} />}</Each>
                <If when={!p.form().inheritedMembers.length}><Label text={tr("无继承成员", "No inherited members")} faint size={11.5} /></If>
              </View>
            </Scroll>
          </View>
        </Collapsible>
      </If>

      {/* Editable member list (either appended or full custom) */}
      <Row justify="space-between" gap={8}>
        <Label
          text={p.form().explicitMembers
            ? tr(`成员列表 (${p.form().members.length})`, `Members (${p.form().members.length})`)
            : tr(`本地追加成员 (${p.form().appendMembers.length})`, `Appended local members (${p.form().appendMembers.length})`)}
          muted size={12} weight={600}
        />
        <Action testID="group-add-members-btn" size="small" icon="plus" label={p.form().explicitMembers ? tr("添加成员…", "Add members…") : tr("追加成员", "Append members")} onPress={openPicker} />
      </Row>

      <View style={{ borderRadius: 10, borderWidth: 1, borderColor: colors().border, backgroundColor: colors().surface, overflow: "hidden" }}>
        <If when={activeList().length > 0}>
          <Each of={activeList()}>{(m, i) => reorderRow(m, i, () => activeList().length)}</Each>
        </If>
        <If when={activeList().length === 0}>
          <View style={{ padding: 14, alignItems: "center" }}>
            <Label
              text={p.form().explicitMembers ? tr("请至少添加一个成员", "Add at least one member") : tr("尚未追加本地节点，点击上方按钮添加", "No local members appended yet")}
              faint size={11.5}
            />
          </View>
        </If>
      </View>
    </If>

    <If when={p.inModal && modalTab() === "add"}>
      <MemberPickerList
        available={p.available}
        excludeNames={excludeInPicker()}
        selected={selectedSet}
        onToggle={togglePickerMember}
        height={220}
      />
    </If>
  </View>;
}

/** Quick Add Node Modal (width 560, single-layer, works even at 0 profiles via createLocal). */
export function QuickAddNodeModal(p: {
  open: boolean;
  onClose: () => void;
  page: PageProps;
  activeProfileId: string;
  activeProfileKind?: string;
  defaultGroupName?: string;
  groups: any[];
  onSaved: (notice: string) => void;
}) {
  const tab = signal("paste");
  const rawText = signal("");
  const parseMode = signal<"auto" | "nodes" | "url">("auto");
  const parseRes = signal<any>(null);
  const parsing = signal(false);
  const selectedIndexes = signal<Set<number>>(new Set());
  const errorsOpen = signal(false);
  const targetGroup = signal("");
  const manualForm = signal<StructuredNodeForm>(extractNodeForm({ type: "vless", port: 443 }));
  const saving = signal(false);
  const err = signal("");

  let parseTimer: any = null;
  let parseSeq = 0;

  effect(() => {
    if (!p.open) return;
    const def = p.defaultGroupName || p.groups.find((g: any) => ["select", "selector"].includes(String(g.type).toLowerCase()) && g.name !== "GLOBAL")?.name || "";
    if (!targetGroup() && def) targetGroup.set(def);
  });

  const runParse = (text: string, mode: "auto" | "nodes" | "url") => {
    clearTimeout(parseTimer);
    if (!text.trim()) {
      parseRes.set(null);
      selectedIndexes.set(new Set());
      parsing.set(false);
      return;
    }
    const seq = ++parseSeq;
    parsing.set(true);
    parseTimer = setTimeout(async () => {
      try {
        const res = await p.page.call("desktop.proxies.parse", { text, mode });
        if (seq !== parseSeq) return;
        parseRes.set(res);
        const nodes: any[] = res?.nodes ?? [];
        selectedIndexes.set(new Set(nodes.map((_, i) => i)));
        err.set("");
      } catch (e) {
        if (seq === parseSeq) err.set(e instanceof Error ? e.message : String(e));
      } finally {
        if (seq === parseSeq) parsing.set(false);
      }
    }, 220);
  };

  const resetAndClose = () => {
    rawText.set("");
    parseMode.set("auto");
    parseRes.set(null);
    selectedIndexes.set(new Set());
    manualForm.set(extractNodeForm({ type: "vless", port: 443 }));
    err.set("");
    p.onClose();
  };

  const handleClose = async () => {
    const dirty = Boolean(rawText().trim() || manualForm().server.trim() || manualForm().name.trim());
    if (dirty) {
      const discard = await confirmDialog({
        title: tr("放弃新增节点？", "Discard new proxy?"),
        description: tr("当前填写或粘贴的内容尚未保存。", "Your entered or pasted proxy details have not been saved."),
        confirmLabel: tr("放弃更改", "Discard"),
        cancelLabel: tr("继续编辑", "Keep editing"),
        destructive: true,
      });
      if (!discard) return;
    }
    resetAndClose();
  };

  const toggleIndex = (idx: number) => {
    const next = new Set(selectedIndexes());
    if (next.has(idx)) next.delete(idx);
    else next.add(idx);
    selectedIndexes.set(next);
  };

  const parsedNodes = (): any[] => parseRes()?.nodes ?? [];
  const chosenParsed = () => parsedNodes().filter((_, i) => selectedIndexes().has(i));
  const failures = (): any[] => parseRes()?.failures ?? [];

  const manualGroups = () => p.groups.filter((g: any) => !g.hidden && g.name !== "GLOBAL");

  const save = async () => {
    if (saving()) return;
    saving.set(true);
    err.set("");
    try {
      let nodesToAdd: any[] = [];
      if (tab() === "paste") {
        nodesToAdd = chosenParsed();
        if (!nodesToAdd.length) throw new Error(tr("请先粘贴并勾选至少一个有效节点", "Paste and select at least one valid proxy first"));
      } else {
        const built = buildNodeValue(manualForm());
        nodesToAdd = [built];
      }

      if (!p.activeProfileId) {
        // 0 profiles: auto-create "本地节点" local profile!
        await p.page.call("desktop.profiles.createLocal", {
          name: tr("本地节点", "Local Nodes"),
          nodes: nodesToAdd,
          activate: true,
        });
        await p.page.refresh();
        p.onSaved(tr(`已创建“本地节点”并添加 ${nodesToAdd.length} 个节点`, `Created “Local Nodes” with ${nodesToAdd.length} proxies`));
        resetAndClose();
        return;
      }

      const model = await p.page.call<ProxyEditorModel>("desktop.proxies.editor", { id: p.activeProfileId });
      const groupsToAppend = targetGroup() ? [targetGroup()] : [];
      const { edits, addedNames } = buildAppendNodesEdits(model, nodesToAdd, groupsToAppend);
      await p.page.call("desktop.proxies.save", { id: model.id, edits });
      await p.page.refresh();
      p.onSaved(tr(`已添加 ${addedNames.length} 个本地节点`, `Added ${addedNames.length} local proxies`));
      resetAndClose();
    } catch (e) {
      err.set(e instanceof Error ? e.message : String(e));
    } finally {
      saving.set(false);
    }
  };

  const canSave = () => !saving() && (tab() === "paste" ? chosenParsed().length > 0 : !!manualForm().server.trim());

  return <Modal
    testID="quick-add-node-dialog"
    closeTestID="quick-add-node-close"
    title={tr("添加节点", "Add proxy")}
    open={p.open}
    onClose={() => void handleClose()}
    width={560}
    error={err()}
    footer={() => <Row justify="space-between" gap={8}>
      <Label text={!p.activeProfileId ? tr("将自动创建并启用“本地节点”档案", "Will create & activate “Local Nodes”") : tr("本地叠加保存，更新订阅不丢失", "Saved in local overlay")} faint size={11} lines={1} />
      <Row gap={8}>
        <Action label={tr("取消", "Cancel")} onPress={() => void handleClose()} />
        <Action testID="quick-add-node-save" primary label={saving() ? tr("正在保存…", "Saving…") : tab() === "paste" && chosenParsed().length > 1 ? tr(`添加 ${chosenParsed().length} 个节点`, `Add ${chosenParsed().length} proxies`) : tr("保存并添加", "Save & add")} disabled={!canSave()} onPress={() => void save()} />
      </Row>
    </Row>}
  >
    <Tabs testIDPrefix="quick-add-tab" value={tab()} onChange={(v) => { tab.set(v); err.set(""); }} items={[{ id: "paste", label: tr("粘贴链接 / 批量", "Paste links / batch") }, { id: "manual", label: tr("手动填写", "Manual entry") }]} />

    <If when={tab() === "paste"}>
      <Field
        testID="quick-add-paste-input"
        label={tr("节点分享链接、Base64 或 Clash YAML", "Share links, Base64 or Clash YAML")}
        multiline
        height={108}
        value={rawText()}
        onChange={(v) => { rawText.set(v); parseMode.set("auto"); runParse(v, "auto"); }}
        placeholder={tr("粘贴 vless:// / vmess:// / trojan:// / ss:// / hy2:// / tuic:// / wireguard://（支持多行批量）", "Paste vless://, vmess://, trojan://, ss://, hy2://, tuic://, wireguard:// (multi-line supported)")}
      />

      <If when={parsing()}><Label text={tr("正在解析…", "Parsing…")} muted size={11.5} /></If>

      <If when={!!parseRes()?.ambiguousHttp}>
        <Row gap={8} style={{ paddingHorizontal: 10, paddingVertical: 7, borderRadius: 8, backgroundColor: tint(colors().warn, "14"), borderWidth: 1, borderColor: tint(colors().warn, "30") }}>
          <Label text={parseMode() === "nodes" ? tr("按 HTTP 代理节点解析", "Parsed as HTTP proxy node") : tr("识别为订阅链接，如需导入为 HTTP 节点请切换", "Detected as subscription URL")} size={11.5} />
          <View style={{ flex: 1 }} />
          <Action size="small" label={parseMode() === "nodes" ? tr("改为订阅链接", "Treat as URL") : tr("按 HTTP 节点导入", "Parse as HTTP node")} onPress={() => { const next = parseMode() === "nodes" ? "url" : "nodes"; parseMode.set(next); runParse(rawText(), next); }} />
        </Row>
      </If>

      <If when={parseRes()?.kind === "url" && !parsedNodes().length}>
        <Feedback text={tr("这是一个订阅链接。若要导入整份订阅，请前往“订阅”页添加。", "This looks like a subscription URL. Use the Profiles page to import a full subscription.")} />
      </If>

      <If when={parsedNodes().length > 0}>
        <Row justify="space-between">
          <Label text={tr(`已解析 ${parsedNodes().length} 个节点（已选 ${chosenParsed().length}）`, `Parsed ${parsedNodes().length} proxies (${chosenParsed().length} selected)`)} weight={600} size={12} />
          <Row gap={6}>
            <Action size="small" quiet label={tr("全选", "All")} onPress={() => selectedIndexes.set(new Set(parsedNodes().map((_, i) => i)))} />
            <Action size="small" quiet label={tr("清空", "None")} onPress={() => selectedIndexes.set(new Set())} />
          </Row>
        </Row>
        <View style={{ maxHeight: 168, borderRadius: 10, borderWidth: 1, borderColor: colors().border, backgroundColor: colors().surface, overflow: "hidden" }}>
          <Scroll style={{ maxHeight: 168 }}>
            <Each of={parsedNodes()}>{(node, idx) => <View style={{ borderBottomWidth: 1, borderColor: colors().grid }}>
              <CheckRow
                checked={selectedIndexes().has(idx())}
                onChange={() => toggleIndex(idx())}
                label={node().name || `${node().type}-${node().server}`}
                subtitle={`${String(node().type ?? "").toUpperCase()} · ${node().server}:${node().port}`}
              />
            </View>}</Each>
          </Scroll>
        </View>
      </If>

      <If when={failures().length > 0}>
        <Collapsible title={tr(`${failures().length} 条解析失败（其余有效节点仍可导入）`, `${failures().length} failed lines (valid proxies can still be imported)`)} open={errorsOpen()} onToggle={() => errorsOpen.set(!errorsOpen())}>
          <View style={{ gap: 4 }}>
            <Each of={failures()}>{(f) => <Label text={tr(`第 ${f().line} 行：${f().reason}`, `Line ${f().line}: ${f().reason}`)} color={colors().danger} size={11} lines={2} />}</Each>
          </View>
        </Collapsible>
      </If>

      <If when={!!p.activeProfileId && manualGroups().length > 0}>
        <View style={{ gap: 6 }}>
          <Label text={tr("追加到策略组（更新订阅后保留）", "Append to policy group (survives refresh)")} muted size={12} weight={600} />
          <Select label={tr("目标策略组", "Target policy group")} value={targetGroup()} onChange={(v) => targetGroup.set(v)} items={[{ value: "", title: tr("仅保存节点，暂不加入策略组", "Save node only (no group)") }, ...manualGroups().map((g: any) => ({ value: g.name, title: `${g.name} (${groupTypeLabel(g.type ?? "")})` }))]} />
        </View>
      </If>
    </If>

    <If when={tab() === "manual"}>
      <NodeEditorFields
        form={manualForm}
        setForm={(v) => manualForm.set(v)}
        showSubscriptionHint={!!p.activeProfileId && p.activeProfileKind === "url"}
        testIDPrefix="quick-manual"
      />
      <If when={!!p.activeProfileId && manualGroups().length > 0}>
        <View style={{ gap: 6 }}>
          <Label text={tr("追加到策略组", "Append to policy group")} muted size={12} weight={600} />
          <Select label={tr("目标策略组", "Target policy group")} value={targetGroup()} onChange={(v) => targetGroup.set(v)} items={[{ value: "", title: tr("暂不加入策略组", "None") }, ...manualGroups().map((g: any) => ({ value: g.name, title: g.name }))]} />
        </View>
      </If>
    </If>
  </Modal>;
}

/**
 * Single-layer targeted Proxy Manager Modal for INACTIVE profiles
 * (opened from the `+N local · M edited` overlay pill on Subscriptions page).
 */
export function ProfileProxyManagerModal(p: {
  profile: any | null;
  onClose: () => void;
  page: PageProps;
  onSaved: () => void;
}) {
  const model = signal<ProxyEditorModel | null>(null);
  const loading = signal(false);
  const saving = signal(false);
  const tab = signal("nodes");
  const query = signal("");
  const err = signal("");
  const editingNode = signal<any | null>(null);
  const nodeForm = signal<StructuredNodeForm>(extractNodeForm({}));
  const nodeGroups = signal<Set<string>>(new Set());
  const editingGroup = signal<any | null>(null);
  const groupForm = signal<GroupFormState>(extractGroupForm({}));
  const dirty = signal(false);

  const load = async (id: string) => {
    loading.set(true);
    err.set("");
    editingNode.set(null);
    editingGroup.set(null);
    dirty.set(false);
    try {
      const res = await p.page.call<ProxyEditorModel>("desktop.proxies.editor", { id });
      model.set(res);
    } catch (e) {
      err.set(e instanceof Error ? e.message : String(e));
    } finally {
      loading.set(false);
    }
  };

  let loadedId = "";
  effect(() => {
    const id = p.profile?.id ?? "";
    if (!id) { loadedId = ""; model.set(null); return; }
    if (id !== loadedId) { loadedId = id; void load(id); }
  });

  const startEditNode = (row: any) => {
    editingGroup.set(null);
    editingNode.set(row);
    nodeForm.set(extractNodeForm(row));
    nodeGroups.set(new Set(groupsContainingNode(model(), row.name)));
    dirty.set(false);
    err.set("");
  };

  const startNewNode = () => {
    const m = model();
    const defGroup = m?.groups?.find((g: any) => ["select", "selector"].includes(String(g.type).toLowerCase()))?.name;
    editingGroup.set(null);
    editingNode.set({ source: "", localId: "", name: "", type: "vless", server: "", port: 443, options: { type: "vless" } });
    nodeForm.set(extractNodeForm({ type: "vless", port: 443 }));
    nodeGroups.set(new Set(defGroup ? [defGroup] : []));
    dirty.set(false);
    err.set("");
  };

  const startEditGroup = (row: any) => {
    editingNode.set(null);
    editingGroup.set(row);
    groupForm.set(extractGroupForm(row));
    dirty.set(false);
    err.set("");
  };

  const saveEdit = async () => {
    const m = model();
    if (!m || saving()) return;
    saving.set(true);
    err.set("");
    try {
      let nextEdits: any;
      if (editingNode()) {
        nextEdits = buildNodeSaveEdits(m, editingNode(), nodeForm(), nodeGroups()).edits;
      } else if (editingGroup()) {
        nextEdits = buildGroupSaveEdits(m, editingGroup(), groupForm()).edits;
      }
      await p.page.call("desktop.proxies.save", { id: m.id, edits: nextEdits });
      await p.page.refresh();
      p.onSaved();
      await load(m.id);
    } catch (e) {
      err.set(e instanceof Error ? e.message : String(e));
    } finally {
      saving.set(false);
    }
  };

  const deleteOrRestoreNode = async (row: any) => {
    const m = model();
    if (!m || saving()) return;
    saving.set(true);
    err.set("");
    try {
      let nextEdits: any;
      if (!row.source || m.isPureLocal) {
        const res = buildNodeDeleteEdits(m, row);
        if (res.error) throw new Error(res.error);
        nextEdits = res.edits;
      } else {
        nextEdits = buildNodeRestoreEdits(m, row);
      }
      await p.page.call("desktop.proxies.save", { id: m.id, edits: nextEdits });
      await p.page.refresh();
      p.onSaved();
      await load(m.id);
    } catch (e) {
      err.set(e instanceof Error ? e.message : String(e));
    } finally {
      saving.set(false);
    }
  };

  const handleClose = async () => {
    if (dirty()) {
      const ok = await confirmDialog({
        title: tr("放弃未保存更改？", "Discard unsaved changes?"),
        description: tr("当前编辑的节点或策略组尚未保存。", "Your edits have not been saved."),
        confirmLabel: tr("放弃更改", "Discard"),
        cancelLabel: tr("继续编辑", "Keep editing"),
        destructive: true,
      });
      if (!ok) return;
    }
    p.onClose();
  };

  const filteredNodes = () => {
    const q = query().trim().toLowerCase();
    return (model()?.nodes ?? []).filter((n: any) => !q || String(n.name).toLowerCase().includes(q) || String(n.type ?? "").toLowerCase().includes(q) || String(n.server ?? "").toLowerCase().includes(q));
  };

  const availableMembers = () => {
    const m = model();
    if (!m) return [];
    const byName = new Map<string, { name: string; type?: string; local?: boolean }>();
    for (const n of m.nodes ?? []) byName.set(n.name, { name: n.name, type: n.type, local: !n.source });
    for (const g of m.groups ?? []) if (!g.deleted && !byName.has(g.name)) byName.set(g.name, { name: g.name, type: g.type });
    for (const name of m.availableNames ?? m.options ?? []) if (!byName.has(name)) byName.set(name, { name });
    return [...byName.values()];
  };

  const inDetail = () => !!editingNode() || !!editingGroup();

  return <Modal
    testID="profile-proxy-manager-dialog"
    closeTestID="profile-proxy-manager-close"
    title={tr(`管理节点 · ${p.profile?.name ?? ""}`, `Manage proxies · ${p.profile?.name ?? ""}`)}
    open={!!p.profile}
    onClose={() => void handleClose()}
    width={620}
    error={err()}
    footer={() => <Row justify="space-between" gap={8}>
      <If when={inDetail()}>
        <Action quiet label={tr("← 返回列表", "← Back to list")} onPress={() => { editingNode.set(null); editingGroup.set(null); dirty.set(false); err.set(""); }} />
      </If>
      <If when={!inDetail()}><View style={{ flex: 1 }} /></If>
      <Row gap={8}>
        <Action label={tr("关闭", "Close")} onPress={() => void handleClose()} />
        <If when={inDetail()}>
          <Action primary label={saving() ? tr("正在保存…", "Saving…") : tr("保存更改", "Save changes")} disabled={saving()} onPress={() => void saveEdit()} />
        </If>
      </Row>
    </Row>}
  >
    <If when={p.profile && !p.profile.active}>
      <Feedback text={tr(`正在编辑非活跃档案“${p.profile.name}”，保存不影响当前运行配置。`, `Editing inactive profile “${p.profile.name}”. Saving does not affect your active connection.`)} />
    </If>

    <If when={loading()}><Label text={tr("正在读取节点配置…", "Loading proxies…")} muted /></If>

    <If when={!loading() && !!model() && !inDetail()}>
      <Row justify="space-between" gap={8}>
        <Tabs value={tab()} onChange={(v) => tab.set(v)} items={[{ id: "nodes", label: tr(`节点 (${model()?.nodes?.length ?? 0})`, `Proxies (${model()?.nodes?.length ?? 0})`) }, { id: "groups", label: tr(`策略组 (${model()?.groups?.length ?? 0})`, `Groups (${model()?.groups?.length ?? 0})`) }]} />
        <Action size="small" icon="plus" label={tr("新增本地节点", "Add local proxy")} onPress={startNewNode} />
      </Row>
      <If when={tab() === "nodes"}>
        <SearchField value={query()} onChange={(v) => query.set(v)} placeholder={tr("搜索节点名、协议或地址", "Search proxy name, protocol or host")} />
        <Group>
          <Each of={filteredNodes()} key={(n: any) => n.localId || n.source || n.name}>{(n) => <GroupRow hover gap={10} style={{ paddingHorizontal: 12, minHeight: 46 }}>
            <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
              <Row gap={6}>
                <Label text={n().name} weight={500} lines={1} tooltip={n().name} />
                <If when={!n().source}><Tag text={tr("自建", "Local")} tone="accent" /></If>
                <If when={!!n().source && !!n().edited && !n().missing}><Tag text={tr("已修改", "Edited")} tone="warn" /></If>
                <If when={!!n().missing}><Tag text={tr("原节点已下线", "Source missing")} tone="danger" /></If>
              </Row>
              <Label text={`${String(n().type ?? "").toUpperCase()} · ${n().server ?? ""}:${n().port ?? ""}`} faint size={11} lines={1} />
            </View>
            <If when={!n().source || !!n().edited || !!n().missing || !!model()?.isPureLocal}>
              <Action size="small" quiet danger={!n().source || !!n().missing || !!model()?.isPureLocal} label={!n().source || !!model()?.isPureLocal ? tr("删除", "Delete") : n().missing ? tr("移除修改", "Remove edit") : tr("恢复", "Reset")} onPress={() => void deleteOrRestoreNode(n())} />
            </If>
            <Action size="small" quiet label={tr("编辑", "Edit")} onPress={() => startEditNode(n())} />
          </GroupRow>}</Each>
        </Group>
      </If>
      <If when={tab() === "groups"}>
        <Group>
          <Each of={model()?.groups ?? []} key={(g: any) => g.source || g.name}>{(g) => <GroupRow hover gap={10} style={{ paddingHorizontal: 12, minHeight: 46 }}>
            <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
              <Row gap={6}>
                <Label text={g().name} weight={500} lines={1} />
                <If when={!!g().edited}><Tag text={tr("已修改", "Edited")} tone="warn" /></If>
              </Row>
              <Label text={`${groupTypeLabel(g().type ?? "")} · ${(g().members ?? []).length} ${tr("成员", "members")}`} faint size={11} />
            </View>
            <Action size="small" quiet label={tr("编辑", "Edit")} onPress={() => startEditGroup(g())} />
          </GroupRow>}</Each>
        </Group>
      </If>
    </If>

    <If when={!!editingNode()}>
      <NodeEditorFields
        form={nodeForm}
        setForm={(next) => { nodeForm.set(next); dirty.set(true); }}
        groups={model()?.groups ?? []}
        selectedGroups={nodeGroups}
        onToggleGroup={(name, checked) => {
          const next = new Set(nodeGroups());
          if (checked) next.add(name); else next.delete(name);
          nodeGroups.set(next);
          dirty.set(true);
        }}
        showSubscriptionHint={model()?.profileKind === "url"}
      />
    </If>

    <If when={!!editingGroup()}>
      <GroupEditorFields
        row={editingGroup()}
        form={groupForm}
        setForm={(next) => { groupForm.set(next); dirty.set(true); }}
        available={availableMembers()}
        inModal={true}
      />
    </If>
  </Modal>;
}
