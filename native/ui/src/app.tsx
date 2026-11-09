import { cleanup, colorScheme, effect, host, Image, lifecycle, List, reduceMotion, Scroll, setColorScheme, signal, Storage, Text, View } from "nsh";
import { BackHandler } from "nsh/mobile";
import { Button, Input, Progress } from "nsh/base";
import { call } from "./client";
import type { Profile } from "./client";
import { colors } from "./theme";
import { language, tr } from "./i18n";
import { action, activate, activeProfile, appUpdate, busy, changeMode, connected, connecting, error, groupName, groupsIncomplete, loaded, members, message, mutationBusy, page, profiles, query, refresh, selectedGroup, session, startMobileStore, status, tasks, testMember, toggleEngine, visibleGroups } from "./mobile/store";
import { androidCall, androidHost } from "./mobile/platform";
import type { AppScope, AppUpdate, InstalledApp, NotificationSettings } from "./mobile/platform";
import { Action, bytes, compact, Empty, Field, Heading, Icon, Label, Panel } from "./mobile/ui";
import logo from "../assets/sidebar.png";
import overviewIcon from "../assets/icons/layout-dashboard.svg";
import profilesIcon from "../assets/icons/file-text.svg";
import proxiesIcon from "../assets/icons/globe.svg";
import settingsIcon from "../assets/icons/settings-2.svg";
import checkedIcon from "../assets/icons/circle-check.svg";
import circleIcon from "../assets/icons/circle.svg";

