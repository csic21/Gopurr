import { cleanup, colorScheme, createAnimatedValue, effect, host, Image, lifecycle, List, reduceMotion, Scroll, setColorScheme, signal, Storage, Text, View } from "nsh";
import { BackHandler } from "nsh/mobile";
import { Button, Progress } from "nsh/base";
import { call } from "./client";
import type { Profile } from "./client";
import { colors } from "./theme";
import { language, tr } from "./i18n";
import {
  action, activeProfile, appUpdate, awaitingMeshPermission, busy, changeMode, connected,
  connecting, error, groupName, groupsIncomplete, loaded, members, meshAvailable, meshTunnel,
  message, mutationBusy, navigate, openProxiesForGroup, openSubpage, page, profiles, query,
  refresh, selectedGroup, session, showToast, startMobileStore, status, subpage, switchProfile,
  tasks, testMember, toggleEngine, visibleGroups, vpnActive,
} from "./mobile/store";
import { androidCall, androidHost } from "./mobile/platform";
import type { AppScope, AppUpdate, InstalledApp, NotificationSettings } from "./mobile/platform";
import {
  Action, BottomSheet, bytes, compact, confirmDialog, Empty, Field, formatRateParts,
  formatUpdated, Group, GroupRow, Heading, Icon, IconButton, Label, MoreButton, Notice, Panel,
  RADIUS, SearchField, SectionLabel, Segmented, StatusPill, SubpageHeader, Tag, tint, ToastHost,
} from "./mobile/ui";
import type { MobileIconName, Tone } from "./mobile/ui";
import { MeshGlanceCard, MeshPage } from "./mobile/mesh";
import logo from "../assets/sidebar.png";

// Module-level drafts & sheet state survive appearance remounts.
const name = signal(""), source = signal(""), yaml = signal("");
const importKind = signal<"url" | "yaml">("url");
const adding = signal(false);
const importError = signal("");
const appearance = signal<"system" | "light" | "dark">("system");
const editing = signal(""), editName = signal(""), editURL = signal(""), editKind = signal(""), editError = signal("");
const profileMenu = signal<Profile | null>(null);
const quickSwitchOpen = signal(false);
const groupPickerOpen = signal(false);
const notifSheetOpen = signal(false);
const updateSheetOpen = signal(false);
const notifSettings = signal<NotificationSettings | null>(null);
const appScopeSummary = signal<AppScope | null>(null);
const batchProgress = signal<{ done: number; total: number } | null>(null);
const overviewRefreshing = signal(false);
let batchStop = false;

const nav: { id: "overview" | "proxies" | "profiles" | "settings"; zh: string; en: string; icon: MobileIconName }[] = [
  { id: "overview", zh: "连接", en: "Connect", icon: "overview" },
  { id: "proxies", zh: "节点", en: "Nodes", icon: "proxies" },
  { id: "profiles", zh: "订阅", en: "Profiles", icon: "profiles" },
  { id: "settings", zh: "设置", en: "Settings", icon: "settings" },
];

function stateLabel(): string {
  const hasProfile = !!activeProfile();
  const meshOnlyTunnel = meshTunnel() && !connected();
  switch (session()?.phase) {
    case "awaitingPermission": return tr("等待系统授权", "Waiting for VPN permission");
    case "connecting": return tr("正在连接…", "Connecting…");
    case "disconnecting": return tr("正在断开…", "Disconnecting…");
    case "failed": return tr("连接失败", "Connection failed");
    case "connected": return tr("设备 VPN 已连接", "Device VPN connected");
    case "idle":
      if (meshOnlyTunnel && hasProfile) return tr("代理未开启", "Proxy off");
      if (meshOnlyTunnel && !hasProfile) return tr("组网直连运行中", "Mesh direct active");
      return tr("尚未连接", "Disconnected");
  }
  return status()?.tun ? tr("设备 VPN 已连接", "Device VPN connected") : status()?.running ? tr("本地代理已运行", "Local proxy running") : tr("尚未连接", "Disconnected");
}

function stateTone(): Tone {
  const phase = session()?.phase;
  if (phase === "failed") return "danger";
  if (phase === "awaitingPermission" || phase === "connecting" || phase === "disconnecting") return "accent";
  if (connected() || status()?.running) return "success";
  if (meshTunnel() && !activeProfile()) return "success";
  return "muted";
}

function connectionDisabled(): boolean {
  const hasProfile = !!activeProfile();
  if (!hasProfile && meshTunnel() && !connected()) return true;
  const canStart = hasProfile || meshAvailable();
  if (androidHost()) return session()?.phase === "disconnecting" || !!tasks().connection || (!connected() && !connecting() && (!canStart || mutationBusy()));
  return !!tasks().connection || (!connected() && (!canStart || mutationBusy()));
}

function connectionAction(): string {
  if (session()?.phase === "disconnecting") return tr("正在断开…", "Disconnecting…");
  if (androidHost() && connecting()) return tr("取消连接", "Cancel connection");
  if (tasks().connection) return tr("处理中…", "Working…");
  if (!activeProfile() && meshTunnel() && !connected()) return tr("组网直连运行中", "Mesh direct active");
  return status()?.vpnSupported
    ? connected() ? tr("断开连接", "Disconnect") : tr("连接 VPN", "Connect VPN")
    : connected() ? tr("停止本地代理", "Stop local proxy") : tr("启动本地代理", "Start local proxy");
}

function groupTypeLabel(kind: string): string {
  switch (String(kind).toLowerCase()) {
    case "selector": case "select": return tr("手动选择", "Manual");
    case "urltest": case "url-test": return tr("自动测速", "Auto");
    case "fallback": return tr("故障转移", "Fallback");
    case "loadbalance": case "load-balance": return tr("负载均衡", "Load balance");
    case "relay": return tr("链式代理", "Relay");
    default: return kind;
  }
}

function scopeSummaryText(scope: AppScope | null): string {
  if (!scope) return tr("所有应用", "All apps");
  if (scope.mode === "include") return tr(`仅 ${scope.packages.length} 个应用`, `${scope.packages.length} selected`);
  if (scope.mode === "exclude") return tr(`排除 ${scope.packages.length} 个应用`, `${scope.packages.length} excluded`);
  return tr("所有应用", "All apps");
}

async function requestSwitchProfile(target: Profile) {
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
  const ok = await confirmDialog({
    title: tr(`删除「${target.name}」？`, `Delete "${target.name}"?`),
    description: tr("将删除本地订阅和缓存，此操作无法撤销。", "This removes the saved profile and cache and cannot be undone."),
    confirmLabel: tr("删除订阅", "Delete profile"),
    cancelLabel: tr("取消", "Cancel"),
    destructive: true,
  });
  if (!ok) return;
  const done = await action("delete", async () => {
    await call("profiles.delete", { id: target.id });
  });
  if (done) showToast(tr("已删除订阅", "Profile deleted"));
}

async function importProfile() {
  if (!name().trim() || !(importKind() === "url" ? source().trim() : yaml().trim())) return;
  importError.set("");
  const ok = await action("import", async () => {
    await call("profiles.import", { name: name().trim(), ...(importKind() === "url" ? { url: source().trim() } : { yaml: yaml() }) });
    source.set(""); yaml.set(""); name.set(""); adding.set(false);
  });
  if (ok) showToast(tr("订阅已导入", "Profile imported"));
  else importError.set(error());
}

async function beginEdit(id: string) {
  editError.set("");
  await action("editor", async () => {
    const p = await call<{ id: string; name: string; url: string; kind: string }>("profiles.editor", { id });
    editing.set(p.id); editName.set(p.name); editURL.set(p.url ?? ""); editKind.set(p.kind);
  });
}

async function saveEdit() {
  if (!editName().trim()) return;
  editError.set("");
  const ok = await action("edit", async () => {
    await call("profiles.update", { id: editing(), name: editName().trim(), ...(editKind() === "url" ? { url: editURL().trim() } : {}) });
    editing.set("");
  });
  if (ok) showToast(tr("订阅信息已保存", "Profile details saved"));
  else editError.set(error());
}

async function refreshNotificationSettings() {
  if (!androidHost()) return;
  try { notifSettings.set(await androidCall<NotificationSettings>("android.notifications")); }
  catch (value) { error.set(message(value)); }
}

async function refreshAppScopeSummary() {
  if (!androidHost()) return;
  try { appScopeSummary.set(await androidCall<AppScope>("android.scope.get")); }
  catch { /* Keep previous summary when host query is unavailable. */ }
}

