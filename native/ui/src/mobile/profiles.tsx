import { Scroll, signal, Text, View } from "nsh";
import { Button } from "nsh/base";
import { call } from "../client";
import type { ParseResult, Profile, ProfileEditor, ProfileMutation } from "../client";
import { tr } from "../i18n";
import { colors } from "../theme";
import { androidCall, androidHost, haptic } from "./platform";
import {
  acceptStatus, action, activeProfile, error, loadProxyEditor, mutationBusy,
  openProxiesFilteredLocal, profiles, proxyEditorData, refresh, refreshLists,
  saveProxyEdits, selectedGroup, showToast, status, switchProfile, tasks, vpnActive,
} from "./store";
import { buildBatchAddNodesEdits, findDefaultTargetGroups } from "./proxy-helpers";
import { openGroupsManager } from "./group-editor";
import {
  Action, BottomSheet, bytes, confirmDialog, Field, formatExpiry, formatUpdated,
  Group, GroupRow, hasQuotaInfo, Heading, Icon, Label, MoreButton, Notice, Panel,
  ProgressBar, quotaPercent, RADIUS, SectionLabel, Segmented, Tag, tint, ToggleSwitch,
} from "./ui";

const UA_PRESETS = [
  { id: "", zh: "默认", en: "Default" },
  { id: "clash.meta", zh: "Mihomo", en: "Mihomo" },
  { id: "clash-verge/v2.0.0", zh: "Clash Verge", en: "Clash Verge" },
];

const AUTO_UPDATE_OPTIONS = [
  { id: "global", zh: "跟随全局", en: "Follow global" },
  { id: "0", zh: "关闭", en: "Off" },
  { id: "6", zh: "每 6 小时", en: "6h" },
  { id: "12", zh: "每 12 小时", en: "12h" },
  { id: "24", zh: "每 24 小时", en: "24h" },
  { id: "custom", zh: "自定义", en: "Custom" },
];

// Module-level drafts & sheet state survive appearance remounts.
const name = signal("");
const smartInput = signal("");
const importModeOverride = signal<"auto" | "url" | "nodes">("auto");
const detectedParse = signal<ParseResult | null>(null);
const nodeDestination = signal<"append" | "newLocal">("append");
const importAdvancedOpen = signal(false);
const importUserAgent = signal("");
const importUseProxy = signal(false);
const adding = signal(false);
const importError = signal("");

const editing = signal("");
const editDetail = signal<ProfileEditor | null>(null);
const editName = signal("");
const editURL = signal("");
const editKind = signal("");
const editUserAgent = signal("");
const editUseProxy = signal(false);
const editUpdatePreset = signal("global");
const editCustomHours = signal("24");
const editError = signal("");

const profileMenu = signal<Profile | null>(null);
const aggregateDetail = signal<Profile | null>(null);

let detectTimer: ReturnType<typeof setTimeout> | undefined;
let detectSeq = 0;

export function openAddProfileSheet(): void {
  importError.set("");
  adding.set(true);
}

function quickHeuristicKind(raw: string): "empty" | "url" | "nodes" | "clash_yaml" {
  const s = raw.trim();
  if (!s) return "empty";
  if (/^https?:\/\/\S+$/i.test(s) && !s.includes("\n")) {
    if (/^https?:\/\/[^/\s@]+@[^/\s]+/i.test(s)) return "nodes";
    return "url";
  }
  if (/^(vless|vmess|trojan|ss|ssr|hysteria|hysteria2|hy2|tuic|wireguard|wg|socks|socks5|socks5h):\/\//i.test(s)) {
    return "nodes";
  }
  if (/^\s*(proxies|proxy-groups|rules)\s*:/m.test(s)) {
    return "clash_yaml";
  }
  return "clash_yaml";
}

function scheduleSmartDetect(text: string, mode: "auto" | "url" | "nodes") {
  clearTimeout(detectTimer);
  const trimmed = text.trim();
  if (!trimmed) {
    detectedParse.set(null);
    return;
  }
  const seq = ++detectSeq;
  detectTimer = setTimeout(() => {
    void call<ParseResult>("proxies.parse", { text: trimmed, mode })
      .then(res => {
        if (seq === detectSeq) detectedParse.set(res);
      })
      .catch(() => {
        // Fallback silently to heuristic when preview transport doesn't respond
      });
  }, 180);
}

function updateSmartInput(value: string) {
  smartInput.set(value);
  importModeOverride.set("auto");
  if (importError()) importError.set("");
  scheduleSmartDetect(value, "auto");
}

function toggleHttpAmbiguity() {
  haptic("selection");
  const currentKind = effectiveDetectedKind();
  const nextMode = currentKind === "url" ? "nodes" : "url";
  importModeOverride.set(nextMode);
  scheduleSmartDetect(smartInput(), nextMode);
}

function effectiveDetectedKind(): "empty" | "url" | "nodes" | "clash_yaml" | "unknown" {
  const raw = smartInput().trim();
  if (!raw) return "empty";
  if (importModeOverride() === "url") return "url";
  if (importModeOverride() === "nodes") return "nodes";
  const parsed = detectedParse();
  if (parsed && parsed.kind !== "empty") return parsed.kind;
  return quickHeuristicKind(raw);
}

