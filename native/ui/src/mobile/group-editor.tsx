import { cleanup, List, Scroll, signal, Text, View } from "nsh";
import { BackHandler } from "nsh/mobile";
import { Button } from "nsh/base";
import type { ProxyEditorGroup, ProxyEditorNode } from "../client";
import { tr } from "../i18n";
import { colors } from "../theme";
import { haptic } from "./platform";
import {
  groups as runtimeGroups, loadProxyEditor, message, openSubpage, popSubpage,
  proxyEditorData, proxyEditorLoading, saveProxyEdits, showToast, status,
} from "./store";
import type { Subpage } from "./store";
import {
  buildDeleteGroupEdits, buildResetGroupEdits, buildSaveGroupEdits,
  protocolLabel, wouldCreateGroupCycle,
} from "./proxy-helpers";
import { openNodeEditor } from "./node-editor";
import {
  Action, BottomSheet, confirmDialog, Empty, Field, Group, GroupRow, groupTypeLabel,
  Icon, IconButton, Label, MobileCheckbox, Notice, RADIUS, SearchField, SectionLabel,
  Segmented, SubpageHeader, Tag,
} from "./ui";

const GROUP_TYPES = [
  { id: "select", zh: "手动", en: "Manual" },
  { id: "url-test", zh: "自动测速", en: "Auto" },
  { id: "fallback", zh: "故障转移", en: "Fallback" },
  { id: "load-balance", zh: "负载均衡", en: "Balance" },
];

// Module-level state for GroupsManager & GroupEditor (survives theme switches)
const managerProfileId = signal("");
const managerTab = signal<"groups" | "nodes">("groups");
const managerSearch = signal("");

const editingGroup = signal<ProxyEditorGroup | null>(null);
const groupProfileId = signal("");
const gName = signal("");
const gType = signal("select");
const gExplicitMode = signal(false);
const gInherited = signal<string[]>([]);
const gAppendMembers = signal<string[]>([]);
const gMembers = signal<string[]>([]);
const gInitial = signal("");
const gSaving = signal(false);
const gError = signal("");
const inheritedExpanded = signal(false);

const memberPickerOpen = signal(false);
const memberPickerSearch = signal("");
const memberPickerProto = signal("all");

function serializeGroupDraft(): string {
  return JSON.stringify({
    name: gName(),
    type: gType(),
    explicit: gExplicitMode(),
    append: gAppendMembers(),
    members: gMembers(),
  });
}

export function groupEditorIsDirty(): boolean {
  return serializeGroupDraft() !== gInitial();
}

export async function openGroupsManager(options: {
  profileId?: string;
  initialTab?: "groups" | "nodes";
} = {}): Promise<void> {
  const targetId = options.profileId || status()?.activeProfile || "";
  managerProfileId.set(targetId);
  managerTab.set(options.initialTab ?? "groups");
  managerSearch.set("");
  openSubpage("groups-manager");
  if (targetId) {
    try { await loadProxyEditor(targetId); }
    catch (e) { showToast(message(e)); }
  }
}

export async function openGroupEditor(options: {
  group?: ProxyEditorGroup | null;
  profileId?: string;
  returnTo?: Subpage;
} = {}): Promise<void> {
  const targetId = options.profileId || managerProfileId() || status()?.activeProfile || "";
  groupProfileId.set(targetId);
  gError.set("");
  inheritedExpanded.set(false);
  memberPickerSearch.set("");
  memberPickerProto.set("all");

  if (targetId && (!proxyEditorData() || (proxyEditorData()!.profileId || proxyEditorData()!.id) !== targetId)) {
    try { await loadProxyEditor(targetId); }
    catch (e) { gError.set(message(e)); }
  }

  const editor = proxyEditorData();
  const group = options.group ?? null;
  editingGroup.set(group);
  if (group) {
    const isSubGroup = !!group.source && !editor?.isPureLocal;
    const explicit = !isSubGroup || group.explicitMembers || group.explicit;
    gName.set(group.name);
    gType.set(group.type || "select");
    gExplicitMode.set(explicit);
    gInherited.set([...(group.inheritedMembers ?? [])]);
    gAppendMembers.set([...(group.appendMembers ?? [])]);
    gMembers.set([...(group.members ?? [])]);
  } else {
    gName.set("");
    gType.set("select");
    gExplicitMode.set(true); // New local group always uses explicit member list
    gInherited.set([]);
    gAppendMembers.set([]);
    gMembers.set(["DIRECT"]);
  }
  gInitial.set(serializeGroupDraft());
  openSubpage("group-editor", options.returnTo ?? "groups-manager");
}