function Navigation() {
  return <View role="navigation" style={{
    width: compact() ? "100%" : 184,
    flexDirection: compact() ? "row" : "column",
    paddingHorizontal: compact() ? 8 : 10,
    paddingVertical: compact() ? 6 : 10,
    gap: 4,
    backgroundColor: colors().side,
    borderColor: colors().border,
    borderRightWidth: compact() ? 0 : 1,
    borderTopWidth: compact() ? 1 : 0,
  }}>
    <If when={!compact()}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: 10, paddingHorizontal: 10, paddingTop: 6, paddingBottom: 20 }}>
        <Image src={logo} style={{ width: 30, height: 30, objectFit: "contain" }} />
        <Label text="Gopurr" size={16} bold />
      </View>
    </If>
    <Each of={nav}>{item => {
      const active = () => page() === item().id;
      return <Button
        testID={`nav-${item().id}`}
        selected={active()}
        onPress={() => navigate(item().id)}
        style={{
          flex: compact() ? 1 : undefined,
          minHeight: compact() ? 52 : 46,
          flexDirection: compact() ? "column" : "row",
          alignItems: "center",
          justifyContent: compact() ? "center" : "flex-start",
          gap: compact() ? 3 : 10,
          paddingHorizontal: compact() ? 4 : 12,
          borderRadius: 10,
          backgroundColor: active() ? colors().selected : "transparent",
          transitionProperty: "background-color",
          transitionDuration: reduceMotion() ? 0 : 140,
        }}
        hoverStyle={{ backgroundColor: active() ? colors().selected : colors().hover }}
        focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}
        activeStyle={{ opacity: 0.72 }}>
        <Icon name={item().icon} selected={active()} size={20} />
        <Text style={{ color: active() ? colors().accent : colors().muted, fontSize: compact() ? 11 : 13.5, fontWeight: active() ? "600" : "500", numberOfLines: 1 }}>{tr(item().zh, item().en)}</Text>
      </Button>;
    }}</Each>
  </View>;
}

function Overview() {
  const hasProfile = () => !!activeProfile();
  const isMeshTunnelOnly = () => meshTunnel() && !connected();
  const primaryTitle = () => activeProfile()?.name
    ?? (meshAvailable()
      ? (isMeshTunnelOnly() ? tr("组网直连运行中", "Mesh direct active") : tr("组网直连模式", "Mesh direct mode"))
      : tr("先添加一个订阅", "Add your first profile"));

  const heroSubtitle = () => {
    if (connected() && status()?.vpnSupported) {
      return activeProfile()
        ? tr("设备流量按当前配置转发。", "Device traffic follows your active profile.")
        : tr("设备组网流量通过 VPN 直连转发。", "Device mesh traffic routes directly via VPN.");
    }
    if (isMeshTunnelOnly() && hasProfile()) {
      return tr("仅接管组网流量，点击下方按钮开启代理。", "Mesh tunnel active. Connect to enable proxy routing.");
    }
    if (isMeshTunnelOnly() && !hasProfile()) {
      return tr("组网直连隧道已运行，无需代理订阅。", "Mesh split-tunnel is running without a proxy profile.");
    }
    if (status()?.running && !status()?.vpnSupported) {
      return tr(`监听本机端口 ${status()?.mixedPort}`, `Listening locally on port ${status()?.mixedPort}`);
    }
    if (session()?.phase === "awaitingPermission") {
      return tr("请在系统弹窗中允许创建 VPN 连接。", "Allow VPN access in the system prompt.");
    }
    if (meshAvailable() && !hasProfile()) {
      return tr("已启用组网，可直接连接 VPN，无需代理订阅。", "Mesh is ready. Connect VPN directly without a proxy profile.");
    }
    if (!hasProfile()) {
      return tr("选择或导入订阅后即可启动连接。", "Import and select a profile to start.");
    }
    return status()?.vpnSupported
      ? tr("点击下方按钮连接设备 VPN。", "Tap below to connect device VPN.")
      : tr("本地代理运行不会自动接管设备流量。", "Starting the local proxy does not route device traffic automatically.");
  };

  const downParts = () => formatRateParts(status()?.traffic.down ?? 0);
  const upParts = () => formatRateParts(status()?.traffic.up ?? 0);
  const showRate = () => loaded() && !!status()?.running;
  const pullRefresh = () => {
    overviewRefreshing.set(true);
    void refresh().finally(() => overviewRefreshing.set(false));
  };

  return <View style={{ flex: 1 }}>
    <If when={compact()}>
      <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", paddingHorizontal: 16, paddingTop: 10, paddingBottom: 6, gap: 8 }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
          <Image src={logo} style={{ width: 24, height: 24, objectFit: "contain" }} />
          <Label text="Gopurr" size={16} bold />
        </View>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 4 }}>
          <StatusPill testID="overview-status-pill" label={stateLabel()} tone={stateTone()} />
          <IconButton testID="overview-refresh" icon="refresh" label={tr("刷新", "Refresh")} onPress={() => void refresh()} />
        </View>
      </View>
    </If>
    <Scroll testID="overview-scroll" refreshing={overviewRefreshing()} onRefresh={pullRefresh} style={{ flex: 1 }} contentStyle={{ padding: 16, paddingTop: compact() ? 8 : 16, gap: 14 }}>
      <If when={!compact()}>
        <Heading title={tr("连接", "Connect")} action={() => <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
          <StatusPill testID="overview-status-pill" label={stateLabel()} tone={stateTone()} />
          <IconButton testID="overview-refresh" icon="refresh" label={tr("刷新", "Refresh")} onPress={() => void refresh()} />
        </View>} />
      </If>

      <Panel accentLeft accentColor={connected() || status()?.running ? colors().success : colors().border} gap={14}>
        <View style={{ gap: 6 }}>
          <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
            <Label text={hasProfile() ? tr("当前配置", "Active profile") : tr("连接状态", "Connection")} muted size={12} bold />
            <If when={profiles().length > 0}>
              <Button testID="overview-switch-profile" onPress={() => quickSwitchOpen.set(true)} disabled={mutationBusy()}
                style={{ flexDirection: "row", alignItems: "center", gap: 2, minHeight: 36, paddingHorizontal: 8, borderRadius: 8 }}
                hitSlop={{ top: 4, bottom: 4, left: 4, right: 4 }}
                hoverStyle={{ backgroundColor: colors().hover }} activeStyle={{ opacity: 0.7 }} disabledStyle={{ opacity: 0.4 }}>
                <Text style={{ fontSize: 12.5, fontWeight: "500", color: colors().accent }}>{tr("切换", "Switch")}</Text>
                <Icon name="chevronRight" size={14} color={colors().accent} />
              </Button>
            </If>
          </View>
          <Text style={{ fontSize: 21, fontWeight: "600", letterSpacing: -0.3, color: colors().text, numberOfLines: 2 }}>{primaryTitle()}</Text>
          <Label text={heroSubtitle()} muted size={12.5} />
        </View>

        <If when={!!status()?.running && !!selectedGroup()}>
          <Button testID="overview-current-node" onPress={() => openProxiesForGroup(selectedGroup()?.name)}
            style={{
              flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 10,
              minHeight: 44, paddingHorizontal: 12, paddingVertical: 8,
              borderRadius: RADIUS.control, backgroundColor: colors().bg, borderWidth: 1, borderColor: colors().border,
            }}
            hoverStyle={{ backgroundColor: colors().hover }} activeStyle={{ opacity: 0.8 }} focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}>
            <View style={{ flexDirection: "row", alignItems: "center", gap: 8, flex: 1, minWidth: 0 }}>
              <Icon name="proxies" size={15} color={colors().accent} />
              <Text style={{ flex: 1, fontSize: 13, fontWeight: "500", color: colors().text, numberOfLines: 1, textOverflow: "ellipsis" }}>
                {`${selectedGroup()?.name} · ${selectedGroup()?.now}`}
              </Text>
            </View>
            <View style={{ flexDirection: "row", alignItems: "center", gap: 2, flexShrink: 0 }}>
              <Text style={{ fontSize: 12, color: colors().muted }}>{tr("节点", "Nodes")}</Text>
              <Icon name="chevronRight" size={14} color={colors().faint} />
            </View>
          </Button>
        </If>

        <Action testID="toggle-engine" primary full size="large" icon="power" label={connectionAction()} onPress={toggleEngine} disabled={connectionDisabled()} />

        <If when={!!status()?.reconnectRequired}>
          <Notice
            testID="reconnect-required"
            tone="warn"
            text={tr("新配置已保存，重新连接后生效。", "The new profile is saved. Reconnect to apply it.")}
            action={() => <If when={connected()}><Action size="compact" quiet label={tr("断开", "Disconnect")} disabled={connectionDisabled()} onPress={toggleEngine} /></If>} />
        </If>

        <Segmented
          testIDPrefix="mode"
          value={status()?.mode ?? "rule"}
          disabled={!status()?.running || mutationBusy()}
          onChange={id => void changeMode(id as "rule" | "global" | "direct")}
          items={[
            { id: "rule", label: tr("规则", "Rule") },
            { id: "global", label: tr("全局", "Global") },
            { id: "direct", label: tr("直连", "Direct") },
          ]} />
        <If when={!status()?.vpnSupported && !!status()?.running}>
          <Label text={tr("本地代理运行不会自动接管设备流量。", "Starting the local proxy does not route device traffic automatically.")} muted size={12} />
        </If>
      </Panel>

      <Panel gap={12}>
        <View style={{ flexDirection: "row", gap: 16 }}>
          <View style={{ flex: 1, gap: 4 }}>
            <View style={{ flexDirection: "row", alignItems: "center", gap: 4 }}>
              <Icon name="arrowDown" size={13} color={colors().down} />
              <Label text={tr("下载", "Download")} muted size={12} />
            </View>
            <If when={showRate()}>
              <View style={{ flexDirection: "row", alignItems: "baseline", gap: 4 }}>
                <Text style={{ color: colors().down, fontSize: 26, fontWeight: "600", fontVariant: "tabular-nums" }}>{downParts().num}</Text>
                <Text style={{ color: colors().down, fontSize: 12, fontWeight: "500" }}>{downParts().unit}</Text>
              </View>
            </If>
            <If when={!showRate()}>
              <Text style={{ color: colors().muted, fontSize: 24, fontWeight: "600" }}>—</Text>
            </If>
          </View>
          <View style={{ flex: 1, gap: 4 }}>
            <View style={{ flexDirection: "row", alignItems: "center", gap: 4 }}>
              <Icon name="arrowUp" size={13} color={colors().up} />
              <Label text={tr("上传", "Upload")} muted size={12} />
            </View>
            <If when={showRate()}>
              <View style={{ flexDirection: "row", alignItems: "baseline", gap: 4 }}>
                <Text style={{ color: colors().up, fontSize: 26, fontWeight: "600", fontVariant: "tabular-nums" }}>{upParts().num}</Text>
                <Text style={{ color: colors().up, fontSize: 12, fontWeight: "500" }}>{upParts().unit}</Text>
              </View>
            </If>
            <If when={!showRate()}>
              <Text style={{ color: colors().muted, fontSize: 24, fontWeight: "600" }}>—</Text>
            </If>
          </View>
        </View>
        <View style={{ height: 1, backgroundColor: colors().border }} />
        <View style={{ flexDirection: "row", gap: 12 }}>
          <View style={{ flex: 1, gap: 2 }}>
            <Label text={tr("当前连接", "Connections")} faint size={11} />
            <Label text={String(status()?.traffic.connections ?? 0)} bold size={14} tabular />
          </View>
          <View style={{ flex: 1, gap: 2 }}>
            <Label text={tr("累计下载", "Downloaded")} faint size={11} />
            <Label text={bytes(status()?.traffic.downTotal ?? 0)} bold size={14} tabular />
          </View>
          <View style={{ flex: 1, gap: 2 }}>
            <Label text={tr("累计上传", "Uploaded")} faint size={11} />
            <Label text={bytes(status()?.traffic.upTotal ?? 0)} bold size={14} tabular />
          </View>
        </View>
      </Panel>

      <MeshGlanceCard onOpen={() => openSubpage("mesh")} />

      <If when={!profiles().length && loaded()}>
        <Action testID="overview-add-profile" label={tr("添加订阅", "Add profile")} primary={!meshAvailable()} full onPress={() => { navigate("profiles"); adding.set(true); }} />
      </If>
    </Scroll>

    <BottomSheet
      testID="quick-switch-sheet"
      open={quickSwitchOpen()}
      onClose={() => quickSwitchOpen.set(false)}
      title={tr("切换配置", "Switch profile")}
      subtitle={vpnActive() ? tr("当前已连接 VPN，切换将先断开再重新连接。", "VPN is active. Switching disconnects first, then reconnects.") : tr("选择本次连接使用的配置。", "Choose the profile for your connection.")}
      scrollable>
      <Group>
        <Each of={[...profiles()].sort((a, b) => Number(b.active) - Number(a.active))} key={p => p.id}>{p => <GroupRow
          testID={`quick-profile-${p().id}`}
          title={p().name}
          subtitle={[p().kind === "file" || p().kind === "local" ? tr("本地配置", "Local profile") : p().source, formatUpdated(p().updated)].filter(Boolean).join(" · ")}
          trailing={() => <If when={p().active}><Tag text={status()?.running ? tr("使用中", "Active") : tr("已选择", "Selected")} tone={status()?.running ? "success" : "accent"} /></If>}
          chevron={!p().active}
          disabled={p().active || mutationBusy()}
          onPress={() => {
            const target = p();
            quickSwitchOpen.set(false);
            void requestSwitchProfile(target);
          }} />}</Each>
      </Group>
      <Action quiet full label={tr("管理全部订阅", "Manage all profiles")} disabled={false} onPress={() => { quickSwitchOpen.set(false); navigate("profiles"); }} />
    </BottomSheet>
  </View>;
}