function isAmbiguousHttpInput(): boolean {
  const raw = smartInput().trim();
  if (!raw || raw.includes("\n")) return false;
  if (detectedParse()?.ambiguousHttp) return true;
  return /^https?:\/\/[^/\s]+/i.test(raw);
}

function autoUpdateBadge(hours?: number | null): string {
  if (hours === null || hours === undefined) return "";
  if (hours === 0) return tr("已关闭自动更新", "Auto-update off");
  return tr(`每 ${hours}h 自动更新`, `Every ${hours}h`);
}

function overlaySummaryBadge(p: Profile): string {
  const local = p.localNodeCount ?? 0;
  const edited = (p.editedNodeCount ?? 0) + (p.editedGroupCount ?? 0);
  const parts: string[] = [];
  if (local > 0) parts.push(tr(`+${local} 自建`, `+${local} local`));
  if (edited > 0) parts.push(tr(`${edited} 修改`, `${edited} edited`));
  return parts.join(" · ");
}

export async function requestSwitchProfile(target: Profile) {
  if (target.active || mutationBusy()) return;
  if (vpnActive()) {
    const ok = await confirmDialog({
      title: tr("切换配置需要断开当前连接", "Disconnect to switch profile?"),
      description: tr(`将先断开当前 VPN，切换到「${target.name}」后重新连接。`, `Current VPN will disconnect, switch to "${target.name}", and reconnect.`),
      confirmLabel: tr("断开并切换", "Disconnect & switch"),
      cancelLabel: tr("取消", "Cancel"),
    });
    if (!ok) return;
    await switchProfile(target.id, { name: target.name, reconnectVpn: true });
    return;
  }
  await switchProfile(target.id, { name: target.name, reconnectVpn: false });
}

async function requestDeleteProfile(target: Profile) {
  if (target.active && status()?.running) {
    showToast(tr("请先断开连接后再删除当前配置", "Disconnect before deleting the active profile"));
    return;
  }
  const deps = target.dependentAggregates ?? [];
  const depNote = deps.length
    ? tr(`\n注意：聚合配置「${deps.map(d => d.name).join("、")}」依赖此订阅。`, `\nNote: aggregate profiles (${deps.map(d => d.name).join(", ")}) depend on this profile.`)
    : "";
  const ok = await confirmDialog({
    title: tr(`删除「${target.name}」？`, `Delete "${target.name}"?`),
    description: tr("将删除本地订阅和缓存，此操作无法撤销。", "This removes the saved profile and cache and cannot be undone.") + depNote,
    confirmLabel: tr("删除订阅", "Delete profile"),
    cancelLabel: tr("取消", "Cancel"),
    destructive: true,
  });
  if (!ok) return;
  const done = await action("delete", async () => {
    await call("profiles.delete", { id: target.id });
  });
  if (done) { haptic("confirm"); showToast(tr("已删除订阅", "Profile deleted")); }
}

async function importProfile() {
  const raw = smartInput().trim();
  if (!raw) return;
  importError.set("");
  const mode = importModeOverride();
  const ok = await action("import", async () => {
    let parsed: ParseResult | null = null;
    try {
      parsed = await call<ParseResult>("proxies.parse", { text: raw, mode });
    } catch {
      parsed = null;
    }

    const kind = mode !== "auto" ? mode : (parsed?.kind ?? quickHeuristicKind(raw));
    const trimmedName = name().trim();

    if (kind === "nodes" && parsed?.nodes && parsed.nodes.length > 0) {
      const hasActive = !!status()?.activeProfile && profiles().length > 0;
      if (hasActive && nodeDestination() === "append") {
        let ed = proxyEditorData();
        const activeId = status()!.activeProfile;
        if (!ed || (ed.profileId || ed.id) !== activeId) {
          ed = await loadProxyEditor(activeId);
        }
        if (!ed) throw new Error(tr("无法加载当前订阅", "Could not load active profile"));
        const targetGroups = findDefaultTargetGroups(ed, selectedGroup()?.name);
        const edits = buildBatchAddNodesEdits(ed, parsed.nodes, targetGroups);
        const res = await saveProxyEdits(ed.profileId || ed.id, edits);
        smartInput.set(""); name.set(""); detectedParse.set(null); importModeOverride.set("auto"); adding.set(false);
        haptic("confirm");
        showToast(res.reconnectRequired
          ? tr(`已追加 ${parsed.nodes.length} 个自建节点，重连后生效`, `Appended ${parsed.nodes.length} local nodes. Reconnect to apply.`)
          : tr(`已追加 ${parsed.nodes.length} 个自建节点到当前订阅`, `Appended ${parsed.nodes.length} local nodes`));
        return;
      }
      const res = await call<ProfileMutation>("profiles.createLocal", {
        name: trimmedName || tr("本地节点", "Local Nodes"),
        nodes: parsed.nodes,
        activate: profiles().length === 0,
      });
      if (res.status) acceptStatus(res.status);
      await refreshLists();
      smartInput.set(""); name.set(""); detectedParse.set(null); importModeOverride.set("auto"); adding.set(false);
      haptic("confirm");
      showToast(tr(`已创建本地档案（${parsed.nodes.length} 个节点）`, `Created local profile (${parsed.nodes.length} nodes)`));
      return;
    }

    if (kind === "url") {
      const payload: Record<string, unknown> = {
        url: parsed?.url || raw,
      };
      if (trimmedName) payload.name = trimmedName;
      if (importUserAgent().trim()) payload.userAgent = importUserAgent().trim();
      if (importUseProxy()) payload.useProxy = true;
      // DESIGN.md:90: Do NOT auto-activate when an active profile already exists.
      if (profiles().length === 0) payload.activate = false;
      await call("profiles.import", payload);
      smartInput.set(""); name.set(""); importUserAgent.set(""); importUseProxy.set(false); detectedParse.set(null); importModeOverride.set("auto"); adding.set(false);
      return;
    }

    // Clash YAML (or Base64 decoded YAML)
    const yamlContent = parsed?.yaml || raw;
    const payload: Record<string, unknown> = { yaml: yamlContent };
    if (trimmedName) payload.name = trimmedName;
    await call("profiles.import", payload);
    smartInput.set(""); name.set(""); detectedParse.set(null); importModeOverride.set("auto"); adding.set(false);
  });

  if (ok) {
    haptic("confirm");
    showToast(tr("配置已导入", "Profile imported"));
  } else {
    importError.set(error());
  }
}

