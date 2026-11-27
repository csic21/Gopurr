import { colorScheme, effect, Scroll, Text, View, signal, windowSize } from "nsh";
import { Button } from "nsh/base";
import { colors } from "./theme";
import { tr } from "./i18n";
import { nativeCall, pendingImport, pendingProxiesFilter } from "./client";
import {
  Action, CheckRow, Collapsible, Empty, Feedback, Field, Group, GroupRow, Heading, Icon,
  IconButton, Label, Modal, QuotaBar, RADIUS, Row, Section, Select, Tabs, Tag, Toggle,
  confirmDialog, elevation, fmtBytes, tint,
} from "./ui";
import { watchDesktopDraft } from "./drafts";
import {
  autoUpdateSummary, detectInputKind, fmtExpiryCountdown, fmtRelativeTime,
} from "./proxy-helpers";
import {
  ProfileProxyManagerModal, buildAppendNodesEdits,
  type ProxyEditorModel,
} from "./proxy-editor-panel";
import type { PageProps } from "./types";

export function profileSource(value: any): string {
  if (value?.isPureLocal) return tr("纯本地档案", "Local profile");
  const raw = value?.source ?? value?.url ?? value?.path ?? "";
  try { return new URL(raw).hostname; } catch { return raw.split(/[\\/]/).pop() ?? raw; }
}

function profileIcon(value: any): string {
  return value?.kind === "file" ? "profiles" : value?.kind === "aggregate" ? "mesh" : "subs";
}

const REGION_PRESETS = [
  { id: "hk", labelZh: "香港", labelEn: "HK", token: "香港|HK|Hong\\s*Kong" },
  { id: "jp", labelZh: "日本", labelEn: "JP", token: "日本|JP|Japan|Tokyo|Osaka" },
  { id: "sg", labelZh: "新加坡", labelEn: "SG", token: "新加坡|SG|Singapore" },
  { id: "us", labelZh: "美国", labelEn: "US", token: "美国|US|United\\s*States" },
  { id: "tw", labelZh: "台湾", labelEn: "TW", token: "台湾|TW|Taiwan" },
];
const EXCLUDE_INFO_REGEX = "剩余流量|套餐到期|官网|到期时间|重置|过期|expire|traffic|website";
const UA_PRESETS = [
  { value: "", title: "Default (clash-verge / mihomo)" },
  { value: "clash.meta", title: "clash.meta" },
  { value: "ClashForAndroid/2.5.12", title: "ClashForAndroid" },
  { value: "v2rayN/6.0", title: "v2rayN" },
];

// Module-level signals survive theme switches and 1s snapshot polling
const adding = signal(false);
const editing = signal<any | null>(null);
const deleting = signal<any | null>(null);
const aggregate = signal(false);
const proxyManagerProfile = signal<any | null>(null);

// Unified Smart Import state
const name = signal("");
const url = signal("");
const importMode = signal<"auto" | "nodes" | "url">("auto");
const importUA = signal("");
const importUseProxy = signal(false);
const importActivate = signal(false);
const importFetchOptsOpen = signal(false);
const importDest = signal<"local" | "append">("local");
const parseResult = signal<any>(null);
const parseSelected = signal<Set<number>>(new Set());
const parseErrorsOpen = signal(false);
const queuedDeepLink = signal<{ url: string; name?: string } | null>(null);

// Profile Settings state
const draft = signal<any>({});
const content = signal("");
const autoUpdatePreset = signal("global");
const customHours = signal("");
const showConfigYaml = signal(false);

// Aggregation Editor state
const aggId = signal("");
const aggBase = signal("");
const sourceOrder = signal<string[]>([]);
const sources = signal<Record<string, boolean>>({});
const prefixes = signal<Record<string, string>>({});
const include = signal("");
const exclude = signal("");
const dedupe = signal(true);
const aggPreview = signal<{ stats: { input: number; matched: number; output: number; duplicates: number }; names: string[]; truncated: boolean } | null>(null);
const aggPreviewLoading = signal(false);
const aggPreviewErr = signal("");

// Errors & Feedback
const localError = signal("");
const pageNotice = signal("");
const cardErrors = signal<Record<string, string>>({});
const expandedCardErrors = signal<Record<string, boolean>>({});