function ImportFormFields() {
  return <View style={{ gap: 12 }}>
    <Segmented
      testIDPrefix="import-kind"
      value={importKind()}
      disabled={mutationBusy()}
      onChange={id => importKind.set(id as "url" | "yaml")}
      items={[
        { id: "url", label: tr("订阅链接", "Subscription URL") },
        { id: "yaml", label: tr("本地 YAML", "Local YAML") },
      ]} />
    <Field label={tr("名称", "Name")} testID="profile-name" value={name()} onChange={value => name.set(value)} placeholder={tr("例如：日常使用", "For example: Everyday")} disabled={mutationBusy()} />
    <If when={importKind() === "url"}>
      <Field label={tr("订阅链接", "Subscription URL")} testID="profile-url" value={source()} onChange={value => source.set(value)} placeholder="https://" disabled={mutationBusy()} />
    </If>
    <If when={importKind() === "yaml"}>
      <If when={androidHost()}>
        <Action testID="import-file" icon="profiles" full label={tr("从文件导入", "Import from file")} disabled={mutationBusy()} onPress={() => void action("file", async () => {
          const file = await androidCall<{ name?: string; content?: string; cancelled?: boolean }>("android.file.pick");
          if (file.cancelled || !file.content) return;
          yaml.set(file.content);
          if (!name()) name.set(file.name ?? tr("本地配置", "Local profile"));
          importKind.set("yaml");
        })} />
      </If>
      <Field label={tr("配置内容", "Configuration YAML")} testID="profile-yaml" value={yaml()} onChange={value => yaml.set(value)} placeholder={tr("粘贴完整 YAML 配置", "Paste a complete YAML profile")} multiline disabled={mutationBusy()} />
    </If>
    <Label text={tr("导入后在连接页启动；其他订阅需明确启用。", "Connect after importing. Activate another profile explicitly to switch.")} muted size={12} />
  </View>;
}

