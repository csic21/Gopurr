import { cleanup, colorScheme, computed, effect, keymap, List, Scroll, Text, View, signal, windowSize } from "nsh";
import { Button } from "nsh/base";
import { colors } from "./theme";
import { tr } from "./i18n";
import { pendingProxiesFilter, sidebarCollapsed } from "./client";
import {
  Action, Empty, Feedback, Heading, Icon, IconButton, Label, MenuPopover, Modal, RADIUS, ROW_HEIGHT,
  Row, SearchField, Select, Tabs, Tag, confirmDialog, elevation, groupTypeLabel, tint,
  type MenuPopoverItem,
} from "./ui";
import { SaveBar } from "./settings";
import { watchDesktopDraft } from "./drafts";
import {
  extractNodeForm, isBuiltinNode, mergeDiff, runConcurrentPool,
  type StructuredNodeForm,
} from "./proxy-helpers";
import {
  GroupEditorFields, MemberPickerList, NodeEditorFields, QuickAddNodeModal,
  buildGroupDeleteEdits, buildGroupRestoreEdits, buildGroupSaveEdits,
  buildNodeDeleteEdits, buildNodeRestoreEdits, buildNodeSaveEdits,
  extractGroupForm, groupsContainingNode,
  type GroupFormState, type ProxyEditorModel,
} from "./proxy-editor-panel";
import type { PageProps } from "./types";

export { mergeDiff };

const ALL_NODES_VIEW = "__gopurr_all_nodes__";
const MOTION = { transitionProperty: "background-color, border-color, color", transitionDuration: 140, transitionTimingFunction: "ease-out" } as const;

// Module-level signals survive Light/Dark remount and 1s polling.
const groupName = signal("");
const query = signal("");
const filterKind = signal("all");
const sort = signal("original");
const quickAddOpen = signal(false);
const notice = signal("");
const pageError = signal("");

// Editor metadata & inspector state
const editorModel = signal<ProxyEditorModel | null>(null);
const editorProfileLoaded = signal("");
const inspectorTarget = signal<{ kind: "node" | "group"; row: any; isNew?: boolean } | null>(null);
const nodeForm = signal<StructuredNodeForm>(extractNodeForm({}));
const nodeBase = signal("");
const nodeGroups = signal<Set<string>>(new Set());
const nodeGroupsBase = signal("");
const groupForm = signal<GroupFormState>(extractGroupForm({}));
const groupBase = signal("");
const inspectorError = signal("");
const inspectorSaving = signal(false);
const memberPickerOpen = signal(false);
const focusedNode = signal("");

// Client-side concurrent delay testing state (decoupled from global p.busy)
const liveDelays = signal<Record<string, { delay: number; delayKnown: boolean }>>({});
const testing = signal<{ active: boolean; done: number; total: number }>({ active: false, done: 0, total: 0 });
let testRunSeq = 0;

function delayLabel(item: any): string {
  if (!item?.delayKnown) return tr("未测试", "Not tested");
  if (item.delay === 0) return tr("超时", "Timeout");
  return item.delay + " ms";
}

function delayColor(item: any): string {
  if (!item?.delayKnown) return colors().faint;
  if (!item.delay || item.delay >= 500) return colors().danger;
  if (item.delay >= 200) return colors().warn;
  return colors().success;
}

function nodeKind(item: any): string {
  if (item?.name === "DIRECT") return tr("直连", "Direct");
  if (String(item?.name ?? "").startsWith("REJECT")) return tr("拦截", "Reject");
  return item?.type || tr("代理节点", "Proxy");
}

function DelayTag(p: { item: any }) {
  const known = () => !!p.item?.delayKnown;
  const color = () => delayColor(p.item);
  const dark = () => colorScheme() === "dark";
  return <View style={{ alignItems: "flex-end", justifyContent: "center" }}>
    <If when={!known()}><Text style={{ fontSize: 11, fontVariant: "tabular-nums", color: colors().faint, numberOfLines: 1 }}>{delayLabel(p.item)}</Text></If>
    <If when={known()}><View style={{ height: 22, paddingHorizontal: 8, borderRadius: 6, flexDirection: "row", alignItems: "center", gap: 5, backgroundColor: tint(color(), dark() ? "15" : "10") }}>
      <View style={{ width: 5, height: 5, borderRadius: 2.5, backgroundColor: color() }} />
      <Text style={{ fontSize: 11, fontWeight: "600", fontVariant: "tabular-nums", letterSpacing: 0.1, color: color(), numberOfLines: 1 }}>{delayLabel(p.item)}</Text>
    </View></If>
  </View>;
}