async function beginEdit(id: string) {
  editError.set("");
  await action("editor", async () => {
    const p = await call<ProfileEditor>("profiles.editor", { id });
    editDetail.set(p);
    editing.set(p.id);
    editName.set(p.name);
    editURL.set(p.url ?? "");
    editKind.set(p.kind);
    editUserAgent.set(p.userAgent ?? "");
    editUseProxy.set(!!p.useProxy);
    if (p.updateHours === null || p.updateHours === undefined) {
      editUpdatePreset.set("global");
    } else if ([0, 6, 12, 24].includes(p.updateHours)) {
      editUpdatePreset.set(String(p.updateHours));
    } else {
      editUpdatePreset.set("custom");
      editCustomHours.set(String(p.updateHours));
    }
  });
}

async function saveEdit() {
  if (!editName().trim()) return;
  editError.set("");
  const isUrl = editKind() === "url";
  const preset = editUpdatePreset();
  let updatePayload: Record<string, unknown> = {};
  if (isUrl) {
    if (preset === "global") {
      updatePayload = { clearUpdateHours: true };
    } else if (preset === "custom") {
      const h = Math.floor(Number(editCustomHours()));
      if (!Number.isFinite(h) || h < 1 || h > 720) {
        editError.set(tr("自定义更新间隔须为 1–720 小时", "Custom interval must be 1–720 hours"));
        return;
      }
      updatePayload = { updateHours: h };
    } else {
      updatePayload = { updateHours: Number(preset) };
    }
  }

  const ok = await action("edit", async () => {
    await call("profiles.update", {
      id: editing(),
      name: editName().trim(),
      ...(isUrl ? {
        url: editURL().trim(),
        userAgent: editUserAgent().trim(),
        useProxy: editUseProxy(),
        ...updatePayload,
      } : {}),
    });
    editing.set("");
    editDetail.set(null);
  });
  if (ok) { haptic("confirm"); showToast(tr("订阅设置已保存", "Profile settings saved")); }
  else editError.set(error());
}

async function refreshSingleProfile(target: Profile) {
  await action(`refresh-${target.id}`, async () => {
    await call("profiles.refresh", { id: target.id });
    haptic("confirm");
    showToast(target.active && status()?.running
      ? tr("更新已保存，重连后生效", "Update saved, reconnect to apply")
      : target.isAggregate
        ? tr("聚合配置已重新生成", "Aggregate profile regenerated")
        : tr("订阅已更新", "Profile refreshed"));
  });
}

function handleOverlayBadgePress(p: Profile) {
  haptic("selection");
  if (p.active) {
    openProxiesFilteredLocal();
    return;
  }
  void openGroupsManager({ profileId: p.id, initialTab: "nodes" });
}

function QuotaBlock(props: { profile: Profile }) {
  const info = () => props.profile.info;
  const show = () => props.profile.kind === "url" && hasQuotaInfo(info());
  const used = () => Math.max(0, (info()?.upload ?? 0) + (info()?.download ?? 0));
  const total = () => info()?.total ?? 0;
  const pct = () => quotaPercent(info());
  const exp = () => formatExpiry(info()?.expire);

  return <If when={show()}>
    <View testID={`quota-${props.profile.id}`} style={{ gap: 6 }}>
      <If when={total() > 0}>
        <ProgressBar percent={pct()} expired={!!exp()?.expired} testID={`quota-bar-${props.profile.id}`} />
      </If>
      <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8, flexWrap: "wrap" }}>
        <If when={total() > 0}>
          <Label text={`${bytes(used())} / ${bytes(total())} (${Math.round(pct())}%)`} muted size={11.5} tabular />
        </If>
        <If when={total() <= 0 && used() > 0}>
          <Label text={tr(`已用 ${bytes(used())}`, `Used ${bytes(used())}`)} muted size={11.5} tabular />
        </If>
        <If when={!!exp()}>
          <Label
            text={exp()!.label}
            color={exp()!.expired ? colors().danger : exp()!.soon ? colors().warn : colors().muted}
            size={11.5}
            tabular />
        </If>
      </View>
    </View>
  </If>;
}