function Profiles() {
  const active = () => profiles().find(p => p.active);
  const others = () => profiles().filter(p => !p.active);
  const canImport = () => !mutationBusy() && !!name().trim() && !!(importKind() === "url" ? source().trim() : yaml().trim());

  return <View style={{ flex: 1 }}>
    <Scroll testID="profiles-scroll" style={{ flex: 1 }} contentStyle={{ padding: 16, gap: 14 }}>
      <Heading
        title={tr("订阅", "Profiles")}
        description={vpnActive() ? tr("切换配置时将先断开当前 VPN 再重连。", "Switching profiles disconnects VPN first, then reconnects.") : tr("管理并选择本次连接使用的配置。", "Manage and select the profile for your connection.")}
        action={() => <If when={profiles().length > 0}><Action testID="add-profile" label={tr("添加订阅", "Add profile")} primary disabled={mutationBusy() || !!editing()} onPress={() => { importError.set(""); adding.set(true); }} /></If>} />

      <If when={!profiles().length}>
        <Panel>
          <Label text={tr("添加第一个订阅", "Add your first profile")} bold size={16} />
          <ImportFormFields />
          <If when={!!importError()}><Notice text={importError()} tone="danger" /></If>
          <Action testID="import-profile" primary full label={tasks().import ? tr("正在导入…", "Importing…") : tr("导入", "Import")} onPress={() => void importProfile()} disabled={!canImport()} />
        </Panel>
      </If>

      <If when={!!active()}>
        <SectionLabel title={tr("当前使用", "Current")} />
        <Panel accentLeft gap={10}>
          <View style={{ flexDirection: "row", alignItems: "flex-start", justifyContent: "space-between", gap: 10 }}>
            <View style={{ flex: 1, minWidth: 0, gap: 4 }}>
              <Text style={{ color: colors().text, fontSize: 16, fontWeight: "600", numberOfLines: 2 }}>{active()!.name}</Text>
              <Text style={{ color: colors().muted, fontSize: 12, numberOfLines: 2, textOverflow: "ellipsis-middle" }}>
                {[active()!.kind === "file" || active()!.kind === "local" ? tr("本地配置", "Local profile") : active()!.source, formatUpdated(active()!.updated)].filter(Boolean).join(" · ")}
              </Text>
            </View>
            <Tag testID={`active-tag-${active()!.id}`} text={status()?.running ? tr("使用中", "Active") : tr("已选择", "Selected")} tone={status()?.running ? "success" : "accent"} />
          </View>
          <If when={!!active()!.reconnectRequired}>
            <Notice tone="warn" text={tr("已保存 · 重新连接后生效", "Saved · reconnect to apply")} />
          </If>
          <View style={{ height: 1, backgroundColor: colors().border }} />
          <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
            <View style={{ flexDirection: "row", alignItems: "center", gap: 8, flexWrap: "wrap", flex: 1 }}>
              <If when={active()!.kind === "url"}>
                <Action
                  testID={`refresh-${active()!.id}`}
                  size="compact"
                  icon="refresh"
                  label={tasks()[`refresh-${active()!.id}`] ? tr("正在更新…", "Refreshing…") : tr("更新", "Refresh")}
                  disabled={mutationBusy()}
                  onPress={() => void action(`refresh-${active()!.id}`, async () => {
                    await call("profiles.refresh", { id: active()!.id });
                    showToast(status()?.running ? tr("更新已保存，重连后生效", "Update saved, reconnect to apply") : tr("订阅已更新", "Profile refreshed"));
                  })} />
              </If>
              <Action testID={`edit-${active()!.id}`} size="compact" quiet label={tr("编辑", "Edit")} disabled={mutationBusy() || !!editing()} onPress={() => void beginEdit(active()!.id)} />
            </View>
            <MoreButton testID={`more-${active()!.id}`} label={tr("更多操作", "More actions")} disabled={mutationBusy()} onPress={() => profileMenu.set(active()!)} />
          </View>
        </Panel>
      </If>

      <If when={others().length > 0}>
        <SectionLabel title={tr("其他配置", "Other profiles")} trailing={String(others().length)} />
        <Group>
          <Each of={others()} key={p => p.id}>{p => <GroupRow
            title={p().name}
            subtitle={[p().kind === "file" || p().kind === "local" ? tr("本地配置", "Local profile") : p().source, formatUpdated(p().updated)].filter(Boolean).join(" · ")}
            trailing={() => <View style={{ flexDirection: "row", alignItems: "center", gap: 4 }}>
              <Action testID={`activate-${p().id}`} size="compact" label={tr("启用", "Activate")} disabled={mutationBusy()} onPress={() => void requestSwitchProfile(p())} />
              <MoreButton testID={`more-${p().id}`} label={tr("更多操作", "More actions")} disabled={mutationBusy()} onPress={() => profileMenu.set(p())} />
            </View>} />}</Each>
        </Group>
      </If>
    </Scroll>

    <BottomSheet
      testID="import-profile-sheet"
      open={adding() && profiles().length > 0}
      onClose={() => adding.set(false)}
      title={tr("添加订阅", "Add profile")}
      scrollable
      error={importError()}
      footer={() => <View style={{ flexDirection: "row", gap: 10 }}>
        <View style={{ flex: 1 }}><Action full label={tr("取消", "Cancel")} disabled={mutationBusy()} onPress={() => adding.set(false)} /></View>
        <View style={{ flex: 1 }}><Action testID="import-profile" primary full label={tasks().import ? tr("正在导入…", "Importing…") : tr("导入", "Import")} onPress={() => void importProfile()} disabled={!canImport()} /></View>
      </View>}>
      <ImportFormFields />
    </BottomSheet>

    <BottomSheet
      testID="edit-profile-sheet"
      open={!!editing()}
      onClose={() => editing.set("")}
      title={tr("编辑订阅", "Edit profile")}
      scrollable
      error={editError()}
      footer={() => <View style={{ flexDirection: "row", gap: 10 }}>
        <View style={{ flex: 1 }}><Action full label={tr("放弃修改", "Discard")} disabled={mutationBusy()} onPress={() => editing.set("")} /></View>
        <View style={{ flex: 1 }}><Action testID="save-profile" primary full label={tr("保存", "Save")} disabled={mutationBusy() || !editName().trim()} onPress={() => void saveEdit()} /></View>
      </View>}>
      <Field label={tr("名称", "Name")} testID="edit-profile-name" value={editName()} onChange={value => editName.set(value)} disabled={mutationBusy()} />
      <If when={editKind() === "url"}>
        <Field label={tr("订阅链接", "Subscription URL")} testID="edit-profile-url" value={editURL()} onChange={value => editURL.set(value)} disabled={mutationBusy()} helper={tr("保存只修改来源，点击更新后下载新内容。", "Saving changes the source. Refresh to download its contents.")} />
      </If>
    </BottomSheet>

    <BottomSheet
      testID="profile-actions-sheet"
      open={!!profileMenu()}
      onClose={() => profileMenu.set(null)}
      title={profileMenu()?.name ?? ""}
      subtitle={profileMenu() ? (profileMenu()!.kind === "file" || profileMenu()!.kind === "local" ? tr("本地配置", "Local profile") : profileMenu()!.source) : ""}>
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
          <If when={profileMenu()!.kind === "url"}>
            <GroupRow
              testID={`sheet-refresh-${profileMenu()!.id}`}
              icon="refresh"
              title={tasks()[`refresh-${profileMenu()!.id}`] ? tr("正在更新…", "Refreshing…") : tr("更新订阅内容", "Refresh profile")}
              onPress={() => {
                const target = profileMenu()!;
                profileMenu.set(null);
                void action(`refresh-${target.id}`, async () => {
                  await call("profiles.refresh", { id: target.id });
                  showToast(target.active && status()?.running ? tr("更新已保存，重连后生效", "Update saved, reconnect to apply") : tr("订阅已更新", "Profile refreshed"));
                });
              }} />
          </If>
          <GroupRow
            testID={`sheet-edit-${profileMenu()!.id}`}
            icon="profiles"
            title={tr("编辑名称与来源", "Edit details")}
            onPress={() => {
              const id = profileMenu()!.id;
              profileMenu.set(null);
              void beginEdit(id);
            }} />
          <GroupRow
            testID={`delete-${profileMenu()!.id}`}
            icon="x"
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
  </View>;
}

function delayTone(delay: number): Tone {
  if (delay <= 0) return "danger";
  if (delay < 150) return "success";
  if (delay < 400) return "warn";
  return "danger";
}