export function GroupsManagerPage() {
  const editor = () => proxyEditorData();
  const isInactiveProfile = () => {
    const id = managerProfileId();
    return !!id && id !== status()?.activeProfile;
  };
  const visibleGroups = () => (editor()?.groups ?? []).filter(g => !g.deleted && !g.missing);
  const filteredGroups = () => {
    const q = managerSearch().trim().toLowerCase();
    return visibleGroups().filter(g => !q || g.name.toLowerCase().includes(q));
  };
  const filteredNodes = () => {
    const q = managerSearch().trim().toLowerCase();
    return (editor()?.nodes ?? []).filter(n => !q || `${n.name} ${n.type} ${n.server}`.toLowerCase().includes(q));
  };

  const liveNow = (name: string) => {
    if (isInactiveProfile()) return "";
    return runtimeGroups().find(g => g.name === name)?.now ?? "";
  };

  cleanup(BackHandler.on("press", () => {
    popSubpage();
    return true;
  }));

  const headerTitle = () => isInactiveProfile()
    ? (editor()?.profileName || tr("配置节点与策略组", "Profile nodes & groups"))
    : tr("策略组管理", "Policy groups");

  return <View testID="groups-manager-page" style={{ flex: 1, backgroundColor: colors().bg }}>
    <SubpageHeader
      title={headerTitle()}
      subtitle={isInactiveProfile() ? tr("正在编辑非活跃配置（不影响当前连接）", "Editing inactive profile (active VPN unaffected)") : editor()?.profileName}
      backTestID="groups-manager-back"
      onBack={() => popSubpage()}
      action={() => <Action
        testID={managerTab() === "groups" ? "groups-add-new" : "manager-add-node"}
        size="compact"
        primary
        icon="plus"
        label={managerTab() === "groups" ? tr("新建", "New") : tr("新增节点", "Add node")}
        disabled={proxyEditorLoading()}
        onPress={() => {
          if (managerTab() === "groups") {
            void openGroupEditor({ group: null, profileId: managerProfileId(), returnTo: "groups-manager" });
          } else {
            void openNodeEditor({ profileId: managerProfileId(), returnTo: "groups-manager" });
          }
        }} />} />

    <View style={{ flex: 1, padding: 16, gap: 12 }}>
      <If when={isInactiveProfile()}>
        <Segmented
          testIDPrefix="manager-tab"
          value={managerTab()}
          onChange={id => managerTab.set(id as "groups" | "nodes")}
          items={[
            { id: "nodes", label: tr(`节点 (${editor()?.nodes.length ?? 0})`, `Nodes (${editor()?.nodes.length ?? 0})`) },
            { id: "groups", label: tr(`策略组 (${visibleGroups().length})`, `Groups (${visibleGroups().length})`) },
          ]} />
      </If>

      <SearchField
        testID="groups-manager-search"
        value={managerSearch()}
        onChange={v => managerSearch.set(v)}
        placeholder={managerTab() === "groups" ? tr("搜索策略组", "Search policy groups") : tr("搜索节点", "Search nodes")} />

      <If when={managerTab() === "groups"}>
        <Scroll style={{ flex: 1 }} contentStyle={{ gap: 12, paddingBottom: 16 }}>
          <If when={!filteredGroups().length}>
            <Empty
              icon="layers"
              title={tr("暂无策略组", "No policy groups")}
              description={tr("点击右上角「新建」创建策略组。", "Tap New to create a policy group.")} />
          </If>
          <If when={filteredGroups().length > 0}>
            <Group>
              <Each of={filteredGroups()} key={g => g.source || g.name}>{g => {
                const now = () => liveNow(g().name);
                const sourceTag = () => !g().source
                  ? tr("自建", "Local")
                  : g().edited && !editor()?.isPureLocal
                    ? tr("已修改", "Edited")
                    : "";
                const subParts = () => [
                  `${g().members.length} ${tr("成员", "members")}`,
                  now() ? `${tr("当前", "Now")}: ${now()}` : "",
                  g().appendMembers.length > 0 && !g().explicitMembers ? tr(`+${g().appendMembers.length} 追加`, `+${g().appendMembers.length} appended`) : "",
                ].filter(Boolean).join(" · ");
                return <GroupRow
                  testID={`manage-group-${g().name}`}
                  icon="layers"
                  title={g().name}
                  subtitle={subParts()}
                  trailing={() => <View style={{ flexDirection: "row", alignItems: "center", gap: 6 }}>
                    <If when={!!sourceTag()}><Tag text={sourceTag()} tone={!g().source ? "accent" : "warn"} /></If>
                    <Tag text={groupTypeLabel(g().type)} tone="muted" />
                  </View>}
                  chevron
                  onPress={() => void openGroupEditor({ group: g(), profileId: managerProfileId(), returnTo: "groups-manager" })} />;
              }}</Each>
            </Group>
          </If>
        </Scroll>
      </If>

      <If when={managerTab() === "nodes"}>
        <List
          testID="manager-nodes-list"
          each={filteredNodes()}
          keyExtractor={(n: ProxyEditorNode) => n.localId || n.source || n.name}
          style={{ flex: 1, borderRadius: RADIUS.surface, backgroundColor: colors().surface, borderWidth: 1, borderColor: colors().border }}
          empty={<Empty icon="server" title={tr("暂无节点", "No nodes")} description={tr("点击右上角「新增节点」添加。", "Tap Add node to create one.")} />}>
          {n => {
            const badge = () => !n().source
              ? tr("自建", "Local")
              : n().missing
                ? tr("来源缺失", "Missing")
                : n().edited && !editor()?.isPureLocal
                  ? tr("已修改", "Edited")
                  : "";
            return <GroupRow
              testID={`manage-node-${n().name}`}
              title={n().name}
              subtitle={[badge(), protocolLabel(n().type), n().server ? `${n().server}:${n().port}` : ""].filter(Boolean).join(" · ")}
              chevron
              onPress={() => void openNodeEditor({ node: n(), profileId: managerProfileId(), returnTo: "groups-manager" })} />;
          }}
        </List>
      </If>
    </View>
  </View>;
}