function ImportFormFields() {
  const kind = () => effectiveDetectedKind();
  const parsed = () => detectedParse();
  const nodeCount = () => parsed()?.nodes?.length ?? 0;
  const ambiguous = () => isAmbiguousHttpInput();

  const detectionBadgeText = () => {
    switch (kind()) {
      case "url": return tr("识别为订阅链接", "Detected: Subscription URL");
      case "nodes": return nodeCount() > 0 ? tr(`识别到 ${nodeCount()} 个代理节点`, `Detected ${nodeCount()} proxy nodes`) : tr("识别为代理节点链接", "Detected: Proxy node");
      case "clash_yaml": return tr("识别为 Clash YAML 配置", "Detected: Clash YAML");
      default: return "";
    }
  };

  return <View style={{ gap: 12 }}>
    <Field
      label={tr("名称（选填）", "Name (optional)")}
      testID="profile-name"
      value={name()}
      onChange={value => name.set(value)}
      placeholder={tr("留空自动识别名称", "Auto-detected if left blank")}
      disabled={mutationBusy()} />

    <Field
      label={tr("订阅链接 / 节点链接 / Clash YAML", "Subscription URL / Node links / Clash YAML")}
      testID="profile-url"
      value={smartInput()}
      onChange={updateSmartInput}
      placeholder={tr("粘贴 https:// 订阅链接、vless:// 等节点链接、Base64 或 YAML", "Paste https:// subscription, vless:// links, Base64, or YAML")}
      multiline={smartInput().includes("\n") || smartInput().length > 120}
      height={120}
      disabled={mutationBusy()} />

    <If when={!!detectionBadgeText()}>
      <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", flexWrap: "wrap", gap: 8 }}>
        <Tag testID="import-detect-tag" text={detectionBadgeText()} tone="accent" />
        <If when={ambiguous()}>
          <Button
            testID="import-http-toggle"
            onPress={toggleHttpAmbiguity}
            style={{
              minHeight: 30, paddingHorizontal: 10, borderRadius: RADIUS.pill,
              backgroundColor: colors().hover, borderWidth: 1, borderColor: colors().border,
              alignItems: "center", justifyContent: "center",
            }}>
            <Text style={{ fontSize: 11.5, fontWeight: "500", color: colors().accent }}>
              {kind() === "url" ? tr("按 HTTP 节点导入", "Import as HTTP node") : tr("按订阅链接导入", "Import as subscription URL")}
            </Text>
          </Button>
        </If>
      </View>
    </If>

    <If when={kind() === "nodes" && !!status()?.activeProfile && profiles().length > 0}>
      <View style={{ gap: 6 }}>
        <Label text={tr("节点保存位置", "Destination")} bold size={12} muted />
        <Segmented
          testIDPrefix="import-node-dest"
          value={nodeDestination()}
          onChange={id => nodeDestination.set(id as "append" | "newLocal")}
          items={[
            { id: "append", label: tr("追加到当前订阅", "Append to active") },
            { id: "newLocal", label: tr("新建本地档案", "New local profile") },
          ]} />
        <If when={nodeDestination() === "append"}>
          <Label text={tr("作为自建节点附加到当前订阅，更新订阅不会删除，保留原订阅规则。", "Appended as local nodes; subscription refreshes preserve them.")} muted size={11.5} />
        </If>
      </View>
    </If>

    <If when={kind() === "url"}>
      <Group>
        <GroupRow
          testID="import-advanced-toggle"
          icon="sliders"
          title={tr("高级下载选项", "Advanced fetch options")}
          subtitle={[importUserAgent().trim() ? `UA: ${importUserAgent().trim()}` : tr("默认 UA", "Default UA"), importUseProxy() ? tr("通过代理下载", "Via proxy") : tr("直连下载", "Direct")].join(" · ")}
          trailing={() => <Icon name={importAdvancedOpen() ? "chevronDown" : "chevronRight"} size={16} color={colors().muted} />}
          onPress={() => { haptic("selection"); importAdvancedOpen.set(!importAdvancedOpen()); }} />
      </Group>
      <If when={importAdvancedOpen()}>
        <View style={{ gap: 10 }}>
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 6 }}>
            <Each of={UA_PRESETS} key={u => u.id || "default"}>{u => {
              const active = () => importUserAgent().trim() === u().id;
              return <Button
                onPress={() => { haptic("selection"); importUserAgent.set(u().id); }}
                style={{
                  minHeight: 34, paddingHorizontal: 12, borderRadius: RADIUS.pill,
                  alignItems: "center", justifyContent: "center",
                  backgroundColor: active() ? colors().selected : colors().bg,
                  borderWidth: 1, borderColor: active() ? colors().selectedBorder : colors().border,
                }}>
                <Text style={{ fontSize: 12, fontWeight: active() ? "600" : "500", color: active() ? colors().accent : colors().muted }}>{tr(u().zh, u().en)}</Text>
              </Button>;
            }}</Each>
          </View>
          <Field
            testID="import-user-agent"
            label="User-Agent"
            value={importUserAgent()}
            onChange={v => importUserAgent.set(v)}
            placeholder={tr("留空使用默认 User-Agent", "Leave blank for default")}
            disabled={mutationBusy()} />
          <Group>
            <GroupRow
              title={tr("通过当前代理下载", "Download via proxy")}
              subtitle={tr("仅在代理运行时生效，用于访问受限订阅源", "Uses active local proxy when downloading")}
              trailing={() => <ToggleSwitch testID="import-use-proxy" checked={importUseProxy()} onChange={v => importUseProxy.set(v)} label={tr("通过代理下载", "Download via proxy")} disabled={mutationBusy()} />} />
          </Group>
        </View>
      </If>
    </If>

    <If when={androidHost()}>
      <Action testID="import-file" icon="profiles" full label={tr("从文件导入", "Import from file")} disabled={mutationBusy()} onPress={() => void action("file", async () => {
        const file = await androidCall<{ name?: string; content?: string; cancelled?: boolean }>("android.file.pick");
        if (file.cancelled || !file.content) return;
        updateSmartInput(file.content);
        if (!name()) name.set(file.name ?? tr("本地配置", "Local profile"));
      }, false)} />
    </If>

    <Label text={tr("导入后在连接页启动；已有启用配置时导入不会自动切换。", "Connect after importing. Importing never auto-switches away from an active profile.")} muted size={12} />
  </View>;
}