function Proxies() {
  const isManualGroup = () => selectedGroup()?.type === "Selector";
  const toggleBatchTest = () => {
    if (batchProgress()) {
      batchStop = true;
      return;
    }
    const names = selectedGroup()?.members.map(m => m.name) ?? [];
    if (!names.length) return;
    batchStop = false;
    batchProgress.set({ done: 0, total: names.length });
    void action("delay-group", async () => {
      try {
        for (let i = 0; i < names.length; i++) {
          if (batchStop || !status()?.running) break;
          await testMember(names[i]);
          batchProgress.set({ done: i + 1, total: names.length });
        }
      } finally {
        batchProgress.set(null);
        batchStop = false;
      }
    }, false);
  };

  const batchLabel = () => {
    const p = batchProgress();
    if (p) return tr(`测速 ${p.done}/${p.total} · 停止`, `${p.done}/${p.total} · Stop`);
    return tr("测试本组", "Test group");
  };

  return <View style={{ flex: 1, padding: 16, gap: 12 }}>
    <Heading
      title={tr("节点", "Nodes")}
      action={() => <If when={visibleGroups().length > 0}>
        <Action
          testID="test-group"
          size="compact"
          icon="radar"
          label={batchLabel()}
          disabled={!status()?.running || (!batchProgress() && !!busy())}
          onPress={toggleBatchTest} />
      </If>} />

    <If when={!status()?.activeProfile}>
      <Panel><Empty icon="profiles" title={tr("尚未选择配置", "No profile selected")} description={tr("导入并选择订阅后，无需连接即可查看节点。", "Import and select a profile to browse nodes without connecting.")} action={() => <Action label={tr("查看订阅", "Open profiles")} primary onPress={() => navigate("profiles")} />} /></Panel>
    </If>

    <If when={!!status()?.activeProfile && !visibleGroups().length}>
      <Panel><Empty icon="proxies" title={tr("没有策略组", "No proxy groups")} description={tr("当前配置没有可显示的策略组。", "This profile has no visible proxy groups.")} /></Panel>
    </If>

    <If when={visibleGroups().length > 0}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
        <Scroll horizontal scrollbar={false} style={{ flex: 1, height: 44 }} contentStyle={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
          <Each of={visibleGroups()} key={g => g.name}>{g => {
            const active = () => selectedGroup()?.name === g().name;
            return <Button
              testID={`group-${g().name}`}
              selected={active()}
              onPress={() => { groupName.set(g().name); query.set(""); }}
              style={{
                minHeight: 40, paddingHorizontal: 14, borderRadius: RADIUS.pill,
                alignItems: "center", justifyContent: "center",
                backgroundColor: active() ? colors().selected : colors().surface,
                borderWidth: 1,
                borderColor: active() ? colors().selectedBorder : colors().border,
              }}
              hitSlop={{ top: 2, bottom: 2 }}
              hoverStyle={{ backgroundColor: active() ? colors().selected : colors().hover }}
              activeStyle={{ opacity: 0.78 }}
              focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}>
              <Text style={{ fontSize: 13, fontWeight: active() ? "600" : "500", color: active() ? colors().accent : colors().text, numberOfLines: 1 }}>{g().name}</Text>
            </Button>;
          }}</Each>
        </Scroll>
        <If when={visibleGroups().length >= 4}>
          <Button
            testID="all-groups-trigger"
            onPress={() => groupPickerOpen.set(true)}
            style={{
              flexDirection: "row", alignItems: "center", gap: 4,
              minHeight: 40, paddingHorizontal: 12, borderRadius: RADIUS.pill,
              backgroundColor: colors().surface, borderWidth: 1, borderColor: colors().border,
            }}
            hitSlop={{ top: 2, bottom: 2 }}
            hoverStyle={{ backgroundColor: colors().hover }}
            activeStyle={{ opacity: 0.78 }}>
            <Text style={{ fontSize: 12.5, fontWeight: "500", color: colors().muted }}>{tr("全部", "All")}</Text>
            <Icon name="chevronDown" size={14} color={colors().muted} />
          </Button>
        </If>
      </View>

      <SearchField testID="node-search" value={query()} onChange={value => query.set(value)} placeholder={tr("搜索节点", "Search nodes")} />

      <If when={!status()?.running}>
        <Notice
          tone="info"
          text={tr("未连接 · 浏览已保存的节点，连接后可切换和测速。", "Disconnected · Browse saved nodes. Connect to switch or test.")}
          action={() => <Action size="compact" quiet label={tr("去连接", "Connect")} disabled={false} onPress={() => navigate("overview")} />} />
      </If>
      <If when={!status()?.running && groupsIncomplete()}>
        <Label text={tr("部分提供器节点尚无可用本地预览，连接后加载完整列表。", "Some provider nodes have no local preview. Connect to load the full list.")} muted size={12} />
      </If>

      <If when={!!status()?.running && !!selectedGroup() && !isManualGroup()}>
        <Notice
          tone="info"
          text={tr(`${groupTypeLabel(selectedGroup()!.type)} · 由系统自动调度出口，单节点测速仍可用。`, `${groupTypeLabel(selectedGroup()!.type)} · Outbound is managed automatically; individual testing stays available.`)} />
      </If>

      <List
        testID="node-list"
        each={members()}
        keyExtractor={item => item.name}
        keyboardDismissMode="on-drag"
        style={{ flex: 1, borderRadius: RADIUS.surface, backgroundColor: colors().surface, borderWidth: 1, borderColor: colors().border }}
        empty={<Empty
          icon={query().trim() ? "search" : "proxies"}
          title={!query().trim() && !status()?.running && groupsIncomplete() ? tr("节点等待加载", "Nodes not loaded yet") : tr("没有匹配的节点", "No matching nodes")}
          description={!query().trim() && !status()?.running && groupsIncomplete() ? tr("连接后可加载此策略组的提供器节点。", "Connect to load provider nodes for this group.") : tr("换个名称，或清除搜索条件。", "Try another name or clear the search.")}
          action={() => <If when={!!query().trim()}><Action label={tr("重置筛选", "Reset filter")} disabled={false} onPress={() => query.set("")} /></If>} />}>
        {m => {
          const isSelected = () => selectedGroup()?.now === m().name;
          const testing = () => !!tasks()[`delay-${m().name}`];
          const hasDelay = () => !!status()?.running && m().delay !== undefined;
          return <View style={{
            flexDirection: "row", minHeight: 58, alignItems: "center", gap: 6,
            borderBottomWidth: 1, borderColor: colors().border,
            borderLeftWidth: 3, borderLeftColor: isSelected() ? colors().accent : "transparent",
            backgroundColor: isSelected() ? colors().selected : colors().surface,
            paddingRight: 8,
          }}>
            <Button
              testID={`node-${m().name}`}
              selected={isSelected()}
              disabled={!status()?.running || mutationBusy() || !isManualGroup()}
              onPress={() => void action("select", async () => { await call("proxies.select", { group: selectedGroup()?.name, member: m().name }); })}
              style={{ flex: 1, flexDirection: "row", alignItems: "center", gap: 10, minHeight: 58, paddingHorizontal: 12, paddingVertical: 8 }}
              hoverStyle={{ backgroundColor: colors().hover }}
              focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}
              disabledStyle={{ opacity: status()?.running ? 0.8 : 1 }}>
              <Icon name={isSelected() ? "circleCheck" : "circle"} selected={isSelected()} size={18} />
              <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
                <Text style={{ fontSize: 13.5, color: colors().text, fontWeight: isSelected() ? "600" : "500", numberOfLines: 2 }}>{m().name}</Text>
                <Label text={isSelected() ? (status()?.running ? tr("当前出口", "Selected") : tr("下次连接使用", "For next connection")) : m().type} muted size={11.5} />
              </View>
            </Button>
            <Button
              testID={`delay-${m().name}`}
              accessibilityLabel={tr(`测速 ${m().name}`, `Test ${m().name}`)}
              disabled={!status()?.running || !!busy()}
              onPress={() => void testMember(m().name)}
              style={{ minWidth: 64, minHeight: 44, paddingHorizontal: 10, borderRadius: 8, alignItems: "center", justifyContent: "center", backgroundColor: "transparent" }}
              hoverStyle={{ backgroundColor: colors().hover }}
              activeStyle={{ backgroundColor: colors().selected, opacity: 0.78 }}
              focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}
              disabledStyle={{ opacity: 0.4 }}>
              <If when={testing()}>
                <Text style={{ fontSize: 12, color: colors().muted }}>{tr("测速中…", "Testing…")}</Text>
              </If>
              <If when={!testing() && hasDelay()}>
                <Tag text={m().delay! > 0 ? `${m().delay} ms` : tr("超时", "Timeout")} tone={delayTone(m().delay!)} />
              </If>
              <If when={!testing() && !hasDelay()}>
                <Text style={{ fontSize: 12.5, fontWeight: "500", color: colors().muted }}>{tr("测速", "Test")}</Text>
              </If>
            </Button>
          </View>;
        }}
      </List>
    </If>

    <BottomSheet
      testID="all-groups-sheet"
      open={groupPickerOpen()}
      onClose={() => groupPickerOpen.set(false)}
      title={tr("选择策略组", "Select group")}
      scrollable>
      <Group>
        <Each of={visibleGroups()} key={g => g.name}>{g => <GroupRow
          title={g().name}
          subtitle={`${groupTypeLabel(g().type)} · ${g().now || "—"}`}
          trailing={() => <If when={selectedGroup()?.name === g().name}><Tag text={tr("当前", "Current")} tone="accent" /></If>}
          chevron={selectedGroup()?.name !== g().name}
          onPress={() => { groupName.set(g().name); query.set(""); groupPickerOpen.set(false); }} />}</Each>
      </Group>
    </BottomSheet>
  </View>;
}

