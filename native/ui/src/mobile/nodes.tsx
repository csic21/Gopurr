import { cleanup, effect, List, Scroll, signal, Text, View } from "nsh";
import { BackHandler } from "nsh/mobile";
import { Button } from "nsh/base";
import { call } from "../client";
import type { Member, ParsedProxyNode, ParseFailure, ParseResult, ProfileMutation, ProxyEditorNode } from "../client";
import { tr } from "../i18n";
import { colors } from "../theme";
import { androidCall, androidHost, haptic } from "./platform";
import {
  acceptStatus, action, ALL_NODES_GROUP, busy, changeMode, groupName, groups,
  groupsIncomplete, loadProxyEditor, message, mutationBusy, navigate,
  nodeFilterLocalOnly, openSubpage, popSubpage, profiles, proxyEditorData,
  query, reconnectVpn, refreshLists, saveProxyEdits, selectedGroup, showToast,
  status, tasks, testNodeDelay, visibleGroups,
} from "./store";
import {
  buildAssignGroupsEdits, buildBatchAddNodesEdits, buildDeleteNodeEdits,
  buildResetNodeEdits, findDefaultTargetGroups, isBuiltinProxy,
  normalizeProtocolId, protocolLabel,
} from "./proxy-helpers";
import { NodeEditorBody, nodeEditorIsDirty, openNodeEditor } from "./node-editor";
import { openGroupsManager } from "./group-editor";
import {
  Action, BottomSheet, confirmDialog, delayTone, Empty, Field, Group, GroupRow,
  groupTypeLabel, Icon, IconButton, Label, MobileCheckbox, MoreButton, Notice,
  Panel, RADIUS, SearchField, SectionLabel, Segmented, SubpageHeader, Tag, tint,
  wideMasterDetail,
} from "./ui";

// Module-level UI state (survives appearance switches)
const modeSheetOpen = signal(false);
const groupPickerOpen = signal(false);
const filterSheetOpen = signal(false);
const addNodeSheetOpen = signal(false);
const pasteSheetOpen = signal(false);
const pasteText = signal("");
const pasteParsing = signal(false);
const pasteError = signal("");

const sortBy = signal<"default" | "delay" | "name">("default");
const filterAvailableOnly = signal(false);
const filterProtocol = signal("all");

const nodeActionTarget = signal<DisplayNode | null>(null);
const assignTargetNodes = signal<string[]>([]);
const assignSelectedGroups = signal<string[]>([]);
const assignSheetMode = signal<"set" | "add">("set");
const assignSheetOpen = signal(false);

const batchSelectMode = signal(false);
const batchSelectedNames = signal<string[]>([]);

// Concurrent delay test pool state
const batchProgress = signal<{ done: number; total: number } | null>(null);
let batchCancelToken = 0;

// Wide Master-Detail (>=904px) right pane state
const inspectedNodeName = signal("");
const inlineEditing = signal(false);

// Parse preview subpage state
const parsedNodes = signal<ParsedProxyNode[]>([]);
const parsedFailures = signal<ParseFailure[]>([]);
const parsedSelectedIndices = signal<number[]>([]);
const parsedTargetGroups = signal<string[]>([]);
const parsedFailuresExpanded = signal(false);
const parsedImporting = signal(false);
const parsedError = signal("");

interface DisplayNode {
  name: string;
  type: string;
  delay?: number;
  server?: string;
  port?: number;
  isLocal: boolean;
  isEdited: boolean;
  isMissing: boolean;
  isProvider: boolean;
  isBuiltin: boolean;
  isGroup: boolean;
  editorNode?: ProxyEditorNode;
}

function modeLabel(mode?: string): string {
  switch (mode) {
    case "global": return tr("全局", "Global");
    case "direct": return tr("直连", "Direct");
    default: return tr("规则", "Rule");
  }
}

export function openParsePreview(result: ParseResult): void {
  const nodes = result.nodes ?? [];
  parsedNodes.set(nodes);
  parsedFailures.set(result.failures ?? []);
  parsedSelectedIndices.set(nodes.map((_, idx) => idx));
  parsedTargetGroups.set(findDefaultTargetGroups(proxyEditorData(), selectedGroup()?.name));
  parsedFailuresExpanded.set(false);
  parsedError.set("");
  openSubpage("parse-preview");
}

function cancelBatchTest(): void {
  batchCancelToken++;
  batchProgress.set(null);
}

async function runConcurrentDelayPool(names: string[], concurrency = 6): Promise<void> {
  const filtered = names.filter(n => !isBuiltinProxy(n));
  if (!filtered.length) return;
  const token = ++batchCancelToken;
  batchProgress.set({ done: 0, total: filtered.length });
  let cursor = 0;
  let done = 0;

  const worker = async () => {
    while (cursor < filtered.length) {
      if (token !== batchCancelToken || !status()?.running) return;
      const name = filtered[cursor++];
      await testNodeDelay(name);
      if (token !== batchCancelToken) return;
      done++;
      batchProgress.set({ done, total: filtered.length });
    }
  };

  const workerCount = Math.min(concurrency, filtered.length);
  try {
    await Promise.all(Array.from({ length: workerCount }, () => worker()));
  } finally {
    if (token === batchCancelToken) {
      batchProgress.set(null);
    }
  }
}