export function Profiles() {
  const active = () => profiles().find(p => p.active);
  const others = () => profiles().filter(p => !p.active);
  const canImport = () => !mutationBusy() && !!smartInput().trim();

  const metaLine = (p: Profile) => {
    const sourceLabel = p.isAggregate
      ? tr(`聚合 (${p.aggregate?.sources?.length ?? 0} 源)`, `Aggregate (${p.aggregate?.sources?.length ?? 0} sources)`)
      : (p.isPureLocal || p.kind === "file" || p.kind === "local")
        ? tr("本地配置", "Local profile")
        : p.source;
    return [
      sourceLabel,
      (p.nodeCount ?? 0) > 0 ? tr(`${p.nodeCount} 节点`, `${p.nodeCount} nodes`) : "",
      formatUpdated(p.updated),
      p.kind === "url" ? autoUpdateBadge(p.updateHours) : "",
    ].filter(Boolean).join(" · ");
  };

  return <View style={{ flex: 1 }}>
    <Scroll testID="profiles-scroll" style={{ flex: 1 }} contentStyle={{ padding: 16, gap: 14 }}>
      <Heading
        title={tr("订阅", "Profiles")}
        description={vpnActive() ? tr("切换配置时将先断开当前 VPN 再重连。", "Switching profiles disconnects VPN first, then reconnects.") : tr("管理订阅来源、流量额度与本地自建叠加层。", "Manage subscriptions, quotas, and local node overlays.")}
        action={() => <If when={profiles().length > 0}><Action testID="add-profile" label={tr("添加订阅", "Add profile")} primary disabled={mutationBusy() || !!editing()} onPress={openAddProfileSheet} /></If>} />

      <If when={!profiles().length}>
        <Panel>
          <Label text={tr("添加第一个订阅或节点", "Add your first profile")} bold size={16} />
          <ImportFormFields />
          <If when={!!importError()}><Notice text={importError()} tone="danger" /></If>
          <Action testID="import-profile" primary full label={tasks().import ? tr("正在导入…", "Importing…") : tr("导入", "Import")} onPress={() => void importProfile()} disabled={!canImport()} />
        </Panel>
      </If>

      {/* Active Profile Card */}
      <If when={!!active()}>
        {(() => {
          const p = active()!;
          const overlayText = overlaySummaryBadge(p);
          const showUpdate = p.kind === "url" || !!p.isAggregate;
          return <>
            <SectionLabel title={tr("当前使用", "Current")} />
            <Panel accentLeft gap={10}>
              <View style={{ flexDirection: "row", alignItems: "flex-start", justifyContent: "space-between", gap: 10 }}>
                <View style={{ flex: 1, minWidth: 0, gap: 4 }}>
                  <View style={{ flexDirection: "row", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
                    <Text style={{ color: colors().text, fontSize: 16, fontWeight: "600", numberOfLines: 2 }}>{p.name}</Text>
                    <If when={!!p.isAggregate}><Tag text={tr("聚合", "Aggregate")} tone="accent" /></If>
                    <If when={!!p.isPureLocal}><Tag text={tr("纯本地", "Local")} tone="muted" /></If>
                  </View>
                  <Text style={{ color: colors().muted, fontSize: 12, numberOfLines: 2, textOverflow: "ellipsis-middle" }}>
                    {metaLine(p)}
                  </Text>
                </View>
                <Tag testID={`active-tag-${p.id}`} text={status()?.running ? tr("使用中", "Active") : tr("已选择", "Selected")} tone={status()?.running ? "success" : "accent"} />
              </View>

              <QuotaBlock profile={p} />

              <If when={!!overlayText}>
                <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
                  <Button
                    testID={`overlay-badge-${p.id}`}
                    onPress={() => handleOverlayBadgePress(p)}
                    style={{
                      flexDirection: "row", alignItems: "center", gap: 4,
                      minHeight: 30, paddingHorizontal: 10, borderRadius: RADIUS.pill,
                      backgroundColor: tint(colors().accent, "14"),
                      borderWidth: 1, borderColor: tint(colors().accent, "2c"),
                    }}>
                    <Text style={{ fontSize: 11.5, fontWeight: "600", color: colors().accent }}>{overlayText}</Text>
                    <Icon name="chevronRight" size={12} color={colors().accent} />
                  </Button>
                </View>
              </If>

              <If when={!!p.lastError}>
                <Notice
                  tone="danger"
                  text={p.lastError ?? ""}
                  action={() => <Action size="compact" quiet label={tr("重试", "Retry")} disabled={mutationBusy()} onPress={() => void refreshSingleProfile(p)} />} />
              </If>

              <If when={!!p.reconnectRequired}>
                <Notice tone="warn" text={tr("已保存 · 重新连接后生效", "Saved · reconnect to apply")} />
              </If>

              <View style={{ height: 1, backgroundColor: colors().border }} />
              <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
                <View style={{ flexDirection: "row", alignItems: "center", gap: 8, flexWrap: "wrap", flex: 1 }}>
                  <If when={showUpdate}>
                    <Action
                      testID={`refresh-${p.id}`}
                      size="compact"
                      icon="refresh"
                      label={tasks()[`refresh-${p.id}`]
                        ? tr("正在更新…", "Refreshing…")
                        : p.isAggregate ? tr("重新聚合", "Re-aggregate") : tr("更新", "Refresh")}
                      disabled={mutationBusy()}
                      onPress={() => void refreshSingleProfile(p)} />
                  </If>
                  <Action testID={`edit-${p.id}`} size="compact" quiet label={tr("设置", "Settings")} disabled={mutationBusy() || !!editing()} onPress={() => void beginEdit(p.id)} />
                  <If when={!!p.isAggregate}>
                    <Action size="compact" quiet label={tr("来源", "Sources")} disabled={false} onPress={() => aggregateDetail.set(p)} />
                  </If>
                </View>
                <MoreButton testID={`more-${p.id}`} label={tr("更多操作", "More actions")} disabled={mutationBusy()} onPress={() => profileMenu.set(p)} />
              </View>
            </Panel>
          </>;
        })()}
      </If>

      {/* Other Profiles */}
      <If when={others().length > 0}>
        <SectionLabel title={tr("其他配置", "Other profiles")} trailing={String(others().length)} />
        <Group>
          <Each of={others()} key={p => p.id}>{p => {
            const overlayText = () => overlaySummaryBadge(p());
            return <GroupRow
              title={p().name}
              subtitle={metaLine(p())}
              trailing={() => <View style={{ flexDirection: "row", alignItems: "center", gap: 4 }}>
                <Action testID={`activate-${p().id}`} size="compact" label={tr("启用", "Activate")} disabled={mutationBusy()} onPress={() => void requestSwitchProfile(p())} />
                <MoreButton testID={`more-${p().id}`} label={tr("更多操作", "More actions")} disabled={mutationBusy()} onPress={() => profileMenu.set(p())} />
              </View>}>
              <If when={hasQuotaInfo(p().info) || !!overlayText() || !!p().lastError || !!p().isAggregate}>
                <View style={{ gap: 8 }}>
                  <QuotaBlock profile={p()} />
                  <View style={{ flexDirection: "row", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
                    <If when={!!p().isAggregate}><Tag text={tr("聚合", "Aggregate")} tone="accent" /></If>
                    <If when={!!p().isPureLocal}><Tag text={tr("纯本地", "Local")} tone="muted" /></If>
                    <If when={!!overlayText()}>
                      <Button
                        testID={`overlay-badge-${p().id}`}
                        onPress={() => handleOverlayBadgePress(p())}
                        style={{
                          flexDirection: "row", alignItems: "center", gap: 4,
                          minHeight: 28, paddingHorizontal: 10, borderRadius: RADIUS.pill,
                          backgroundColor: tint(colors().accent, "14"),
                        }}>
                        <Text style={{ fontSize: 11.5, fontWeight: "600", color: colors().accent }}>{overlayText()}</Text>
                        <Icon name="chevronRight" size={12} color={colors().accent} />
                      </Button>
                    </If>
                  </View>
                  <If when={!!p().lastError}>
                    <Notice tone="danger" text={p().lastError ?? ""} />
                  </If>
                </View>
              </If>
            </GroupRow>;
          }}</Each>
        </Group>
      </If>
    </Scroll>

    {/* Add Profile BottomSheet */}
    <BottomSheet
      testID="import-profile-sheet"
      open={adding() && profiles().length > 0}
      onClose={() => adding.set(false)}
      title={tr("添加订阅或节点", "Add profile or nodes")}
      scrollable
      error={importError()}
      footer={() => <View style={{ flexDirection: "row", gap: 10 }}>
        <View style={{ flex: 1 }}><Action full label={tr("取消", "Cancel")} disabled={mutationBusy()} onPress={() => adding.set(false)} /></View>
        <View style={{ flex: 1 }}><Action testID="import-profile" primary full label={tasks().import ? tr("正在导入…", "Importing…") : tr("导入", "Import")} onPress={() => void importProfile()} disabled={!canImport()} /></View>
      </View>}>
      <ImportFormFields />
    </BottomSheet>

    {/* Full Profile Settings BottomSheet */}
    <BottomSheet
      testID="edit-profile-sheet"
      open={!!editing()}
      onClose={() => { editing.set(""); editDetail.set(null); }}
      title={tr("订阅设置", "Profile settings")}
      scrollable
      error={editError()}
      footer={() => <View style={{ flexDirection: "row", gap: 10 }}>
        <View style={{ flex: 1 }}><Action full label={tr("放弃修改", "Discard")} disabled={mutationBusy()} onPress={() => { editing.set(""); editDetail.set(null); }} /></View>
        <View style={{ flex: 1 }}><Action testID="save-profile" primary full label={tr("保存", "Save")} disabled={mutationBusy() || !editName().trim()} onPress={() => void saveEdit()} /></View>
      </View>}>
      <Field label={tr("名称", "Name")} testID="edit-profile-name" value={editName()} onChange={value => editName.set(value)} disabled={mutationBusy()} />

      <If when={editKind() === "url"}>
        <Field label={tr("订阅链接", "Subscription URL")} testID="edit-profile-url" value={editURL()} onChange={value => editURL.set(value)} disabled={mutationBusy()} helper={tr("保存只修改来源，点击更新后下载新内容。", "Saving changes the source. Refresh to download its contents.")} />

        <View style={{ gap: 6 }}>
          <Label text="User-Agent" bold size={12} muted />
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 6 }}>
            <Each of={UA_PRESETS} key={u => u.id || "default"}>{u => {
              const active = () => editUserAgent().trim() === u().id;
              return <Button
                onPress={() => { haptic("selection"); editUserAgent.set(u().id); }}
                style={{
                  minHeight: 34, paddingHorizontal: 12, borderRadius: RADIUS.pill,
                  alignItems: "center", justifyContent: "center",
                  backgroundColor: active() ? colors().selected : colors().bg,
                  borderWidth: 1, borderColor: active() ? colors().selectedBorder : colors().border,
                }}>
                <Text style={{ fontSize: 12, fontWeight: active() ? "600" : "500", color: active() ? colors().accent : colors().muted }}>{tr(u().zh, u().en)}</Text>
              </Button>;
            }}</Each>
          </View>
          <Field testID="edit-profile-ua" value={editUserAgent()} onChange={v => editUserAgent.set(v)} placeholder={tr("留空使用默认 User-Agent", "Default User-Agent")} disabled={mutationBusy()} />
        </View>

        <Group>
          <GroupRow
            title={tr("通过代理下载", "Download via proxy")}
            subtitle={tr("更新此订阅时经由本地代理请求", "Use active local proxy when refreshing this subscription")}
            trailing={() => <ToggleSwitch testID="edit-profile-use-proxy" checked={editUseProxy()} onChange={v => editUseProxy.set(v)} label={tr("通过代理下载", "Download via proxy")} disabled={mutationBusy()} />} />
        </Group>

        <View style={{ gap: 8 }}>
          <Label text={tr("自动更新频率", "Auto-update interval")} bold size={12} muted />
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 6 }}>
            <Each of={AUTO_UPDATE_OPTIONS} key={opt => opt.id}>{opt => {
              const active = () => editUpdatePreset() === opt().id;
              return <Button
                testID={`edit-update-${opt().id}`}
                onPress={() => { haptic("selection"); editUpdatePreset.set(opt().id); }}
                style={{
                  minHeight: 36, paddingHorizontal: 12, borderRadius: RADIUS.pill,
                  alignItems: "center", justifyContent: "center",
                  backgroundColor: active() ? colors().selected : colors().bg,
                  borderWidth: 1, borderColor: active() ? colors().selectedBorder : colors().border,
                }}>
                <Text style={{ fontSize: 12, fontWeight: active() ? "600" : "500", color: active() ? colors().accent : colors().muted }}>{tr(opt().zh, opt().en)}</Text>
              </Button>;
            }}</Each>
          </View>
          <If when={editUpdatePreset() === "custom"}>
            <Field
              testID="edit-update-custom-hours"
              label={tr("自定义间隔（小时）", "Custom interval (hours)")}
              keyboardType="numeric"
              value={editCustomHours()}
              onChange={v => editCustomHours.set(v.replace(/[^\d]/g, ""))}
              placeholder="24"
              disabled={mutationBusy()} />
          </If>
        </View>
      </If>

      {/* Overlay Summary Card */}
      <If when={!!editDetail()}>
        <Panel testID="profile-overlay-summary" padding={12} gap={8}>
          <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
            <Label text={tr("本地叠加摘要", "Local overlay summary")} bold size={12.5} />
            <Label text={tr(`${editDetail()!.nodeCount ?? 0} 总节点`, `${editDetail()!.nodeCount ?? 0} total`)} faint size={11.5} tabular />
          </View>
          <Label
            text={tr(
              `自建节点 ${editDetail()!.localNodeCount ?? 0} · 修改节点 ${editDetail()!.editedNodeCount ?? 0} · 修改策略组 ${editDetail()!.editedGroupCount ?? 0}`,
              `Local nodes: ${editDetail()!.localNodeCount ?? 0} · Edited nodes: ${editDetail()!.editedNodeCount ?? 0} · Edited groups: ${editDetail()!.editedGroupCount ?? 0}`,
            )}
            muted
            size={12}
            tabular />
          <Label text={tr("更新订阅仅刷新远端基底，本地自建与修改自动保留。", "Refreshing updates the remote base while keeping local additions and edits.")} faint size={11.5} />
          <Action
            testID="edit-profile-jump-nodes"
            size="compact"
            icon="layers"
            label={tr("管理节点与策略组 →", "Manage nodes & groups →")}
            disabled={mutationBusy()}
            onPress={() => {
              const targetId = editing();
              const isAct = targetId === activeProfile()?.id;
              editing.set("");
              editDetail.set(null);
              if (isAct) openProxiesFilteredLocal();
              else void openGroupsManager({ profileId: targetId, initialTab: "nodes" });
            }} />
        </Panel>
      </If>
    </BottomSheet>

    {/* Profile Actions BottomSheet */}
    <BottomSheet
      testID="profile-actions-sheet"
      closeTestID="profile-actions-close"
      open={!!profileMenu()}
      onClose={() => profileMenu.set(null)}
      title={profileMenu()?.name ?? ""}
      subtitle={profileMenu() ? metaLine(profileMenu()!) : ""}>
      <If when={!!profileMenu()}>
        <Group>
          <If when={!profileMenu()!.active}>
            <GroupRow
              icon="check"
              title={tr("启用此配置", "Activate profile")}
              subtitle={vpnActive() ? tr("将先断开当前 VPN 再重连", "Disconnects VPN first, then reconnects") : undefined}
              onPress={() => {
                const target = profileMenu()!;
                profileMenu.set(null);
                void requestSwitchProfile(target);
              }} />
          </If>
          <If when={profileMenu()!.kind === "url" || !!profileMenu()!.isAggregate}>
            <GroupRow
              testID={`sheet-refresh-${profileMenu()!.id}`}
              icon="refresh"
              title={tasks()[`refresh-${profileMenu()!.id}`]
                ? tr("正在更新…", "Refreshing…")
                : profileMenu()!.isAggregate ? tr("重新聚合", "Re-aggregate") : tr("更新订阅内容", "Refresh profile")}
              onPress={() => {
                const target = profileMenu()!;
                profileMenu.set(null);
                void refreshSingleProfile(target);
              }} />
          </If>
          <GroupRow
            testID={`sheet-nodes-${profileMenu()!.id}`}
            icon="layers"
            title={tr("管理节点与策略组", "Manage nodes & groups")}
            subtitle={profileMenu()!.active ? tr("编辑当前配置的节点与策略组", "Edit nodes and groups for active profile") : tr("直接编辑此非活跃配置，不影响当前连接", "Edit this inactive profile without affecting active VPN")}
            onPress={() => {
              const target = profileMenu()!;
              profileMenu.set(null);
              void openGroupsManager({ profileId: target.id, initialTab: "nodes" });
            }} />
          <GroupRow
            testID={`sheet-edit-${profileMenu()!.id}`}
            icon="settings"
            title={tr("订阅设置与叠加摘要", "Profile settings")}
            onPress={() => {
              const id = profileMenu()!.id;
              profileMenu.set(null);
              void beginEdit(id);
            }} />
          <GroupRow
            testID={`delete-${profileMenu()!.id}`}
            icon="trash"
            danger
            disabled={profileMenu()!.active && !!status()?.running}
            title={tr("删除订阅", "Delete profile")}
            subtitle={profileMenu()!.active && !!status()?.running ? tr("运行中的配置须先断开连接后再删除", "Disconnect before deleting the active profile") : tr("删除本地配置和缓存，不可撤销", "Removes saved profile and cache")}
            onPress={() => {
              const target = profileMenu()!;
              profileMenu.set(null);
              void requestDeleteProfile(target);
            }} />
        </Group>
      </If>
    </BottomSheet>

    {/* Aggregate Sources Detail Sheet */}
    <BottomSheet
      testID="aggregate-sources-sheet"
      open={!!aggregateDetail()}
      onClose={() => aggregateDetail.set(null)}
      title={aggregateDetail()?.name ?? tr("聚合来源", "Aggregate sources")}
      subtitle={tr("聚合读取各源本地缓存，不含订阅内局部修改。", "Reads local cached sources; excludes per-profile overlays.")}
      footer={() => <If when={!!aggregateDetail()}>
        <Action
          primary
          full
          icon="refresh"
          label={tr("重新聚合", "Re-aggregate")}
          disabled={mutationBusy()}
          onPress={() => {
            const target = aggregateDetail()!;
            aggregateDetail.set(null);
            void refreshSingleProfile(target);
          }} />
      </If>}>
      <If when={!!aggregateDetail()?.aggregate}>
        <Group>
          <Each of={aggregateDetail()!.aggregate!.sources} key={s => s.id}>{s => (
            <GroupRow
              icon="profiles"
              title={s().name || s().id}
              subtitle={s().prefix ? tr(`前缀：${s().prefix}`, `Prefix: ${s().prefix}`) : tr("无前缀", "No prefix")} />
          )}</Each>
        </Group>
        <Label
          text={[
            aggregateDetail()!.aggregate!.include ? tr(`包含: ${aggregateDetail()!.aggregate!.include}`, `Include: ${aggregateDetail()!.aggregate!.include}`) : "",
            aggregateDetail()!.aggregate!.exclude ? tr(`排除: ${aggregateDetail()!.aggregate!.exclude}`, `Exclude: ${aggregateDetail()!.aggregate!.exclude}`) : "",
            aggregateDetail()!.aggregate!.dedupe ? tr("已开启去重", "Deduplication on") : tr("未去重", "Deduplication off"),
          ].filter(Boolean).join(" · ")}
          muted
          size={12} />
      </If>
    </BottomSheet>
  </View>;
}