function AppsPage() {
  const apps = signal<InstalledApp[]>([]);
  const scope = signal<AppScope>({ mode: "all", packages: [], excludeSelf: true });
  const initialScope = signal("");
  const search = signal("");
  const saving = signal(false);
  const ready = signal(false);
  const scopeError = signal("");

  const serialize = (s: AppScope) => `${s.mode}:${[...s.packages].sort().join(",")}`;
  const isDirty = () => ready() && serialize(scope()) !== initialScope();

  void Promise.all([androidCall<{ apps: InstalledApp[] }>("android.apps"), androidCall<AppScope>("android.scope.get")])
    .then(([list, value]) => {
      apps.set(list.apps);
      scope.set(value);
      appScopeSummary.set(value);
      initialScope.set(serialize(value));
      ready.set(true);
    })
    .catch(e => scopeError.set(message(e)));

  const filtered = () => {
    const q = search().trim().toLowerCase();
    const selected = new Set(scope().packages);
    return apps()
      .filter(a => !q || `${a.name} ${a.packageName}`.toLowerCase().includes(q))
      .sort((a, b) => Number(selected.has(b.packageName)) - Number(selected.has(a.packageName)) || a.name.localeCompare(b.name));
  };

  const toggle = (pkg: string) => scope.set({
    ...scope(),
    packages: scope().packages.includes(pkg) ? scope().packages.filter(p => p !== pkg) : [...scope().packages, pkg],
  });

  const handleBack = () => {
    if (!isDirty()) { subpage.set(""); return; }
    void confirmDialog({
      title: tr("放弃未保存的更改？", "Discard unsaved changes?"),
      description: tr("当前应用范围修改尚未保存，返回将丢失已选变更。", "Your app scope edits have not been saved."),
      confirmLabel: tr("放弃更改", "Discard"),
      cancelLabel: tr("继续编辑", "Keep editing"),
      destructive: true,
    }).then(discard => { if (discard) subpage.set(""); });
  };

  cleanup(BackHandler.on("press", () => {
    if (saving()) return true;
    handleBack();
    return true;
  }));

  const saveScope = () => {
    saving.set(true); scopeError.set("");
    void androidCall<AppScope>("android.scope.set", { mode: scope().mode, packages: scope().packages })
      .then(saved => {
        appScopeSummary.set(saved ?? scope());
        showToast(tr("应用范围已保存", "App scope saved"));
        subpage.set("");
      })
      .catch(e => scopeError.set(message(e)))
      .finally(() => saving.set(false));
  };

  return <View style={{ flex: 1 }}>
    <SubpageHeader title={tr("应用范围", "App scope")} backTestID="apps-back" backDisabled={saving()} onBack={handleBack} />
    <View style={{ flex: 1, padding: 16, gap: 12 }}>
      <Label text={tr("决定哪些应用参与 VPN，不改变代理组；Gopurr 自身始终排除。", "Choose which apps use VPN without changing proxy groups. Gopurr itself is always excluded.")} muted size={12} />
      <If when={connected() || connecting()}>
        <Notice
          tone="warn"
          text={tr("VPN 连接中无法保存修改，请先断开连接。", "Disconnect VPN before saving app scope changes.")}
          action={() => <Action size="compact" quiet label={tr("断开", "Disconnect")} disabled={connectionDisabled()} onPress={toggleEngine} />} />
      </If>
      <If when={!!scopeError()}><Notice text={scopeError()} tone="danger" /></If>

      <Segmented
        testIDPrefix="scope-mode"
        value={scope().mode}
        disabled={!ready() || saving()}
        onChange={id => scope.set({ ...scope(), mode: id as AppScope["mode"] })}
        items={[
          { id: "all", label: tr("所有应用", "All apps") },
          { id: "include", label: tr("仅选中", "Selected only") },
          { id: "exclude", label: tr("排除选中", "Exclude selected") },
        ]} />

      <If when={scope().mode !== "all"}>
        <SearchField testID="scope-search" value={search()} onChange={value => search.set(value)} placeholder={tr("搜索应用名称或包名", "Search app name or package")} />
        <Label text={tr(`已选 ${scope().packages.length} 个应用（已选项自动置顶）`, `${scope().packages.length} apps selected (selected shown first)`)} size={12} muted />
        <List
          each={filtered()}
          keyExtractor={a => a.packageName}
          keyboardDismissMode="on-drag"
          style={{ flex: 1, borderRadius: RADIUS.surface, backgroundColor: colors().surface, borderWidth: 1, borderColor: colors().border }}
          empty={<Empty icon="search" title={tr("没有匹配的应用", "No matching apps")} description={tr("可选择系统可见且已安装的应用。", "Choose from installed apps visible to Gopurr.")} />}>
          {a => {
            const checked = () => scope().packages.includes(a().packageName);
            return <Button
              testID={`scope-app-${a().packageName}`}
              selected={checked()}
              disabled={saving()}
              onPress={() => toggle(a().packageName)}
              style={{
                flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 14, paddingVertical: 10, minHeight: 60,
                borderBottomWidth: 1, borderColor: colors().border,
                backgroundColor: checked() ? colors().selected : colors().surface,
              }}
              hoverStyle={{ backgroundColor: colors().hover }}
              activeStyle={{ opacity: 0.8 }}>
              <Icon name={checked() ? "circleCheck" : "circle"} selected={checked()} size={18} />
              <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
                <Label text={a().name} bold lines={1} />
                <Label text={a().packageName} muted size={11.5} lines={1} />
              </View>
            </Button>;
          }}
        </List>
      </If>

      <If when={scope().mode === "all"}><View style={{ flex: 1 }} /></If>

      <Action
        testID="save-app-scope"
        primary
        full
        label={saving() ? tr("正在保存…", "Saving…") : tr("保存应用范围", "Save app scope")}
        disabled={!ready() || saving() || connected() || connecting() || (scope().mode === "include" && !scope().packages.length)}
        onPress={saveScope} />
    </View>
  </View>;
}

function LogsPage() {
  const logs = signal<{ at: string; level: string; text: string }[]>([]);
  const search = signal("");
  const levelFilter = signal<"all" | "warn" | "error">("all");
  const logError = signal("");
  const refreshing = signal(false);

  const reload = () => {
    refreshing.set(true);
    void call<{ logs: { at: string; level: string; text: string }[] }>("logs.list")
      .then(r => { logs.set(r.logs); logError.set(""); })
      .catch(e => logError.set(message(e)))
      .finally(() => refreshing.set(false));
  };
  reload();

  const filtered = () => logs().filter(l => {
    const lv = l.level.toLowerCase();
    if (levelFilter() === "warn" && !lv.includes("warn")) return false;
    if (levelFilter() === "error" && !lv.includes("err") && !lv.includes("fatal")) return false;
    const q = search().trim().toLowerCase();
    return !q || `${l.level} ${l.text}`.toLowerCase().includes(q);
  });

  const copyLogs = () => {
    const text = filtered().map(l => `${l.at} [${l.level}] ${l.text}`).join("\n");
    if (!text) return;
    if (androidHost()) {
      void androidCall("android.clipboard.write", { text })
        .then(() => showToast(tr(`已复制 ${filtered().length} 条日志`, `Copied ${filtered().length} log entries`)))
        .catch(() => showToast(tr("当前环境无法写入剪贴板，请长按日志选中复制", "Clipboard unavailable; long-press log text to copy")));
      return;
    }
    showToast(tr("请选中日志正文进行复制", "Select log text directly to copy"));
  };

  return <View style={{ flex: 1 }}>
    <SubpageHeader
      title={tr("诊断日志", "Diagnostic logs")}
      backTestID="logs-back"
      onBack={() => subpage.set("")}
      action={() => <View style={{ flexDirection: "row", alignItems: "center", gap: 4 }}>
        <Action testID="logs-copy" size="compact" quiet label={tr("复制", "Copy")} disabled={!filtered().length} onPress={copyLogs} />
        <IconButton testID="logs-refresh" icon="refresh" label={tr("刷新日志", "Refresh logs")} onPress={reload} />
      </View>} />
    <View style={{ flex: 1, padding: 16, gap: 10 }}>
      <SearchField testID="logs-search" value={search()} onChange={value => search.set(value)} placeholder={tr("搜索日志", "Search logs")} />
      <Segmented
        testIDPrefix="logs-level"
        value={levelFilter()}
        onChange={id => levelFilter.set(id as "all" | "warn" | "error")}
        items={[
          { id: "all", label: tr("全部", "All") },
          { id: "warn", label: tr("警告", "Warn") },
          { id: "error", label: tr("错误", "Error") },
        ]} />
      <If when={!!logError()}><Notice text={logError()} tone="danger" /></If>
      <List
        testID="logs-list"
        each={filtered()}
        keyExtractor={(l, i) => `${i}-${l.at}`}
        refreshing={refreshing()}
        onRefresh={reload}
        keyboardDismissMode="on-drag"
        style={{ flex: 1, backgroundColor: colors().surface, borderRadius: RADIUS.surface, borderWidth: 1, borderColor: colors().border }}
        empty={<Empty
          icon={search().trim() || levelFilter() !== "all" ? "search" : "logs"}
          title={search().trim() || levelFilter() !== "all" ? tr("没有匹配的日志", "No matching logs") : tr("暂无日志", "No logs")}
          description={search().trim() || levelFilter() !== "all" ? tr("调整筛选条件或清空搜索。", "Adjust the filter or clear the search.") : tr("连接运行后将在此显示日志。", "Logs appear here when the connection runs.")}
          action={() => <If when={!!search().trim() || levelFilter() !== "all"}><Action label={tr("重置筛选", "Reset filter")} disabled={false} onPress={() => { search.set(""); levelFilter.set("all"); }} /></If>} />}>
        {l => {
          const lv = () => l().level.toLowerCase();
          const isErr = () => lv().includes("err") || lv().includes("fatal");
          const isWarn = () => lv().includes("warn");
          return <View selectable style={{
            padding: 12, gap: 4, borderBottomWidth: 1, borderColor: colors().border,
            borderLeftWidth: isErr() || isWarn() ? 3 : 0,
            borderLeftColor: isErr() ? colors().danger : colors().warn,
          }}>
            <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
              <Label text={l().at} faint size={11} tabular />
              <Tag text={l().level} tone={isErr() ? "danger" : isWarn() ? "warn" : "muted"} />
            </View>
            <Text style={{ fontSize: 12, lineHeight: 17, fontFamily: "monospace", color: colors().text }}>{l().text}</Text>
          </View>;
        }}
      </List>
    </View>
  </View>;
}