export function GroupEditorPage() {
  const editor = () => proxyEditorData();
  const group = () => editingGroup();
  const isNew = () => !group();
  const isSubGroup = () => !!group()?.source && !editor()?.isPureLocal;
  const canReset = () => isSubGroup() && !!group()?.edited;
  const canDelete = () => !!group();

  const currentList = () => gExplicitMode() ? gMembers() : gAppendMembers();
  const setCurrentList = (next: string[]) => {
    if (gExplicitMode()) gMembers.set(next);
    else gAppendMembers.set(next);
    if (gError()) gError.set("");
  };

  const moveItem = (index: number, delta: -1 | 1) => {
    const list = [...currentList()];
    const target = index + delta;
    if (target < 0 || target >= list.length) return;
    haptic("selection");
    [list[index], list[target]] = [list[target], list[index]];
    setCurrentList(list);
  };

  const removeItem = (name: string) => {
    haptic("selection");
    setCurrentList(currentList().filter(m => m !== name));
  };

  const switchToExplicit = () => {
    haptic("selection");
    const merged: string[] = [];
    for (const m of [...gInherited(), ...gAppendMembers()]) {
      if (!merged.includes(m)) merged.push(m);
    }
    gMembers.set(merged);
    gExplicitMode.set(true);
  };

  const switchToAppendMode = () => {
    haptic("selection");
    const inheritedSet = new Set(gInherited());
    const extra = gMembers().filter(m => !inheritedSet.has(m));
    gAppendMembers.set(extra);
    gExplicitMode.set(false);
  };

  // Candidate members for picker (with cycle prevention & inherited dedup)
  const allCandidateItems = () => {
    const ed = editor();
    if (!ed) return [];
    const curGroupName = gName().trim() || group()?.name || "";
    const inheritedSet = new Set(gExplicitMode() ? [] : gInherited());
    const nodeTypeMap = new Map<string, string>();
    for (const n of ed.nodes) nodeTypeMap.set(n.name, protocolLabel(n.type));
    const groupTypeMap = new Map<string, string>();
    for (const g of ed.groups) if (!g.deleted && !g.missing) groupTypeMap.set(g.name, groupTypeLabel(g.type));

    const out: { name: string; kind: string; proto: string; disabled: boolean; reason?: string }[] = [];
    for (const name of ed.availableNames ?? ed.options ?? []) {
      if (inheritedSet.has(name)) continue;
      const isGroup = groupTypeMap.has(name);
      const createsCycle = isGroup && wouldCreateGroupCycle(ed.groups, curGroupName, name);
      const proto = name === "DIRECT" || name === "REJECT" ? tr("内置", "Built-in") : (nodeTypeMap.get(name) ?? groupTypeMap.get(name) ?? tr("节点", "Node"));
      out.push({
        name,
        kind: isGroup ? tr("策略组", "Group") : proto,
        proto: isGroup ? "group" : (nodeTypeMap.get(name) ?? "builtin").toLowerCase(),
        disabled: createsCycle,
        reason: createsCycle ? tr("会产生循环引用", "Would cause a circular reference") : undefined,
      });
    }
    return out;
  };

  const pickerProtocols = () => {
    const set = new Set<string>();
    for (const item of allCandidateItems()) {
      if (item.proto !== "builtin" && item.proto !== "group") set.add(item.proto);
    }
    return ["all", ...Array.from(set).slice(0, 6)];
  };

  const filteredCandidates = () => {
    const q = memberPickerSearch().trim().toLowerCase();
    const p = memberPickerProto();
    return allCandidateItems().filter(item => {
      if (p !== "all" && item.proto !== p) return false;
      if (q && !`${item.name} ${item.kind}`.toLowerCase().includes(q)) return false;
      return true;
    });
  };

  const toggleCandidate = (name: string) => {
    const list = currentList();
    if (list.includes(name)) setCurrentList(list.filter(m => m !== name));
    else setCurrentList([...list, name]);
  };

  const handleBack = () => {
    if (gSaving()) return;
    if (!groupEditorIsDirty()) { popSubpage(); return; }
    void confirmDialog({
      title: tr("放弃未保存的更改？", "Discard unsaved changes?"),
      description: tr("当前策略组修改尚未保存，返回将丢失更改。", "Your policy group edits have not been saved."),
      confirmLabel: tr("放弃更改", "Discard"),
      cancelLabel: tr("继续编辑", "Keep editing"),
      destructive: true,
    }).then(discard => { if (discard) popSubpage(); });
  };

  cleanup(BackHandler.on("press", () => {
    if (gSaving()) return true;
    handleBack();
    return true;
  }));

  const handleSave = async () => {
    const ed = editor();
    if (!ed) return;
    const trimmed = gName().trim();
    if (!trimmed) {
      gError.set(tr("请填写策略组名称", "Policy group name is required"));
      haptic("reject");
      return;
    }
    const effectiveCount = gExplicitMode()
      ? gMembers().length
      : (gInherited().length + gAppendMembers().length + (group()?.use?.length ?? 0) + (group()?.includeAll ? 1 : 0));
    if (effectiveCount === 0) {
      gError.set(tr("策略组至少需要一个成员", "A policy group needs at least one member"));
      haptic("reject");
      return;
    }

    gSaving.set(true);
    gError.set("");
    try {
      const edits = buildSaveGroupEdits(ed, {
        editingGroup: group(),
        name: trimmed,
        type: gType(),
        explicitMode: gExplicitMode(),
        members: gMembers(),
        appendMembers: gAppendMembers(),
      });
      const res = await saveProxyEdits(ed.profileId || ed.id, edits);
      haptic("confirm");
      showToast(res.reconnectRequired ? tr("策略组已更新，重新连接后生效", "Policy group saved. Reconnect to apply.") : tr("策略组已保存", "Policy group saved"));
      gInitial.set(serializeGroupDraft());
      popSubpage();
    } catch (e) {
      haptic("reject");
      gError.set(message(e));
    } finally {
      gSaving.set(false);
    }
  };

  const handleReset = async () => {
    const ed = editor();
    const target = group();
    if (!ed || !target) return;
    const ok = await confirmDialog({
      title: tr(`恢复策略组「${target.name}」为订阅原值？`, `Reset group "${target.name}"?`),
      description: tr("将移除对该策略组的本地修改与追加成员。", "Removes local modifications and appended members for this group."),
      confirmLabel: tr("恢复原值", "Reset"),
      cancelLabel: tr("取消", "Cancel"),
      destructive: true,
    });
    if (!ok) return;
    gSaving.set(true);
    gError.set("");
    try {
      const edits = buildResetGroupEdits(ed, target);
      const res = await saveProxyEdits(ed.profileId || ed.id, edits);
      haptic("confirm");
      showToast(res.reconnectRequired ? tr("已恢复策略组原值，重连后生效", "Group reset. Reconnect to apply.") : tr("已恢复策略组原值", "Group reset"));
      gInitial.set(serializeGroupDraft());
      popSubpage();
    } catch (e) {
      haptic("reject");
      gError.set(message(e));
    } finally {
      gSaving.set(false);
    }
  };

  const handleDelete = async () => {
    const ed = editor();
    const target = group();
    if (!ed || !target) return;
    const ok = await confirmDialog({
      title: tr(`删除策略组「${target.name}」？`, `Delete group "${target.name}"?`),
      description: tr("若此策略组仍被规则或其他策略组引用，保存时将被安全拦截。", "Blocked safely if still referenced by rules or other groups."),
      confirmLabel: tr("删除策略组", "Delete group"),
      cancelLabel: tr("取消", "Cancel"),
      destructive: true,
    });
    if (!ok) return;
    gSaving.set(true);
    gError.set("");
    try {
      const edits = buildDeleteGroupEdits(ed, target);
      const res = await saveProxyEdits(ed.profileId || ed.id, edits);
      haptic("confirm");
      showToast(res.reconnectRequired ? tr("策略组已删除，重连后生效", "Group deleted. Reconnect to apply.") : tr("策略组已删除", "Group deleted"));
      gInitial.set(serializeGroupDraft());
      popSubpage();
    } catch (e) {
      haptic("reject");
      gError.set(message(e));
    } finally {
      gSaving.set(false);
    }
  };

  const renderMemberRow = (m: () => string, idx: () => number, listLen: () => number) => <View style={{
    flexDirection: "row", alignItems: "center", gap: 6, minHeight: 50,
    paddingLeft: 14, paddingRight: 4, backgroundColor: colors().surface,
  }}>
    <Text style={{ flex: 1, minWidth: 0, fontSize: 13.5, fontWeight: "500", color: colors().text, numberOfLines: 1, textOverflow: "ellipsis" }}>{m()}</Text>
    <IconButton
      icon="arrowMoveUp"
      size={16}
      label={tr("上移", "Move up")}
      disabled={gSaving() || idx() === 0}
      onPress={() => moveItem(idx(), -1)} />
    <IconButton
      icon="arrowMoveDown"
      size={16}
      label={tr("下移", "Move down")}
      disabled={gSaving() || idx() >= listLen() - 1}
      onPress={() => moveItem(idx(), 1)} />
    <IconButton
      icon="x"
      size={16}
      color={colors().danger}
      label={tr("移除", "Remove")}
      disabled={gSaving()}
      onPress={() => removeItem(m())} />
  </View>;

  return <View testID="group-editor-page" style={{ flex: 1, backgroundColor: colors().bg }}>
    <SubpageHeader
      title={isNew() ? tr("新建策略组", "New policy group") : tr("编辑策略组", "Edit policy group")}
      subtitle={group()?.name || editor()?.profileName || ""}
      backTestID="group-editor-back"
      backDisabled={gSaving()}
      onBack={handleBack} />

    <Scroll
      keyboardShouldPersistTaps="handled"
      keyboardDismissMode="on-drag"
      style={{ flex: 1 }}
      contentStyle={{ padding: 16, paddingBottom: 24, gap: 14 }}>

      <Field
        testID="group-editor-name"
        label={tr("策略组名称", "Group name")}
        value={gName()}
        onChange={v => { gName.set(v); if (gError()) gError.set(""); }}
        placeholder={tr("例如：自建精选", "e.g. Custom Select")}
        disabled={gSaving()} />

      <View style={{ gap: 6 }}>
        <Label text={tr("调度类型", "Type")} bold size={12} muted />
        <Segmented
          testIDPrefix="group-type"
          value={gType()}
          disabled={gSaving()}
          onChange={id => gType.set(id)}
          items={GROUP_TYPES.map(t => ({ id: t.id, label: tr(t.zh, t.en) }))} />
        <If when={gType() !== "select"}>
          <Label text={tr("测速参数使用默认配置（generate_204 · 300s）。", "Health check uses defaults (generate_204 · 300s).")} muted size={11.5} />
        </If>
      </View>

      {/* Mode A: AppendMembers (follow subscription updates) */}
      <If when={!gExplicitMode() && isSubGroup()}>
        <SectionLabel
          title={tr("成员模式 · 跟随订阅新增", "Members · Follow subscription")}
          trailing={tr(`${gInherited().length} 继承 + ${gAppendMembers().length} 追加`, `${gInherited().length} inherited + ${gAppendMembers().length} appended`)} />

        <Group>
          <GroupRow
            testID="group-inherited-toggle"
            icon="proxies"
            title={tr(`订阅继承成员 (${gInherited().length})`, `Inherited subscription members (${gInherited().length})`)}
            subtitle={tr("只读 · 订阅刷新时自动同步新增或下线节点", "Read-only · Automatically updates when subscription refreshes")}
            trailing={() => <Icon name={inheritedExpanded() ? "chevronDown" : "chevronRight"} size={16} color={colors().muted} />}
            onPress={() => { haptic("selection"); inheritedExpanded.set(!inheritedExpanded()); }} />
          <If when={inheritedExpanded()}>
            <View style={{ backgroundColor: colors().surface, paddingHorizontal: 16, paddingVertical: 10, gap: 6 }}>
              <Each of={gInherited()} key={m => m}>{m => (
                <Text style={{ fontSize: 12.5, color: colors().muted, numberOfLines: 1, textOverflow: "ellipsis" }}>{`· ${m()}`}</Text>
              )}</Each>
            </View>
          </If>
        </Group>

        <SectionLabel title={tr("本地追加成员", "Local appended members")} trailing={String(gAppendMembers().length)} />
        <If when={gAppendMembers().length > 0}>
          <Group>
            <Each of={gAppendMembers()} key={m => m}>{(m, idx) => renderMemberRow(m, idx, () => gAppendMembers().length)}</Each>
          </Group>
        </If>
        <If when={gAppendMembers().length === 0}>
          <Notice tone="info" text={tr("暂无本地追加成员，点击下方按钮追加自建或其他节点。", "No locally appended members yet. Tap below to append nodes.")} />
        </If>

        <View style={{ flexDirection: "row", gap: 10 }}>
          <View style={{ flex: 1 }}>
            <Action testID="group-append-member" full icon="plus" label={tr("追加成员", "Append members")} disabled={gSaving()} onPress={() => memberPickerOpen.set(true)} />
          </View>
          <View style={{ flex: 1 }}>
            <Action testID="group-switch-explicit" full quiet label={tr("转为自定义完整列表", "Switch to custom list")} disabled={gSaving()} onPress={switchToExplicit} />
          </View>
        </View>
      </If>

      {/* Mode B: Explicit Custom Member List */}
      <If when={gExplicitMode() || !isSubGroup()}>
        <SectionLabel
          title={tr("自定义完整成员列表", "Custom member list")}
          trailing={String(gMembers().length)} />
        <If when={gMembers().length > 0}>
          <Group>
            <Each of={gMembers()} key={m => m}>{(m, idx) => renderMemberRow(m, idx, () => gMembers().length)}</Each>
          </Group>
        </If>
        <If when={gMembers().length === 0}>
          <Notice tone="warn" text={tr("成员列表为空，请至少添加一个节点或内置策略。", "Member list is empty. Add at least one node or built-in policy.")} />
        </If>
        <View style={{ flexDirection: "row", gap: 10 }}>
          <View style={{ flex: 1 }}>
            <Action testID="group-add-member" full icon="plus" label={tr("添加成员…", "Add members…")} disabled={gSaving()} onPress={() => memberPickerOpen.set(true)} />
          </View>
          <If when={isSubGroup()}>
            <View style={{ flex: 1 }}>
              <Action testID="group-switch-append" full quiet label={tr("恢复跟随订阅新增", "Follow subscription")} disabled={gSaving()} onPress={switchToAppendMode} />
            </View>
          </If>
        </View>
      </If>

      <If when={!!gError()}><Notice testID="group-editor-error" text={gError()} tone="danger" /></If>

      <If when={canReset() || canDelete()}>
        <View style={{ flexDirection: "row", gap: 10, paddingTop: 4 }}>
          <If when={canReset()}>
            <View style={{ flex: 1 }}>
              <Action testID="group-editor-reset" full icon="rotateCcw" label={tr("恢复订阅原值", "Reset to sub")} disabled={gSaving()} onPress={() => void handleReset()} />
            </View>
          </If>
          <If when={canDelete()}>
            <View style={{ flex: 1 }}>
              <Action testID="group-editor-delete" full danger icon="trash" label={tr("删除策略组", "Delete group")} disabled={gSaving()} onPress={() => void handleDelete()} />
            </View>
          </If>
        </View>
      </If>
    </Scroll>

    <View style={{
      paddingHorizontal: 16, paddingVertical: 12,
      paddingBottom: "env(keyboard-inset-height)",
      backgroundColor: colors().surface,
      borderTopWidth: 1, borderColor: colors().border,
    }}>
      <Action
        testID="group-editor-save"
        primary
        full
        label={gSaving() ? tr("正在保存…", "Saving…") : tr("保存策略组", "Save policy group")}
        disabled={gSaving()}
        onPress={() => void handleSave()} />
    </View>

    {/* Virtualized Searchable Multi-Select Member Picker BottomSheet */}
    <BottomSheet
      testID="group-member-picker-sheet"
      open={memberPickerOpen()}
      onClose={() => memberPickerOpen.set(false)}
      title={gExplicitMode() ? tr("添加策略组成员", "Select members") : tr("追加成员到策略组", "Append members")}
      subtitle={tr(`已选 ${currentList().length} 项 · 自动阻止循环引用`, `${currentList().length} selected · circular references prevented`)}
      footer={() => <Action primary full label={tr("完成", "Done")} disabled={false} onPress={() => memberPickerOpen.set(false)} />}>
      <View style={{ gap: 10 }}>
        <SearchField
          testID="member-picker-search"
          value={memberPickerSearch()}
          onChange={v => memberPickerSearch.set(v)}
          placeholder={tr("搜索节点或策略组", "Search nodes or groups")} />
        <If when={pickerProtocols().length > 2}>
          <Scroll horizontal scrollbar={false} style={{ height: 36 }} contentStyle={{ flexDirection: "row", gap: 6, alignItems: "center" }}>
            <Each of={pickerProtocols()} key={p => p}>{p => {
              const active = () => memberPickerProto() === p();
              return <Button
                onPress={() => { haptic("selection"); memberPickerProto.set(p()); }}
                style={{
                  minHeight: 32, paddingHorizontal: 12, borderRadius: RADIUS.pill,
                  alignItems: "center", justifyContent: "center",
                  backgroundColor: active() ? colors().selected : colors().bg,
                  borderWidth: 1, borderColor: active() ? colors().selectedBorder : colors().border,
                }}>
                <Text style={{ fontSize: 12, fontWeight: active() ? "600" : "500", color: active() ? colors().accent : colors().muted }}>
                  {p() === "all" ? tr("全部", "All") : p().toUpperCase()}
                </Text>
              </Button>;
            }}</Each>
          </Scroll>
        </If>
        <List
          testID="member-picker-list"
          each={filteredCandidates()}
          keyExtractor={item => item.name}
          style={{ height: 300, borderRadius: RADIUS.control, backgroundColor: colors().bg, borderWidth: 1, borderColor: colors().border }}
          empty={<Empty icon="search" title={tr("没有可选成员", "No available members")} description={tr("调整筛选或清除搜索。", "Adjust filter or clear search.")} />}>
          {item => {
            const checked = () => currentList().includes(item().name);
            return <Button
              testID={`member-pick-${item().name}`}
              disabled={item().disabled}
              onPress={() => toggleCandidate(item().name)}
              style={{
                flexDirection: "row", alignItems: "center", gap: 10, minHeight: 50,
                paddingHorizontal: 12, paddingVertical: 8,
                borderBottomWidth: 1, borderColor: colors().border,
                backgroundColor: checked() ? colors().selected : colors().surface,
              }}
              disabledStyle={{ opacity: 0.45 }}>
              <MobileCheckbox checked={checked()} disabled={item().disabled} onChange={() => toggleCandidate(item().name)} />
              <View style={{ flex: 1, minWidth: 0, gap: 1 }}>
                <Text style={{ fontSize: 13.5, fontWeight: checked() ? "600" : "500", color: colors().text, numberOfLines: 1, textOverflow: "ellipsis" }}>{item().name}</Text>
                <Label text={item().reason ?? item().kind} muted size={11.5} lines={1} />
              </View>
            </Button>;
          }}
        </List>
      </View>
    </BottomSheet>
  </View>;
}