export function ProfilesPage(p: PageProps) {
  const aggSignature = () => JSON.stringify({
    name: name(),
    order: sourceOrder(),
    sources: sources(),
    prefixes: prefixes(),
    include: include(),
    exclude: exclude(),
    dedupe: dedupe(),
  });

  const isAddingDirty = () => adding() && !!(name().trim() || url().trim());
  const isEditingDirty = () => !!editing() && JSON.stringify(draft()) !== JSON.stringify(editing());
  const isAggDirty = () => aggregate() && aggSignature() !== aggBase();

  watchDesktopDraft("profiles", () => isAddingDirty() || isEditingDirty() || isAggDirty());

  const rows = () => p.snapshot().profiles ?? [];
  const activeRows = () => rows().filter((v: any) => !!v.active);
  const otherRows = () => rows().filter((v: any) => !v.active);
  const activeProfile = () => activeRows()[0] ?? null;
  const nonAggregateRows = () => rows().filter((v: any) => v.id !== aggId() && v.kind !== "aggregate");
  const hasUrlProfiles = () => rows().some((v: any) => v.kind === "url");
  const globalAutoHours = () => Number(p.snapshot().prefs?.autoUpdateHours ?? 0);

  const compact = () => windowSize().height < 660;
  const narrow = () => windowSize().width < 960;
  const padX = () => compact() || narrow() ? 16 : 24;
  const padY = () => compact() ? 12 : 20;
  const gap = () => compact() ? 12 : 16;
  const dark = () => colorScheme() === "dark";

  const showNotice = (msg: string) => {
    pageNotice.set(msg);
    setTimeout(() => { if (pageNotice() === msg) pageNotice.set(""); }, 4500);
  };

  const resetModals = () => {
    adding.set(false);
    editing.set(null);
    aggregate.set(false);
    localError.set("");
    showConfigYaml.set(false);
  };

  const guardedClose = async () => {
    const dirty = isAddingDirty() || isEditingDirty() || isAggDirty();
    if (dirty) {
      const ok = await confirmDialog({
        title: tr("放弃未保存的更改？", "Discard unsaved changes?"),
        description: tr("当前编辑器中还有未保存的内容。", "Your current form has unsaved changes."),
        confirmLabel: tr("放弃更改", "Discard"),
        cancelLabel: tr("继续编辑", "Keep editing"),
        destructive: true,
      });
      if (!ok) return;
    }
    resetModals();
  };

  const perform = async (key: string, fn: () => Promise<any>) => {
    localError.set("");
    await p.run(key, async () => {
      try {
        await fn();
        resetModals();
      } catch (e) {
        localError.set(String(e instanceof Error ? e.message : e));
        throw e;
      }
    });
  };

  // Smart Import debounced parser
  let parseTimer: any = null;
  let parseSeq = 0;
  const triggerParse = (text: string, mode: "auto" | "nodes" | "url") => {
    clearTimeout(parseTimer);
    if (!text.trim()) {
      parseResult.set(null);
      parseSelected.set(new Set());
      return;
    }
    const seq = ++parseSeq;
    parseTimer = setTimeout(async () => {
      try {
        const res = await p.call("desktop.proxies.parse", { text, mode });
        if (seq !== parseSeq) return;
        parseResult.set(res);
        const nodes: any[] = res?.nodes ?? [];
        parseSelected.set(new Set(nodes.map((_, i) => i)));
      } catch {
        // Fallback to client heuristic silently
      }
    }, 200);
  };

  const openAdd = (prefill?: { url?: string; name?: string }) => {
    name.set(prefill?.name ?? "");
    url.set(prefill?.url ?? "");
    importMode.set("auto");
    importUA.set("");
    importUseProxy.set(false);
    // 0 profiles -> default activate on first import; existing profiles -> keep import & activate separate
    importActivate.set(rows().length === 0);
    importFetchOptsOpen.set(false);
    importDest.set(activeProfile() ? "append" : "local");
    parseResult.set(null);
    parseSelected.set(new Set());
    parseErrorsOpen.set(false);
    localError.set("");
    adding.set(true);
    if (prefill?.url) triggerParse(prefill.url, "auto");
  };

  // Deep-link pendingImport: queue safely if another modal has active draft instead of aborting with error
  effect(() => {
    const incoming = pendingImport();
    if (!incoming) return;
    pendingImport.set(null);
    if (adding() || editing() || aggregate()) {
      queuedDeepLink.set(incoming);
      return;
    }
    openAdd(incoming);
  });

  // Aggregation Editor + 300ms debounced lightweight preview (includeYaml: false)
  let aggPreviewTimer: any = null;
  let aggPreviewSeq = 0;

  const currentAggSpec = () => {
    const orderedIds = sourceOrder().filter((id) => sources()[id] && rows().some((r: any) => r.id === id && r.kind !== "aggregate" && r.id !== aggId()));
    return {
      sources: orderedIds.map((id) => ({ id, prefix: prefixes()[id] ?? "" })),
      include: include(),
      exclude: exclude(),
      dedupe: dedupe(),
    };
  };

  const scheduleAggPreview = () => {
    if (!aggregate()) return;
    clearTimeout(aggPreviewTimer);
    const spec = currentAggSpec();
    if (!spec.sources.length) {
      aggPreview.set(null);
      aggPreviewErr.set("");
      aggPreviewLoading.set(false);
      return;
    }
    const seq = ++aggPreviewSeq;
    aggPreviewLoading.set(true);
    aggPreviewTimer = setTimeout(async () => {
      try {
        const res = await p.call<any>("desktop.profiles.aggregatePreview", { spec, includeYaml: false });
        if (seq !== aggPreviewSeq) return;
        aggPreview.set({
          stats: res?.stats ?? { input: 0, matched: 0, output: 0, duplicates: 0 },
          names: res?.names ?? [],
          truncated: !!res?.truncated,
        });
        aggPreviewErr.set("");
      } catch (e) {
        if (seq === aggPreviewSeq) {
          aggPreview.set(null);
          aggPreviewErr.set(e instanceof Error ? e.message : String(e));
        }
      } finally {
        if (seq === aggPreviewSeq) aggPreviewLoading.set(false);
      }
    }, 300);
  };

  effect(() => {
    if (!aggregate()) return;
    // Track reactive dependencies for debounced preview
    void sourceOrder();
    void sources();
    void prefixes();
    void include();
    void exclude();
    void dedupe();
    scheduleAggPreview();
  });

  const openAgg = (profile?: any) => {
    const candidates = rows().filter((v: any) => v.id !== (profile?.id ?? "") && v.kind !== "aggregate").map((v: any) => v.id);
    const existingSrcs: any[] = profile?.aggregate?.sources ?? [];
    const existingIds = existingSrcs.map((s: any) => s.id).filter((id: string) => candidates.includes(id));
    const rest = candidates.filter((id: string) => !existingIds.includes(id));
    aggId.set(profile?.id ?? "");
    name.set(profile?.name ?? "");
    sourceOrder.set([...existingIds, ...rest]);
    sources.set(Object.fromEntries(existingSrcs.map((s: any) => [s.id, true])));
    prefixes.set(Object.fromEntries(existingSrcs.map((s: any) => [s.id, s.prefix ?? ""])));
    include.set(profile?.aggregate?.include ?? "");
    exclude.set(profile?.aggregate?.exclude ?? "");
    dedupe.set(profile?.aggregate?.dedupe ?? true);
    aggPreview.set(null);
    aggPreviewErr.set("");
    localError.set("");
    aggBase.set(aggSignature());
    aggregate.set(true);
  };

  const moveSourceOrder = (id: string, dir: number) => {
    const list = [...sourceOrder()];
    const idx = list.indexOf(id);
    if (idx < 0 || idx + dir < 0 || idx + dir >= list.length) return;
    [list[idx], list[idx + dir]] = [list[idx + dir], list[idx]];
    sourceOrder.set(list);
  };

  const toggleRegionChip = (token: string) => {
    const cur = include().trim();
    const parts = cur ? cur.split("|").map((s) => s.trim()).filter(Boolean) : [];
    const tokenParts = token.split("|");
    const hasAll = tokenParts.every((tp) => parts.includes(tp));
    if (hasAll) {
      include.set(parts.filter((p) => !tokenParts.includes(p)).join("|"));
    } else {
      include.set([...new Set([...parts, ...tokenParts])].join("|"));
    }
  };

  const toggleExcludeInfoChip = () => {
    if (exclude().includes("剩余流量")) exclude.set("");
    else exclude.set(exclude().trim() ? `${exclude().trim()}|${EXCLUDE_INFO_REGEX}` : EXCLUDE_INFO_REGEX);
  };

  // Profile Settings open & auto-update preset wiring
  const editProfile = (id: string) => void p.run("edit-load", async () => {
    const v = await p.call("desktop.profiles.get", { id });
    if (v.aggregate) { openAgg(v); return; }
    draft.set({ ...v });
    content.set(v.content ?? "");
    const h = v.updateHours;
    if (h === null || h === undefined) { autoUpdatePreset.set("global"); customHours.set(""); }
    else if (h === 0) { autoUpdatePreset.set("0"); customHours.set(""); }
    else if (h === 6 || h === 12 || h === 24) { autoUpdatePreset.set(String(h)); customHours.set(""); }
    else { autoUpdatePreset.set("custom"); customHours.set(String(h)); }
    showConfigYaml.set(false);
    editing.set(v);
    localError.set("");
  });

  const changeDraft = (key: string, v: any) => draft.set({ ...draft(), [key]: v });
  const setAutoUpdatePreset = (preset: string) => {
    autoUpdatePreset.set(preset);
    if (preset === "global") changeDraft("updateHours", null);
    else if (preset === "custom") changeDraft("updateHours", Number(customHours()) || 12);
    else changeDraft("updateHours", Number(preset));
  };

  const importFile = () => void p.run("import-file", async () => {
    const result = await nativeCall("desktop.file.open", { kind: "profile" });
    if (result?.path) {
      await p.call("desktop.profiles.import", { path: result.path, activate: rows().length === 0 });
      if (adding()) resetModals();
      showNotice(tr("已从本地文件导入配置", "Profile imported from file"));
    }
  });

  const exportProfileYaml = async (item: any, yamlText: string) => {
    const res = await nativeCall("desktop.file.save", { kind: "profile", name: `${item.name || "profile"}.yaml`, content: yamlText });
    if (res?.path) showNotice(tr("配置已导出", "Configuration exported"));
  };

  // Single profile update with delta feedback & per-card error capture
  const updateSingleProfile = async (item: any, forceProxy = false) => {
    const beforeCount = Number(item.nodeCount ?? 0);
    const localCount = Number(item.localNodeCount ?? 0);
    const editedCount = Number(item.editedNodeCount ?? 0);
    const nextErrs = { ...cardErrors() };
    delete nextErrs[item.id];
    cardErrors.set(nextErrs);

    await p.run("profile-update", async () => {
      try {
        if (forceProxy && item.kind === "url" && !item.useProxy) {
          const full = await p.call("desktop.profiles.get", { id: item.id });
          await p.call("desktop.profiles.edit", { profile: { ...full, useProxy: true } });
        }
        await p.call("desktop.profiles.update", { id: item.id });
        await p.refresh();
        const after = (p.snapshot().profiles ?? []).find((x: any) => x.id === item.id);
        const afterCount = Number(after?.nodeCount ?? beforeCount);
        const delta = afterCount - beforeCount;
        const deltaText = delta === 0 ? "" : delta > 0 ? ` (+${delta})` : ` (${delta})`;
        const overlayNote = localCount || editedCount
          ? tr(`，已保留 ${localCount} 自建 · ${editedCount} 修改`, `, kept ${localCount} local · ${editedCount} edited`)
          : "";
        showNotice(tr(`已更新“${item.name}”：${beforeCount} → ${afterCount} 节点${deltaText}${overlayNote}`, `Updated “${item.name}”: ${beforeCount} → ${afterCount} proxies${deltaText}${overlayNote}`));
      } catch (e) {
        const msg = e instanceof Error ? e.message : String(e);
        cardErrors.set({ ...cardErrors(), [item.id]: msg });
      }
    });
  };

  const updateAllProfiles = () => void p.run("profiles-update-all", async () => {
    await p.call("desktop.profiles.updateAll");
    showNotice(tr("已更新全部远程订阅，本地节点与修改均已保留", "Updated all remote subscriptions; local nodes & edits preserved"));
  });

  // Smart Import submit handler
  const detectedKind = (): "empty" | "url" | "nodes" | "clash_yaml" | "unknown" => {
    if (importMode() === "url" && /^https?:\/\//i.test(url().trim())) return "url";
    if (parseResult()?.kind && parseResult().kind !== "empty") return parseResult().kind;
    return detectInputKind(url());
  };

  const parsedImportNodes = (): any[] => parseResult()?.nodes ?? [];
  const chosenImportNodes = () => parsedImportNodes().filter((_, i) => parseSelected().has(i));
  const importFailures = (): any[] => parseResult()?.failures ?? [];

  const submitSmartImport = () => {
    const raw = url().trim();
    const kind = detectedKind();
    if (!raw) return;

    if (kind === "nodes") {
      const nodes = chosenImportNodes();
      if (!nodes.length) {
        localError.set(tr("请勾选至少一个有效节点", "Select at least one valid proxy"));
        return;
      }
      void perform("profile-import-nodes", async () => {
        if (importDest() === "append" && activeProfile()) {
          const model = await p.call<ProxyEditorModel>("desktop.proxies.editor", { id: activeProfile().id });
          const defaultGroup = (model.groups ?? []).find((g: any) => !g.deleted && ["select", "selector"].includes(String(g.type).toLowerCase()) && g.name !== "GLOBAL")?.name;
          const { edits, addedNames } = buildAppendNodesEdits(model, nodes, defaultGroup ? [defaultGroup] : []);
          await p.call("desktop.proxies.save", { id: model.id, edits });
          showNotice(tr(`已将 ${addedNames.length} 个自建节点追加到“${activeProfile().name}”（更新订阅保留）`, `Appended ${addedNames.length} local proxies to “${activeProfile().name}” (survives refresh)`));
        } else {
          await p.call("desktop.profiles.createLocal", {
            name: name().trim() || tr("本地节点", "Local Nodes"),
            nodes,
            activate: importActivate(),
          });
          showNotice(tr(`已创建本地档案（含 ${nodes.length} 个节点）`, `Created local profile with ${nodes.length} proxies`));
        }
      });
      return;
    }

    if (kind === "clash_yaml") {
      void perform("profile-import-yaml", async () => {
        await p.call("desktop.profiles.import", {
          name: name().trim(),
          content: parseResult()?.yaml || raw,
          activate: importActivate(),
        });
        showNotice(tr("已导入 Clash YAML 配置", "Clash YAML profile imported"));
      });
      return;
    }

    // Subscription URL import (with initial User-Agent & UseProxy wired!)
    void perform("profile-import", async () => {
      await p.call("desktop.profiles.import", {
        name: name().trim(),
        url: parseResult()?.url || raw,
        ...(importUA().trim() ? { userAgent: importUA().trim() } : {}),
        ...(importUseProxy() ? { useProxy: true } : {}),
        activate: importActivate(),
      });
      showNotice(tr("订阅已导入", "Subscription imported"));
    });
  };

  const openOverlayJump = (item: any) => {
    if (item.active) {
      pendingProxiesFilter.set("local_edited");
      p.navigate("proxies");
    } else {
      proxyManagerProfile.set(item);
    }
  };

  // Overlay summary pill (`+N 自建 · M 修改`)
  const overlayPill = (item: any) => {
    const localN = Number(item.localNodeCount ?? 0);
    const editN = Number(item.editedNodeCount ?? 0);
    const editG = Number(item.editedGroupCount ?? 0);
    if (!localN && !editN && !editG) return null;
    const parts = [
      localN ? tr(`+${localN} 自建`, `+${localN} local`) : "",
      editN ? tr(`${editN} 修改`, `${editN} edited`) : "",
      !localN && !editN && editG ? tr(`${editG} 组修改`, `${editG} groups`) : "",
    ].filter(Boolean);
    return <Button
      testID={`overlay-pill-${item.id}`}
      onPress={() => openOverlayJump(item)}
      style={{ cursor: "pointer", height: 22, paddingHorizontal: 8, borderRadius: 6, flexDirection: "row", alignItems: "center", gap: 4, backgroundColor: tint(colors().accent, dark() ? "18" : "10"), borderWidth: 1, borderColor: tint(colors().accent, dark() ? "30" : "22") }}
      hoverStyle={{ backgroundColor: tint(colors().accent, dark() ? "26" : "1c") }}
      focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}
    >
      <Text style={{ fontSize: 10.5, fontWeight: "600", color: colors().accent, numberOfLines: 1 }}>{parts.join(" · ")}</Text>
    </Button>;
  };

  const metaLine = (item: any): string => {
    const parts: string[] = [];
    if (item.kind === "aggregate") parts.push(tr("聚合配置 · 本地缓存", "Aggregate · cached sources"));
    else parts.push(profileSource(item));
    if (item.nodeCount > 0) parts.push(tr(`${item.nodeCount} 节点`, `${item.nodeCount} proxies`));
    if (item.updated) parts.push(fmtRelativeTime(item.updated));
    if (item.kind === "url") parts.push(autoUpdateSummary(item.updateHours, globalAutoHours()));
    return parts.filter(Boolean).join(" · ");
  };

  const quotaSection = (item: any) => {
    if (item.kind !== "url" || !item.info?.total) return null;
    const used = Number(item.info.upload ?? 0) + Number(item.info.download ?? 0);
    const total = Number(item.info.total ?? 0);
    const ratio = total > 0 ? used / total : 0;
    const pct = Math.min(100, Math.round(ratio * 100));
    const expiry = fmtExpiryCountdown(item.info.expire);
    const tone: "accent" | "warn" | "danger" = ratio >= 0.98 || expiry?.expired ? "danger" : ratio >= 0.85 || expiry?.soon ? "warn" : "accent";
    return <View style={{ gap: 5 }}>
      <QuotaBar testID={`quota-bar-${item.id}`} ratio={ratio} tone={tone} label={tr("流量使用率", "Traffic quota")} />
      <Row justify="space-between" gap={8}>
        <Text style={{ fontSize: 11, color: tone === "danger" ? colors().danger : colors().muted, fontVariant: "tabular-nums", numberOfLines: 1 }}>
          {`${fmtBytes(used)} / ${fmtBytes(total)} (${pct}%)`}
        </Text>
        <If when={!!expiry}>
          <Text style={{ fontSize: 11, color: expiry?.expired ? colors().danger : expiry?.soon ? colors().warn : colors().faint, fontVariant: "tabular-nums", numberOfLines: 1 }}>
            {expiry?.text ?? ""}
          </Text>
        </If>
      </Row>
    </View>;
  };

  const cardErrorBox = (item: any) => {
    const errText = cardErrors()[item.id] || item.lastError || "";
    if (!errText) return null;
    const expanded = !!expandedCardErrors()[item.id];
    return <View testID={`profile-error-${item.id}`} style={{ paddingHorizontal: 11, paddingVertical: 8, borderRadius: 8, backgroundColor: tint(colors().danger, "10"), borderWidth: 1, borderColor: tint(colors().danger, "28"), gap: 6 }}>
      <Row justify="space-between" gap={8}>
        <Text style={{ flex: 1, minWidth: 0, fontSize: 11.5, color: colors().danger, numberOfLines: expanded ? 6 : 1, textOverflow: "ellipsis" }}>{errText}</Text>
        <Row gap={4} style={{ flexShrink: 0 }}>
          <Action size="small" quiet label={tr("重试", "Retry")} disabled={!!p.busy()} onPress={() => void updateSingleProfile(item, false)} />
          <If when={item.kind === "url"}>
            <Action size="small" quiet label={tr("通过代理重试", "Retry via proxy")} disabled={!!p.busy()} onPress={() => void updateSingleProfile(item, true)} />
          </If>
          <Action size="small" quiet label={expanded ? tr("收起", "Hide") : tr("详情", "Details")} onPress={() => expandedCardErrors.set({ ...expandedCardErrors(), [item.id]: !expanded })} />
        </Row>
      </Row>
    </View>;
  };

  const iconTile = (icon: string, size: number, active = false) => (
    <View style={{ width: size, height: size, borderRadius: 9, alignItems: "center", justifyContent: "center", backgroundColor: active ? tint(colors().accent, dark() ? "18" : "12") : (dark() ? "#ffffff08" : colors().hover), borderWidth: active ? 0 : 1, borderColor: colors().border, flexShrink: 0 }}>
      <Icon name={icon} size={size > 28 ? 16 : 15} color={active ? colors().accent : colors().muted} />
    </View>
  );

  const rowActions = (item: any) => <Row gap={4} style={{ flexShrink: 0 }}>
    <If when={item.kind === "url" || (item.kind === "file" && item.hasPath)}>
      <Action size="small" quiet label={item.kind === "file" ? tr("重新读取", "Reload") : tr("更新", "Update")} disabled={!!p.busy()} onPress={() => void updateSingleProfile(item)} />
    </If>
    <Action size="small" quiet testID={"edit-profile-" + item.id} label={tr("设置…", "Settings…")} disabled={!!p.busy()} onPress={() => editProfile(item.id)} />
    <If when={!item.active}>
      <Action size="small" label={tr("启用", "Activate")} disabled={!!p.busy()} onPress={() => void p.run("profile-activate", async () => { await p.call("desktop.profiles.activate", { id: item.id }); showNotice(tr(`已切换至“${item.name}”`, `Activated “${item.name}”`)); })} />
    </If>
    <Action size="small" quiet danger label={tr("删除…", "Delete…")} disabled={!!p.busy() || !!item.active} onPress={() => deleting.set(item)} />
  </Row>;

  const heading = () => <Heading
    title={tr("订阅", "Profiles")}
    description={pageNotice() || tr(`${rows().length} 个配置 · 本地自建节点与修改在更新订阅时自动保留`, `${rows().length} profiles · local nodes and edits survive subscription updates`)}
    action={() => <Row gap={6} style={{ flexShrink: 0 }}>
      <If when={rows().length > 0}>
        <Action testID="update-all-profiles" icon="refresh" label={tr("全部更新", "Update all")} disabled={!hasUrlProfiles() || !!p.busy()} onPress={updateAllProfiles} />
        <Action testID="add-aggregate" label={tr("新建聚合…", "New aggregate…")} onPress={() => openAgg()} disabled={!nonAggregateRows().length || !!p.busy()} />
        <Action testID="add-profile" primary icon="plus" label={tr("添加订阅", "Add profile")} onPress={() => openAdd()} />
      </If>
    </Row>}
  />;

  const currentCard = (item: any) => <View style={{ borderRadius: RADIUS.surface, backgroundColor: colors().surface, borderWidth: 1, borderColor: tint(colors().accent, dark() ? "45" : "4d"), boxShadow: elevation("surface"), overflow: "hidden", flexDirection: "row" }}>
    <View style={{ width: 3.5, backgroundColor: colors().accent }} />
    <View style={{ flex: 1, minWidth: 0, paddingHorizontal: 18, paddingVertical: 15, gap: 10 }}>
      <Row gap={12} style={{ minWidth: 0 }}>
        {iconTile(profileIcon(item), 32, true)}
        <View style={{ flex: 1, minWidth: 0, gap: 3 }}>
          <Row gap={7} style={{ minWidth: 0 }}>
            <Text style={{ fontSize: 16, fontWeight: "600", letterSpacing: -0.2, color: colors().text, numberOfLines: 1, textOverflow: "ellipsis", flexShrink: 1, minWidth: 0 }} tooltip={item.name}>{item.name}</Text>
            <Tag text={tr("使用中", "Active")} tone="accent" />
            {overlayPill(item)}
          </Row>
          <Text style={{ fontSize: 11.5, color: colors().muted, numberOfLines: 1, textOverflow: "ellipsis" }} tooltip={metaLine(item)}>{metaLine(item)}</Text>
        </View>
        {rowActions(item)}
      </Row>
      <If when={item.kind === "url" && !!item.info?.total}><View style={{ paddingLeft: 44 }}>{quotaSection(item)}</View></If>
      <If when={!!(cardErrors()[item.id] || item.lastError)}><View style={{ paddingLeft: 44 }}>{cardErrorBox(item)}</View></If>
    </View>
  </View>;

  const emptyState = () => <View style={{ flex: 1, minHeight: 240, borderRadius: RADIUS.surface, backgroundColor: colors().surface, borderWidth: 1, borderColor: colors().border, boxShadow: elevation("surface") }}>
    <Empty
      icon="subs"
      title={tr("添加第一个订阅或自建节点", "Add your first profile or VPS nodes")}
      description={tr("支持订阅链接、VPS 节点链接（VLESS / VMess / Trojan / SS / Hysteria2 / TUIC / WireGuard）、Base64 与 Clash YAML。", "Supports subscription URLs, VPS share links (VLESS, VMess, Trojan, SS, Hysteria2, TUIC, WireGuard), Base64 and Clash YAML.")}
      action={() => <Row gap={8}>
        <Action label={tr("导入文件…", "Import file…")} onPress={importFile} />
        <Action testID="add-profile" primary icon="plus" label={tr("添加订阅", "Add profile")} onPress={() => openAdd()} />
      </Row>}
    />
  </View>;

  // Profile Settings overlay summary card
  const editingProfileMeta = () => rows().find((r: any) => r.id === editing()?.id) ?? editing();
  const autoUpdateItems = () => [
    { value: "global", title: globalAutoHours() > 0 ? tr(`跟随全局（当前：每 ${globalAutoHours()} 小时）`, `Follow global (every ${globalAutoHours()}h)`) : tr("跟随全局（当前：关闭）", "Follow global (off)") },
    { value: "0", title: tr("关闭自动更新", "Off") },
    { value: "6", title: tr("每 6 小时", "Every 6 hours") },
    { value: "12", title: tr("每 12 小时", "Every 12 hours") },
    { value: "24", title: tr("每 24 小时", "Every 24 hours") },
    { value: "custom", title: tr("自定义间隔…", "Custom interval…") },
  ];

  const smartSubmitLabel = () => {
    const k = detectedKind();
    if (k === "nodes") {
      const n = chosenImportNodes().length;
      if (importDest() === "append" && activeProfile()) return tr(`追加 ${n} 个节点`, `Append ${n} proxies`);
      return tr(`创建本地档案 (${n})`, `Create local profile (${n})`);
    }
    if (k === "clash_yaml") return tr("导入配置", "Import YAML");
    return tr("添加", "Add");
  };

  const canSubmitImport = () => {
    if (!!p.busy() || !url().trim()) return false;
    if (detectedKind() === "nodes") return chosenImportNodes().length > 0;
    return true;
  };

  return <View testID="desktop-profiles" style={{ flex: 1 }}>
    <If when={!rows().length}>
      <View style={{ flex: 1, paddingHorizontal: padX(), paddingTop: padY(), paddingBottom: padY(), gap: gap() }}>
        {heading()}
        {emptyState()}
      </View>
    </If>

    <If when={!!rows().length}>
      <Scroll style={{ flex: 1 }} contentStyle={{ paddingHorizontal: padX(), paddingTop: padY(), paddingBottom: padY(), gap: gap() }}>
        {heading()}

        <If when={!!queuedDeepLink()}>
          <Row justify="space-between" gap={10} style={{ paddingHorizontal: 13, paddingVertical: 9, borderRadius: 10, backgroundColor: tint(colors().accent, "12"), borderWidth: 1, borderColor: tint(colors().accent, "2c") }}>
            <Label text={tr(`收到外部导入链接：${queuedDeepLink()?.url ?? ""}`, `Queued import link: ${queuedDeepLink()?.url ?? ""}`)} size={12} lines={1} />
            <Row gap={6} style={{ flexShrink: 0 }}>
              <Action size="small" label={tr("打开导入", "Open import")} onPress={() => { const q = queuedDeepLink(); queuedDeepLink.set(null); if (q) openAdd(q); }} />
              <Action size="small" quiet label={tr("忽略", "Dismiss")} onPress={() => queuedDeepLink.set(null)} />
            </Row>
          </Row>
        </If>

        <If when={!!activeRows().length}>
          <View style={{ gap: 8 }}>
            <Section title={tr("当前配置", "Current profile")} />
            <Each of={activeRows()} key={(item: any) => item.id}>{(item) => <View>{currentCard(item())}</View>}</Each>
          </View>
        </If>

        <If when={!!otherRows().length}>
          <View style={{ gap: 8 }}>
            <Section title={tr("其他配置", "Other profiles")} />
            <Group>
              <Each of={otherRows()} key={(item: any) => item.id}>{(item) => <View style={{ backgroundColor: colors().surface, paddingHorizontal: 18, paddingVertical: 12, gap: 8 }} hoverStyle={{ backgroundColor: colors().hover }}>
                <Row gap={12} style={{ minWidth: 0 }}>
                  {iconTile(profileIcon(item()), 30)}
                  <View style={{ flex: 1, minWidth: 0, gap: 3 }}>
                    <Row gap={7} style={{ minWidth: 0 }}>
                      <Text style={{ fontSize: 13.5, fontWeight: "500", color: colors().text, numberOfLines: 1, textOverflow: "ellipsis", flexShrink: 1, minWidth: 0 }} tooltip={item().name}>{item().name}</Text>
                      <If when={item().kind === "aggregate"}><Tag text={tr("聚合", "Aggregate")} tone="muted" /></If>
                      <If when={!!item().isPureLocal}><Tag text={tr("纯本地", "Local")} tone="muted" /></If>
                      {overlayPill(item())}
                    </Row>
                    <Text style={{ fontSize: 11.5, color: colors().faint, numberOfLines: 1, textOverflow: "ellipsis" }} tooltip={metaLine(item())}>{metaLine(item())}</Text>
                  </View>
                  {rowActions(item())}
                </Row>
                <If when={item().kind === "url" && !!item().info?.total}><View style={{ paddingLeft: 42 }}>{quotaSection(item())}</View></If>
                <If when={!!(cardErrors()[item().id] || item().lastError)}><View style={{ paddingLeft: 42 }}>{cardErrorBox(item())}</View></If>
              </View>}</Each>
            </Group>
          </View>
        </If>
      </Scroll>
    </If>

    {/* Unified Smart Import Modal */}
    <Modal
      testID="profile-add-dialog"
      closeTestID="profile-add-close"
      title={tr("添加订阅 / 导入节点", "Add profile / import proxies")}
      open={adding()}
      onClose={() => void guardedClose()}
      error={localError()}
      width={560}
      footer={() => <Row justify="space-between" gap={8}>
        <Action quiet icon="profiles" label={tr("从文件选择…", "Choose file…")} disabled={!!p.busy()} onPress={importFile} />
        <Row gap={8}>
          <Action label={tr("取消", "Cancel")} onPress={() => void guardedClose()} />
          <Action testID="profile-add-submit" primary label={smartSubmitLabel()} disabled={!canSubmitImport()} onPress={submitSmartImport} />
        </Row>
      </Row>}
    >
      <Field
        testID="profile-url"
        label={tr("订阅链接、VPS 节点链接、Base64 或 Clash YAML", "Subscription URL, VPS node links, Base64 or Clash YAML")}
        multiline={url().includes("\n") || url().length > 90}
        height={url().includes("\n") || url().length > 90 ? 96 : 32}
        value={url()}
        onChange={(v) => { url.set(v); importMode.set("auto"); triggerParse(v, "auto"); }}
        placeholder={tr("https://... 或粘贴 vless:// / vmess:// / trojan:// / ss:// / hy2:// / YAML", "https://... or paste vless://, vmess://, trojan://, ss://, hy2://, YAML")}
      />

      {/* Smart detection badge + http:// ambiguity 1-click toggle chip */}
      <If when={!!url().trim()}>
        <Row gap={8} wrap>
          <Tag
            text={
              detectedKind() === "url" ? tr("识别为订阅链接", "Detected: Subscription URL")
                : detectedKind() === "nodes" ? tr(`识别为节点链接 (${parsedImportNodes().length})`, `Detected: ${parsedImportNodes().length} proxy links`)
                  : detectedKind() === "clash_yaml" ? tr("识别为 Clash YAML 配置", "Detected: Clash YAML")
                    : tr("待识别内容", "Unrecognized input")
            }
            tone={detectedKind() === "unknown" ? "warn" : "accent"}
          />
          <If when={!!parseResult()?.ambiguousHttp || (detectedKind() === "url" && /^https?:\/\//i.test(url().trim()))}>
            <Action
              testID="import-http-toggle"
              size="small"
              quiet
              label={importMode() === "nodes" ? tr("改为订阅链接", "Treat as subscription URL") : tr("按 HTTP 节点导入", "Import as HTTP proxy node")}
              onPress={() => {
                const next = importMode() === "nodes" ? "url" : "nodes";
                importMode.set(next);
                triggerParse(url(), next);
              }}
            />
          </If>
        </Row>
      </If>

      {/* Node URIs preview + destination choice */}
      <If when={detectedKind() === "nodes" && parsedImportNodes().length > 0}>
        <If when={!!activeProfile()}>
          <View style={{ gap: 6 }}>
            <Label text={tr("导入目标", "Destination")} muted size={12} weight={600} />
            <Tabs
              testIDPrefix="import-nodes-dest"
              value={importDest()}
              onChange={(v) => importDest.set(v as "local" | "append")}
              items={[
                { id: "append", label: tr(`追加到当前订阅（${activeProfile()?.name ?? ""}）`, `Append to active (${activeProfile()?.name ?? ""})`) },
                { id: "local", label: tr("新建本地档案", "New local profile") },
              ]}
            />
            <If when={importDest() === "append"}>
              <Label text={tr("追加为本地自建节点，保留当前订阅规则且更新订阅时不丢失。", "Appended as local nodes: keeps your subscription rules and survives refreshes.")} muted size={11.5} />
            </If>
          </View>
        </If>

        <View style={{ maxHeight: 150, borderRadius: 10, borderWidth: 1, borderColor: colors().border, backgroundColor: colors().surface, overflow: "hidden" }}>
          <Scroll style={{ maxHeight: 150 }}>
            <Each of={parsedImportNodes()}>{(node, idx) => <View style={{ borderBottomWidth: 1, borderColor: colors().grid }}>
              <CheckRow
                checked={parseSelected().has(idx())}
                onChange={() => {
                  const next = new Set(parseSelected());
                  if (next.has(idx())) next.delete(idx()); else next.add(idx());
                  parseSelected.set(next);
                }}
                label={node().name || `${node().type}-${node().server}`}
                subtitle={`${String(node().type ?? "").toUpperCase()} · ${node().server}:${node().port}`}
              />
            </View>}</Each>
          </Scroll>
        </View>

        <If when={importFailures().length > 0}>
          <Collapsible title={tr(`${importFailures().length} 条链接解析失败`, `${importFailures().length} lines failed to parse`)} open={parseErrorsOpen()} onToggle={() => parseErrorsOpen.set(!parseErrorsOpen())}>
            <Each of={importFailures()}>{(f) => <Label text={tr(`第 ${f().line} 行：${f().reason}`, `Line ${f().line}: ${f().reason}`)} color={colors().danger} size={11} />}</Each>
          </Collapsible>
        </If>
      </If>

      <If when={detectedKind() !== "nodes" || importDest() === "local"}>
        <Field label={tr("名称（可选）", "Name (optional)")} value={name()} onChange={(v) => name.set(v)} placeholder={tr("留空将自动从订阅标题或域名识别", "Auto-detected from title or host if blank")} />
      </If>

      {/* Initial fetch options (User-Agent & UseProxy wired directly to desktop.profiles.import!) */}
      <If when={detectedKind() === "url" || detectedKind() === "empty"}>
        <Collapsible
          testID="import-fetch-options"
          title={tr("高级下载选项", "Download options")}
          summary={[importUseProxy() ? tr("通过代理", "Via proxy") : "", importUA().trim() ? "Custom UA" : ""].filter(Boolean).join(" · ") || tr("默认", "Default")}
          open={importFetchOptsOpen()}
          onToggle={() => importFetchOptsOpen.set(!importFetchOptsOpen())}
        >
          <Row justify="space-between">
            <View style={{ flex: 1, gap: 2 }}>
              <Label text={tr("通过当前代理下载", "Download through proxy")} weight={500} size={12.5} />
              <Label text={tr("首次拉取与后续更新均通过本地代理发送请求", "Used on both initial download and future refreshes")} faint size={11} />
            </View>
            <Toggle testID="import-use-proxy" showLabel={false} label={tr("通过代理下载", "Download through proxy")} checked={importUseProxy()} onChange={(v) => importUseProxy.set(v)} />
          </Row>
          <Field testID="import-user-agent" label="User-Agent" value={importUA()} onChange={(v) => importUA.set(v)} placeholder={tr("留空使用默认 User-Agent", "Leave blank for default User-Agent")} />
          <Row gap={6} wrap>
            <Each of={UA_PRESETS}>{(preset) => <Action size="small" quiet label={preset().title} onPress={() => importUA.set(preset().value)} />}</Each>
          </Row>
        </Collapsible>
      </If>

      <If when={detectedKind() !== "nodes" || importDest() === "local"}>
        <Row justify="space-between">
          <View style={{ flex: 1, gap: 2 }}>
            <Label text={tr("导入后立即启用", "Activate immediately after import")} weight={500} size={12.5} />
            <Label text={rows().length === 0 ? tr("首个配置默认立即启用", "First profile activates automatically by default") : tr("保持当前活跃配置不变，稍后手动切换", "Keep your current active profile unchanged until you switch")} faint size={11} />
          </View>
          <Toggle testID="import-activate" showLabel={false} label={tr("导入后立即启用", "Activate after import")} checked={importActivate()} onChange={(v) => importActivate.set(v)} />
        </Row>
      </If>

      <Label text={tr("下载失败会保留已填写内容。", "Failed downloads preserve your entered draft.")} faint size={11.5} />
    </Modal>

    {/* Redesigned Profile Settings Modal (no 170px raw YAML dump!) */}
    <Modal
      testID="profile-edit-dialog"
      closeTestID="profile-edit-close"
      title={tr("订阅设置", "Profile settings")}
      open={!!editing()}
      onClose={() => void guardedClose()}
      error={localError()}
      width={600}
      footer={() => <Row justify="space-between" gap={8}>
        <Row gap={6}>
          <If when={draft().kind === "url" && !!draft().url}>
            <Action quiet icon="copy" label={tr("复制链接", "Copy URL")} onPress={() => void nativeCall("desktop.clipboard.write", { text: draft().url }).then(() => showNotice(tr("已复制订阅链接", "Subscription URL copied")))} />
          </If>
          <Action quiet label={showConfigYaml() ? tr("隐藏配置", "Hide YAML") : tr("查看 / 导出配置", "View / export YAML")} onPress={() => showConfigYaml.set(!showConfigYaml())} />
        </Row>
        <Row gap={8}>
          <Action label={tr("取消", "Cancel")} onPress={() => void guardedClose()} />
          <Action primary label={tr("保存", "Save")} disabled={!!p.busy() || !draft().name?.trim()} onPress={() => void perform("profile-edit", () => p.call("desktop.profiles.edit", { profile: { ...draft(), content: content() } }))} />
        </Row>
      </Row>}
    >
      <Field label={tr("名称", "Name")} value={draft().name ?? ""} onChange={(v) => changeDraft("name", v)} />

      <If when={draft().kind === "url" || (draft().kind === "file" && !!draft().path)}>
        <Field label={draft().kind === "file" ? tr("文件路径", "File path") : tr("订阅链接", "Subscription URL")} value={draft().kind === "file" ? draft().path ?? "" : draft().url ?? ""} onChange={(v) => changeDraft(draft().kind === "file" ? "path" : "url", v)} />
      </If>

      {/* Clear Auto-Update Selector + UseProxy + User-Agent for URL profiles */}
      <If when={draft().kind === "url"}>
        <Row gap={12} style={{ alignItems: "flex-end" }}>
          <View style={{ flex: 1, gap: 6 }}>
            <Label text={tr("自动更新", "Auto-update")} muted size={12} weight={600} />
            <Select testID="profile-auto-update" label={tr("自动更新", "Auto-update")} value={autoUpdatePreset()} onChange={setAutoUpdatePreset} items={autoUpdateItems()} />
          </View>
          <If when={autoUpdatePreset() === "custom"}>
            <View style={{ width: 100 }}>
              <Field label={tr("小时", "Hours")} value={customHours()} onChange={(v) => { customHours.set(v); changeDraft("updateHours", Math.max(1, Number(v) || 1)); }} placeholder="12" />
            </View>
          </If>
          <View style={{ height: 32, justifyContent: "center" }}>
            <Toggle label={tr("通过代理下载", "Use proxy")} checked={!!draft().useProxy} onChange={(v) => changeDraft("useProxy", v)} />
          </View>
        </Row>
        <Field label="User-Agent" value={draft().userAgent ?? ""} onChange={(v) => changeDraft("userAgent", v)} placeholder={tr("留空使用默认 User-Agent", "Leave blank for default User-Agent")} />
      </If>

      {/* Overlay Summary Card */}
      <View style={{ padding: 13, borderRadius: 12, borderWidth: 1, borderColor: colors().border, backgroundColor: dark() ? "#ffffff04" : tint(colors().bg, "80"), gap: 9 }}>
        <Row justify="space-between" gap={8}>
          <Label text={tr("本地叠加层摘要", "Local overlay summary")} weight={600} size={12.5} />
          <Row gap={6}>
            <Action size="small" label={tr("管理节点与策略组…", "Manage proxies…")} onPress={() => {
              const target = editingProfileMeta();
              resetModals();
              if (target) openOverlayJump(target);
            }} />
            <Action size="small" quiet label={tr("规则覆写", "Rule overrides")} onPress={() => { resetModals(); p.navigate("rules"); }} />
          </Row>
        </Row>
        <Row gap={14} wrap>
          <Label text={tr(`节点总数 ${editingProfileMeta()?.nodeCount ?? 0}`, `Proxies ${editingProfileMeta()?.nodeCount ?? 0}`)} size={12} tabular />
          <Label text={tr(`自建节点 ${editingProfileMeta()?.localNodeCount ?? 0}`, `Local ${editingProfileMeta()?.localNodeCount ?? 0}`)} size={12} color={(editingProfileMeta()?.localNodeCount ?? 0) > 0 ? colors().accent : colors().muted} tabular />
          <Label text={tr(`修改节点 ${editingProfileMeta()?.editedNodeCount ?? 0}`, `Edited ${editingProfileMeta()?.editedNodeCount ?? 0}`)} size={12} muted tabular />
          <Label text={tr(`修改策略组 ${editingProfileMeta()?.editedGroupCount ?? 0}`, `Groups ${editingProfileMeta()?.editedGroupCount ?? 0}`)} size={12} muted tabular />
          <Label text={tr(`额外规则 ${editingProfileMeta()?.extraRuleCount ?? 0}`, `Rules ${editingProfileMeta()?.extraRuleCount ?? 0}`)} size={12} muted tabular />
          <Label text={tr(`模板 ${editingProfileMeta()?.templateCount ?? 0}`, `Templates ${editingProfileMeta()?.templateCount ?? 0}`)} size={12} muted tabular />
        </Row>
        <Label text={tr("更新订阅只刷新远端基底配置，你的自建节点、节点修改和规则覆写均自动保留。", "Updating this subscription only refreshes the upstream base; your local nodes, edits, and rules are preserved.")} faint size={11} lines={2} />
      </View>

      {/* On-demand YAML viewer + export (hidden by default!) */}
      <If when={showConfigYaml()}>
        <View style={{ gap: 6 }}>
          <Row justify="space-between">
            <Label text={tr("缓存配置（只读）", "Cached configuration (read-only)")} muted size={12} weight={600} />
            <Action size="small" label={tr("导出 YAML…", "Export YAML…")} onPress={() => void exportProfileYaml(draft(), content())} />
          </Row>
          <Field readonly multiline height={150} value={content()} onChange={() => {}} />
        </View>
      </If>
    </Modal>

    {/* Live Aggregation Editor (Sources & Order + Filters & Presets + 300ms Debounced Live Preview) */}
    <Modal
      testID="aggregate-dialog"
      closeTestID="aggregate-dialog-close"
      title={aggId() ? tr("编辑聚合配置", "Edit aggregate") : tr("新建聚合配置", "New aggregate")}
      open={aggregate()}
      onClose={() => void guardedClose()}
      width={640}
      error={localError()}
      footer={() => <Row justify="space-between" gap={8}>
        <Label text={tr("仅读本地缓存 · 不继承原规则 · 自动生成 Select/Auto", "Cached sources only · no upstream rules · generates Select/Auto")} faint size={11} lines={1} />
        <Row gap={8}>
          <Action label={tr("取消", "Cancel")} onPress={() => void guardedClose()} />
          <Action primary label={tr("保存聚合", "Save aggregate")} disabled={!!p.busy() || !name().trim() || !Object.values(sources()).some(Boolean)} onPress={() => void perform("aggregate", () => p.call("desktop.profiles.aggregate", { id: aggId() || undefined, name: name().trim(), spec: currentAggSpec() }))} />
        </Row>
      </Row>}
    >
      <Field testID="aggregate-name" label={tr("名称", "Name")} value={name()} onChange={(v) => name.set(v)} placeholder={tr("例如：全节点精选", "For example: Combined Nodes")} />

      {/* Section 1: Sources & Priority Order */}
      <View style={{ gap: 6 }}>
        <Label text={tr("1. 来源与优先级顺序（仅使用本地缓存）", "1. Sources & priority order (local cache only)")} muted size={12} weight={600} />
        <Group>
          <Each of={sourceOrder().map((id) => nonAggregateRows().find((r: any) => r.id === id)).filter(Boolean)} key={(item: any) => item.id}>{(item, idx) => <GroupRow gap={8} style={{ paddingHorizontal: 12, paddingVertical: 8, minHeight: 48 }}>
            <Toggle testID={`agg-source-${item().id}`} showLabel={false} label={item().name} checked={!!sources()[item().id]} onChange={(v) => sources.set({ ...sources(), [item().id]: v })} />
            <View style={{ flex: 1, minWidth: 0 }}>
              <Text style={{ fontSize: 12.5, fontWeight: "500", color: colors().text, numberOfLines: 1, textOverflow: "ellipsis" }} tooltip={item().name}>{item().name}</Text>
              <If when={item().nodeCount > 0}><Text style={{ fontSize: 10.5, color: colors().faint }}>{tr(`${item().nodeCount} 节点`, `${item().nodeCount} proxies`)}</Text></If>
            </View>
            <View style={{ width: 132, flexShrink: 0 }}>
              <Field value={prefixes()[item().id] ?? ""} placeholder={tr("前缀（可选）", "Prefix")} onChange={(v) => prefixes.set({ ...prefixes(), [item().id]: v })} />
            </View>
            <Row gap={2} style={{ flexShrink: 0 }}>
              <IconButton size={26} icon="arrowUp" label={tr("上移来源", "Move source up")} disabled={idx() === 0} onPress={() => moveSourceOrder(item().id, -1)} />
              <IconButton size={26} icon="arrowDown" label={tr("下移来源", "Move source down")} disabled={idx() >= nonAggregateRows().length - 1} onPress={() => moveSourceOrder(item().id, 1)} />
            </Row>
          </GroupRow>}</Each>
        </Group>
      </View>

      {/* Section 2: Filters, Quick Chips & Dedupe */}
      <View style={{ gap: 8 }}>
        <Label text={tr("2. 筛选与去重", "2. Filter & deduplicate")} muted size={12} weight={600} />
        <Row gap={6} wrap>
          <Each of={REGION_PRESETS}>{(chip) => <Action size="small" label={tr(chip().labelZh, chip().labelEn)} onPress={() => toggleRegionChip(chip().token)} />}</Each>
          <Action size="small" label={tr("排除信息节点", "Exclude info nodes")} onPress={toggleExcludeInfoChip} />
        </Row>
        <Row gap={10}>
          <View style={{ flex: 1 }}><Field testID="aggregate-include" label={tr("包含（正则表达式）", "Include (regex)")} value={include()} onChange={(v) => include.set(v)} placeholder="香港|日本|SG" /></View>
          <View style={{ flex: 1 }}><Field testID="aggregate-exclude" label={tr("排除（正则表达式）", "Exclude (regex)")} value={exclude()} onChange={(v) => exclude.set(v)} placeholder="过期|流量" /></View>
        </Row>
        <Toggle label={tr("合并重复节点（按服务器/端口/协议去重）", "Deduplicate identical proxies")} checked={dedupe()} onChange={(v) => dedupe.set(v)} />
      </View>

      {/* Section 3: Lightweight Live Preview (includeYaml: false) */}
      <View testID="aggregate-preview-panel" style={{ padding: 12, borderRadius: 10, borderWidth: 1, borderColor: colors().border, backgroundColor: dark() ? "#ffffff04" : tint(colors().bg, "80"), gap: 8 }}>
        <Row justify="space-between" gap={8}>
          <Label text={tr("3. 实时结果预览", "3. Live result preview")} weight={600} size={12} />
          <If when={aggPreviewLoading()}><Label text={tr("计算中…", "Calculating…")} faint size={11} /></If>
          <If when={!aggPreviewLoading() && !!aggPreview()}>
            <Text testID="aggregate-preview-stats" style={{ fontSize: 11.5, fontWeight: "600", fontVariant: "tabular-nums", color: colors().accent }}>
              {tr(
                `原始 ${aggPreview()!.stats.input} → 筛选 ${aggPreview()!.stats.matched} → 去重保留 ${aggPreview()!.stats.output}`,
                `Input ${aggPreview()!.stats.input} → Matched ${aggPreview()!.stats.matched} → Kept ${aggPreview()!.stats.output}`,
              )}
            </Text>
          </If>
        </Row>
        <If when={!!aggPreviewErr()}><Label text={aggPreviewErr()} color={colors().danger} size={11.5} /></If>
        <If when={!aggPreview() && !aggPreviewErr() && !aggPreviewLoading()}>
          <Label text={tr("勾选至少一个来源即可实时预览合并结果。", "Select at least one source to preview matched nodes live.")} faint size={11.5} />
        </If>
        <If when={!!aggPreview() && aggPreview()!.names.length > 0}>
          <View style={{ maxHeight: 116, borderRadius: 8, borderWidth: 1, borderColor: colors().grid, backgroundColor: colors().surface, overflow: "hidden" }}>
            <Scroll style={{ maxHeight: 116 }} contentStyle={{ paddingHorizontal: 10, paddingVertical: 6, gap: 3 }}>
              <Each of={aggPreview()!.names}>{(n) => <Label text={n()} size={11.5} lines={1} tooltip={n()} />}</Each>
              <If when={aggPreview()!.truncated}><Label text={tr("仅展示前 200 条预览…", "Showing first 200 preview items…")} faint size={11} /></If>
            </Scroll>
          </View>
        </If>
        <Label text={tr("聚合仅读取各来源的基底缓存，不含订阅内局部修改。", "Aggregation reads each source's cached base config and excludes per-profile local edits.")} faint size={10.5} />
      </View>
    </Modal>

    {/* Delete confirmation Modal */}
    <Modal title={tr("删除配置？", "Delete profile?")} open={!!deleting()} onClose={() => deleting.set(null)} footer={() => <Row justify="flex-end"><Action label={tr("取消", "Cancel")} onPress={() => deleting.set(null)} /><Action danger label={tr("删除", "Delete")} disabled={!!p.busy()} onPress={() => void p.run("delete-profile", async () => { await p.call("desktop.profiles.delete", { id: deleting().id }); deleting.set(null); })} /></Row>}>
      <Label text={tr(`将删除“${deleting()?.name ?? ""}”。此操作不会删除原始订阅服务器上的内容。`, `Delete “${deleting()?.name ?? ""}”? The original subscription remains on its server.`)} />
      <If when={(deleting()?.dependentAggregates?.length ?? 0) > 0}>
        <Feedback text={tr(`以下聚合配置引用了此来源：${deleting().dependentAggregates.map((d: any) => d.name).join("、")}`, `Referenced by aggregates: ${deleting().dependentAggregates.map((d: any) => d.name).join(", ")}`)} danger />
      </If>
    </Modal>

    {/* Targeted Proxy Manager Modal for INACTIVE profiles */}
    <ProfileProxyManagerModal
      profile={proxyManagerProfile()}
      onClose={() => proxyManagerProfile.set(null)}
      page={p}
      onSaved={() => showNotice(tr("非活跃档案的节点修改已保存", "Saved proxy changes for inactive profile"))}
    />
  </View>;
}