function updateSummary(value: AppUpdate): string {
  if (!value.supported) return tr("当前为开发构建；正式签名版本支持应用内更新。", "Debug builds cannot install release updates. Use a signed release to enable in-app updates.");
  switch (value.phase) {
    case "idle": return tr("检查 GitHub Releases 上的新版本。", "Check GitHub Releases for a new version.");
    case "checking": return tr("正在检查新版本…", "Checking for a new version…");
    case "current": return tr("当前已是最新版本。", "You are up to date.");
    case "available": return tr(`发现 Gopurr ${value.version}`, `Gopurr ${value.version} is available`);
    case "downloading": return tr(`正在下载 Gopurr ${value.version}`, `Downloading Gopurr ${value.version}`);
    case "ready": return value.canInstall ? tr(`Gopurr ${value.version} 已下载并校验。`, `Gopurr ${value.version} is downloaded and verified.`) : tr(`Gopurr ${value.version} 已下载；允许安装未知应用后继续。`, `Gopurr ${value.version} is downloaded. Allow app installs to continue.`);
    case "installing": return tr("正在等待系统安装程序…", "Waiting for the system installer…");
    case "installed": return tr("更新已安装，重新打开应用后生效。", "The update was installed. Reopen the app to use it.");
    case "failed": return tr("检查更新失败。", "Could not check for updates.");
    default: return "";
  }
}

function updateAction(method: string) {
  void androidCall<AppUpdate>(`android.update.${method}`).then(value => appUpdate.set(value)).catch(value => error.set(message(value)));
}

function NotificationSheet() {
  const settings = notifSettings;
  const configure = (traffic: boolean) => void action("notification", async () => {
    settings.set(await androidCall<NotificationSettings>("android.notifications.configure", { traffic }));
  }, false);
  const permission = () => void action("notification", async () => {
    settings.set(await androidCall<NotificationSettings>("android.notifications.request"));
  }, false);
  const systemSettings = () => void action("notification", async () => {
    await androidCall("android.notifications.settings");
  }, false);

  return <BottomSheet
    testID="notification-sheet"
    open={notifSheetOpen()}
    onClose={() => notifSheetOpen.set(false)}
    title={tr("状态栏通知", "Status notification")}
    subtitle={settings()?.enabled ? tr("已允许显示通知", "Notifications enabled") : tr("未允许显示通知", "Notifications disabled")}>
    <Label text={tr("VPN 连接时常驻，点击通知返回连接页，也可直接断开。", "Stays visible while VPN is connected. Tap to open Connect, or disconnect directly.")} muted size={12.5} />
    <View style={{ gap: 6 }}>
      <Label text={tr("通知内容", "Notification content")} bold size={12} muted />
      <Segmented
        value={settings()?.traffic === false ? "state" : "traffic"}
        disabled={!settings() || !!tasks().notification}
        onChange={id => configure(id === "traffic")}
        items={[
          { id: "traffic", label: tr("实时流量", "Live traffic"), testID: "notification-traffic" },
          { id: "state", label: tr("仅连接状态", "Connection only"), testID: "notification-state-only" },
        ]} />
    </View>
    <Label text={tr("亮屏时每 2 秒更新速率，省电模式下每 10 秒更新；熄屏暂停，亮屏后恢复。", "Rates update every 2 seconds, or 10 seconds in battery saver. Updates pause with the screen off and resume when it wakes.")} muted size={12} />
    <View style={{ gap: 8 }}>
      <If when={!!settings()?.canRequest}>
        <Action testID="notification-permission" primary full label={tr("允许通知", "Allow notifications")} disabled={!!tasks().notification} onPress={permission} />
      </If>
      <Action testID="notification-settings" full label={tr("系统通知设置", "System notification settings")} disabled={!!tasks().notification} onPress={systemSettings} />
    </View>
  </BottomSheet>;
}

function UpdateSheet() {
  const value = appUpdate;
  return <BottomSheet
    testID="update-sheet"
    open={updateSheetOpen()}
    onClose={() => updateSheetOpen.set(false)}
    title="Gopurr"
    subtitle={tr(`当前版本 ${value()?.currentVersion ?? "—"}`, `Version ${value()?.currentVersion ?? "—"}`)}
    scrollable>
    <Label text={tr("让连接简单一点。", "Connections, simplified.")} muted size={12.5} />
    <If when={!value()}><Label text={tr("此版本未提供应用内更新。", "This build does not provide in-app updates.")} muted size={12.5} /></If>
    <If when={!!value()}>
      <Label text={updateSummary(value()!)} size={13.5} bold />
      <If when={!!value()?.detail}><Label text={value()?.detail ?? ""} muted size={12} /></If>
      <If when={!!value()?.error}><Notice text={value()?.error ?? ""} tone="danger" /></If>
      <If when={!!value()?.notes && value()?.phase === "available"}>
        <Panel padding={12} gap={6}>
          <Label text={tr("版本说明", "Release notes")} muted size={11.5} bold />
          <View selectable><Text style={{ fontSize: 12, lineHeight: 18, color: colors().muted }}>{value()?.notes ?? ""}</Text></View>
        </Panel>
      </If>
      <If when={value()?.phase === "checking" || value()?.phase === "downloading"}>
        <Progress
          testID="update-progress"
          value={value()?.phase === "checking" ? 0 : Math.min(100, ((value()?.downloaded ?? 0) / Math.max(1, value()?.total ?? 0)) * 100)}
          indeterminate={value()?.phase === "checking"}
          accessibilityLabel={tr("更新进度", "Update progress")}
          trackStyle={{ height: 6, borderRadius: 3, backgroundColor: colors().border, overflow: "hidden" }}
          indicatorStyle={{ height: 6, borderRadius: 3, backgroundColor: colors().accent }} />
      </If>
      <If when={value()?.phase === "downloading"}>
        <Label text={`${bytes(value()?.downloaded ?? 0)} / ${(value()?.total ?? 0) > 0 ? bytes(value()?.total ?? 0) : tr("大小待确认", "Size unknown")}`} muted size={12} tabular />
      </If>
      <View style={{ gap: 8, paddingTop: 4 }}>
        <If when={!value()?.supported}><Action full label={tr("仅正式版可用", "Release builds only")} disabled onPress={() => {}} /></If>
        <If when={!!value()?.supported && (value()?.phase === "idle" || value()?.phase === "current" || value()?.phase === "failed" || value()?.phase === "installed")}>
          <Action testID="update-check" full primary={value()?.phase !== "installed"} label={value()?.phase === "current" ? tr("重新检查", "Check again") : tr("检查更新", "Check for updates")} disabled={false} onPress={() => updateAction("check")} />
        </If>
        <If when={value()?.phase === "checking"}><Action full label={tr("正在检查…", "Checking…")} disabled onPress={() => {}} /></If>
        <If when={value()?.phase === "available"}><Action testID="update-download" full primary label={tr("下载更新", "Download update")} disabled={false} onPress={() => updateAction("download")} /></If>
        <If when={value()?.phase === "downloading"}><Action testID="update-cancel" full label={tr("取消下载", "Cancel download")} disabled={false} onPress={() => updateAction("cancel")} /></If>
        <If when={value()?.phase === "ready"}><Action testID="update-install" full primary label={value()?.canInstall ? tr("安装更新", "Install update") : tr("允许安装", "Allow install")} disabled={false} onPress={() => updateAction("install")} /></If>
        <If when={value()?.phase === "installing"}><Action full label={tr("等待确认", "Waiting for confirmation")} disabled onPress={() => {}} /></If>
      </View>
    </If>
  </BottomSheet>;
}