export function ProxiesPage(p: PageProps) {
  // 3-tier responsive breakpoints based on exact contentWidth
  const contentWidth = () => windowSize().width - (sidebarCollapsed() ? 0 : 184);
  const compact = () => windowSize().height < 660;
  const padX = () => compact() || windowSize().width < 960 ? 16 : 24;
  const padY = () => compact() ? 12 : 20;
  const gap = () => compact() ? 10 : 14;

  const inspectorOpen = () => !!inspectorTarget();
  const inlineInspector = () => inspectorOpen() && contentWidth() >= 800;
  const modalInspector = () => inspectorOpen() && contentWidth() < 800;
  // Auto-collapse left group rail into a native Select at top of node list when 800 <= contentWidth < 960 and inspector is open!
  const collapseRailToSelect = () => inlineInspector() && contentWidth() < 960;

  const inspectorDirty = () => {
    const t = inspectorTarget();
    if (!t) return false;
    if (t.kind === "node") {
      return JSON.stringify(nodeForm()) !== nodeBase() || JSON.stringify([...nodeGroups()].sort()) !== nodeGroupsBase();
    }
    return JSON.stringify(groupForm()) !== groupBase();
  };

  watchDesktopDraft("proxies", () => inspectorDirty() || inspectorSaving());

  // Consume cross-page filter preset (from Subscriptions overlay badge)
  effect(() => {
    const preset = pendingProxiesFilter();
    if (preset) {
      pendingProxiesFilter.set(null);
      filterKind.set(preset);
      groupName.set(ALL_NODES_VIEW);
    }
  });

  // Load editor metadata for active profile (for local/edited badges, All Nodes view, and inline editing)
  let editorLoadSeq = 0;
  const loadEditorModel = async (profileId: string, force = false) => {
    if (!profileId) {
      editorModel.set(null);
      editorProfileLoaded.set("");
      return;
    }
    if (!force && editorProfileLoaded() === profileId && editorModel()?.id === profileId) return;
    const seq = ++editorLoadSeq;
    try {
      const m = await p.call<ProxyEditorModel>("desktop.proxies.editor", { id: profileId });
      if (seq === editorLoadSeq) {
        editorModel.set(m);
        editorProfileLoaded.set(profileId);
      }
    } catch {
      // Non-fatal: runtime groups still work even if editor call fails
    }
  };

  effect(() => {
    const activeId = String(p.snapshot().activeProfile ?? "");
    if (activeId !== editorProfileLoaded()) {
      liveDelays.set({});
      void loadEditorModel(activeId, true);
    }
  });

  cleanup(() => {
    testRunSeq++;
    if (testing().active) testing.set({ active: false, done: 0, total: 0 });
  });

  const groups = () => p.snapshot().groups ?? [];
  const mode = () => String(p.snapshot().status.mode ?? "rule");
  const kernelRunning = () => !!p.snapshot().status.kernel;
  const offlinePreview = () => !kernelRunning() || !!p.snapshot().groupsPreview;
  const activeProfile = () => (p.snapshot().profiles ?? []).find((x: any) => x.id === p.snapshot().activeProfile) ?? null;

  const visibleGroups = () => groups().filter((v: any) => !v.hidden && ((mode() === "global") === (v.name === "GLOBAL")));
  const isAllNodesView = () => groupName() === ALL_NODES_VIEW || mode() === "direct";

  const selectedGroup = () => {
    if (isAllNodesView()) return null;
    return (
      visibleGroups().find((v: any) => v.name === groupName()) ??
      (mode() === "global" ? visibleGroups().find((v: any) => v.name === "GLOBAL") : visibleGroups().find((v: any) => v.name !== "GLOBAL")) ??
      visibleGroups()[0] ??
      null
    );
  };

  // Map editor nodes & provider nodes by name for fast O(1) metadata lookups
  const editorNodeByName = computed(() => {
    const map = new Map<string, any>();
    for (const n of editorModel()?.nodes ?? []) map.set(n.name, n);
    return map;
  });
  const providerNodeSet = computed(() => {
    const set = new Set<string>();
    for (const pn of editorModel()?.providerNodes ?? []) set.add(pn.name);
    return set;
  });
  const groupNameSet = computed(() => new Set<string>(groups().map((g: any) => g.name)));

  // Build All Nodes collection (includes ungrouped dialer-proxy jump nodes + orphan missing edits + provider nodes)
  const allNodesList = computed<any[]>(() => {
    const byName = new Map<string, any>();
    const gSet = groupNameSet();
    for (const g of groups()) {
      for (const m of g.members ?? []) {
        if (gSet.has(m.name) || isBuiltinNode(m.name)) continue;
        if (!byName.has(m.name)) byName.set(m.name, { ...m });
      }
    }
    for (const n of editorModel()?.nodes ?? []) {
      const existing = byName.get(n.name);
      byName.set(n.name, {
        name: n.name,
        type: existing?.type || n.type || "proxy",
        delay: existing?.delay ?? 0,
        delayKnown: existing?.delayKnown ?? false,
        server: n.server,
        port: n.port,
        source: n.source,
        localId: n.localId,
        edited: n.edited,
        missing: n.missing,
      });
    }
    for (const pn of editorModel()?.providerNodes ?? []) {
      if (!byName.has(pn.name)) {
        byName.set(pn.name, { name: pn.name, type: pn.type || "proxy", delay: 0, delayKnown: false, provider: pn.provider });
      }
    }
    return [...byName.values()];
  });

  const rawMembers = (): any[] => {
    if (isAllNodesView()) return allNodesList();
    return selectedGroup()?.members ?? [];
  };

  const latestMember = (name: string) => {
    const base = rawMembers().find((item: any) => item.name === name) ?? { name, type: "", delay: 0, delayKnown: false };
    const override = liveDelays()[name];
    return override ? { ...base, ...override } : base;
  };

  const availableProtocols = () => {
    const set = new Set<string>();
    for (const m of rawMembers()) {
      if (isBuiltinNode(m.name) || groupNameSet().has(m.name)) continue;
      const meta = editorNodeByName().get(m.name);
      const t = String(meta?.type || m.type || "").toLowerCase();
      if (t) set.add(t);
    }
    return [...set].sort();
  };

  const filtered = () => {
    const q = query().trim().toLowerCase();
    const fk = filterKind();
    const enMap = editorNodeByName();
    const list = rawMembers().filter((item: any) => {
      const meta = enMap.get(item.name);
      const live = latestMember(item.name);
      if (q) {
        const hay = [item.name, item.type, meta?.type, meta?.server].filter(Boolean).join(" ").toLowerCase();
        if (!hay.includes(q)) return false;
      }
      if (fk === "available") {
        if (!live.delayKnown || !live.delay) return false;
      } else if (fk === "local_edited") {
        if (!meta || (meta.source && !meta.edited && !meta.missing)) return false;
      } else if (fk.startsWith("proto:")) {
        const want = fk.slice(6);
        const actual = String(meta?.type || item.type || "").toLowerCase();
        if (actual !== want) return false;
      }
      return true;
    });
    if (sort() === "name") list.sort((a: any, b: any) => a.name.localeCompare(b.name));
    if (sort() === "delay") {
      list.sort((a: any, b: any) => {
        const la = latestMember(a.name), lb = latestMember(b.name);
        const da = la.delayKnown && la.delay > 0 ? la.delay : Infinity;
        const db = lb.delayKnown && lb.delay > 0 ? lb.delay : Infinity;
        return da - db;
      });
    }
    return list;
  };

  // Preserve item identities across 1s snapshots so virtualized List never drifts scroll position!
  const listMembers = computed<any[]>((previous) => {
    const next = filtered();
    if (previous && previous.length === next.length && previous.every((item, index) => item.name === next[index].name && item.type === next[index].type)) {
      return previous;
    }
    return next;
  });

  const testableInList = (items: any[]): string[] => {
    const gSet = groupNameSet();
    const seen = new Set<string>();
    for (const m of items) {
      if (!gSet.has(m.name) && !isBuiltinNode(m.name) && !seen.has(m.name)) {
        const meta = editorNodeByName().get(m.name);
        if (!meta?.missing) seen.add(m.name);
      }
    }
    return [...seen];
  };

  const allTestableNames = (): string[] => {
    const gSet = groupNameSet();
    const seen = new Set<string>();
    for (const g of groups()) {
      for (const m of g.members ?? []) {
        if (!gSet.has(m.name) && !isBuiltinNode(m.name) && !seen.has(m.name)) seen.add(m.name);
      }
    }
    return [...seen];
  };

  const stopTesting = () => {
    testRunSeq++;
    testing.set({ active: false, done: 0, total: 0 });
  };

  const runBatchDelay = async (names: string[]) => {
    if (!names.length || offlinePreview()) return;
    const seq = ++testRunSeq;
    const concurrency = Math.max(6, Math.min(8, Number(p.snapshot().prefs?.testConcurrency) || 6));
    testing.set({ active: true, done: 0, total: names.length });
    await runConcurrentPool(
      names,
      concurrency,
      async (name) => {
        try {
          const res = await p.call<{ delay?: number }>("desktop.node.delay", { name });
          if (seq !== testRunSeq) return;
          const d = Number(res?.delay ?? 0);
          liveDelays.set({ ...liveDelays(), [name]: { delay: d, delayKnown: true } });
        } catch {
          if (seq !== testRunSeq) return;
          liveDelays.set({ ...liveDelays(), [name]: { delay: 0, delayKnown: true } });
        }
      },
      () => seq !== testRunSeq,
      (done, total) => {
        if (seq === testRunSeq) testing.set({ active: true, done, total });
      },
    );
    if (seq === testRunSeq) {
      testing.set({ active: false, done: names.length, total: names.length });
      void p.refresh();
    }
  };

  const testSingleNode = async (name: string) => {
    if (offlinePreview() || isBuiltinNode(name)) return;
    try {
      const res = await p.call<{ delay?: number }>("desktop.node.delay", { name });
      const d = Number(res?.delay ?? 0);
      liveDelays.set({ ...liveDelays(), [name]: { delay: d, delayKnown: true } });
      void p.refresh();
    } catch {
      liveDelays.set({ ...liveDelays(), [name]: { delay: 0, delayKnown: true } });
    }
  };

  const testGroup = () => {
    if (testing().active) { stopTesting(); return; }
    const names = testableInList(rawMembers());
    if (names.length) void runBatchDelay(names);
  };

  const testAll = () => {
    if (testing().active) { stopTesting(); return; }
    const names = allTestableNames();
    if (names.length) void runBatchDelay(names);
  };

  // Dirty-draft guard before any navigation/switch inside ProxiesPage
  const confirmDiscardIfDirty = async (): Promise<boolean> => {
    if (!inspectorDirty()) return true;
    return await confirmDialog({
      title: tr("放弃未保存更改？", "Discard unsaved changes?"),
      description: tr("当前编辑器中有未保存的节点或策略组修改。", "You have unsaved changes in the proxy inspector."),
      confirmLabel: tr("放弃更改", "Discard"),
      cancelLabel: tr("继续编辑", "Keep editing"),
      destructive: true,
    });
  };

  const closeInspector = async (force = false) => {
    if (!force && !(await confirmDiscardIfDirty())) return;
    memberPickerOpen.set(false);
    inspectorTarget.set(null);
    inspectorError.set("");
  };

  const ensureEditorModel = async (): Promise<ProxyEditorModel | null> => {
    const activeId = String(p.snapshot().activeProfile ?? "");
    if (!activeId) return null;
    if (editorModel()?.id === activeId) return editorModel();
    await loadEditorModel(activeId, true);
    return editorModel();
  };

  const openNodeInspector = async (nodeName: string, duplicate = false) => {
    if (!(await confirmDiscardIfDirty())) return;
    const m = await ensureEditorModel();
    if (!m) {
      quickAddOpen.set(true);
      return;
    }
    const row = m.nodes.find((n: any) => n.name === nodeName);
    if (!row) return;
    if (duplicate) {
      const clonedForm = extractNodeForm({ ...row, name: `${row.name} ${tr("副本", "Copy")}` });
      const defGroup = selectedGroup()?.name && ["select", "selector"].includes(String(selectedGroup()?.type).toLowerCase()) ? [selectedGroup().name] : [];
      const gs = new Set(defGroup);
      inspectorTarget.set({ kind: "node", row: { source: "", localId: "", name: clonedForm.name, type: clonedForm.type, server: clonedForm.server, port: Number(clonedForm.port) || 443, options: row.options }, isNew: true });
      nodeForm.set(clonedForm);
      nodeBase.set(""); // immediately dirty so user can save copy directly
      nodeGroups.set(gs);
      nodeGroupsBase.set(JSON.stringify([...gs].sort()));
    } else {
      const form = extractNodeForm(row);
      const gs = new Set(groupsContainingNode(m, row.name));
      inspectorTarget.set({ kind: "node", row });
      nodeForm.set(form);
      nodeBase.set(JSON.stringify(form));
      nodeGroups.set(gs);
      nodeGroupsBase.set(JSON.stringify([...gs].sort()));
    }
    inspectorError.set("");
  };

  const openGroupInspector = async (targetGroupName?: string) => {
    if (!(await confirmDiscardIfDirty())) return;
    const m = await ensureEditorModel();
    if (!m) return;
    if (!targetGroupName) {
      const row = { source: "", name: tr("新策略组", "New group"), type: "select", members: ["DIRECT"] };
      const form = extractGroupForm(row);
      inspectorTarget.set({ kind: "group", row, isNew: true });
      groupForm.set(form);
      groupBase.set("");
      inspectorError.set("");
      return;
    }
    const row = m.groups.find((g: any) => g.name === targetGroupName);
    if (!row) return;
    const form = extractGroupForm(row);
    inspectorTarget.set({ kind: "group", row });
    groupForm.set(form);
    groupBase.set(JSON.stringify(form));
    inspectorError.set("");
  };

  const switchGroup = async (nextName: string) => {
    if (nextName === groupName()) return;
    if (!(await confirmDiscardIfDirty())) return;
    stopTesting();
    groupName.set(nextName);
    query.set("");
  };

  const changeMode = async (value: string) => {
    if (!(await confirmDiscardIfDirty())) return;
    stopTesting();
    groupName.set(value === "global" ? "GLOBAL" : "");
    query.set("");
    void p.run("mode", () => p.call("desktop.mode.set", { mode: value }));
  };

  const saveInspector = async () => {
    const t = inspectorTarget();
    const m = editorModel();
    if (!t || !m || inspectorSaving()) return;
    inspectorSaving.set(true);
    inspectorError.set("");
    try {
      let nextEdits: any;
      if (t.kind === "node") {
        nextEdits = buildNodeSaveEdits(m, t.row, nodeForm(), nodeGroups()).edits;
      } else {
        nextEdits = buildGroupSaveEdits(m, t.row, groupForm()).edits;
      }
      await p.call("desktop.proxies.save", { id: m.id, edits: nextEdits });
      await p.refresh();
      await loadEditorModel(m.id, true);
      notice.set(tr("已保存并应用更改", "Changes saved and applied"));
      setTimeout(() => { if (notice() === tr("已保存并应用更改", "Changes saved and applied")) notice.set(""); }, 4000);
      await closeInspector(true);
    } catch (e) {
      inspectorError.set(e instanceof Error ? e.message : String(e));
    } finally {
      inspectorSaving.set(false);
    }
  };

  const discardInspector = () => {
    const t = inspectorTarget();
    if (!t) return;
    if (t.isNew) {
      void closeInspector(true);
      return;
    }
    if (t.kind === "node") {
      const form = extractNodeForm(t.row);
      const gs = new Set(groupsContainingNode(editorModel(), t.row.name));
      nodeForm.set(form);
      nodeBase.set(JSON.stringify(form));
      nodeGroups.set(gs);
      nodeGroupsBase.set(JSON.stringify([...gs].sort()));
    } else {
      const form = extractGroupForm(t.row);
      groupForm.set(form);
      groupBase.set(JSON.stringify(form));
    }
    inspectorError.set("");
  };

  const deleteOrRestoreNode = async (row: any) => {
    const m = await ensureEditorModel();
    if (!m) return;
    const isDelete = !row.source || !!m.isPureLocal;
    const ok = await confirmDialog({
      title: isDelete
        ? tr(`删除节点“${row.name}”？`, `Delete proxy “${row.name}”?`)
        : row.missing
          ? tr(`移除失联修改“${row.name}”？`, `Remove orphaned edit “${row.name}”?`)
          : tr(`恢复“${row.name}”为订阅原值？`, `Reset “${row.name}” to subscription value?`),
      description: isDelete
        ? tr("该本地节点将从档案和所属策略组中移除。", "This local proxy will be removed from the profile and its policy groups.")
        : tr("本地修改将被清除，节点恢复为订阅原始配置。", "Local overrides will be cleared and restored to the subscription value."),
      confirmLabel: isDelete ? tr("删除节点", "Delete proxy") : row.missing ? tr("移除修改", "Remove edit") : tr("恢复原值", "Reset"),
      cancelLabel: tr("取消", "Cancel"),
      destructive: true,
    });
    if (!ok) return;
    inspectorSaving.set(true);
    inspectorError.set("");
    pageError.set("");
    try {
      let nextEdits: any;
      if (isDelete) {
        const res = buildNodeDeleteEdits(m, row);
        if (res.error) throw new Error(res.error);
        nextEdits = res.edits;
      } else {
        nextEdits = buildNodeRestoreEdits(m, row);
      }
      await p.call("desktop.proxies.save", { id: m.id, edits: nextEdits });
      await p.refresh();
      await loadEditorModel(m.id, true);
      if (inspectorTarget()?.kind === "node" && inspectorTarget()?.row?.name === row.name) {
        await closeInspector(true);
      }
      notice.set(isDelete ? tr("已删除节点", "Proxy deleted") : tr("已恢复订阅原值", "Restored to subscription"));
      setTimeout(() => notice.set(""), 4000);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      if (inspectorOpen()) inspectorError.set(msg);
      else pageError.set(msg);
    } finally {
      inspectorSaving.set(false);
    }
  };

  const deleteOrRestoreGroup = async (row: any, modeAction: "delete" | "restore") => {
    const m = editorModel();
    if (!m) return;
    const ok = await confirmDialog({
      title: modeAction === "delete" ? tr(`删除策略组“${row.name}”？`, `Delete group “${row.name}”?`) : tr(`恢复策略组“${row.name}”？`, `Reset group “${row.name}”?`),
      description: modeAction === "delete"
        ? tr("删除后若被规则引用会提示错误。", "Deletion fails if any rule still references this group.")
        : tr("清除此策略组的本地修改并恢复订阅配置。", "Clear local changes and restore this group from the subscription."),
      confirmLabel: modeAction === "delete" ? tr("删除策略组", "Delete group") : tr("恢复原值", "Reset"),
      cancelLabel: tr("取消", "Cancel"),
      destructive: true,
    });
    if (!ok) return;
    inspectorSaving.set(true);
    inspectorError.set("");
    try {
      const nextEdits = modeAction === "delete" ? buildGroupDeleteEdits(m, row) : buildGroupRestoreEdits(m, row);
      await p.call("desktop.proxies.save", { id: m.id, edits: nextEdits });
      await p.refresh();
      await loadEditorModel(m.id, true);
      await closeInspector(true);
    } catch (e) {
      inspectorError.set(e instanceof Error ? e.message : String(e));
    } finally {
      inspectorSaving.set(false);
    }
  };

  // Keyboard shortcuts
  keymap.bind("mod+n", () => { if (!quickAddOpen()) quickAddOpen.set(true); });
  keymap.bind("mod+e", () => {
    const name = focusedNode() || selectedGroup()?.now;
    if (name && !isBuiltinNode(name)) void openNodeInspector(name);
  });
  keymap.bind("mod+t", () => { if (!offlinePreview() && mode() !== "direct") testGroup(); });
  keymap.bind("escape", () => {
    if (memberPickerOpen()) { memberPickerOpen.set(false); return; }
    if (inspectorOpen()) void closeInspector();
  });
  keymap.bind("down", () => {
    if (inspectorOpen() || quickAddOpen()) return;
    const list = listMembers();
    if (!list.length) return;
    const idx = list.findIndex((x: any) => x.name === (focusedNode() || selectedGroup()?.now));
    focusedNode.set(list[(idx + 1 + list.length) % list.length].name);
  });
  keymap.bind("up", () => {
    if (inspectorOpen() || quickAddOpen()) return;
    const list = listMembers();
    if (!list.length) return;
    const idx = list.findIndex((x: any) => x.name === (focusedNode() || selectedGroup()?.now));
    focusedNode.set(list[(idx - 1 + list.length) % list.length].name);
  });
  keymap.bind("enter", () => {
    if (inspectorOpen() || quickAddOpen()) return;
    const g = selectedGroup();
    const target = focusedNode();
    if (g && target && ["selector", "select"].includes(String(g.type).toLowerCase()) && !isAllNodesView()) {
      void p.run("select-proxy", () => p.call("desktop.group.select", { group: g.name, member: target }));
    }
  });

  const availableMembersForPicker = () => {
    const m = editorModel();
    if (!m) return [];
    const byName = new Map<string, { name: string; type?: string; local?: boolean }>();
    for (const n of m.nodes ?? []) byName.set(n.name, { name: n.name, type: n.type, local: !n.source });
    for (const g of m.groups ?? []) if (!g.deleted && !byName.has(g.name)) byName.set(g.name, { name: g.name, type: g.type });
    for (const name of m.availableNames ?? m.options ?? []) if (!byName.has(name)) byName.set(name, { name });
    return [...byName.values()];
  };

  const hasAnyContent = () => visibleGroups().length > 0 || allNodesList().length > 0;
  const isZeroProfileEmpty = () => !p.snapshot().activeProfile && !hasAnyContent();

  // Header overflow `⋮` menu items
  const overflowItems = (): MenuPopoverItem[] => [
    {
      id: "test-all",
      label: testing().active ? tr("停止测速", "Stop latency test") : tr("测试全部节点", "Test all proxies"),
      icon: testing().active ? "stop" : "refresh",
      disabled: !testing().active && (!allTestableNames().length || offlinePreview() || mode() === "direct"),
      onPress: testAll,
    },
    {
      id: "new-group",
      label: tr("新建策略组", "New policy group"),
      icon: "plus",
      disabled: !p.snapshot().activeProfile,
      onPress: () => void openGroupInspector(),
    },
    {
      id: "delay-settings",
      label: tr("延迟测试设置…", "Latency test settings…"),
      icon: "sliders",
      dividerBefore: true,
      onPress: () => p.navigate("settings"),
    },
  ];

  const filterItems = () => [
    { value: "all", title: tr("全部节点", "All") },
    { value: "available", title: tr("仅可用", "Available only") },
    { value: "local_edited", title: tr("仅自建与已修改", "Local & edited") },
    ...availableProtocols().map((pr) => ({ value: `proto:${pr}`, title: pr.toUpperCase() })),
  ];

  const collapsedGroupItems = () => [
    ...visibleGroups().map((g: any) => ({ value: g.name, title: `${g.name} (${(g.members ?? []).length})` })),
    { value: ALL_NODES_VIEW, title: tr(`全部节点 (${allNodesList().length})`, `All proxies (${allNodesList().length})`) },
  ];

  const paneTitle = () => {
    if (isAllNodesView()) return tr("全部节点", "All proxies");
    return selectedGroup()?.name ?? "";
  };
  const paneCaption = () => {
    if (isAllNodesView()) {
      const localCount = (editorModel()?.nodes ?? []).filter((n: any) => !n.source).length;
      const editedCount = (editorModel()?.nodes ?? []).filter((n: any) => n.source && n.edited).length;
      return [
        tr(`共 ${allNodesList().length} 个节点`, `${allNodesList().length} proxies`),
        localCount ? tr(`${localCount} 自建`, `${localCount} local`) : "",
        editedCount ? tr(`${editedCount} 已修改`, `${editedCount} edited`) : "",
      ].filter(Boolean).join(" · ");
    }
    const g = selectedGroup();
    if (!g) return "";
    const parts = [groupTypeLabel(g.type ?? ""), `${g.members?.length ?? 0} ${tr("个成员", "members")}`];
    if (g.now) parts.push(`${tr("当前使用", "In use")} ${g.now}`);
    return parts.join(" · ");
  };

  // Node row renderer (50px virtualized List item)
  const nodeRow = (item: () => any) => {
    const name = () => item().name;
    const live = () => latestMember(name());
    const meta = () => editorNodeByName().get(name());
    const isProvider = () => providerNodeSet().has(name());
    const isGroupRef = () => groupNameSet().has(name());
    const builtin = () => isBuiltinNode(name());
    const canSelect = () => !isAllNodesView() && mode() !== "direct" && ["selector", "select"].includes(String(selectedGroup()?.type).toLowerCase()) && !meta()?.missing;
    const isSelected = () => !isAllNodesView() && selectedGroup()?.now === name();
    const isEditingThis = () => inspectorTarget()?.kind === "node" && inspectorTarget()?.row?.name === name();
    const isLocal = () => !!meta() && !meta().source;
    const isEdited = () => !!meta() && !!meta().source && !!meta().edited && !meta().missing;
    const isMissing = () => !!meta()?.missing;
    const canEdit = () => !!meta() && !builtin() && !isGroupRef() && !isProvider() && !isMissing();
    const canTest = () => !builtin() && !isGroupRef() && !isMissing();

    const subText = () => {
      const parts: string[] = [];
      if (isSelected()) parts.push(tr("当前使用", "In use"));
      parts.push(nodeKind(live()));
      if (meta()?.server) parts.push(meta().server);
      if (isProvider()) parts.push(tr("来自外部节点集（只读）", "External provider (read-only)"));
      return parts.join(" · ");
    };

    const rowMenuItems = (): MenuPopoverItem[] => {
      const items: MenuPopoverItem[] = [];
      if (canEdit()) {
        items.push({ id: "edit", label: tr("编辑节点", "Edit proxy"), icon: "pencil", onPress: () => void openNodeInspector(name()) });
        items.push({ id: "duplicate", label: tr("复制为新节点", "Duplicate as local"), icon: "copy", onPress: () => void openNodeInspector(name(), true) });
      }
      if (canTest()) {
        items.push({ id: "test", label: tr("测试延迟", "Test latency"), icon: "refresh", disabled: offlinePreview(), onPress: () => void testSingleNode(name()) });
      }
      if (isEdited()) {
        items.push({ id: "reset", label: tr("恢复订阅原值", "Reset to subscription"), icon: "rotateCcw", dividerBefore: true, onPress: () => void deleteOrRestoreNode(meta()) });
      }
      if (isMissing()) {
        items.push({ id: "remove-orphan", label: tr("移除失联修改", "Remove orphaned edit"), icon: "trash", danger: true, onPress: () => void deleteOrRestoreNode(meta()) });
      }
      if (isLocal() || (meta() && editorModel()?.isPureLocal)) {
        items.push({ id: "delete", label: tr("删除节点", "Delete proxy"), icon: "trash", danger: true, dividerBefore: true, onPress: () => void deleteOrRestoreNode(meta()) });
      }
      return items;
    };

    return <View style={{ flexDirection: "row", alignItems: "center", borderBottomWidth: 1, borderColor: colors().grid, backgroundColor: isSelected() || isEditingThis() ? colors().selected : colors().surface, ...MOTION }} hoverStyle={{ backgroundColor: isSelected() || isEditingThis() ? colors().selected : colors().hover }}>
      <View style={{ position: "absolute", left: 4, top: 10, bottom: 10, width: 2.5, borderRadius: 2, backgroundColor: isSelected() ? colors().accent : isEditingThis() ? colors().warn : "transparent" }} />
      <Button
        testID={"proxy-" + name()}
        selected={isSelected()}
        disabled={!!p.busy() || !canSelect()}
        onPress={() => { focusedNode.set(name()); void p.run("select-proxy", () => p.call("desktop.group.select", { group: selectedGroup()?.name, member: name() })); }}
        style={{ cursor: !!p.busy() || !canSelect() ? "default" : "pointer", flex: 1, minWidth: 0, flexDirection: "row", alignItems: "center", gap: 10, paddingLeft: 14, paddingRight: 6, height: ROW_HEIGHT, backgroundColor: "transparent" }}
        hoverStyle={{ backgroundColor: "transparent" }}
        activeStyle={{ backgroundColor: colors().selected }}
        focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}
        disabledStyle={{ opacity: isMissing() ? 0.6 : 0.85 }}
      >
        <Icon name={isSelected() ? "check" : "circle"} size={16} color={isSelected() ? colors().accent : colors().faint} />
        <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
          <Row gap={6} style={{ minWidth: 0 }}>
            <Text style={{ fontSize: 13, fontWeight: isSelected() ? "600" : "500", color: isSelected() ? colors().accent : colors().text, numberOfLines: 1, textOverflow: "ellipsis", flexShrink: 1, minWidth: 0 }} tooltip={name()}>{name()}</Text>
            <If when={isLocal()}><Tag text={tr("自建", "Local")} tone="accent" /></If>
            <If when={isEdited()}><Tag text={tr("已修改", "Edited")} tone="warn" /></If>
            <If when={isMissing()}><Tag text={tr("原节点已下线", "Source missing")} tone="danger" /></If>
            <If when={isProvider()}><Tag text={tr("只读", "Read-only")} tone="muted" /></If>
          </Row>
          <Text style={{ fontSize: 11, color: isSelected() ? colors().muted : colors().faint, numberOfLines: 1, textOverflow: "ellipsis" }} tooltip={subText()}>{subText()}</Text>
        </View>
        <View style={{ width: 68, alignItems: "flex-end", flexShrink: 0 }}><DelayTag item={live()} /></View>
      </Button>
      <Row gap={2} style={{ paddingRight: 8, flexShrink: 0 }}>
        <If when={canEdit()}>
          <Action testID={"edit-proxy-" + name()} size="small" quiet label={tr("编辑", "Edit")} onPress={() => void openNodeInspector(name())} />
        </If>
        <If when={isMissing()}>
          <Action size="small" quiet danger label={tr("移除修改", "Remove")} onPress={() => void deleteOrRestoreNode(meta())} />
        </If>
        <If when={canTest()}>
          <IconButton testID={"test-proxy-" + name()} size={26} icon="refresh" label={tr("测试 " + name() + " 的延迟", "Test latency for " + name())} disabled={offlinePreview()} onPress={() => void testSingleNode(name())} />
        </If>
        <If when={rowMenuItems().length > 0}>
          <MenuPopover testID={"proxy-menu-" + name()} triggerSize={26} triggerLabel={tr("更多操作", "More actions")} items={rowMenuItems()} />
        </If>
      </Row>
    </View>;
  };

  // Inspector header & body (shared between 328px inline Card and 560px Modal!)
  const inspectorTitle = () => {
    const t = inspectorTarget();
    if (!t) return "";
    if (t.kind === "node") return t.isNew ? tr("新建节点", "New proxy") : tr(`编辑节点`, `Edit proxy`);
    return t.isNew ? tr("新建策略组", "New policy group") : tr(`编辑策略组`, `Edit group`);
  };

  const inspectorSubtitle = () => {
    const t = inspectorTarget();
    if (!t) return "";
    if (t.kind === "node") {
      if (t.isNew || !t.row?.source) return tr("自建节点 · 保存后更新订阅保留", "Local proxy · survives subscription refresh");
      return t.row?.edited ? tr("订阅节点 · 本地已修改", "Subscription proxy · locally modified") : tr("订阅节点 · 仅保存修改字段", "Subscription proxy · diff-only overlay");
    }
    return t.row?.source ? tr("订阅策略组", "Subscription policy group") : tr("本地策略组", "Local policy group");
  };

  const inspectorBody = (inModal: boolean) => {
    const t = inspectorTarget();
    if (!t) return null;
    if (t.kind === "node") {
      return <View style={{ gap: 10 }}>
        <NodeEditorFields
          form={nodeForm}
          setForm={(next) => nodeForm.set(next)}
          groups={editorModel()?.groups ?? []}
          selectedGroups={nodeGroups}
          onToggleGroup={(gName, checked) => {
            const next = new Set(nodeGroups());
            if (checked) next.add(gName); else next.delete(gName);
            nodeGroups.set(next);
          }}
          showSubscriptionHint={editorModel()?.profileKind === "url"}
          testIDPrefix="inspector-node"
        />
        <If when={!t.isNew && (!t.row?.source || !!t.row?.edited || !!editorModel()?.isPureLocal)}>
          <Row justify="flex-start" style={{ paddingTop: 4 }}>
            <If when={!t.row?.source || !!editorModel()?.isPureLocal}>
              <Action quiet danger icon="trash" label={tr("删除此节点", "Delete proxy")} onPress={() => void deleteOrRestoreNode(t.row)} />
            </If>
            <If when={!!t.row?.source && !!t.row?.edited && !editorModel()?.isPureLocal}>
              <Action quiet icon="rotateCcw" label={tr("恢复订阅原值", "Reset to subscription")} onPress={() => void deleteOrRestoreNode(t.row)} />
            </If>
          </Row>
        </If>
      </View>;
    }
    return <View style={{ gap: 10 }}>
      <GroupEditorFields
        row={t.row}
        form={groupForm}
        setForm={(next) => groupForm.set(next)}
        available={availableMembersForPicker()}
        inModal={inModal}
        onRequestModalPicker={() => memberPickerOpen.set(true)}
      />
      <If when={!t.isNew}>
        <Row justify="flex-start" gap={8} style={{ paddingTop: 4 }}>
          <If when={!!t.row?.source && !!t.row?.edited}>
            <Action quiet icon="rotateCcw" label={tr("恢复策略组", "Reset group")} onPress={() => void deleteOrRestoreGroup(t.row, "restore")} />
          </If>
          <Action quiet danger icon="trash" label={tr("删除策略组", "Delete group")} onPress={() => void deleteOrRestoreGroup(t.row, "delete")} />
        </Row>
      </If>
    </View>;
  };

  // Single primary button rule: "Test group" is primary ONLY when inspector is closed & not empty state
  const testGroupIsPrimary = () => !inspectorOpen() && !isZeroProfileEmpty() && mode() !== "direct";
  const testButtonLabel = () => {
    if (testing().active) return tr(`停止 (${testing().done}/${testing().total})`, `Stop (${testing().done}/${testing().total})`);
    return tr("测试本组", "Test group");
  };

  const pickerExcludeSet = () => {
    const f = groupForm();
    const ex = new Set<string>([f.name.trim() || inspectorTarget()?.row?.name || ""]);
    if (!f.explicitMembers) for (const m of f.inheritedMembers) ex.add(m);
    return ex;
  };
  const pickerSelectedSet = () => new Set(groupForm().explicitMembers ? groupForm().members : groupForm().appendMembers);
  const togglePickerMember = (name: string) => {
    const f = groupForm();
    if (f.explicitMembers) {
      const next = f.members.includes(name) ? f.members.filter((m) => m !== name) : [...f.members, name];
      groupForm.set({ ...f, members: next });
    } else {
      const next = f.appendMembers.includes(name) ? f.appendMembers.filter((m) => m !== name) : [...f.appendMembers, name];
      groupForm.set({ ...f, appendMembers: next });
    }
  };

  return <View testID="desktop-proxies" style={{ flex: 1, minHeight: 0 }}>
    <View style={{ flex: 1, minHeight: 0, paddingHorizontal: padX(), paddingTop: padY(), paddingBottom: padY(), gap: gap() }}>
      <Heading
      title={tr("代理", "Proxies")}
      description={notice() || (offlinePreview() ? tr("内核未运行 · 离线预览（可编辑节点，启动后生效）", "Kernel stopped · offline preview (edits apply after starting)") : tr("策略组与节点选择 · 测速不会改变当前出口", "Policy groups and proxies · latency tests do not change selection"))}
      action={() => <Row gap={6} style={{ flexShrink: 0 }}>
        <Tabs testIDPrefix="proxy-mode" disabled={!!p.busy()} items={[{ id: "rule", label: tr("规则", "Rule") }, { id: "global", label: tr("全局", "Global") }, { id: "direct", label: tr("直连", "Direct") }]} value={mode()} onChange={(v) => void changeMode(v)} />
        <Action testID="add-proxy-btn" icon="plus" label={tr("添加节点", "Add proxy")} onPress={() => quickAddOpen.set(true)} />
        <If when={testGroupIsPrimary()}>
          <Action
            testID="test-group-btn"
            primary
            icon={testing().active ? "stop" : undefined}
            label={testButtonLabel()}
            disabled={!testing().active && (offlinePreview() || mode() === "direct" || !testableInList(rawMembers()).length)}
            onPress={testGroup}
          />
        </If>
        <If when={!testGroupIsPrimary()}>
          <Action
            testID="test-group-btn"
            icon={testing().active ? "stop" : undefined}
            label={testButtonLabel()}
            disabled={!testing().active && (offlinePreview() || mode() === "direct" || !testableInList(rawMembers()).length)}
            onPress={testGroup}
          />
        </If>
        <MenuPopover testID="proxies-overflow" triggerLabel={tr("更多操作", "More actions")} items={overflowItems()} />
      </Row>}
    />

    <If when={!!pageError()}><Feedback text={pageError()} danger /></If>

    {/* Direct mode quiet banner (still allows viewing All Nodes and editing!) */}
    <If when={mode() === "direct"}>
      <View style={{ paddingHorizontal: 13, paddingVertical: 9, borderRadius: 10, backgroundColor: tint(colors().accent, colorScheme() === "dark" ? "14" : "0d"), borderWidth: 1, borderColor: tint(colors().accent, colorScheme() === "dark" ? "26" : "1e"), flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 10 }}>
        <Label text={tr("当前为直连模式 · 所有流量直接连接。你仍可在下方管理节点，切换到规则或全局模式后可选择出口。", "Direct mode is active · all traffic connects directly. You can still manage nodes below; switch to Rule or Global to select an exit.")} size={12} />
        <Action size="small" label={tr("切回规则模式", "Switch to Rule")} onPress={() => void changeMode("rule")} />
      </View>
    </If>

    {/* Zero-profile empty state */}
    <If when={isZeroProfileEmpty() && mode() !== "direct"}>
      <View style={{ flex: 1, borderRadius: RADIUS.surface, backgroundColor: colors().surface, borderWidth: 1, borderColor: colors().border, boxShadow: elevation("surface") }}>
        <Empty
          icon="proxies"
          title={tr("还没有可用节点", "No proxies yet")}
          description={tr("直接粘贴 VPS 节点链接或手动填写，Gopurr 会自动创建本地档案；也可以先在“订阅”页导入订阅链接。", "Paste VPS share links or add a node manually to create a local profile automatically, or import a subscription first.")}
          action={() => <Row gap={8}>
            <Action label={tr("前往订阅", "Open profiles")} onPress={() => p.navigate("subs")} />
            <Action primary icon="plus" label={tr("添加节点", "Add proxy")} onPress={() => quickAddOpen.set(true)} />
          </Row>}
        />
      </View>
    </If>

    {/* Main 3-tier responsive workspace */}
    <If when={!isZeroProfileEmpty() || mode() === "direct"}>
      <View style={{ flex: 1, flexDirection: "row", gap: gap(), minHeight: 0 }}>
        {/* Left 168px Policy Group Rail (hidden in direct mode or when auto-collapsed to Select at 800..959 with inspector open) */}
        <If when={mode() !== "direct" && !collapseRailToSelect()}>
          <View testID="proxies-group-rail" style={{ width: 168, gap: 8, minHeight: 0, flexShrink: 0 }}>
            <Row justify="space-between" style={{ height: 28, paddingHorizontal: 4 }}>
              <Text style={{ fontSize: 12, fontWeight: "600", letterSpacing: 0.2, color: colors().muted, numberOfLines: 1 }}>{tr("策略组", "Policy groups")}</Text>
              <Text style={{ fontSize: 11, color: colors().faint, fontVariant: "tabular-nums", numberOfLines: 1 }}>{String(visibleGroups().length)}</Text>
            </Row>
            <View style={{ flex: 1, minHeight: 0, borderRadius: RADIUS.surface, backgroundColor: colors().surface, borderWidth: 1, borderColor: colors().border, boxShadow: elevation("surface"), overflow: "hidden" }}>
              <Scroll style={{ flex: 1, minHeight: 0 }} contentStyle={{ padding: 6, gap: 2 }}>
                <Each of={visibleGroups()} key={(group: any) => group.name}>{(group) => {
                  const active = () => !isAllNodesView() && selectedGroup()?.name === group().name;
                  return <View style={{ flexDirection: "row", alignItems: "center", borderRadius: 8, backgroundColor: active() ? colors().selected : "transparent", ...MOTION }} hoverStyle={{ backgroundColor: active() ? colors().selected : colors().hover }}>
                    <View style={{ position: "absolute", left: 3, top: 10, bottom: 10, width: 2.5, borderRadius: 2, backgroundColor: active() ? colors().accent : "transparent" }} />
                    <Button testID={"group-" + group().name} selected={active()} onPress={() => void switchGroup(group().name)}
                      style={{ cursor: "pointer", flex: 1, minWidth: 0, paddingLeft: 11, paddingRight: 4, paddingVertical: 7, minHeight: 50, justifyContent: "center", backgroundColor: "transparent" }}
                      focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}>
                      <View style={{ gap: 2, minWidth: 0 }}>
                        <Text style={{ fontSize: 13, fontWeight: active() ? "600" : "500", color: active() ? colors().accent : colors().text, numberOfLines: 1, textOverflow: "ellipsis" }} tooltip={group().name}>{group().name}</Text>
                        <Text style={{ fontSize: 11, color: colors().muted, numberOfLines: 1, textOverflow: "ellipsis" }} tooltip={group().now || groupTypeLabel(group().type ?? "")}>
                          {group().now ? `${group().now}` : `${groupTypeLabel(group().type ?? "")}`}
                        </Text>
                        <Text style={{ fontSize: 10, color: colors().faint, fontVariant: "tabular-nums", numberOfLines: 1 }}>
                          {`${groupTypeLabel(group().type ?? "")} · ${String(group().members?.length ?? 0)}`}
                        </Text>
                      </View>
                    </Button>
                    <If when={!!p.snapshot().activeProfile && group().name !== "GLOBAL"}>
                      <View style={{ paddingRight: 3, flexShrink: 0 }}>
                        <IconButton testID={"edit-group-" + group().name} size={24} icon="pencil" label={tr("编辑策略组 " + group().name, "Edit group " + group().name)} onPress={() => void openGroupInspector(group().name)} />
                      </View>
                    </If>
                  </View>;
                }}</Each>
              </Scroll>

              {/* Bottom hairline: Virtual "All Proxies (N)" view + "+ New group" */}
              <View style={{ borderTopWidth: 1, borderColor: colors().border, padding: 6, gap: 2 }}>
                <Button testID="group-all-nodes" selected={isAllNodesView()} onPress={() => void switchGroup(ALL_NODES_VIEW)}
                  style={{ cursor: "pointer", width: "100%", flexDirection: "row", alignItems: "center", justifyContent: "space-between", paddingHorizontal: 10, minHeight: 36, borderRadius: 8, backgroundColor: isAllNodesView() ? colors().selected : "transparent", ...MOTION }}
                  hoverStyle={{ backgroundColor: isAllNodesView() ? colors().selected : colors().hover }}
                  focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}>
                  <Row gap={6} style={{ flex: 1, minWidth: 0 }}>
                    <Icon name="proxies" size={14} color={isAllNodesView() ? colors().accent : colors().muted} />
                    <Text style={{ fontSize: 12.5, fontWeight: isAllNodesView() ? "600" : "500", color: isAllNodesView() ? colors().accent : colors().text, numberOfLines: 1 }}>{tr("全部节点", "All proxies")}</Text>
                  </Row>
                  <Text style={{ fontSize: 11, color: colors().faint, fontVariant: "tabular-nums" }}>{String(allNodesList().length)}</Text>
                </Button>
                <If when={!!p.snapshot().activeProfile}>
                  <Button testID="add-group-btn" onPress={() => void openGroupInspector()}
                    style={{ cursor: "pointer", width: "100%", flexDirection: "row", alignItems: "center", gap: 6, paddingHorizontal: 10, minHeight: 32, borderRadius: 8, backgroundColor: "transparent", ...MOTION }}
                    hoverStyle={{ backgroundColor: colors().hover }}
                    focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}>
                    <Icon name="plus" size={13.5} color={colors().muted} />
                    <Text style={{ fontSize: 12, fontWeight: "500", color: colors().muted, numberOfLines: 1 }}>{tr("新建策略组", "New group")}</Text>
                  </Button>
                </If>
              </View>
            </View>
          </View>
        </If>

        {/* Middle Node List Column */}
        <View style={{ flex: 1, minWidth: 0, gap: 8, minHeight: 0 }}>
          {/* Auto-collapsed group selector bar (when 800 <= contentWidth < 960 and inline inspector is open) */}
          <If when={mode() !== "direct" && collapseRailToSelect()}>
            <Row testID="proxies-collapsed-group-bar" gap={6} style={{ minWidth: 0 }}>
              <Select testID="collapsed-group-select" label={tr("选择策略组", "Policy group")} value={isAllNodesView() ? ALL_NODES_VIEW : (selectedGroup()?.name ?? "")} onChange={(v) => void switchGroup(v)} items={collapsedGroupItems()} />
              <If when={!isAllNodesView() && !!selectedGroup() && !!p.snapshot().activeProfile && selectedGroup()?.name !== "GLOBAL"}>
                <IconButton testID="collapsed-edit-group" size={30} icon="pencil" label={tr("编辑当前策略组", "Edit current group")} onPress={() => void openGroupInspector(selectedGroup()?.name)} />
              </If>
              <If when={!!p.snapshot().activeProfile}>
                <IconButton size={30} icon="plus" label={tr("新建策略组", "New group")} onPress={() => void openGroupInspector()} />
              </If>
              <View style={{ flex: 1, minWidth: 0 }} />
              <Label text={paneCaption()} muted size={11.5} lines={1} tooltip={paneCaption()} />
            </Row>
          </If>

          {/* Standard pane header when rail is visible */}
          <If when={!collapseRailToSelect()}>
            <View style={{ gap: 2, paddingHorizontal: 2, minWidth: 0 }}>
              <Text style={{ fontSize: 15.5, fontWeight: "600", letterSpacing: -0.2, color: colors().text, numberOfLines: 1, textOverflow: "ellipsis" }} tooltip={paneTitle()}>{paneTitle()}</Text>
              <Text style={{ fontSize: 11.5, color: colors().muted, numberOfLines: 1, textOverflow: "ellipsis" }} tooltip={paneCaption()}>{paneCaption()}</Text>
            </View>
          </If>

          {/* Search + Filter + Sort bar */}
          <Row gap={6}>
            <View style={{ flex: 1, minWidth: 0 }}>
              <SearchField testID="proxy-search" value={query()} onChange={(v) => query.set(v)} placeholder={tr("搜索节点名、协议或服务器", "Search proxy, protocol or host")} />
            </View>
            <Select testID="proxy-filter" label={tr("筛选节点", "Filter proxies")} value={filterKind()} onChange={(v) => filterKind.set(v)} items={filterItems()} />
            <Select testID="proxy-sort" label={tr("节点排序", "Proxy order")} value={sort()} onChange={(v) => sort.set(v)} items={[{ value: "original", title: tr("原始顺序", "Original") }, { value: "delay", title: tr("延迟最低", "Latency") }, { value: "name", title: tr("名称排序", "Name") }]} />
          </Row>

          {/* Virtualized Node List Card */}
          <View style={{ flex: 1, minHeight: 0, borderRadius: RADIUS.surface, backgroundColor: colors().surface, borderWidth: 1, borderColor: colors().border, boxShadow: elevation("surface"), overflow: "hidden" }}>
            <View style={{ flexDirection: "row", alignItems: "center", height: 30, paddingLeft: 14, paddingRight: 102, borderBottomWidth: 1, borderColor: colors().border }}>
              <Text style={{ fontSize: 10.5, fontWeight: "500", letterSpacing: 0.3, color: colors().faint, numberOfLines: 1 }}>{tr("节点", "Proxies")}</Text>
              <View style={{ flex: 1, minWidth: 0 }} />
              <View style={{ width: 68, alignItems: "flex-end" }}><Text style={{ fontSize: 10.5, fontWeight: "500", letterSpacing: 0.3, color: colors().faint, numberOfLines: 1 }}>{tr("延迟", "Latency")}</Text></View>
            </View>
            <List
              testID="proxies-list"
              each={listMembers()}
              keyExtractor={(v: any) => v.name}
              style={{ flex: 1, minHeight: 0 }}
              empty={<View style={{ flex: 1, justifyContent: "center" }}><Empty icon="search" title={tr("没有匹配节点", "No matching proxies")} description={tr("试试调整搜索词或筛选条件。", "Try adjusting your search or filter.")} action={() => <Action label={tr("重置筛选", "Reset filters")} onPress={() => { query.set(""); filterKind.set("all"); }} />} /></View>}
            >
              {(item) => nodeRow(item)}
            </List>
            <View style={{ flexDirection: "row", alignItems: "center", height: 28, paddingHorizontal: 14, borderTopWidth: 1, borderColor: colors().border, backgroundColor: colorScheme() === "dark" ? tint(colors().surface, "00") : tint(colors().bg, "55") }}>
              <Text style={{ fontSize: 11, color: colors().faint, numberOfLines: 1, textOverflow: "ellipsis", fontVariant: "tabular-nums" }}>
                {tr("显示", "Showing") + " " + String(listMembers().length) + " / " + String(rawMembers().length) + (testing().active ? ` · ${tr("正在测速", "Testing")} ${testing().done}/${testing().total}` : "")}
              </Text>
            </View>
          </View>
        </View>

        {/* Right 328px On-Demand Inline Inspector Card (contentWidth >= 800) */}
        <If when={inlineInspector()}>
          <View testID="proxy-inline-inspector" style={{ width: 328, flexShrink: 0, minHeight: 0, borderRadius: RADIUS.surface, backgroundColor: colors().surface, borderWidth: 1, borderColor: colors().border, boxShadow: elevation("surface"), overflow: "hidden" }}>
            <Row justify="space-between" gap={8} style={{ paddingLeft: 14, paddingRight: 8, paddingVertical: 10, borderBottomWidth: 1, borderColor: colors().border }}>
              <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
                <Text style={{ fontSize: 14, fontWeight: "600", letterSpacing: -0.2, color: colors().text, numberOfLines: 1, textOverflow: "ellipsis" }}>{inspectorTitle()}</Text>
                <Text style={{ fontSize: 11, color: colors().muted, numberOfLines: 1, textOverflow: "ellipsis" }}>{inspectorSubtitle()}</Text>
              </View>
              <IconButton testID="proxy-inspector-close" icon="x" label={tr("关闭检查器", "Close inspector")} onPress={() => void closeInspector()} />
            </Row>
            <Scroll style={{ flex: 1, minHeight: 0 }} contentStyle={{ padding: 14, gap: 12 }}>
              {inspectorBody(false)}
            </Scroll>
            <View style={{ paddingHorizontal: 14, paddingBottom: 12 }}>
              <SaveBar dirty={inspectorDirty()} busy={inspectorSaving()} error={inspectorError()} onSave={() => void saveInspector()} onDiscard={discardInspector} />
            </View>
          </View>
        </If>
      </View>
    </If>
    </View>

    {/* Narrow viewport (<800 contentWidth, e.g. 840x560 expanded sidebar) single-layer Modal Inspector */}
    <Modal
      testID="proxy-inspector-modal"
      closeTestID="proxy-inspector-modal-close"
      title={inspectorTitle()}
      open={modalInspector()}
      onClose={() => void closeInspector()}
      width={560}
      error={inspectorError()}
      footer={() => <SaveBar dirty={inspectorDirty()} busy={inspectorSaving()} onSave={() => void saveInspector()} onDiscard={discardInspector} />}
    >
      <Label text={inspectorSubtitle()} muted size={12} />
      {inspectorBody(true)}
    </Modal>

    {/* Single-layer Member Picker Modal (ONLY opened from inline inspector when contentWidth >= 800; ZERO nested modals!) */}
    <Modal
      testID="group-member-picker-modal"
      closeTestID="group-member-picker-close"
      title={groupForm().explicitMembers ? tr("选择策略组成员", "Select group members") : tr("追加本地成员", "Append local members")}
      open={memberPickerOpen() && inlineInspector()}
      onClose={() => memberPickerOpen.set(false)}
      width={520}
      footer={() => <Row justify="space-between">
        <Label text={tr(`已选 ${pickerSelectedSet().size} 项`, `${pickerSelectedSet().size} selected`)} muted size={12} tabular />
        <Action primary label={tr("完成", "Done")} onPress={() => memberPickerOpen.set(false)} />
      </Row>}
    >
      <MemberPickerList
        available={availableMembersForPicker()}
        excludeNames={pickerExcludeSet()}
        selected={pickerSelectedSet}
        onToggle={togglePickerMember}
        height={290}
      />
    </Modal>

    {/* Quick Add Node Modal (single-layer, works even at 0 profiles!) */}
    <QuickAddNodeModal
      open={quickAddOpen()}
      onClose={() => quickAddOpen.set(false)}
      page={p}
      activeProfileId={String(p.snapshot().activeProfile ?? "")}
      activeProfileKind={activeProfile()?.kind}
      defaultGroupName={selectedGroup()?.name}
      groups={visibleGroups()}
      onSaved={(msg) => {
        notice.set(msg);
        setTimeout(() => notice.set(""), 4000);
        void loadEditorModel(String(p.snapshot().activeProfile ?? ""), true);
      }}
    />
  </View>;
}