const name = signal(""), source = signal(""), yaml = signal("");
const importKind = signal<"url" | "yaml">("url"), adding = signal(false);
const appearance = signal<"system" | "light" | "dark">("system");
const subpage = signal<"" | "apps" | "logs">("");
const editing = signal(""), editName = signal(""), editURL = signal(""), editKind = signal("");
const removing = signal<Profile | null>(null);
const notice = signal("");
let noticeTimer: ReturnType<typeof setTimeout> | undefined;
function showNotice(value: string) { clearTimeout(noticeTimer); notice.set(value); noticeTimer = setTimeout(() => notice.set(""), 4000); }
const nav = [
  { id: "overview" as const, zh: "连接", en: "Connect", icon: overviewIcon },
  { id: "proxies" as const, zh: "节点", en: "Nodes", icon: proxiesIcon },
  { id: "profiles" as const, zh: "订阅", en: "Profiles", icon: profilesIcon },
  { id: "settings" as const, zh: "设置", en: "Settings", icon: settingsIcon },
];
function navigate(id: typeof nav[number]["id"]) { subpage.set(""); page.set(id); }
function stateLabel() {
  switch (session()?.phase) {
    case "awaitingPermission": return tr("等待系统授权", "Waiting for VPN permission");
    case "connecting": return tr("正在连接…", "Connecting…");
    case "disconnecting": return tr("正在断开…", "Disconnecting…");
    case "failed": return tr("连接失败", "Connection failed");
    case "connected": return tr("设备 VPN 已连接", "Device VPN connected");
    case "idle": return tr("尚未连接", "Disconnected");
  }
  return status()?.tun ? tr("设备 VPN 已连接", "Device VPN connected") : status()?.running ? tr("本地代理已运行", "Local proxy running") : tr("尚未连接", "Disconnected");
}
function connectionDisabled() {
  if (androidHost()) return session()?.phase === "disconnecting" || !!tasks().connection || (!connected() && !connecting() && (!status()?.activeProfile || mutationBusy()));
  return !!tasks().connection || (!connected() && (!status()?.activeProfile || mutationBusy()));
}
function connectionAction() {
  if (session()?.phase === "disconnecting") return tr("正在断开…", "Disconnecting…");
  if (androidHost() && connecting()) return tr("取消连接", "Cancel connection");
  if (tasks().connection) return tr("处理中…", "Working…");
  return status()?.vpnSupported ? connected() ? tr("断开连接", "Disconnect") : tr("连接 VPN", "Connect VPN") : connected() ? tr("停止本地代理", "Stop local proxy") : tr("启动本地代理", "Start local proxy");
}
function Navigation() {
  return <View role="navigation" style={{ width: compact() ? "100%" : 184, flexDirection: compact() ? "row" : "column", padding: 8, gap: 4, backgroundColor: colors().side, borderColor: colors().border, borderRightWidth: compact() ? 0 : 1, borderTopWidth: compact() ? 1 : 0 }}>
    <If when={!compact()}><View style={{ flexDirection: "row", alignItems: "center", gap: 8, padding: 12, paddingBottom: 24 }}><Image src={logo} style={{ width: 32, height: 32, objectFit: "contain" }} /><Label text="Gopurr" size={16} bold /></View></If>
    <Each of={nav}>{item => <Button testID={`nav-${item().id}`} selected={page() === item().id} onPress={() => navigate(item().id)} style={{ flex: compact() ? 1 : undefined, minHeight: compact() ? 52 : 48, flexDirection: compact() ? "column" : "row", alignItems: "center", justifyContent: compact() ? "center" : "flex-start", gap: compact() ? 4 : 10, paddingHorizontal: 12, borderRadius: 8, backgroundColor: page() === item().id ? colors().selected : "transparent", transitionProperty: "background-color", transitionDuration: reduceMotion() ? 0 : 140 }} hoverStyle={{ backgroundColor: colors().hover }} focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }} activeStyle={{ opacity: 0.7 }}>
      <Icon source={item().icon} selected={page() === item().id} size={20} /><Text style={{ color: page() === item().id ? colors().accent : colors().muted, fontSize: compact() ? 11 : 13, fontWeight: page() === item().id ? "600" : "400" }}>{tr(item().zh, item().en)}</Text>
    </Button>}</Each>
  </View>;
}
function Overview() {
  return <Scroll testID="overview-scroll" style={{ flex: 1 }} contentStyle={{ padding: 16, gap: 16 }}>
    <Heading title={tr("连接", "Connect")} description={tr("当前配置与网络连接。", "Your profile and network connection.")} action={() => <Action label={tr("刷新", "Refresh")} disabled={false} onPress={() => void refresh()} />} />
    <Panel>
      <View style={{ gap: 8 }}><Text style={{ color: connected() ? colors().success : colors().muted, fontSize: 13, fontWeight: "600" }}>{stateLabel()}</Text>
        <Text style={{ fontSize: 22, fontWeight: "600", color: colors().text, numberOfLines: 2 }}>{activeProfile()?.name ?? tr("先添加一个订阅", "Add your first profile")}</Text>
        <Label text={connected() && status()?.vpnSupported ? tr("设备流量按当前配置转发。", "Device traffic follows your active profile.") : status()?.running && !status()?.vpnSupported ? tr(`监听本机端口 ${status()?.mixedPort}`, `Listening locally on port ${status()?.mixedPort}`) : tr("选择配置后即可启动连接。", "Choose a profile to start a connection.")} muted size={12} />
      </View>
      <If when={!!status()?.running && !!selectedGroup()}><Label text={`${selectedGroup()?.name}: ${selectedGroup()?.now}`} muted size={12} /></If>
      <Action testID="toggle-engine" primary label={connectionAction()} onPress={toggleEngine} disabled={connectionDisabled()} />
      <If when={!!status()?.reconnectRequired}><View testID="reconnect-required" style={{ padding: 12, gap: 6, borderRadius: 8, backgroundColor: colors().selected }}><Label text={tr("新配置已保存，重新连接后生效。", "The new profile is saved. Reconnect to apply it.")} size={12} /></View></If>
      <View style={{ flexDirection: "row", gap: 16, paddingVertical: 8 }}>
        <View style={{ flex: 1, gap: 6 }}><Label text={tr("下载", "Download")} muted size={12} /><Text style={{ color: colors().down, fontSize: 27, fontWeight: "600", fontVariant: "tabular-nums" }}>{loaded() && status()?.running ? bytes(status()?.traffic.down ?? 0, true) : "—"}</Text></View>
        <View style={{ flex: 1, gap: 6 }}><Label text={tr("上传", "Upload")} muted size={12} /><Text style={{ color: colors().up, fontSize: 27, fontWeight: "600", fontVariant: "tabular-nums" }}>{loaded() && status()?.running ? bytes(status()?.traffic.up ?? 0, true) : "—"}</Text></View>
      </View>
      <View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap" }}><Each of={[{ id: "rule" as const, zh: "规则", en: "Rule" }, { id: "global" as const, zh: "全局", en: "Global" }, { id: "direct" as const, zh: "直连", en: "Direct" }]}>{mode => <Action testID={`mode-${mode().id}`} label={tr(mode().zh, mode().en)} primary={status()?.mode === mode().id} disabled={!status()?.running || mutationBusy()} onPress={() => void changeMode(mode().id)} />}</Each></View>
      <Label text={status()?.vpnSupported ? tr("首次连接需系统授权；切换订阅前请先断开。", "First connection needs VPN permission. Disconnect before switching profiles.") : tr("本地代理运行不会自动接管设备流量。", "Starting the local proxy does not route device traffic automatically.")} muted size={12} />
    </Panel>
    <Panel><View style={{ flexDirection: "row", gap: 16, flexWrap: "wrap" }}>
      <View style={{ flex: 1, minWidth: 80, gap: 4 }}><Label text={tr("当前连接", "Connections")} muted size={12} /><Label text={String(status()?.traffic.connections ?? 0)} bold size={16} /></View>
      <View style={{ flex: 1, minWidth: 80, gap: 4 }}><Label text={tr("累计下载", "Downloaded")} muted size={12} /><Label text={bytes(status()?.traffic.downTotal ?? 0)} bold size={16} /></View>
      <View style={{ flex: 1, minWidth: 80, gap: 4 }}><Label text={tr("累计上传", "Uploaded")} muted size={12} /><Label text={bytes(status()?.traffic.upTotal ?? 0)} bold size={16} /></View>
    </View></Panel>
    <If when={!profiles().length && loaded()}><Action label={tr("添加订阅", "Add profile")} primary onPress={() => { navigate("profiles"); adding.set(true); }} /></If>
  </Scroll>;
}
async function importProfile() {
  if (!name().trim() || !(importKind() === "url" ? source().trim() : yaml().trim())) return;
  await action("import", async () => {
    await call("profiles.import", { name: name().trim(), ...(importKind() === "url" ? { url: source().trim() } : { yaml: yaml() }) });
    source.set(""); yaml.set(""); name.set(""); adding.set(false);
  });
}
function ImportForm() {
  return <Panel><Label text={tr("添加订阅", "Add profile")} bold size={16} />
    <View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap" }}><Action label={tr("订阅链接", "Subscription URL")} primary={importKind() === "url"} onPress={() => importKind.set("url")} /><Action label={tr("粘贴 YAML", "Paste YAML")} primary={importKind() === "yaml"} onPress={() => importKind.set("yaml")} /><If when={androidHost()}><Action testID="import-file" label={tr("选择文件", "Choose file")} onPress={() => void action("file", async () => { const file = await androidCall<{ name?: string; content?: string; cancelled?: boolean }>("android.file.pick"); if (file.cancelled || !file.content) return; yaml.set(file.content); if (!name()) name.set(file.name ?? tr("本地配置", "Local profile")); importKind.set("yaml"); })} /></If></View>
    <Field label={tr("名称", "Name")} testID="profile-name" value={name()} onChange={value => name.set(value)} placeholder={tr("例如：日常使用", "For example: Everyday")} disabled={mutationBusy()} />
    <If when={importKind() === "url"}><Field label={tr("订阅链接", "Subscription URL")} testID="profile-url" value={source()} onChange={value => source.set(value)} placeholder="https://" disabled={mutationBusy()} /></If>
    <If when={importKind() === "yaml"}><Field label={tr("配置内容", "Configuration YAML")} testID="profile-yaml" value={yaml()} onChange={value => yaml.set(value)} placeholder={tr("粘贴完整 YAML 配置", "Paste a complete YAML profile")} multiline disabled={mutationBusy()} /></If>
    <Label text={tr("导入后在连接页启动；其他订阅需明确启用。", "Connect after importing. Activate another profile explicitly to switch.")} muted size={12} />
    <View style={{ flexDirection: "row", gap: 8, justifyContent: "flex-end" }}><If when={profiles().length > 0}><Action label={tr("收起", "Close")} onPress={() => adding.set(false)} /></If><Action testID="import-profile" primary label={tasks().import ? tr("正在导入…", "Importing…") : tr("导入", "Import")} onPress={() => void importProfile()} disabled={mutationBusy() || !name().trim() || !(importKind() === "url" ? source().trim() : yaml().trim())} /></View>
  </Panel>;
}
async function beginEdit(id: string) {
  await action("editor", async () => { const p = await call<{ id: string; name: string; url: string; kind: string }>("profiles.editor", { id }); editing.set(p.id); editName.set(p.name); editURL.set(p.url ?? ""); editKind.set(p.kind); });
}
function ProfileEditor() {
  return <Panel><Label text={tr("编辑订阅", "Edit profile")} size={16} bold />
    <Field label={tr("名称", "Name")} testID="edit-profile-name" value={editName()} onChange={value => editName.set(value)} disabled={mutationBusy()} />
    <If when={editKind() === "url"}><Field label={tr("订阅链接", "Subscription URL")} testID="edit-profile-url" value={editURL()} onChange={value => editURL.set(value)} disabled={mutationBusy()} /><Label text={tr("保存只修改来源，点击更新后下载新内容。", "Saving changes the source. Refresh to download its contents.")} muted size={12} /></If>
    <View style={{ flexDirection: "row", gap: 8, justifyContent: "flex-end" }}><Action label={tr("放弃修改", "Discard")} onPress={() => editing.set("")} /><Action testID="save-profile" primary label={tr("保存", "Save")} disabled={mutationBusy() || !editName().trim()} onPress={() => void action("edit", async () => { await call("profiles.update", { id: editing(), name: editName().trim(), ...(editKind() === "url" ? { url: editURL().trim() } : {}) }); editing.set(""); showNotice(tr("订阅信息已保存。", "Profile details saved.")); })} /></View>
  </Panel>;
}
function Profiles() {
  return <Scroll testID="profiles-scroll" style={{ flex: 1 }} contentStyle={{ padding: 16, gap: 12 }}>
    <Heading title={tr("订阅", "Profiles")} description={status()?.tun ? tr("更新可以保存；切换配置前请先断开 VPN。", "Updates can be saved. Disconnect VPN before switching profiles.") : tr("选择本次连接使用的配置。", "Choose the profile for your connection.")} action={() => <If when={profiles().length > 0}><Action testID="add-profile" label={tr("添加订阅", "Add profile")} primary disabled={mutationBusy() || !!editing()} onPress={() => adding.set(true)} /></If>} />
    <If when={!!notice()}><Label text={notice()} muted /></If><If when={adding() || !profiles().length}><ImportForm /></If><If when={!!editing()}><ProfileEditor /></If>
    <If when={!!removing()}><Panel><Label text={tr(`删除“${removing()?.name}”？`, `Delete “${removing()?.name}”?`)} bold /><Label text={tr("将删除本地订阅和缓存，此操作无法撤销。", "This removes the saved profile and cache and cannot be undone.")} muted size={12} /><View style={{ flexDirection: "row", gap: 8, justifyContent: "flex-end" }}><Action label={tr("取消", "Cancel")} onPress={() => removing.set(null)} /><Action testID="confirm-delete-profile" danger label={tr("删除订阅", "Delete profile")} onPress={() => void action("delete", async () => { await call("profiles.delete", { id: removing()!.id }); removing.set(null); })} /></View></Panel></If>
    <Each of={[...profiles()].sort((a, b) => Number(b.active) - Number(a.active))} key={p => p.id}>{p => <View style={{ padding: 16, borderRadius: 10, borderLeftWidth: p().active ? 3 : 0, borderColor: colors().accent, backgroundColor: colors().surface, gap: 12 }}>
      <Text style={{ color: colors().text, fontSize: 16, fontWeight: "600", numberOfLines: 2 }}>{p().name}</Text><Text style={{ color: colors().muted, fontSize: 12, numberOfLines: 2, textOverflow: "ellipsis-middle" }}>{p().kind === "file" || p().kind === "local" ? tr("本地配置", "Local profile") : p().source}</Text>
      <If when={!!p().reconnectRequired}><Label text={tr("已保存 · 重新连接后生效", "Saved · reconnect to apply")} size={12} muted /></If>
      <View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap" }}><Action testID={`activate-${p().id}`} label={p().active ? status()?.running ? tr("使用中", "Active") : tr("已选择", "Selected") : tr("启用", "Activate")} primary={!p().active} disabled={mutationBusy() || p().active || !!status()?.tun} onPress={() => activate(p().id)} />
        <If when={p().kind === "url"}><Action testID={`refresh-${p().id}`} label={tasks()[`refresh-${p().id}`] ? tr("正在更新…", "Refreshing…") : tr("更新", "Refresh")} onPress={() => void action(`refresh-${p().id}`, async () => { await call("profiles.refresh", { id: p().id }); })} /></If>
        <Action testID={`edit-${p().id}`} label={tr("编辑", "Edit")} disabled={mutationBusy() || !!editing()} onPress={() => void beginEdit(p().id)} /><Action testID={`delete-${p().id}`} danger label={tr("删除", "Delete")} disabled={mutationBusy() || !!editing() || (p().active && !!status()?.running)} onPress={() => removing.set(p())} />
      </View>
    </View>}</Each>
  </Scroll>;
}
function Proxies() {
  return <View style={{ flex: 1, padding: 16, gap: 12 }}>
    <Heading title={tr("节点", "Nodes")} description={status()?.running ? tr("选择策略组和节点，测速不会改变出口。", "Choose a group and node. Testing keeps your current selection.") : tr("未连接 · 浏览已保存的节点，连接后可切换和测速。", "Disconnected · Browse saved nodes. Connect to switch or test.")} />
    <If when={!status()?.activeProfile}><Panel><Empty title={tr("尚未选择配置", "No profile selected")} description={tr("导入并选择订阅后，无需连接即可查看节点。", "Import and select a profile to browse nodes without connecting.")} action={() => <Action label={tr("查看订阅", "Open profiles")} primary onPress={() => navigate("profiles")} />} /></Panel></If>
    <If when={!!status()?.activeProfile && !visibleGroups().length}><Panel><Empty title={tr("没有策略组", "No proxy groups")} description={tr("当前配置没有可显示的策略组。", "This profile has no visible proxy groups.")} /></Panel></If>
    <If when={!status()?.running && groupsIncomplete()}><Label text={tr("部分提供器节点尚无可用本地预览，连接后加载完整列表。", "Some provider nodes have no local preview. Connect to load the full list.")} muted size={12} /></If>
    <If when={visibleGroups().length > 0}>
      <Scroll horizontal style={{ flexShrink: 0, height: 48 }} contentStyle={{ flexDirection: "row", gap: 8 }}><Each of={visibleGroups()} key={g => g.name}>{g => <Action label={g().name} primary={selectedGroup()?.name === g().name} disabled={false} onPress={() => { groupName.set(g().name); query.set(""); }} />}</Each></Scroll>
      <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}><Input testID="node-search" value={query()} onChange={({ value }) => query.set(value)} placeholder={tr("搜索节点", "Search nodes")} autoCorrect={false} autoCapitalize="none" style={{ flex: 1, minHeight: 44, padding: 12, borderRadius: 8, borderWidth: 1, borderColor: colors().border, backgroundColor: colors().surface, color: colors().text, placeholderColor: colors().faint, fontSize: 14 }} /><Action testID="test-group" label={tasks()["delay-group"] ? tr("测速中…", "Testing…") : tr("测试本组", "Test group")} disabled={!status()?.running || !!busy()} onPress={() => void action("delay-group", async () => { const names = selectedGroup()?.members.map(m => m.name) ?? []; for (const n of names) { if (!status()?.running) break; await testMember(n); } })} /></View>
      <List testID="node-list" each={members()} keyExtractor={item => item.name} style={{ flex: 1, borderRadius: 10, backgroundColor: colors().surface }} empty={<Empty title={!query().trim() && !status()?.running && groupsIncomplete() ? tr("节点等待加载", "Nodes not loaded yet") : tr("没有匹配的节点", "No matching nodes")} description={!query().trim() && !status()?.running && groupsIncomplete() ? tr("连接后可加载此策略组的提供器节点。", "Connect to load provider nodes for this group.") : tr("换个名称，或清除搜索条件。", "Try another name or clear the search.")} action={() => <If when={!!query().trim()}><Action label={tr("重置筛选", "Reset filter")} disabled={false} onPress={() => query.set("")} /></If>} />}>
        {m => <View style={{ flexDirection: "row", minHeight: 68, alignItems: "center", gap: 8, borderBottomWidth: 1, borderColor: colors().border, borderLeftWidth: 3, borderLeftColor: selectedGroup()?.now === m().name ? colors().accent : "transparent", backgroundColor: selectedGroup()?.now === m().name ? colors().selected : colors().surface, paddingRight: 12 }}>
          <Button testID={`node-${m().name}`} selected={selectedGroup()?.now === m().name} disabled={!status()?.running || mutationBusy() || selectedGroup()?.type !== "Selector"} onPress={() => void action("select", async () => { await call("proxies.select", { group: selectedGroup()?.name, member: m().name }); })} style={{ flex: 1, flexDirection: "row", alignItems: "center", gap: 12, minHeight: 68, paddingHorizontal: 12 }} hoverStyle={{ backgroundColor: colors().hover }} focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }} disabledStyle={{ opacity: status()?.running ? 0.75 : 1 }}>
            <Icon source={selectedGroup()?.now === m().name ? checkedIcon : circleIcon} selected={selectedGroup()?.now === m().name} /><View style={{ flex: 1, gap: 4 }}><Text style={{ fontSize: 13, color: colors().text, fontWeight: selectedGroup()?.now === m().name ? "600" : "400", numberOfLines: 2 }}>{m().name}</Text><Label text={selectedGroup()?.now === m().name ? status()?.running ? tr("当前出口", "Selected") : tr("下次连接使用", "For next connection") : m().type} muted size={11} /></View>
          </Button><Action testID={`delay-${m().name}`} label={tasks()[`delay-${m().name}`] ? tr("测速中…", "Testing…") : status()?.running && m().delay !== undefined ? `${m().delay} ms` : tr("测速", "Test")} disabled={!status()?.running || !!busy()} onPress={() => void testMember(m().name)} />
        </View>}
      </List>
    </If>
  </View>;
}
function AppsPage() {
  const apps = signal<InstalledApp[]>([]), scope = signal<AppScope>({ mode: "all", packages: [], excludeSelf: true }), search = signal(""), saving = signal(false), ready = signal(false), scopeError = signal("");
  void Promise.all([androidCall<{ apps: InstalledApp[] }>("android.apps"), androidCall<AppScope>("android.scope.get")]).then(([list, value]) => { apps.set(list.apps); scope.set(value); ready.set(true); }).catch(e => scopeError.set(message(e)));
  const filtered = () => apps().filter(a => `${a.name} ${a.packageName}`.toLowerCase().includes(search().trim().toLowerCase()));
  const toggle = (pkg: string) => scope.set({ ...scope(), packages: scope().packages.includes(pkg) ? scope().packages.filter(p => p !== pkg) : [...scope().packages, pkg] });
  return <View style={{ flex: 1, padding: 16, gap: 12 }}><Heading title={tr("应用范围", "App scope")} description={tr("决定哪些应用参与 VPN；不改变应用的代理组。", "Choose which apps use VPN. This does not assign proxy groups.")} action={() => <Action label={tr("返回", "Back")} disabled={saving()} onPress={() => subpage.set("")} />} />
    <If when={!!scopeError()}><Label text={scopeError()} /></If><Label text={tr("断开 VPN 后修改。Gopurr 自身始终排除。", "Disconnect before changing. Gopurr itself is always excluded.")} muted size={12} />
    <View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap" }}><Each of={[{ id: "all" as const, zh: "所有应用", en: "All apps" }, { id: "include" as const, zh: "仅选中", en: "Selected only" }, { id: "exclude" as const, zh: "排除选中", en: "Exclude selected" }]}>{m => <Action label={tr(m().zh, m().en)} primary={scope().mode === m().id} disabled={!ready() || saving()} onPress={() => scope.set({ ...scope(), mode: m().id })} />}</Each></View>
    <If when={scope().mode !== "all"}><Field label={tr("搜索应用", "Search apps")} value={search()} onChange={value => search.set(value)} /><Label text={tr(`已选 ${scope().packages.length} 个应用`, `${scope().packages.length} apps selected`)} size={12} muted /><List each={filtered()} keyExtractor={a => a.packageName} style={{ flex: 1, borderRadius: 10, backgroundColor: colors().surface }} empty={<Empty title={tr("没有匹配的应用", "No matching apps")} description={tr("可选择系统可见且已安装的应用。", "Choose from installed apps visible to Gopurr.")} />}>{a => <Button testID={`scope-app-${a().packageName}`} selected={scope().packages.includes(a().packageName)} disabled={saving()} onPress={() => toggle(a().packageName)} style={{ flexDirection: "row", gap: 12, padding: 12, minHeight: 64, borderBottomWidth: 1, borderColor: colors().border, backgroundColor: scope().packages.includes(a().packageName) ? colors().selected : "transparent" }}><Icon source={scope().packages.includes(a().packageName) ? checkedIcon : circleIcon} selected={scope().packages.includes(a().packageName)} /><View style={{ flex: 1, gap: 4 }}><Label text={a().name} bold /><Label text={a().packageName} muted size={11} /></View></Button>}</List></If>
    <If when={scope().mode === "all"}><View style={{ flex: 1 }} /></If><Action testID="save-app-scope" primary label={saving() ? tr("正在保存…", "Saving…") : tr("保存应用范围", "Save app scope")} disabled={!ready() || saving() || connected() || connecting() || (scope().mode === "include" && !scope().packages.length)} onPress={() => { saving.set(true); scopeError.set(""); void androidCall<AppScope>("android.scope.set", { mode: scope().mode, packages: scope().packages }).then(() => subpage.set("")).catch(e => scopeError.set(message(e))).finally(() => saving.set(false)); }} />
  </View>;
}
function LogsPage() {
  const logs = signal<{ at: string; level: string; text: string }[]>([]), search = signal(""), logError = signal("");
  const reload = () => void call<{ logs: { at: string; level: string; text: string }[] }>("logs.list").then(r => logs.set(r.logs)).catch(e => logError.set(message(e)));
  reload();
  return <View style={{ flex: 1, padding: 16, gap: 12 }}><Heading title={tr("诊断日志", "Diagnostic logs")} description={tr("最近的内核日志，便于排查连接问题。", "Recent engine logs for connection troubleshooting.")} action={() => <Action label={tr("返回", "Back")} disabled={false} onPress={() => subpage.set("")} />} /><Field label={tr("搜索日志", "Search logs")} value={search()} onChange={value => search.set(value)} /><If when={!!logError()}><Label text={logError()} /></If><Action label={tr("刷新日志", "Refresh logs")} disabled={false} onPress={reload} /><List each={logs().filter(l => `${l.level} ${l.text}`.toLowerCase().includes(search().toLowerCase()))} keyExtractor={(l, i) => `${i}-${l.at}`} style={{ flex: 1, backgroundColor: colors().surface, borderRadius: 10 }} empty={<Empty title={tr("暂无日志", "No logs")} description={tr("连接运行后将在此显示日志。", "Logs appear here when the connection runs.")} />}>{l => <View selectable style={{ padding: 12, gap: 4, borderBottomWidth: 1, borderColor: colors().border }}><Label text={`${l().at}  ${l().level}`} muted size={11} /><Text style={{ fontSize: 12, fontFamily: "monospace", color: colors().text }}>{l().text}</Text></View>}</List></View>;
}
function NotificationPanel() {
  const settings = signal<NotificationSettings | null>(null);
  effect(() => {
    if (lifecycle() !== "active") return;
    let disposed = false;
    void androidCall<NotificationSettings>("android.notifications").then(value => { if (!disposed) settings.set(value); }).catch(value => { if (!disposed) error.set(message(value)); });
    return () => { disposed = true; };
  });
  const configure = (traffic: boolean) => void action("notification", async () => {
    settings.set(await androidCall<NotificationSettings>("android.notifications.configure", { traffic }));
  }, false);
  const permission = () => void action("notification", async () => {
    settings.set(await androidCall<NotificationSettings>("android.notifications.request"));
  }, false);
  const systemSettings = () => void action("notification", async () => { await androidCall("android.notifications.settings"); }, false);
  return <Panel>
    <Label text={tr("状态栏通知", "Status notification")} bold size={16} />
    <Label text={settings()?.enabled ? tr("已允许显示通知", "Notifications enabled") : tr("未允许显示通知", "Notifications disabled")} muted size={12} />
    <Label text={tr("VPN 连接时常驻，点击通知返回连接页，也可直接断开。", "Stays visible while VPN is connected. Tap to open Connect, or disconnect directly.")} muted size={12} />
    <View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap" }}>
      <Action testID="notification-traffic" label={tr("实时流量", "Live traffic")} primary={settings()?.traffic === true} disabled={!settings() || !!tasks().notification} onPress={() => configure(true)} />
      <Action testID="notification-state-only" label={tr("仅连接状态", "Connection only")} primary={settings()?.traffic === false} disabled={!settings() || !!tasks().notification} onPress={() => configure(false)} />
    </View>
    <Label text={tr("亮屏时每 2 秒更新速率，省电模式下每 10 秒更新；熄屏暂停，亮屏后恢复。", "Rates update every 2 seconds, or 10 seconds in battery saver. Updates pause with the screen off and resume when it wakes.")} muted size={12} />
    <View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap" }}>
      <If when={!!settings()?.canRequest}><Action testID="notification-permission" label={tr("允许通知", "Allow notifications")} disabled={!!tasks().notification} onPress={permission} /></If>
      <Action testID="notification-settings" label={tr("系统通知设置", "System notification settings")} disabled={!!tasks().notification} onPress={systemSettings} />
    </View>
  </Panel>;
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
function UpdatePanel() {
  const value = appUpdate;
  return <Panel>
    <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
      <Label text="Gopurr" bold size={16} />
      <Label text={tr(`当前版本 ${value()?.currentVersion ?? "—"}`, `Version ${value()?.currentVersion ?? "—"}`)} muted size={12} />
    </View>
    <Label text={tr("让连接简单一点。", "Connections, simplified.")} muted size={12} />
    <If when={!value()}><Label text={tr("此版本未提供应用内更新。", "This build does not provide in-app updates.")} muted size={12} /></If>
    <If when={!!value()}>
      <Label text={updateSummary(value()!)} size={13} />
      <If when={!!value()?.detail}><Label text={value()?.detail ?? ""} muted size={12} /></If>
      <If when={!!value()?.error}><Label text={value()?.error ?? ""} size={12} /></If>
      <If when={!!value()?.notes && value()?.phase === "available"}><View selectable><Text style={{ fontSize: 12, lineHeight: 18, color: colors().muted, numberOfLines: 8 }}>{value()?.notes ?? ""}</Text></View></If>
      <If when={value()?.phase === "checking" || value()?.phase === "downloading"}>
        <Progress testID="update-progress" value={value()?.phase === "checking" ? 0 : Math.min(100, ((value()?.downloaded ?? 0) / Math.max(1, value()?.total ?? 0)) * 100)} indeterminate={value()?.phase === "checking"} accessibilityLabel={tr("更新进度", "Update progress")}
          trackStyle={{ height: 5, borderRadius: 3, backgroundColor: colors().border, overflow: "hidden" }} indicatorStyle={{ height: 5, borderRadius: 3, backgroundColor: colors().accent }} />
      </If>
      <If when={value()?.phase === "downloading"}><Label text={`${bytes(value()?.downloaded ?? 0)} / ${(value()?.total ?? 0) > 0 ? bytes(value()?.total ?? 0) : tr("大小待确认", "Size unknown")}`} muted size={11} /></If>
      <View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap", justifyContent: "flex-end" }}>
        <If when={!value()?.supported}><Action label={tr("仅正式版可用", "Release builds only")} disabled onPress={() => {}} /></If>
        <If when={!!value()?.supported && (value()?.phase === "idle" || value()?.phase === "current" || value()?.phase === "failed" || value()?.phase === "installed")}>
          <Action testID="update-check" primary={value()?.phase !== "installed"} label={value()?.phase === "current" ? tr("重新检查", "Check again") : tr("检查更新", "Check for updates")} disabled={false} onPress={() => updateAction("check")} />
        </If>
        <If when={value()?.phase === "checking"}><Action label={tr("正在检查…", "Checking…")} disabled onPress={() => {}} /></If>
        <If when={value()?.phase === "available"}><Action testID="update-download" primary label={tr("下载更新", "Download update")} disabled={false} onPress={() => updateAction("download")} /></If>
        <If when={value()?.phase === "downloading"}><Action testID="update-cancel" label={tr("取消下载", "Cancel download")} disabled={false} onPress={() => updateAction("cancel")} /></If>
        <If when={value()?.phase === "ready"}><Action testID="update-install" primary label={value()?.canInstall ? tr("安装更新", "Install update") : tr("允许安装", "Allow install")} disabled={false} onPress={() => updateAction("install")} /></If>
        <If when={value()?.phase === "installing"}><Action label={tr("等待确认", "Waiting for confirmation")} disabled onPress={() => {}} /></If>
      </View>
    </If>
  </Panel>;
}

function Settings() {
  const changeAppearance = (value: "system" | "light" | "dark") => { appearance.set(value); setColorScheme(value); void Storage.setItem("appearance", value).catch(e => error.set(message(e))); };
  const changeLanguage = (value: "system" | "zh-Hans" | "en") => { language.set(value); void Storage.setItem("language", value).catch(e => error.set(message(e))); };
  return <Scroll testID="settings-scroll" style={{ flex: 1 }} contentStyle={{ padding: 16, gap: 16 }}><Heading title={tr("设置", "Settings")} description={tr("外观与语言立即生效。", "Appearance and language changes apply immediately.")} />
    <If when={androidHost()}><Panel><Label text="VPN" bold size={16} /><Label text={tr("选择参与连接的应用。", "Choose which apps use VPN.")} muted size={12} /><Action testID="open-app-scope" label={tr("应用范围", "App scope")} disabled={false} onPress={() => subpage.set("apps")} /></Panel><NotificationPanel /></If>
    <Panel><Label text={tr("外观", "Appearance")} size={16} bold /><View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap" }}><Action label={tr("跟随系统", "System")} primary={appearance() === "system"} disabled={false} onPress={() => changeAppearance("system")} /><Action label={tr("浅色", "Light")} primary={appearance() === "light"} disabled={false} onPress={() => changeAppearance("light")} /><Action label={tr("深色", "Dark")} primary={appearance() === "dark"} disabled={false} onPress={() => changeAppearance("dark")} /></View></Panel>
    <Panel><Label text={tr("语言", "Language")} size={16} bold /><View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap" }}><Action label={tr("跟随系统", "System")} primary={language() === "system"} disabled={false} onPress={() => changeLanguage("system")} /><Action label="简体中文" primary={language() === "zh-Hans"} disabled={false} onPress={() => changeLanguage("zh-Hans")} /><Action label="English" primary={language() === "en"} disabled={false} onPress={() => changeLanguage("en")} /></View></Panel>
    <Panel><Label text={tr("诊断", "Diagnostics")} size={16} bold /><Action testID="open-logs" label={tr("查看日志", "View logs")} disabled={false} onPress={() => subpage.set("logs")} /><Label text={tr("查看最近的连接与内核事件。", "Review recent connection and engine events.")} muted size={12} /></Panel>
    <If when={androidHost()}><UpdatePanel /></If>
    <If when={!androidHost()}><Panel><Label text="Gopurr" size={16} bold /><Label text={tr("让连接简单一点。", "Connections, simplified.")} muted /></Panel></If>
  </Scroll>;
}
export function GopurrApp(props: { preview?: boolean } = {}) {
  void Promise.all([Storage.getItem("appearance"), Storage.getItem("language")]).then(([theme, lang]) => { if (theme === "system" || theme === "light" || theme === "dark") { appearance.set(theme); setColorScheme(theme); } if (lang === "system" || lang === "zh-Hans" || lang === "en") language.set(lang); }).catch(e => error.set(message(e)));
  startMobileStore();
  if (androidHost()) cleanup(host.on("android.navigate", value => {
    const target = (value as unknown as { page?: string }).page;
    if (target === "overview" || target === "profiles" || target === "proxies" || target === "settings") {
      subpage.set(""); page.set(target);
    }
  }));
  effect(() => {
    const scheme = colorScheme();
    if (androidHost()) {
      // Window chrome is host-owned. Its appearance must follow the effective
      // theme, including system changes, without delaying VPN controls.
      void androidCall("android.appearance", { scheme }).catch(() => {});
    }
  });
  cleanup(() => clearTimeout(noticeTimer));
  // Plain view animation avoids a ShaderView/Vulkan surface on Android. It
  // ends after the page enters and never delays navigation or disables input.
  const pageProgress = signal(1);
  let pageFrame = 0, previousRoute = `${page()}-${subpage()}`;
  effect(() => {
    const route = `${page()}-${subpage()}`;
    const reduced = reduceMotion();
    if (route === previousRoute && !reduced) return;
    previousRoute = route;
    cancelAnimationFrame(pageFrame);
    if (reduced) { pageProgress.set(1); return; }
    pageProgress.set(0);
    const started = Date.now();
    const frame = () => {
      if (reduceMotion()) { pageProgress.set(1); return; }
      const t = Math.min(1, (Date.now() - started) / 180);
      pageProgress.set(1 - Math.pow(1 - t, 3));
      if (t < 1) pageFrame = requestAnimationFrame(frame);
    };
    pageFrame = requestAnimationFrame(frame);
  });
  cleanup(() => cancelAnimationFrame(pageFrame));
  cleanup(BackHandler.on("press", () => { if (subpage()) { subpage.set(""); return true; } if (adding() && profiles().length) { adding.set(false); return true; } if (page() !== "overview") { navigate("overview"); return true; } return false; }));
  return <View style={{ flex: 1, backgroundColor: colors().bg, paddingTop: "env(safe-area-inset-top)", paddingLeft: "env(safe-area-inset-left)", paddingRight: "env(safe-area-inset-right)", paddingBottom: "env(safe-area-inset-bottom)", flexDirection: compact() ? "column" : "row" }}>
    <If when={!compact()}><Navigation /></If><View style={{ flex: 1, minHeight: 0 }}>
      <If when={compact()}><View style={{ flexDirection: "row", gap: 8, alignItems: "center", paddingHorizontal: 16, paddingTop: 12, paddingBottom: 8 }}><Image src={logo} style={{ width: 26, height: 26, objectFit: "contain" }} /><Label text="Gopurr" size={16} bold /></View></If>
      <If when={!!props.preview}><View style={{ paddingHorizontal: 16, paddingVertical: 6, backgroundColor: colors().selected }}><Text style={{ fontSize: 11, color: colors().accent }}>{tr("界面预览 · 演示数据，不连接真实代理", "UI preview · Fixture data, no live proxy")}</Text></View></If>
      <If when={!!error()}><View testID="error-banner" role="alert" style={{ margin: 16, marginBottom: 0, padding: 12, borderRadius: 8, borderWidth: 1, borderColor: colors().danger, backgroundColor: colors().surface, gap: 8 }}><Text style={{ color: colors().danger, fontSize: 12 }}>{error()}</Text><View style={{ flexDirection: "row", gap: 8 }}><Action label={tr("重试", "Retry")} disabled={false} onPress={() => void refresh()} /><Action label={tr("关闭提示", "Dismiss")} disabled={false} onPress={() => error.set("")} /></View></View></If>
      <View style={{ flex: 1, minHeight: 0, opacity: pageProgress(), transform: [{ translateY: 4 * (1 - pageProgress()) }] }}>
        <If when={!subpage() && page() === "overview"}><Overview /></If><If when={!subpage() && page() === "profiles"}><Profiles /></If><If when={!subpage() && page() === "proxies"}><Proxies /></If><If when={!subpage() && page() === "settings"}><Settings /></If><If when={subpage() === "apps"}><AppsPage /></If><If when={subpage() === "logs"}><LogsPage /></If>
      </View>
    </View><If when={compact() && !subpage()}><Navigation /></If>
  </View>;
}