function Settings() {
  effect(() => {
    if (lifecycle() !== "active" || !androidHost()) return;
    void refreshNotificationSettings();
    void refreshAppScopeSummary();
  });

  const changeAppearance = (value: "system" | "light" | "dark") => {
    appearance.set(value);
    setColorScheme(value);
    void Storage.setItem("appearance", value).catch(e => error.set(message(e)));
  };
  const changeLanguage = (value: "system" | "zh-Hans" | "en") => {
    language.set(value);
    void Storage.setItem("language", value).catch(e => error.set(message(e)));
  };

  const meshValue = () => {
    if (awaitingMeshPermission()) return tr("等待授权", "Permission needed");
    if (status()?.mesh?.running) return tr(`已连接 · ${status()?.mesh?.peerCount ?? 0} 台`, `Connected · ${status()?.mesh?.peerCount ?? 0}`);
    return tr("未连接", "Disconnected");
  };

  const notifValue = () => {
    const s = notifSettings();
    if (!s) return "";
    if (!s.enabled) return tr("未开启", "Off");
    return s.traffic ? tr("实时流量", "Live traffic") : tr("仅连接状态", "Status only");
  };

  const updateBadge = () => {
    const u = appUpdate();
    if (!u) return null;
    if (u.phase === "available") return { text: tr(`新版本 ${u.version}`, `New ${u.version}`), tone: "accent" as Tone };
    if (u.phase === "ready") return { text: tr("待安装", "Ready"), tone: "success" as Tone };
    if (u.phase === "downloading") return { text: `${Math.min(100, Math.round(((u.downloaded ?? 0) / Math.max(1, u.total ?? 1)) * 100))}%`, tone: "accent" as Tone };
    if (u.phase === "failed") return { text: tr("检查失败", "Failed"), tone: "danger" as Tone };
    return null;
  };

  return <View style={{ flex: 1 }}>
    <Scroll testID="settings-scroll" style={{ flex: 1 }} contentStyle={{ padding: 16, gap: 14 }}>
      <Heading title={tr("设置", "Settings")} />

      <SectionLabel title={tr("连接与网络", "Connection & network")} />
      <Group>
        <GroupRow
          testID="open-mesh"
          icon="network"
          iconColor={status()?.mesh?.running ? colors().success : colors().muted}
          title={tr("设备组网", "Mesh")}
          subtitle={tr("连接自己的设备，不受代理模式影响", "Connect your own devices independently of proxy mode")}
          value={meshValue()}
          valueTone={status()?.mesh?.running ? "success" : awaitingMeshPermission() ? "accent" : "muted"}
          chevron
          onPress={() => openSubpage("mesh")} />
        <If when={androidHost()}>
          <GroupRow
            testID="open-app-scope"
            icon="layers"
            title={tr("应用范围", "App scope")}
            subtitle={tr("选择参与 VPN 的应用", "Choose which apps use VPN")}
            value={scopeSummaryText(appScopeSummary())}
            chevron
            onPress={() => openSubpage("apps")} />
          <GroupRow
            testID="open-notifications"
            icon="activity"
            title={tr("状态栏通知", "Status notification")}
            subtitle={tr("常驻通知与实时速率显示", "Persistent VPN notification & live rates")}
            value={notifValue()}
            valueTone={notifSettings()?.enabled ? "muted" : "warn"}
            chevron
            onPress={() => notifSheetOpen.set(true)} />
        </If>
      </Group>

      <SectionLabel title={tr("外观与语言", "Appearance & language")} />
      <Group>
        <GroupRow title={tr("外观", "Appearance")}>
          <Segmented
            testIDPrefix="appearance"
            value={appearance()}
            onChange={id => changeAppearance(id as "system" | "light" | "dark")}
            items={[
              { id: "system", label: tr("跟随系统", "System") },
              { id: "light", label: tr("浅色", "Light") },
              { id: "dark", label: tr("深色", "Dark") },
            ]} />
        </GroupRow>
        <GroupRow title={tr("语言", "Language")}>
          <Segmented
            testIDPrefix="language"
            value={language()}
            onChange={id => changeLanguage(id as "system" | "zh-Hans" | "en")}
            items={[
              { id: "system", label: tr("跟随系统", "System") },
              { id: "zh-Hans", label: "简体中文" },
              { id: "en", label: "English" },
            ]} />
        </GroupRow>
      </Group>

      <SectionLabel title={tr("诊断与关于", "Diagnostics & about")} />
      <Group>
        <GroupRow
          testID="open-logs"
          icon="logs"
          title={tr("诊断日志", "Diagnostic logs")}
          subtitle={tr("查看最近的连接与内核事件", "Review recent connection and engine events")}
          chevron
          onPress={() => openSubpage("logs")} />
        <If when={androidHost()}>
          <GroupRow
            testID="open-update"
            icon="shield"
            title={tr("关于与更新", "About & updates")}
            subtitle={tr(`当前版本 ${appUpdate()?.currentVersion ?? "—"} · 让连接简单一点。`, `Version ${appUpdate()?.currentVersion ?? "—"} · Connections, simplified.`)}
            trailing={() => <If when={!!updateBadge()}><Tag text={updateBadge()!.text} tone={updateBadge()!.tone} /></If>}
            chevron
            onPress={() => updateSheetOpen.set(true)}>
            <If when={appUpdate()?.phase === "downloading"}>
              <View style={{ gap: 4 }}>
                <Progress
                  value={Math.min(100, ((appUpdate()?.downloaded ?? 0) / Math.max(1, appUpdate()?.total ?? 0)) * 100)}
                  accessibilityLabel={tr("更新进度", "Update progress")}
                  trackStyle={{ height: 5, borderRadius: 3, backgroundColor: colors().border, overflow: "hidden" }}
                  indicatorStyle={{ height: 5, borderRadius: 3, backgroundColor: colors().accent }} />
                <Label text={`${bytes(appUpdate()?.downloaded ?? 0)} / ${(appUpdate()?.total ?? 0) > 0 ? bytes(appUpdate()?.total ?? 0) : tr("大小待确认", "Size unknown")}`} muted size={11} tabular />
              </View>
            </If>
          </GroupRow>
        </If>
        <If when={!androidHost()}>
          <GroupRow icon="shield" title="Gopurr" subtitle={tr("让连接简单一点。", "Connections, simplified.")} />
        </If>
      </Group>
    </Scroll>

    <If when={androidHost()}>
      <NotificationSheet />
      <UpdateSheet />
    </If>
  </View>;
}

function MobilePageStack() {
  return <View style={{ flex: 1, minHeight: 0 }}>
    <If when={!subpage() && page() === "overview"}><Overview /></If>
    <If when={!subpage() && page() === "profiles"}><Profiles /></If>
    <If when={!subpage() && page() === "proxies"}><Proxies /></If>
    <If when={!subpage() && page() === "settings"}><Settings /></If>
    <If when={subpage() === "apps"}><AppsPage /></If>
    <If when={subpage() === "logs"}><LogsPage /></If>
    <If when={subpage() === "mesh"}><MeshPage onBack={() => subpage.set("")} /></If>
  </View>;
}

export function GopurrApp(props: { preview?: boolean } = {}) {
  void Promise.all([Storage.getItem("appearance"), Storage.getItem("language")]).then(([theme, lang]) => {
    if (theme === "system" || theme === "light" || theme === "dark") { appearance.set(theme); setColorScheme(theme); }
    if (lang === "system" || lang === "zh-Hans" || lang === "en") language.set(lang);
  }).catch(e => error.set(message(e)));

  startMobileStore();

  if (androidHost()) {
    cleanup(host.on("android.navigate", value => {
      const target = (value as unknown as { page?: string }).page;
      if (target === "overview" || target === "profiles" || target === "proxies" || target === "settings") {
        navigate(target);
      }
    }));
  }

  effect(() => {
    const scheme = colorScheme();
    if (androidHost()) {
      void androidCall("android.appearance", { scheme }).catch(() => {});
    }
  });

  // Plain view animation avoids a ShaderView/Vulkan surface on Android. It
  // ends after the page enters and never delays navigation or disables input.
  const pageProgress = createAnimatedValue(1);
  let previousRoute = `${page()}-${subpage()}`;
  effect(() => {
    const route = `${page()}-${subpage()}`;
    const reduced = reduceMotion();
    if (route === previousRoute && !reduced) return;
    previousRoute = route;
    void pageProgress.stop();
    if (reduced) { pageProgress.set(1); return; }
    pageProgress.set(0);
    void pageProgress.timing(1, { duration: 180, easing: "easeOut" });
  });
  cleanup(() => void pageProgress.stop());

  // Open BottomSheet instances consume Android back natively via closeOnEscape.
  cleanup(BackHandler.on("press", () => {
    if (subpage()) { subpage.set(""); return true; }
    if (page() !== "overview") { navigate("overview"); return true; }
    return false;
  }));

  return <View style={{
    flex: 1,
    backgroundColor: colors().bg,
    paddingTop: "env(safe-area-inset-top)",
    paddingLeft: "env(safe-area-inset-left)",
    paddingRight: "env(safe-area-inset-right)",
    paddingBottom: "env(safe-area-inset-bottom)",
    flexDirection: compact() ? "column" : "row",
  }}>
    <If when={!compact()}><Navigation /></If>
    <View style={{ flex: 1, minHeight: 0 }}>
      <If when={!!props.preview}>
        <View style={{ paddingHorizontal: 16, paddingVertical: 6, backgroundColor: colors().selected }}>
          <Text style={{ fontSize: 11, color: colors().accent }}>{tr("界面预览 · 演示数据，不连接真实代理", "UI preview · Fixture data, no live proxy")}</Text>
        </View>
      </If>
      <If when={!!error()}>
        <View testID="error-banner" role="alert" style={{ margin: 16, marginBottom: 0 }}>
          <Notice
            text={error()}
            tone="danger"
            action={() => <View style={{ flexDirection: "row", gap: 4 }}>
              <Action size="compact" quiet label={tr("重试", "Retry")} disabled={false} onPress={() => void refresh()} />
              <Action size="compact" quiet label={tr("关闭", "Dismiss")} disabled={false} onPress={() => error.set("")} />
            </View>} />
        </View>
      </If>
      <View style={{ flex: 1, minHeight: 0, opacity: pageProgress, transform: [{ translateY: pageProgress.interpolate({ inputRange: [0, 1], outputRange: [4, 0] }) }] }}>
        <If when={colorScheme() === "light"}><MobilePageStack /></If>
        <If when={colorScheme() === "dark"}><MobilePageStack /></If>
      </View>
    </View>
    <If when={compact() && !subpage()}><Navigation /></If>
    <ToastHost />
  </View>;
}