export function Proxies() {
  // Load proxy editor metadata lazily for active profile (for host/source badges & editing)
  effect(() => {
    const activeId = status()?.activeProfile;
    if (!activeId) return;
    const loaded = proxyEditorData();
    if (!loaded || (loaded.profileId || loaded.id) !== activeId) {
      void loadProxyEditor(activeId).catch(() => {});
    }
  });

  // Cancel active batch test immediately if disconnected
  effect(() => {
    if (!status()?.running && batchProgress()) {
      cancelBatchTest();
    }
  });

  cleanup(() => {
    cancelBatchTest();
  });

  const editor = () => {
    const data = proxyEditorData();
    const activeId = status()?.activeProfile;
    if (!data || !activeId) return null;
    if ((data.profileId || data.id) !== activeId) return null;
    return data;
  };

  const editorNodeByName = () => {
    const map = new Map<string, ProxyEditorNode>();
    for (const n of editor()?.nodes ?? []) map.set(n.name, n);
    return map;
  };

  const providerNameSet = () => {
    const set = new Set<string>();
    for (const pn of editor()?.providerNodes ?? []) set.add(pn.name);
    return set;
  };

  const groupNameSet = () => new Set(groups().map(g => g.name));

  // Build unified delay lookup across all runtime groups
  const delayMap = () => {
    const map = new Map<string, Member>();
    for (const g of groups()) {
      for (const m of g.members) {
        const prev = map.get(m.name);
        if (!prev || (prev.delay === undefined && m.delay !== undefined)) {
          map.set(m.name, m);
        }
      }
    }
    return map;
  };

  // Virtual "All Nodes" list: combines editor nodes (including ungrouped dialer-proxy & missing orphans) + runtime/provider nodes
  const allNodesList = (): DisplayNode[] => {
    const ed = editor();
    const dMap = delayMap();
    const pSet = providerNameSet();
    const gSet = groupNameSet();
    const seen = new Set<string>();
    const out: DisplayNode[] = [];

    for (const n of ed?.nodes ?? []) {
      if (seen.has(n.name)) continue;
      seen.add(n.name);
      const rt = dMap.get(n.name);
      out.push({
        name: n.name,
        type: n.type || rt?.type || "Proxy",
        delay: rt?.delay,
        server: n.server,
        port: n.port,
        isLocal: !n.source,
        isEdited: !!n.source && n.edited && !ed?.isPureLocal,
        isMissing: !!n.missing,
        isProvider: false,
        isBuiltin: false,
        isGroup: false,
        editorNode: n,
      });
    }

    // Add provider & runtime nodes not in base proxies
    for (const [name, rt] of dMap.entries()) {
      if (seen.has(name) || isBuiltinProxy(name) || gSet.has(name)) continue;
      seen.add(name);
      out.push({
        name,
        type: rt.type,
        delay: rt.delay,
        isLocal: false,
        isEdited: false,
        isMissing: false,
        isProvider: pSet.has(name) || !!ed,
        isBuiltin: false,
        isGroup: false,
      });
    }
    return out;
  };

  const isAllNodesView = () => groupName() === ALL_NODES_GROUP || (status()?.mode === "direct") || (!selectedGroup() && !!status()?.activeProfile);
  const isManualGroup = () => !isAllNodesView() && selectedGroup()?.type === "Selector";

  const rawDisplayNodes = (): DisplayNode[] => {
    if (isAllNodesView()) return allNodesList();
    const group = selectedGroup();
    if (!group) return [];
    const ed = editor();
    const byName = editorNodeByName();
    const pSet = providerNameSet();
    const gSet = groupNameSet();

    return group.members.map(m => {
      const en = byName.get(m.name);
      const builtin = isBuiltinProxy(m.name);
      const isGrp = gSet.has(m.name);
      return {
        name: m.name,
        type: en?.type || m.type,
        delay: m.delay,
        server: en?.server,
        port: en?.port,
        isLocal: !!en && !en.source,
        isEdited: !!en && !!en.source && en.edited && !ed?.isPureLocal,
        isMissing: !!en?.missing,
        isProvider: !builtin && !isGrp && !en && (pSet.has(m.name) || !!ed),
        isBuiltin: builtin,
        isGroup: isGrp,
        editorNode: en,
      };
    });
  };

  const availableProtocols = () => {
    const set = new Set<string>();
    for (const item of rawDisplayNodes()) {
      if (!item.isBuiltin && !item.isGroup && item.type) {
        set.add(normalizeProtocolId(item.type));
      }
    }
    return ["all", ...Array.from(set)];
  };

  const hasActiveFilterOrSort = () =>
    sortBy() !== "default" || filterAvailableOnly() || nodeFilterLocalOnly() || filterProtocol() !== "all";

  const resetAllFilters = () => {
    query.set("");
    sortBy.set("default");
    filterAvailableOnly.set(false);
    nodeFilterLocalOnly.set(false);
    filterProtocol.set("all");
  };

  const filteredNodes = (): DisplayNode[] => {
    const q = query().trim().toLowerCase();
    const availOnly = filterAvailableOnly();
    const localOnly = nodeFilterLocalOnly();
    const proto = filterProtocol();

    const list = rawDisplayNodes().filter(item => {
      if (q) {
        const hay = `${item.name} ${item.type} ${protocolLabel(item.type)} ${item.server ?? ""}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      if (availOnly) {
        if (item.isBuiltin) return true;
        if (item.delay === undefined || item.delay <= 0) return false;
      }
      if (localOnly && !item.isLocal && !item.isEdited && !item.isMissing) return false;
      if (proto !== "all" && normalizeProtocolId(item.type) !== proto) return false;
      return true;
    });

    if (sortBy() === "name") {
      return [...list].sort((a, b) => a.name.localeCompare(b.name));
    }
    if (sortBy() === "delay") {
      const rank = (d: DisplayNode) => d.delay !== undefined && d.delay > 0 ? d.delay : Number.MAX_SAFE_INTEGER;
      return [...list].sort((a, b) => rank(a) - rank(b));
    }
    return list;
  };

  const toggleBatchTest = () => {
    if (batchProgress()) {
      haptic("impact");
      cancelBatchTest();
      return;
    }
    const testable = filteredNodes().filter(n => !n.isBuiltin).map(n => n.name);
    if (!testable.length) return;
    haptic("impact");
    void runConcurrentDelayPool(testable, 6);
  };

  const batchLabel = () => {
    const p = batchProgress();
    if (p) return tr(`停止 (${p.done}/${p.total})`, `Stop (${p.done}/${p.total})`);
    return isAllNodesView() ? tr("测试全部", "Test all") : tr("测试本组", "Test group");
  };

  const openAssignGroups = (names: string[], mode: "set" | "add") => {
    const ed = editor();
    if (!ed) return;
    assignTargetNodes.set(names);
    assignSheetMode.set(mode);
    if (mode === "set" && names.length === 1) {
      const n = names[0];
      assignSelectedGroups.set(ed.groups.filter(g => !g.deleted && !g.missing && g.members.includes(n)).map(g => g.name));
    } else {
      assignSelectedGroups.set(findDefaultTargetGroups(ed, selectedGroup()?.name));
    }
    assignSheetOpen.set(true);
  };

  const saveAssignGroups = async () => {
    const ed = editor();
    if (!ed) return;
    const ok = await action("assign-groups", async () => {
      const edits = buildAssignGroupsEdits(ed, assignTargetNodes(), assignSelectedGroups(), assignSheetMode());
      const res = await saveProxyEdits(ed.profileId || ed.id, edits);
      assignSheetOpen.set(false);
      haptic("confirm");
      showToast(res.reconnectRequired ? tr("策略组已更新，重新连接后生效", "Groups updated. Reconnect to apply.") : tr("已更新所属策略组", "Policy groups updated"));
    }, false);
    if (!ok) showToast(tr("保存策略组失败", "Failed to update policy groups"));
  };

  const handleEditNode = async (item: DisplayNode, duplicate = false) => {
    let ed = editor();
    if (!ed && status()?.activeProfile) {
      ed = await loadProxyEditor(status()!.activeProfile);
    }
    const target = item.editorNode ?? ed?.nodes.find(n => n.name === item.name) ?? null;
    if (!target) {
      showToast(tr("该节点来自外部节点集，暂不支持单项编辑", "External provider node cannot be edited individually"));
      return;
    }
    if (wideMasterDetail()) {
      if (inlineEditing() && nodeEditorIsDirty()) {
        const discard = await confirmDialog({
          title: tr("放弃未保存的更改？", "Discard unsaved changes?"),
          description: tr("当前编辑器有未保存修改。", "You have unsaved changes in the editor."),
          confirmLabel: tr("放弃更改", "Discard"),
          cancelLabel: tr("继续编辑", "Keep editing"),
          destructive: true,
        });
        if (!discard) return;
      }
      inspectedNodeName.set(item.name);
      await openNodeEditor({ node: target, duplicate, currentGroup: selectedGroup()?.name, inlineOnly: true });
      inlineEditing.set(true);
      return;
    }
    await openNodeEditor({ node: target, duplicate, currentGroup: selectedGroup()?.name });
  };

  const handleResetNode = async (item: DisplayNode) => {
    const ed = editor();
    if (!ed || !item.editorNode) return;
    const ok = await confirmDialog({
      title: tr(`恢复「${item.name}」为订阅原值？`, `Reset "${item.name}" to subscription defaults?`),
      description: tr("将移除本地覆写，保留订阅原始参数。", "Removes local overrides for this node."),
      confirmLabel: tr("恢复原值", "Reset"),
      cancelLabel: tr("取消", "Cancel"),
      destructive: true,
    });
    if (!ok) return;
    await action("reset-node", async () => {
      const edits = buildResetNodeEdits(ed, item.editorNode!);
      const res = await saveProxyEdits(ed.profileId || ed.id, edits);
      haptic("confirm");
      showToast(res.reconnectRequired ? tr("已恢复原值，重连后生效", "Node reset. Reconnect to apply.") : tr("已恢复订阅原值", "Reset to subscription"));
    }, false);
  };

  const handleDeleteNode = async (item: DisplayNode) => {
    const ed = editor();
    if (!ed || !item.editorNode) return;
    const ok = await confirmDialog({
      title: tr(`删除自建节点「${item.name}」？`, `Delete local node "${item.name}"?`),
      description: tr("将同时从相关策略组移除此节点，此操作无法撤销。", "Also removes references from policy groups. Cannot be undone."),
      confirmLabel: tr("删除节点", "Delete node"),
      cancelLabel: tr("取消", "Cancel"),
      destructive: true,
    });
    if (!ok) return;
    await action("delete-node", async () => {
      const edits = buildDeleteNodeEdits(ed, item.editorNode!);
      const res = await saveProxyEdits(ed.profileId || ed.id, edits);
      haptic("confirm");
      showToast(res.reconnectRequired ? tr("节点已删除，重连后生效", "Node deleted. Reconnect to apply.") : tr("节点已删除", "Node deleted"));
    }, false);
  };

  const handleBatchDelete = async () => {
    const ed = editor();
    if (!ed) return;
    const localTargets = batchSelectedNames()
      .map(name => ed.nodes.find(n => n.name === name))
      .filter((n): n is ProxyEditorNode => !!n && (!n.source || ed.isPureLocal));
    if (!localTargets.length) {
      showToast(tr("所选项中没有可删除的自建节点", "No deletable local nodes selected"));
      return;
    }
    const ok = await confirmDialog({
      title: tr(`删除 ${localTargets.length} 个自建节点？`, `Delete ${localTargets.length} local nodes?`),
      description: tr("订阅节点不会被删除；所选自建节点将从策略组中一并移除。", "Subscription nodes are kept; selected local nodes are removed from groups."),
      confirmLabel: tr("删除自建", "Delete local"),
      cancelLabel: tr("取消", "Cancel"),
      destructive: true,
    });
    if (!ok) return;
    await action("batch-delete", async () => {
      let currentEd = ed;
      let lastRes: ProfileMutation | null = null;
      for (const target of localTargets) {
        const edits = buildDeleteNodeEdits(currentEd, target);
        lastRes = await saveProxyEdits(currentEd.profileId || currentEd.id, edits);
        const refreshed = proxyEditorData();
        if (refreshed) currentEd = refreshed;
      }
      batchSelectedNames.set([]);
      batchSelectMode.set(false);
      haptic("confirm");
      showToast(lastRes?.reconnectRequired ? tr("已删除所选自建节点，重连后生效", "Deleted local nodes. Reconnect to apply.") : tr("已删除所选自建节点", "Deleted selected local nodes"));
    }, false);
  };

  const parseAndOpenPreview = async (raw: string) => {
    const trimmed = raw.trim();
    if (!trimmed) return;
    pasteParsing.set(true);
    pasteError.set("");
    try {
      const result = await call<ParseResult>("proxies.parse", { text: trimmed, mode: "nodes" });
      if (result.nodes && result.nodes.length > 0) {
        pasteSheetOpen.set(false);
        pasteText.set("");
        openParsePreview(result);
        return;
      }
      if (result.failures && result.failures.length > 0) {
        pasteError.set(tr(`解析失败：${result.failures[0].reason}`, `Parse failed: ${result.failures[0].reason}`));
        return;
      }
      pasteError.set(tr("未识别到有效的节点链接或 Clash 节点配置", "No valid proxy links or Clash nodes detected"));
    } catch (e) {
      pasteError.set(message(e));
    } finally {
      pasteParsing.set(false);
    }
  };

  const importNodesFromFile = async () => {
    addNodeSheetOpen.set(false);
    if (!androidHost()) {
      pasteSheetOpen.set(true);
      showToast(tr("请粘贴节点链接或 Clash YAML 配置", "Paste node links or Clash YAML"));
      return;
    }
    await action("pick-node-file", async () => {
      const file = await androidCall<{ name?: string; content?: string; cancelled?: boolean }>("android.file.pick");
      if (file.cancelled || !file.content) return;
      const result = await call<ParseResult>("proxies.parse", { text: file.content, mode: "nodes" });
      if (result.nodes && result.nodes.length > 0) {
        openParsePreview(result);
      } else {
        showToast(tr("文件中未识别到有效节点", "No valid nodes found in file"));
      }
    }, false);
  };

  const toggleBatchSelectItem = (name: string) => {
    haptic("selection");
    const cur = batchSelectedNames();
    batchSelectedNames.set(cur.includes(name) ? cur.filter(x => x !== name) : [...cur, name]);
  };

  const switchGroup = (nextName: string) => {
    if (batchProgress()) cancelBatchTest();
    groupName.set(nextName);
    query.set("");
    if (nextName !== ALL_NODES_GROUP) nodeFilterLocalOnly.set(false);
  };

  const rowSubtitle = (item: DisplayNode, isSelected: boolean): string => {
    const parts: string[] = [];
    if (isSelected) {
      parts.push(status()?.running ? tr("当前出口", "Selected") : tr("下次连接使用", "For next connection"));
    }
    if (item.isLocal) parts.push(tr("自建", "Local"));
    else if (item.isMissing) parts.push(tr("来源已缺失", "Source missing"));
    else if (item.isEdited) parts.push(tr("已修改", "Edited"));
    else if (item.isProvider) parts.push(tr("外部节点集", "Provider"));

    if (item.isBuiltin) {
      if (!isSelected) parts.push(item.type);
    } else if (item.isGroup) {
      parts.push(groupTypeLabel(item.type));
    } else {
      parts.push(protocolLabel(item.type));
      if (item.server) parts.push(item.server);
    }
    return parts.filter(Boolean).join(" · ");
  };

  const inspectedNode = () => {
    const name = inspectedNodeName();
    if (!name) return filteredNodes()[0] ?? null;
    return filteredNodes().find(n => n.name === name) ?? allNodesList().find(n => n.name === name) ?? null;
  };

  const primaryTestIsFilled = () => !(wideMasterDetail() && inlineEditing());

  const renderListColumn = () => <View style={{ flex: 1, minHeight: 0, gap: 10 }}>
    {/* Header: Nodes + Mode Pill + Policy Groups + Add Node */}
    <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", minHeight: 44, gap: 8 }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: 8, flex: 1, minWidth: 0 }}>
        <Text role="header" style={{ fontSize: 24, fontWeight: "600", letterSpacing: -0.5, color: colors().text, numberOfLines: 1 }}>{tr("节点", "Nodes")}</Text>
        <Button
          testID="nodes-mode-pill"
          accessibilityLabel={tr("切换路由模式", "Switch routing mode")}
          disabled={!status()?.running || mutationBusy()}
          onPress={() => { haptic("selection"); modeSheetOpen.set(true); }}
          style={{
            flexDirection: "row", alignItems: "center", gap: 4,
            minHeight: 34, paddingHorizontal: 10, borderRadius: RADIUS.pill,
            backgroundColor: colors().surface, borderWidth: 1, borderColor: colors().border,
          }}
          hitSlop={{ top: 5, bottom: 5, left: 4, right: 4 }}
          hoverStyle={{ backgroundColor: colors().hover }}
          activeStyle={{ opacity: 0.8 }}
          disabledStyle={{ opacity: 0.55 }}>
          <Text style={{ fontSize: 12.5, fontWeight: "600", color: colors().text, numberOfLines: 1 }}>{modeLabel(status()?.mode)}</Text>
          <Icon name="chevronDown" size={14} color={colors().muted} />
        </Button>
      </View>
      <View style={{ flexDirection: "row", alignItems: "center", gap: 2, flexShrink: 0 }}>
        <IconButton
          testID="nodes-groups-btn"
          icon="layers"
          label={tr("管理策略组", "Policy groups")}
          disabled={!status()?.activeProfile || mutationBusy()}
          onPress={() => { haptic("selection"); void openGroupsManager({ profileId: status()?.activeProfile }); }} />
        <IconButton
          testID="nodes-add-btn"
          icon="plus"
          color={colors().accent}
          label={tr("新增节点", "Add node")}
          disabled={mutationBusy()}
          onPress={() => { haptic("selection"); addNodeSheetOpen.set(true); }} />
      </View>
    </View>

    {/* Reconnect Required Banner (Android VPN Safety Iron Law) */}
    <If when={!!status()?.reconnectRequired}>
      <Notice
        testID="nodes-reconnect-banner"
        tone="warn"
        text={tr("节点配置已更新，重新连接后生效", "Node configuration updated. Reconnect to apply.")}
        action={() => <Action
          testID="nodes-reconnect-btn"
          size="compact"
          label={tr("重新连接", "Reconnect")}
          disabled={mutationBusy() || !!tasks().connection}
          onPress={() => void reconnectVpn()} />} />
    </If>

    {/* Empty state when no profile exists */}
    <If when={!status()?.activeProfile}>
      <Panel>
        <Empty
          icon="proxies"
          title={tr("尚未选择配置", "No profile selected")}
          description={tr("可直接新增自建节点（自动创建本地档案），或前往订阅页导入配置。", "Add a node directly to create a local profile, or import a subscription.")}
          action={() => <View style={{ flexDirection: "row", gap: 10, flexWrap: "wrap", justifyContent: "center" }}>
            <Action testID="empty-add-node" primary icon="plus" label={tr("新增节点", "Add node")} disabled={mutationBusy()} onPress={() => addNodeSheetOpen.set(true)} />
            <Action label={tr("查看订阅", "Open profiles")} disabled={false} onPress={() => navigate("profiles")} />
          </View>} />
      </Panel>
    </If>

    <If when={!!status()?.activeProfile}>
      {/* Group Pills + Virtual All Nodes + All Groups Trigger */}
      <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
        <Scroll horizontal scrollbar={false} style={{ flex: 1, height: 44 }} contentStyle={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
          <Each of={visibleGroups()} key={g => g.name}>{g => {
            const active = () => !isAllNodesView() && selectedGroup()?.name === g().name;
            return <Button
              testID={`group-${g().name}`}
              selected={active()}
              onPress={() => { if (!active()) haptic("selection"); switchGroup(g().name); }}
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
          <Button
            testID={`group-${ALL_NODES_GROUP}`}
            selected={isAllNodesView()}
            onPress={() => { if (!isAllNodesView()) haptic("selection"); switchGroup(ALL_NODES_GROUP); }}
            style={{
              minHeight: 40, paddingHorizontal: 14, borderRadius: RADIUS.pill,
              alignItems: "center", justifyContent: "center",
              backgroundColor: isAllNodesView() ? colors().selected : colors().surface,
              borderWidth: 1,
              borderColor: isAllNodesView() ? colors().selectedBorder : colors().border,
            }}
            hitSlop={{ top: 2, bottom: 2 }}
            hoverStyle={{ backgroundColor: isAllNodesView() ? colors().selected : colors().hover }}
            activeStyle={{ opacity: 0.78 }}
            focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}>
            <Text style={{ fontSize: 13, fontWeight: isAllNodesView() ? "600" : "500", color: isAllNodesView() ? colors().accent : colors().text, numberOfLines: 1 }}>
              {tr(`全部节点 (${allNodesList().length})`, `All nodes (${allNodesList().length})`)}
            </Text>
          </Button>
        </Scroll>
        <If when={visibleGroups().length >= 1}>
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

      {/* SearchField + Filter/Sort Button */}
      <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
        <View style={{ flex: 1, minWidth: 0 }}>
          <SearchField
            testID="node-search"
            value={query()}
            onChange={v => query.set(v)}
            placeholder={tr("搜索节点 / 协议 / 地址", "Search nodes")} />
        </View>
        <IconButton
          testID="nodes-filter-btn"
          icon="sliders"
          dot={hasActiveFilterOrSort()}
          color={hasActiveFilterOrSort() ? colors().accent : colors().muted}
          label={tr("筛选与排序", "Filter & sort")}
          onPress={() => { haptic("selection"); filterSheetOpen.set(true); }} />
      </View>

      {/* Current summary + Sole Filled Primary Button [测试本组] */}
      <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
        <View style={{ flex: 1, minWidth: 0, flexDirection: "row", alignItems: "center", gap: 6 }}>
          <Text style={{ flexShrink: 1, fontSize: 12, color: colors().muted, numberOfLines: 1, textOverflow: "ellipsis" }}>
            {isAllNodesView()
              ? tr(`全部节点 · ${filteredNodes().length} 项`, `All nodes · ${filteredNodes().length}`)
              : tr(`当前: ${selectedGroup()?.now || "—"} · ${filteredNodes().length} 项`, `Now: ${selectedGroup()?.now || "—"} · ${filteredNodes().length}`)}
          </Text>
          <If when={hasActiveFilterOrSort()}>
            <Button
              testID="nodes-clear-filter-chip"
              onPress={resetAllFilters}
              style={{ paddingHorizontal: 8, minHeight: 26, borderRadius: RADIUS.pill, backgroundColor: tint(colors().accent, "16"), alignItems: "center", justifyContent: "center" }}>
              <Text style={{ fontSize: 11, fontWeight: "600", color: colors().accent }}>{tr("清除筛选", "Clear")}</Text>
            </Button>
          </If>
        </View>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 6, flexShrink: 0 }}>
          <If when={batchSelectMode()}>
            <Action
              testID="nodes-batch-done"
              size="compact"
              quiet
              label={tr("取消多选", "Cancel")}
              disabled={false}
              onPress={() => { batchSelectMode.set(false); batchSelectedNames.set([]); }} />
          </If>
          <Action
            testID="test-group"
            primary={primaryTestIsFilled()}
            size="compact"
            icon={batchProgress() ? "stop" : "radar"}
            label={batchLabel()}
            disabled={!status()?.running || (!batchProgress() && (filteredNodes().filter(n => !n.isBuiltin).length === 0 || mutationBusy()))}
            onPress={toggleBatchTest} />
        </View>
      </View>

      {/* Notices */}
      <If when={status()?.mode === "direct"}>
        <Notice
          tone="info"
          text={tr("当前为直连模式 · 仍可在此浏览、新增和编辑所有节点。", "Direct mode active · You can still browse, add, and edit all nodes here.")} />
      </If>
      <If when={!status()?.running}>
        <Notice
          tone="info"
          text={tr("未连接 · 浏览已保存的节点，连接后可切换和测速。", "Disconnected · Browse saved nodes. Connect to switch or test.")}
          action={() => <Action size="compact" quiet label={tr("去连接", "Connect")} disabled={false} onPress={() => navigate("overview")} />} />
      </If>
      <If when={!status()?.running && groupsIncomplete()}>
        <Label text={tr("部分提供器节点尚无可用本地预览，连接后加载完整列表。", "Some provider nodes have no local preview. Connect to load the full list.")} muted size={12} />
      </If>
      <If when={!!status()?.running && !isAllNodesView() && !!selectedGroup() && !isManualGroup()}>
        <Notice
          tone="info"
          text={tr(`${groupTypeLabel(selectedGroup()!.type)} · 由系统自动调度出口，单节点测速仍可用。`, `${groupTypeLabel(selectedGroup()!.type)} · Outbound is managed automatically; individual testing stays available.`)} />
      </If>

      {/* Virtualized 60px Node List */}
      <List
        testID="node-list"
        each={filteredNodes()}
        keyExtractor={item => item.name}
        keyboardDismissMode="on-drag"
        style={{ flex: 1, borderRadius: RADIUS.surface, backgroundColor: colors().surface, borderWidth: 1, borderColor: colors().border }}
        empty={<Empty
          icon={query().trim() || hasActiveFilterOrSort() ? "search" : "proxies"}
          title={!query().trim() && !hasActiveFilterOrSort() && !status()?.running && groupsIncomplete() ? tr("节点等待加载", "Nodes not loaded yet") : tr("没有匹配的节点", "No matching nodes")}
          description={!query().trim() && !hasActiveFilterOrSort() && !status()?.running && groupsIncomplete() ? tr("连接后可加载此策略组的提供器节点。", "Connect to load provider nodes for this group.") : tr("调整搜索或筛选条件，或添加新节点。", "Try another name, reset filters, or add a node.")}
          action={() => <If when={!!query().trim() || hasActiveFilterOrSort()}><Action label={tr("重置筛选", "Reset filter")} disabled={false} onPress={resetAllFilters} /></If>} />}>
        {m => {
          const isSelected = () => !isAllNodesView() && selectedGroup()?.now === m().name;
          const isInspected = () => wideMasterDetail() && inspectedNode()?.name === m().name;
          const testing = () => !!tasks()[`delay-${m().name}`];
          const hasDelay = () => !!status()?.running && m().delay !== undefined;
          const checked = () => batchSelectedNames().includes(m().name);
          const canSelectRow = () => batchSelectMode() || wideMasterDetail() || (!!status()?.running && !mutationBusy() && isManualGroup());

          return <View style={{
            flexDirection: "row", minHeight: 60, alignItems: "center", gap: 2,
            borderBottomWidth: 1, borderColor: colors().border,
            borderLeftWidth: 3, borderLeftColor: isSelected() ? colors().accent : isInspected() ? colors().selectedBorder : "transparent",
            backgroundColor: isSelected() ? colors().selected : isInspected() ? colors().hover : colors().surface,
            paddingRight: 4,
          }}>
            <Button
              testID={`node-${m().name}`}
              selected={isSelected()}
              disabled={!canSelectRow()}
              onPress={() => {
                if (batchSelectMode()) {
                  toggleBatchSelectItem(m().name);
                  return;
                }
                if (wideMasterDetail()) {
                  inspectedNodeName.set(m().name);
                }
                if (status()?.running && !mutationBusy() && isManualGroup()) {
                  if (!isSelected()) haptic("selection");
                  void action("select", async () => { await call("proxies.select", { group: selectedGroup()?.name, member: m().name }); });
                }
              }}
              style={{ flex: 1, minWidth: 0, flexDirection: "row", alignItems: "center", gap: 10, minHeight: 60, paddingLeft: 12, paddingRight: 6, paddingVertical: 8 }}
              hoverStyle={{ backgroundColor: colors().hover }}
              focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}
              disabledStyle={{ opacity: status()?.running ? 0.85 : 1 }}>
              <If when={batchSelectMode()}>
                <MobileCheckbox checked={checked()} onChange={() => toggleBatchSelectItem(m().name)} />
              </If>
              <If when={!batchSelectMode() && !isAllNodesView()}>
                <Icon name={isSelected() ? "circleCheck" : "circle"} selected={isSelected()} size={18} />
              </If>
              <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
                <Text style={{ fontSize: 13.5, color: colors().text, fontWeight: isSelected() ? "600" : "500", numberOfLines: 2 }}>{m().name}</Text>
                <Label text={rowSubtitle(m(), isSelected())} muted size={11.5} lines={1} />
              </View>
            </Button>

            {/* Hide latency button on DIRECT / REJECT* built-in policies */}
            <If when={!m().isBuiltin && !batchSelectMode()}>
              <Button
                testID={`delay-${m().name}`}
                accessibilityLabel={tr(`测速 ${m().name}`, `Test ${m().name}`)}
                disabled={!status()?.running || (!!busy() && !batchProgress())}
                onPress={() => { haptic("selection"); void testNodeDelay(m().name); }}
                style={{ minWidth: 56, minHeight: 44, paddingHorizontal: 8, borderRadius: 8, alignItems: "center", justifyContent: "center", backgroundColor: "transparent" }}
                hoverStyle={{ backgroundColor: colors().hover }}
                activeStyle={{ backgroundColor: colors().selected, opacity: 0.78 }}
                focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}
                disabledStyle={{ opacity: 0.4 }}>
                <If when={testing()}>
                  <Text style={{ fontSize: 11.5, color: colors().muted }}>{tr("测速中…", "Testing…")}</Text>
                </If>
                <If when={!testing() && hasDelay()}>
                  <Tag text={m().delay! > 0 ? `${m().delay} ms` : tr("超时", "Timeout")} tone={delayTone(m().delay!)} />
                </If>
                <If when={!testing() && !hasDelay()}>
                  <Text style={{ fontSize: 12, fontWeight: "500", color: colors().muted }}>{tr("测速", "Test")}</Text>
                </If>
              </Button>
            </If>

            <If when={!batchSelectMode()}>
              <MoreButton
                testID={`node-more-${m().name}`}
                label={tr(`更多操作 ${m().name}`, `More actions for ${m().name}`)}
                disabled={mutationBusy()}
                onPress={() => { haptic("selection"); nodeActionTarget.set(m()); }} />
            </If>
          </View>;
        }}
      </List>

      {/* Batch Select Bottom Action Bar */}
      <If when={batchSelectMode()}>
        <View testID="nodes-batch-bar" style={{
          flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8,
          paddingHorizontal: 12, paddingVertical: 10, borderRadius: RADIUS.control,
          backgroundColor: colors().surface, borderWidth: 1, borderColor: colors().border,
        }}>
          <Label text={tr(`已选 ${batchSelectedNames().length}`, `${batchSelectedNames().length} selected`)} bold size={12.5} tabular />
          <View style={{ flexDirection: "row", alignItems: "center", gap: 6 }}>
            <Action
              size="compact"
              label={tr("加入组", "Add to group")}
              disabled={!batchSelectedNames().length || mutationBusy()}
              onPress={() => openAssignGroups(batchSelectedNames(), "add")} />
            <Action
              size="compact"
              label={tr("测速", "Test")}
              disabled={!batchSelectedNames().length || !status()?.running}
              onPress={() => void runConcurrentDelayPool(batchSelectedNames(), 6)} />
            <Action
              size="compact"
              danger
              quiet
              label={tr("删自建", "Delete")}
              disabled={!batchSelectedNames().length || mutationBusy()}
              onPress={() => void handleBatchDelete()} />
          </View>
        </View>
      </If>
    </If>
  </View>;

  // Right pane for wide >=904px Master-Detail (1060x720)
  const renderDetailPane = () => <View testID="nodes-detail-pane" style={{
    flex: 1, minWidth: 348, minHeight: 0,
    borderRadius: RADIUS.surface, backgroundColor: colors().surface,
    borderWidth: 1, borderColor: colors().border, overflow: "hidden",
  }}>
    <If when={inlineEditing()}>
      <View style={{ flex: 1, minHeight: 0 }}>
        <View style={{
          flexDirection: "row", alignItems: "center", justifyContent: "space-between",
          paddingHorizontal: 16, paddingVertical: 12, borderBottomWidth: 1, borderColor: colors().border,
        }}>
          <Label text={tr("节点编辑", "Node editor")} bold size={15} />
          <IconButton icon="x" label={tr("关闭编辑", "Close editor")} onPress={() => inlineEditing.set(false)} />
        </View>
        <NodeEditorBody inline onSaved={() => inlineEditing.set(false)} onClosed={() => inlineEditing.set(false)} />
      </View>
    </If>
    <If when={!inlineEditing()}>
      <Scroll style={{ flex: 1 }} contentStyle={{ padding: 20, gap: 14 }}>
        <If when={!inspectedNode()}>
          <Empty
            icon="server"
            title={tr("选择一个节点查看详情", "Select a node for details")}
            description={tr("点击左侧节点查看协议参数，或点击右上角 + 新增自建节点。", "Pick a node on the left to inspect or edit it, or tap + to add one.")}
            action={() => <Action icon="plus" label={tr("新增节点", "Add node")} disabled={mutationBusy()} onPress={() => addNodeSheetOpen.set(true)} />} />
        </If>
        <If when={!!inspectedNode()}>
          {(() => {
            const item = inspectedNode()!;
            return <View style={{ gap: 14 }}>
              <View style={{ flexDirection: "row", alignItems: "flex-start", justifyContent: "space-between", gap: 10 }}>
                <View style={{ flex: 1, minWidth: 0, gap: 4 }}>
                  <Text style={{ fontSize: 18, fontWeight: "600", color: colors().text, numberOfLines: 2 }}>{item.name}</Text>
                  <Label text={rowSubtitle(item, false)} muted size={12.5} />
                </View>
                <If when={item.isLocal}><Tag text={tr("自建", "Local")} tone="accent" /></If>
                <If when={item.isEdited}><Tag text={tr("已修改", "Edited")} tone="warn" /></If>
              </View>
              <Group>
                <GroupRow title={tr("协议类型", "Protocol")} value={protocolLabel(item.type)} />
                <If when={!!item.server}>
                  <GroupRow title={tr("服务器", "Server")} value={`${item.server}:${item.port ?? ""}`} />
                </If>
                <If when={!item.isBuiltin}>
                  <GroupRow
                    title={tr("连通延迟", "Latency")}
                    value={item.delay !== undefined ? (item.delay > 0 ? `${item.delay} ms` : tr("超时", "Timeout")) : tr("未测速", "Not tested")}
                    valueTone={item.delay !== undefined ? delayTone(item.delay) : "muted"} />
                </If>
              </Group>
              <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 8 }}>
                <If when={!item.isBuiltin && !item.isGroup && !item.isProvider}>
                  <Action icon="pencil" label={tr("编辑节点", "Edit node")} disabled={mutationBusy()} onPress={() => void handleEditNode(item)} />
                </If>
                <If when={!item.isBuiltin && !!status()?.running}>
                  <Action icon="radar" label={tr("测试延迟", "Test latency")} disabled={mutationBusy()} onPress={() => void testNodeDelay(item.name)} />
                </If>
                <If when={!item.isBuiltin && !item.isGroup}>
                  <Action icon="layers" label={tr("所属策略组", "Groups")} disabled={mutationBusy()} onPress={() => openAssignGroups([item.name], "set")} />
                </If>
                <If when={!item.isBuiltin && !item.isGroup && !item.isProvider}>
                  <Action icon="copy" label={tr("复制为新节点", "Duplicate")} disabled={mutationBusy()} onPress={() => void handleEditNode(item, true)} />
                </If>
              </View>
            </View>;
          })()}
        </If>
      </Scroll>
    </If>
  </View>;

  return <View style={{ flex: 1, minHeight: 0 }}>
    <View style={{ flex: 1, minHeight: 0, paddingHorizontal: 16, paddingTop: 12, paddingBottom: 12 }}>
      <If when={!wideMasterDetail()}>{renderListColumn()}</If>
      <If when={wideMasterDetail()}>
        <View style={{ flex: 1, minHeight: 0, flexDirection: "row", gap: 16 }}>
          <View style={{ width: 360, minHeight: 0, flexShrink: 0 }}>{renderListColumn()}</View>
          {renderDetailPane()}
        </View>
      </If>
    </View>

    {/* Routing Mode BottomSheet */}
    <BottomSheet
      testID="mode-sheet"
      open={modeSheetOpen()}
      onClose={() => modeSheetOpen.set(false)}
      title={tr("路由模式", "Routing mode")}>
      <Group>
        <GroupRow
          testID="nodes-mode-rule"
          icon="route"
          title={tr("规则模式", "Rule")}
          subtitle={tr("按订阅分流规则自动路由流量", "Route traffic according to profile rules")}
          trailing={() => <If when={(status()?.mode ?? "rule") === "rule"}><Tag text={tr("当前", "Current")} tone="accent" /></If>}
          onPress={() => { modeSheetOpen.set(false); void changeMode("rule"); }} />
        <GroupRow
          testID="nodes-mode-global"
          icon="proxies"
          title={tr("全局模式", "Global")}
          subtitle={tr("所有流量统一走 GLOBAL 出口", "Route all traffic through GLOBAL")}
          trailing={() => <If when={status()?.mode === "global"}><Tag text={tr("当前", "Current")} tone="accent" /></If>}
          onPress={() => { modeSheetOpen.set(false); void changeMode("global"); }} />
        <GroupRow
          testID="nodes-mode-direct"
          icon="arrowUp"
          title={tr("直连模式", "Direct")}
          subtitle={tr("所有流量直接连接，不经过代理节点", "Connect all traffic directly")}
          trailing={() => <If when={status()?.mode === "direct"}><Tag text={tr("当前", "Current")} tone="accent" /></If>}
          onPress={() => { modeSheetOpen.set(false); void changeMode("direct"); }} />
      </Group>
    </BottomSheet>

    {/* All Groups Picker BottomSheet (includes virtual All Nodes + Batch Select + Manage Groups) */}
    <BottomSheet
      testID="all-groups-sheet"
      open={groupPickerOpen()}
      onClose={() => groupPickerOpen.set(false)}
      title={tr("选择策略组", "Select group")}
      scrollable>
      <Group>
        <GroupRow
          testID="all-groups-virtual-all"
          icon="server"
          title={tr(`全部节点 (${allNodesList().length})`, `All nodes (${allNodesList().length})`)}
          subtitle={tr("查看所有订阅与自建节点，包含未入组跳板节点", "View every subscription & local node including ungrouped relays")}
          trailing={() => <If when={isAllNodesView()}><Tag text={tr("当前", "Current")} tone="accent" /></If>}
          chevron={!isAllNodesView()}
          onPress={() => { haptic("selection"); switchGroup(ALL_NODES_GROUP); groupPickerOpen.set(false); }} />
        <Each of={visibleGroups()} key={g => g.name}>{g => <GroupRow
          title={g().name}
          subtitle={`${groupTypeLabel(g().type)} · ${g().now || "—"}`}
          trailing={() => <If when={!isAllNodesView() && selectedGroup()?.name === g().name}><Tag text={tr("当前", "Current")} tone="accent" /></If>}
          chevron={isAllNodesView() || selectedGroup()?.name !== g().name}
          onPress={() => {
            if (isAllNodesView() || selectedGroup()?.name !== g().name) haptic("selection");
            switchGroup(g().name);
            groupPickerOpen.set(false);
          }} />}</Each>
      </Group>
      <Group>
        <GroupRow
          testID="nodes-batch-toggle"
          icon="check"
          title={batchSelectMode() ? tr("退出批量选择", "Exit batch select") : tr("批量选择节点", "Batch select nodes")}
          subtitle={tr("批量加入策略组、测速或删除自建节点", "Assign groups, test latency, or delete local nodes")}
          onPress={() => {
            groupPickerOpen.set(false);
            batchSelectMode.set(!batchSelectMode());
            batchSelectedNames.set([]);
          }} />
        <GroupRow
          icon="layers"
          title={tr("管理策略组", "Manage policy groups")}
          chevron
          onPress={() => {
            groupPickerOpen.set(false);
            void openGroupsManager({ profileId: status()?.activeProfile });
          }} />
      </Group>
    </BottomSheet>

    {/* Filter & Sort BottomSheet */}
    <BottomSheet
      testID="filter-sort-sheet"
      open={filterSheetOpen()}
      onClose={() => filterSheetOpen.set(false)}
      title={tr("筛选与排序", "Filter & sort")}
      scrollable
      footer={() => <View style={{ flexDirection: "row", gap: 10 }}>
        <View style={{ flex: 1 }}>
          <Action testID="filter-reset-btn" full label={tr("重置", "Reset")} disabled={!hasActiveFilterOrSort()} onPress={resetAllFilters} />
        </View>
        <View style={{ flex: 1 }}>
          <Action testID="filter-done-btn" primary full label={tr("完成", "Done")} disabled={false} onPress={() => filterSheetOpen.set(false)} />
        </View>
      </View>}>
      <View style={{ gap: 8 }}>
        <Label text={tr("排序方式", "Sort by")} bold size={12} muted />
        <Segmented
          testIDPrefix="nodes-sort"
          value={sortBy()}
          onChange={id => sortBy.set(id as "default" | "delay" | "name")}
          items={[
            { id: "default", label: tr("默认", "Default") },
            { id: "delay", label: tr("延迟最低", "Latency") },
            { id: "name", label: tr("名称", "Name") },
          ]} />
      </View>
      <Group>
        <GroupRow
          testID="filter-available-row"
          title={tr("仅显示可用节点", "Available only")}
          subtitle={tr("隐藏超时或尚未测速的节点", "Hide timed-out or untested nodes")}
          trailing={() => <MobileCheckbox checked={filterAvailableOnly()} onChange={v => filterAvailableOnly.set(v)} />}
          onPress={() => filterAvailableOnly.set(!filterAvailableOnly())} />
        <GroupRow
          testID="filter-local-row"
          title={tr("仅显示自建与已修改", "Local & edited only")}
          subtitle={tr("快速定位本地新增和局部覆写节点", "Focus on locally added and modified nodes")}
          trailing={() => <MobileCheckbox checked={nodeFilterLocalOnly()} onChange={v => nodeFilterLocalOnly.set(v)} />}
          onPress={() => nodeFilterLocalOnly.set(!nodeFilterLocalOnly())} />
      </Group>
      <If when={availableProtocols().length > 2}>
        <View style={{ gap: 8 }}>
          <Label text={tr("协议筛选", "Protocol")} bold size={12} muted />
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 8 }}>
            <Each of={availableProtocols()} key={p => p}>{p => {
              const active = () => filterProtocol() === p();
              return <Button
                testID={`filter-proto-${p()}`}
                onPress={() => { haptic("selection"); filterProtocol.set(p()); }}
                style={{
                  minHeight: 38, paddingHorizontal: 14, borderRadius: RADIUS.pill,
                  alignItems: "center", justifyContent: "center",
                  backgroundColor: active() ? colors().selected : colors().bg,
                  borderWidth: 1, borderColor: active() ? colors().selectedBorder : colors().border,
                }}>
                <Text style={{ fontSize: 12.5, fontWeight: active() ? "600" : "500", color: active() ? colors().accent : colors().text }}>
                  {p() === "all" ? tr("全部协议", "All") : protocolLabel(p())}
                </Text>
              </Button>;
            }}</Each>
          </View>
        </View>
      </If>
    </BottomSheet>

    {/* Single-Node Actions BottomSheet */}
    <BottomSheet
      testID="node-actions-sheet"
      open={!!nodeActionTarget()}
      onClose={() => nodeActionTarget.set(null)}
      title={nodeActionTarget()?.name ?? ""}
      subtitle={nodeActionTarget() ? rowSubtitle(nodeActionTarget()!, false) : ""}>
      <If when={!!nodeActionTarget()}>
        {(() => {
          const target = nodeActionTarget()!;
          const editable = !target.isBuiltin && !target.isGroup && !target.isProvider;
          const deletable = !target.isBuiltin && !target.isGroup && (target.isLocal || !!editor()?.isPureLocal);
          return <Group>
            <If when={!target.isBuiltin && !!status()?.running}>
              <GroupRow
                testID="node-action-test"
                icon="radar"
                title={tr("测试延迟", "Test latency")}
                onPress={() => { nodeActionTarget.set(null); void testNodeDelay(target.name); }} />
            </If>
            <If when={!target.isBuiltin && !target.isGroup}>
              <GroupRow
                testID="node-action-edit"
                icon="pencil"
                title={tr("编辑节点", "Edit node")}
                subtitle={target.isProvider ? tr("来自外部节点集，暂不支持单项编辑", "External provider node is read-only") : undefined}
                disabled={!editable}
                onPress={() => { nodeActionTarget.set(null); void handleEditNode(target, false); }} />
              <GroupRow
                testID="node-action-groups"
                icon="layers"
                title={tr("加入 / 移出策略组", "Add / remove policy groups")}
                onPress={() => { nodeActionTarget.set(null); openAssignGroups([target.name], "set"); }} />
              <If when={editable}>
                <GroupRow
                  testID="node-action-duplicate"
                  icon="copy"
                  title={tr("复制为新节点", "Duplicate as new node")}
                  onPress={() => { nodeActionTarget.set(null); void handleEditNode(target, true); }} />
              </If>
            </If>
            <If when={target.isEdited}>
              <GroupRow
                testID="node-action-reset"
                icon="rotateCcw"
                title={tr("恢复订阅原值", "Reset to subscription")}
                onPress={() => { nodeActionTarget.set(null); void handleResetNode(target); }} />
            </If>
            <If when={deletable}>
              <GroupRow
                testID="node-action-delete"
                icon="trash"
                danger
                title={tr("删除节点", "Delete node")}
                onPress={() => { nodeActionTarget.set(null); void handleDeleteNode(target); }} />
            </If>
          </Group>;
        })()}
      </If>
    </BottomSheet>

    {/* Assign Policy Groups BottomSheet */}
    <BottomSheet
      testID="assign-groups-sheet"
      open={assignSheetOpen()}
      onClose={() => assignSheetOpen.set(false)}
      title={tr("所属策略组", "Policy groups")}
      subtitle={tr("自建节点将通过追加方式加入订阅策略组，更新订阅不会删除。", "Local nodes append safely without breaking subscription refreshes.")}
      scrollable
      footer={() => <View style={{ flexDirection: "row", gap: 10 }}>
        <View style={{ flex: 1 }}><Action full label={tr("取消", "Cancel")} disabled={mutationBusy()} onPress={() => assignSheetOpen.set(false)} /></View>
        <View style={{ flex: 1 }}><Action testID="assign-groups-save" primary full label={tr("保存", "Save")} disabled={mutationBusy()} onPress={() => void saveAssignGroups()} /></View>
      </View>}>
      <Group>
        <Each of={(editor()?.groups ?? []).filter(g => !g.deleted && !g.missing)} key={g => g.name}>{g => {
          const checked = () => assignSelectedGroups().includes(g().name);
          const toggle = () => {
            const cur = assignSelectedGroups();
            assignSelectedGroups.set(cur.includes(g().name) ? cur.filter(x => x !== g().name) : [...cur, g().name]);
          };
          return <GroupRow
            title={g().name}
            subtitle={`${groupTypeLabel(g().type)} · ${g().members.length} ${tr("成员", "members")}`}
            trailing={() => <MobileCheckbox checked={checked()} onChange={toggle} />}
            onPress={toggle} />;
        }}</Each>
      </Group>
    </BottomSheet>

    {/* Add Node Entry BottomSheet */}
    <BottomSheet
      testID="add-node-sheet"
      open={addNodeSheetOpen()}
      onClose={() => addNodeSheetOpen.set(false)}
      title={tr("新增节点", "Add node")}
      subtitle={profiles().length === 0 ? tr("将自动创建「本地节点」档案", "Creates a Local Nodes profile automatically") : tr("自建节点将附加到当前配置，更新订阅不会删除", "Added as local nodes; preserved when refreshing subscription")}>
      <Group>
        <GroupRow
          testID="add-node-paste"
          icon="copy"
          iconColor={colors().accent}
          title={tr("粘贴链接 / 配置", "Paste link / config")}
          subtitle={tr("支持 VLESS / VMess / Trojan / SS / Hysteria2 / TUIC / WireGuard、Base64 与 Clash YAML", "Supports VPS URIs, Base64, and Clash YAML")}
          chevron
          onPress={() => {
            addNodeSheetOpen.set(false);
            pasteError.set("");
            pasteSheetOpen.set(true);
          }} />
        <GroupRow
          testID="add-node-manual"
          icon="sliders"
          title={tr("手动填写", "Manual form")}
          subtitle={tr("逐项填写协议、地址、端口与安全传输参数", "Configure protocol, server, auth, and transport")}
          chevron
          onPress={() => {
            addNodeSheetOpen.set(false);
            void openNodeEditor({ currentGroup: selectedGroup()?.name });
          }} />
        <GroupRow
          testID="add-node-file"
          icon="profiles"
          title={tr("从文件导入", "Import from file")}
          subtitle={tr("选择本地 YAML 或节点列表文件（≤8 MiB）", "Pick a local YAML or node list file (≤8 MiB)")}
          chevron
          onPress={() => void importNodesFromFile()} />
      </Group>
    </BottomSheet>

    {/* Paste Node Links BottomSheet */}
    <BottomSheet
      testID="paste-links-sheet"
      open={pasteSheetOpen()}
      onClose={() => pasteSheetOpen.set(false)}
      title={tr("粘贴节点链接或配置", "Paste node links or config")}
      subtitle={tr("可一次粘贴多行节点链接、Base64 订阅内容或 Clash YAML 片段。", "Paste one or more proxy URIs, Base64 text, or Clash YAML.")}
      scrollable
      error={pasteError()}
      footer={() => <View style={{ flexDirection: "row", gap: 10 }}>
        <View style={{ flex: 1 }}><Action full label={tr("取消", "Cancel")} disabled={pasteParsing()} onPress={() => pasteSheetOpen.set(false)} /></View>
        <View style={{ flex: 1 }}><Action testID="paste-links-parse" primary full label={pasteParsing() ? tr("正在解析…", "Parsing…") : tr("解析预览", "Parse & preview")} disabled={pasteParsing() || !pasteText().trim()} onPress={() => void parseAndOpenPreview(pasteText())} /></View>
      </View>}>
      <Field
        testID="paste-links-input"
        multiline
        height={160}
        value={pasteText()}
        onChange={v => { pasteText.set(v); if (pasteError()) pasteError.set(""); }}
        placeholder={"vless://uuid@example.com:443?security=tls#MyVPS\nhysteria2://pass@jp.example.com:443#JP-Hy2"}
        disabled={pasteParsing()} />
    </BottomSheet>
  </View>;
}

export function ParsePreviewPage() {
  const editor = () => proxyEditorData();
  const activeGroups = () => (editor()?.groups ?? []).filter(g => !g.deleted && !g.missing);
  const selectedNodes = () => parsedNodes().filter((_, idx) => parsedSelectedIndices().includes(idx));

  cleanup(BackHandler.on("press", () => {
    if (parsedImporting()) return true;
    popSubpage();
    return true;
  }));

  const toggleNode = (idx: number) => {
    haptic("selection");
    const cur = parsedSelectedIndices();
    parsedSelectedIndices.set(cur.includes(idx) ? cur.filter(i => i !== idx) : [...cur, idx]);
  };

  const toggleGroup = (name: string) => {
    haptic("selection");
    const cur = parsedTargetGroups();
    parsedTargetGroups.set(cur.includes(name) ? cur.filter(x => x !== name) : [...cur, name]);
  };

  const confirmImport = async () => {
    const chosen = selectedNodes();
    if (!chosen.length) return;
    parsedImporting.set(true);
    parsedError.set("");
    try {
      if (!status()?.activeProfile || profiles().length === 0) {
        const res = await call<ProfileMutation>("profiles.createLocal", {
          name: tr("本地节点", "Local Nodes"),
          nodes: chosen,
          activate: true,
        });
        if (res.status) acceptStatus(res.status);
        await refreshLists();
        if (res.profile?.id) await loadProxyEditor(res.profile.id).catch(() => {});
        haptic("confirm");
        showToast(tr(`已创建「本地节点」并导入 ${chosen.length} 个节点`, `Created Local Nodes with ${chosen.length} nodes`));
        popSubpage();
        return;
      }

      let ed = editor();
      if (!ed) ed = await loadProxyEditor(status()!.activeProfile);
      if (!ed) throw new Error(tr("无法加载当前配置", "Could not load active profile"));

      const edits = buildBatchAddNodesEdits(ed, chosen, parsedTargetGroups());
      const res = await saveProxyEdits(ed.profileId || ed.id, edits);
      haptic("confirm");
      showToast(res.reconnectRequired
        ? tr(`已添加 ${chosen.length} 个自建节点，重新连接后生效`, `Added ${chosen.length} local nodes. Reconnect to apply.`)
        : tr(`已添加 ${chosen.length} 个自建节点`, `Added ${chosen.length} local nodes`));
      popSubpage();
    } catch (e) {
      haptic("reject");
      parsedError.set(message(e));
    } finally {
      parsedImporting.set(false);
    }
  };

  return <View testID="parse-preview-page" style={{ flex: 1, backgroundColor: colors().bg }}>
    <SubpageHeader
      title={tr("导入节点预览", "Import nodes preview")}
      subtitle={tr(`已选 ${selectedNodes().length} / ${parsedNodes().length} 个节点`, `${selectedNodes().length} / ${parsedNodes().length} selected`)}
      backTestID="parse-preview-back"
      backDisabled={parsedImporting()}
      onBack={() => popSubpage()} />

    <Scroll style={{ flex: 1 }} contentStyle={{ padding: 16, paddingBottom: 24, gap: 14 }}>
      <Notice
        tone="info"
        text={profiles().length === 0
          ? tr("将自动创建「本地节点」档案并添加所选节点。", "Creates a \"Local Nodes\" profile automatically with the selected nodes.")
          : tr("将作为自建节点附加到当前订阅，更新订阅不会删除，保留原订阅规则。", "Added as local nodes to your current profile. Subscription updates preserve them and keep original rules.")} />

      <If when={parsedFailures().length > 0}>
        <Panel padding={12} gap={8}>
          <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
            <Label text={tr(`有 ${parsedFailures().length} 条链接解析失败（可继续导入有效节点）`, `${parsedFailures().length} lines failed to parse (valid nodes can still be imported)`)} color={colors().warn} bold size={12.5} />
            <Action size="compact" quiet label={parsedFailuresExpanded() ? tr("收起", "Hide") : tr("查看详情", "Details")} disabled={false} onPress={() => parsedFailuresExpanded.set(!parsedFailuresExpanded())} />
          </View>
          <If when={parsedFailuresExpanded()}>
            <View style={{ gap: 4 }}>
              <Each of={parsedFailures()} key={f => `${f.line}-${f.raw}`}>{f => (
                <Label text={tr(`第 ${f().line} 行：${f().reason}`, `Line ${f().line}: ${f().reason}`)} muted size={11.5} />
              )}</Each>
            </View>
          </If>
        </Panel>
      </If>

      <SectionLabel title={tr("已识别节点", "Parsed nodes")} trailing={`${selectedNodes().length}/${parsedNodes().length}`} />
      <Group>
        <Each of={parsedNodes()} key={(n, i) => `${i}-${n.name}-${n.server}`}>{(n, idx) => {
          const checked = () => parsedSelectedIndices().includes(idx());
          return <GroupRow
            testID={`parsed-node-${idx()}`}
            title={n().name || `${protocolLabel(n().type)}-${n().server}`}
            subtitle={`${protocolLabel(n().type)} · ${n().server}:${n().port}`}
            trailing={() => <MobileCheckbox checked={checked()} onChange={() => toggleNode(idx())} />}
            onPress={() => toggleNode(idx())} />;
        }}</Each>
      </Group>

      <If when={activeGroups().length > 0}>
        <SectionLabel title={tr("追加到策略组", "Append to policy groups")} trailing={String(parsedTargetGroups().length)} />
        <Group>
          <Each of={activeGroups()} key={g => g.name}>{g => {
            const checked = () => parsedTargetGroups().includes(g().name);
            return <GroupRow
              testID={`parsed-target-group-${g().name}`}
              title={g().name}
              subtitle={`${groupTypeLabel(g().type)} · ${g().members.length} ${tr("成员", "members")}`}
              trailing={() => <MobileCheckbox checked={checked()} onChange={() => toggleGroup(g().name)} />}
              onPress={() => toggleGroup(g().name)} />;
          }}</Each>
        </Group>
      </If>

      <If when={!!parsedError()}><Notice text={parsedError()} tone="danger" /></If>
    </Scroll>

    <View style={{
      paddingHorizontal: 16, paddingVertical: 12,
      backgroundColor: colors().surface, borderTopWidth: 1, borderColor: colors().border,
    }}>
      <Action
        testID="parse-preview-confirm"
        primary
        full
        label={parsedImporting()
          ? tr("正在导入…", "Importing…")
          : tr(`导入 ${selectedNodes().length} 个节点`, `Import ${selectedNodes().length} nodes`)}
        disabled={parsedImporting() || !selectedNodes().length}
        onPress={() => void confirmImport()} />
    </View>
  </View>;
}
