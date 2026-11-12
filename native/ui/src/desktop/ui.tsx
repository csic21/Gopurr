import { colorScheme, computed, host, Image, platform, reduceMotion, Scroll, Text, View, windowSize } from "nsh";
import type { JSX, ImageAsset } from "nsh";
import { Button, Dialog, Input, Switch, Tabs as NativeTabs, Tab } from "nsh/base";
import { colors } from "./theme";
import { tr } from "./i18n";
import cable from "../../assets/icons/cable.svg";
import layers from "../../assets/icons/layers-2.svg";
import filter from "../../assets/icons/list-filter.svg";
import network from "../../assets/icons/network.svg";
import panel from "../../assets/icons/panel-left.svg";
import terminal from "../../assets/icons/square-terminal.svg";
import down from "../../assets/icons/arrow-down-left.svg";
import up from "../../assets/icons/arrow-up-right.svg";
import overview from "../../assets/icons/layout-dashboard.svg";
import profiles from "../../assets/icons/file-text.svg";
import proxies from "../../assets/icons/globe.svg";
import settings from "../../assets/icons/settings-2.svg";
import check from "../../assets/icons/circle-check.svg";
import circle from "../../assets/icons/circle.svg";
import shield from "../../assets/icons/shield-check.svg";
import search from "../../assets/icons/search.svg";
import x from "../../assets/icons/x.svg";
import chevronDown from "../../assets/icons/chevron-down.svg";
import chevronRight from "../../assets/icons/chevron-right.svg";
import refresh from "../../assets/icons/refresh-cw.svg";
import server from "../../assets/icons/server.svg";
import radar from "../../assets/icons/radar.svg";
import route from "../../assets/icons/route.svg";
import swap from "../../assets/icons/arrow-left-right.svg";
import power from "../../assets/icons/power.svg";
import activity from "../../assets/icons/activity.svg";
const icons: Record<string, ImageAsset> = { overview, subs: layers, profiles, proxies, conns: cable, mesh: network, rules: filter, logs: terminal, panel, down, up, settings, check, circle, search, x, chevronDown, chevronRight, refresh, shield, server, radar, route, swap, power, activity };
export function Icon(p: { name: string; size?: number; color?: string }) { return <Image src={icons[p.name] ?? circle} style={{ width: p.size ?? 16, height: p.size ?? 16, objectFit: "contain", tintColor: p.color ?? colors().muted }} />; }

/**
 * One scale for the whole app. The system font supplies its own line metrics;
 * display sizes carry real weight, labels stay quiet, figures are tabular.
 */
export const TYPE = { display: 24, hero: 36, lead: 16, section: 12, body: 13, secondary: 12, caption: 11, micro: 10.5, metric: 30, mono: 12 } as const;
export const CONTROL_HEIGHT = 30;
export const ROW_HEIGHT = 50;
export const RADIUS = { control: 8, surface: 16, dialog: 16, pill: 999 } as const;
export const SPACE = { xs: 4, sm: 8, md: 12, lg: 16, xl: 20, xxl: 24 } as const;
const MOTION = { transitionProperty: "background-color, border-color, color, opacity, box-shadow", transitionDuration: 140, transitionTimingFunction: "ease-out" } as const;

/** A translucent step of a palette color, written as the 8-digit hex NSH takes. */
export function tint(color: string, alpha: string): string { return color + alpha; }

/** How far a surface floats above the canvas. Kept low in light and deeper in the dark. */
export function elevation(kind: "surface" | "popover" | "dialog" = "surface"): any[] {
  const dark = colorScheme() === "dark";
  if (kind === "popover") return dark ? [{ x: 0, y: 12, blur: 34, color: "#0000008c" }, { x: 0, y: 0, blur: 0, spread: 1, color: "#ffffff0d", inset: true }] : [{ x: 0, y: 12, blur: 32, color: "#131c3026" }, { x: 0, y: 1, blur: 2, color: "#131c300f" }];
  if (kind === "dialog") return dark ? [{ x: 0, y: 30, blur: 76, color: "#000000b8" }, { x: 0, y: 0, blur: 0, spread: 1, color: "#ffffff0f", inset: true }] : [{ x: 0, y: 28, blur: 70, color: "#131c3033" }, { x: 0, y: 2, blur: 6, color: "#131c3012" }];
  return dark ? [{ x: 0, y: 14, blur: 36, color: "#00000047" }, { x: 0, y: 0, blur: 0, spread: 1, color: "#ffffff0a", inset: true }] : [{ x: 0, y: 1, blur: 2, color: "#131c300d" }, { x: 0, y: 10, blur: 28, color: "#131c3012" }];
}

export function Label(p: { text: string; muted?: boolean; faint?: boolean; size?: number; weight?: number; bold?: boolean; color?: string; mono?: boolean; lines?: number; tabular?: boolean; tooltip?: string }) {
  return <Text tooltip={p.tooltip} style={{ fontSize: p.size ?? TYPE.body, fontWeight: p.weight ?? (p.bold ? 600 : 400), fontFamily: p.mono ? "monospace" : "system-ui", color: p.color ?? (p.faint ? colors().faint : p.muted ? colors().muted : colors().text), numberOfLines: p.lines, textOverflow: p.lines === 1 ? "ellipsis" : undefined, fontVariant: p.tabular ? "tabular-nums" : undefined }}>{p.text}</Text>;
}

/**
 * One button geometry for the whole app: 30 high, 7 radius. Primary carries the
 * accent and a soft lift, secondary a hairline, quiet is text-only inside rows
 * and page heads. Destructive actions are red text rather than red fills.
 */
export function Action(p: { label: string; onPress: () => void; primary?: boolean; danger?: boolean; quiet?: boolean; disabled?: boolean; testID?: string; bold?: boolean; icon?: string; size?: "regular" | "small" }) {
  const filled = !!p.primary, plain = !filled && (p.quiet || p.danger);
  const fg = filled ? colors().accentText : p.danger ? colors().danger : colors().text;
  const height = p.size === "small" ? 26 : CONTROL_HEIGHT;
  const shadow = filled ? (colorScheme() === "dark" ? [{ x: 0, y: 1, blur: 2, color: "#00000040" }] : [{ x: 0, y: 1, blur: 2, color: tint(colors().accent, "3d") }]) : [];
  return <Button testID={p.testID} onPress={p.onPress} disabled={p.disabled} style={{ height, minHeight: height, paddingHorizontal: plain ? 8 : filled ? 14 : 12, borderRadius: RADIUS.control, alignItems: "center", justifyContent: "center", borderWidth: filled || plain ? 0 : 1, borderColor: colors().border, backgroundColor: filled ? colors().accent : plain ? "transparent" : colors().surface, boxShadow: shadow, ...MOTION }}
    hoverStyle={{ backgroundColor: filled ? colors().accentHover : colors().hover }}
    activeStyle={{ backgroundColor: filled ? colors().accentPressed : colors().selected, opacity: filled ? 1 : 0.92 }}
    focusStyle={{ outlineWidth: 2, outlineColor: colors().accent, outlineOffset: 2 }}
    disabledStyle={{ opacity: 0.38 }}>
    <View style={{ flexDirection: "row", alignItems: "center", gap: 6 }}>
      <If when={!!p.icon}><Icon name={p.icon ?? "circle"} size={filled ? 14 : 13.5} color={filled ? fg : p.danger ? colors().danger : colors().muted} /></If>
      <Text style={{ fontSize: p.size === "small" ? 12 : 12.5, fontWeight: filled || p.bold ? 600 : 500, color: fg, numberOfLines: 1 }}>{p.label}</Text>
    </View>
  </Button>;
}

export function IconButton(p: { icon: string; label: string; onPress: () => void; disabled?: boolean; testID?: string; flat?: boolean; size?: number }) {
  return <Button testID={p.testID} accessibilityLabel={p.label} disabled={p.disabled} onPress={p.onPress} style={{ width: p.size ?? CONTROL_HEIGHT, height: p.size ?? CONTROL_HEIGHT, alignItems: "center", justifyContent: "center", borderRadius: RADIUS.control, backgroundColor: "transparent", ...MOTION }}
    hoverStyle={{ backgroundColor: colors().hover }}
    activeStyle={{ backgroundColor: colors().selected }}
    focusStyle={{ outlineWidth: 2, outlineColor: colors().accent, outlineOffset: 1 }}
    disabledStyle={{ opacity: 0.35 }}><Icon name={p.icon} size={15} color={colors().muted} /></Button>;
}

export function StatusDot(p: { on: boolean; size?: number; color?: string; glow?: boolean }) {
  const size = p.size ?? 6, color = p.color ?? (p.on ? colors().success : colors().faint);
  return <View style={{ width: size, height: size, borderRadius: size / 2, backgroundColor: color, boxShadow: p.glow ? [{ x: 0, y: 0, blur: 0, spread: size > 4 ? 3 : 2, color: tint(color, "26") }] : undefined }} />;
}

/** A small state label: tinted pill with semantic text, never color alone. */
export function Tag(p: { text: string; tone?: "accent" | "success" | "danger" | "muted" | "warn"; testID?: string }) {
  const color = p.tone === "danger" ? colors().danger : p.tone === "success" ? colors().success : p.tone === "warn" ? colors().warn : p.tone === "muted" ? colors().muted : colors().accent;
  return <View testID={p.testID} style={{ height: 20, paddingHorizontal: 7, justifyContent: "center", borderRadius: RADIUS.pill, backgroundColor: p.tone === "muted" ? colors().hover : tint(color, "17"), borderWidth: p.tone === "muted" ? 0 : 1, borderColor: tint(color, "1f") }}><Text style={{ fontSize: TYPE.micro, fontWeight: "600", letterSpacing: 0.1, color, numberOfLines: 1 }}>{p.text}</Text></View>;
}

/** A floating surface: content card, dialog body or a page section. */
export function Panel(p: { children?: JSX.Element; padding?: number; gap?: number; style?: any; inset?: boolean }) {
  return <View style={{ padding: p.padding ?? 18, gap: p.gap ?? 12, borderRadius: RADIUS.surface, backgroundColor: colors().surface, borderWidth: 1, borderColor: colors().border, boxShadow: p.inset ? undefined : elevation("surface"), ...p.style }}>{p.children}</View>;
}

/**
 * Grouped rows: the container paints the hairline, rows sit on the surface and
 * one-point gaps become separators. No border per row, no nested cards.
 */
export function Group(p: { children?: JSX.Element; style?: any }) {
  return <View style={{ borderRadius: RADIUS.surface, backgroundColor: colors().border, overflow: "hidden", gap: 1, borderWidth: 1, borderColor: colors().border, boxShadow: elevation("surface"), ...p.style }}>{p.children}</View>;
}

export function GroupRow(p: { children?: JSX.Element; gap?: number; style?: any; hover?: boolean }) {
  return <View hoverStyle={p.hover ? { backgroundColor: colors().hover } : undefined} style={{ flexDirection: "row", alignItems: "center", minHeight: ROW_HEIGHT, paddingHorizontal: 16, paddingVertical: 10, gap: p.gap ?? 14, backgroundColor: colors().surface, ...MOTION, ...p.style }}>{p.children}</View>;
}

export function Row(p: { children?: JSX.Element; gap?: number; justify?: "space-between" | "flex-end" | "flex-start" | "center"; wrap?: boolean; style?: any }) {
  return <View style={{ flexDirection: "row", alignItems: "center", gap: p.gap ?? 8, justifyContent: p.justify, flexWrap: p.wrap ? "wrap" : "nowrap", ...p.style }}>{p.children}</View>;
}

export function Column(p: { children?: JSX.Element; gap?: number; flex?: number; style?: any }) {
  return <View style={{ gap: p.gap ?? 8, flex: p.flex, ...p.style }}>{p.children}</View>;
}

export function Heading(p: { title: string; description: string; action?: () => JSX.Element; descriptionTone?: "muted" | "danger" }) {
  return <View style={{ gap: 6 }}>
    <Row justify="space-between" style={{ minHeight: CONTROL_HEIGHT, gap: 12 }}>
      <Text role="header" style={{ fontSize: windowSize().height < 660 ? 21 : TYPE.display, fontWeight: "600", letterSpacing: -0.6, color: colors().text, numberOfLines: 1, flexShrink: 1, minWidth: 0 }}>{p.title}</Text>
      <View style={{ flexShrink: 0 }}>{p.action?.()}</View>
    </Row>
    <Text style={{ fontSize: 12.5, lineHeight: 17, color: p.descriptionTone === "danger" ? colors().danger : colors().muted, numberOfLines: 1, textOverflow: "ellipsis" }}>{p.description}</Text>
  </View>;
}

/** A quiet group label above a grouped surface; the page title stays the loudest element. */
export function Section(p: { title: string; subtitle?: string }) {
  return <Row justify="space-between" style={{ paddingTop: windowSize().height < 660 ? 8 : 14, paddingHorizontal: 2, gap: 8 }}>
    <Text style={{ fontSize: TYPE.section, fontWeight: "600", letterSpacing: 0.2, color: colors().muted, numberOfLines: 1 }}>{p.title}</Text>
    <If when={!!p.subtitle}><Label text={p.subtitle ?? ""} faint size={TYPE.caption} lines={1} tabular /></If>
  </Row>;
}

export function Field(p: { accessibilityLabel?: string; label?: string; value: string; onChange: (value: string) => void; placeholder?: string; multiline?: boolean; height?: number; readonly?: boolean; secret?: boolean; testID?: string }) {
  return <View style={{ gap: 6 }}>
    <If when={!!p.label}><Text style={{ fontSize: TYPE.secondary, fontWeight: "600", color: colors().muted }}>{p.label ?? ""}</Text></If>
    <Input testID={p.testID} accessibilityLabel={p.accessibilityLabel ?? p.label} value={p.value} onChange={({ value }) => p.onChange(value)} placeholder={p.placeholder} multiline={p.multiline} readonly={p.readonly} masked={p.secret} autoCorrect={false} autoCapitalize="none"
      style={{ height: p.height ?? (p.multiline ? 150 : 32), minHeight: 32, paddingHorizontal: 10, paddingVertical: 7, borderRadius: 8, borderWidth: 1, borderColor: colors().border, backgroundColor: colorScheme() === "dark" ? colors().bg : colors().surface, color: colors().text, placeholderColor: colors().faint, fontSize: p.multiline ? TYPE.mono : TYPE.body, fontFamily: p.multiline ? "monospace" : "system-ui", ...MOTION }}
      focusStyle={{ borderColor: colors().accent, backgroundColor: colors().surface, boxShadow: [{ x: 0, y: 0, blur: 0, spread: 3, color: tint(colors().accent, "1f") }] }} />
  </View>;
}

export function SearchField(p: { value: string; onChange: (value: string) => void; placeholder: string; testID?: string; label?: string; style?: any }) {
  return <View style={{ height: 32, minHeight: 32, flexDirection: "row", alignItems: "center", gap: 7, paddingLeft: 10, paddingRight: 4, borderRadius: 8, borderWidth: 1, borderColor: colors().border, backgroundColor: colorScheme() === "dark" ? colors().bg : colors().surface, ...MOTION, ...p.style }}
    focusWithinStyle={{ borderColor: colors().accent, boxShadow: [{ x: 0, y: 0, blur: 0, spread: 3, color: tint(colors().accent, "1c") }] }}>
    <Icon name="search" size={14} color={colors().faint} />
    <Input testID={p.testID} accessibilityLabel={p.label ?? p.placeholder} value={p.value} onChange={({ value }) => p.onChange(value)} placeholder={p.placeholder} autoCorrect={false} autoCapitalize="none" style={{ flex: 1, minWidth: 0, height: 30, paddingHorizontal: 0, paddingVertical: 0, borderWidth: 0, backgroundColor: "transparent", color: colors().text, placeholderColor: colors().faint, fontSize: 12.5 }} />
    <If when={!!p.value}><Button testID={p.testID ? `${p.testID}-clear` : undefined} accessibilityLabel={tr("清空搜索", "Clear search")} onPress={() => p.onChange("")} style={{ width: 24, height: 24, alignItems: "center", justifyContent: "center", borderRadius: 6, ...MOTION }} hoverStyle={{ backgroundColor: colors().hover }} focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}><Icon name="x" size={13} /></Button></If>
  </View>;
}

export function switchThumbTransform(checked: boolean): string {
  return checked ? "translateX(16px)" : "none";
}

export function Toggle(p: { checked: boolean; onChange: (v: boolean) => void; label?: string; disabled?: boolean; testID?: string; showLabel?: boolean }) {
  const duration = reduceMotion() ? 0 : 240;
  return <Row><If when={!!p.label && p.showLabel !== false}><Label text={p.label ?? ""} /></If>
    <Switch testID={p.testID} checked={p.checked} disabled={p.disabled} accessibilityLabel={p.label} onChange={({ checked }) => p.onChange(checked)} style={{ width: 36, height: 20, opacity: p.disabled ? 0.38 : 1 }}
      trackStyle={{ width: 36, height: 20, padding: 2, backgroundColor: p.checked ? colors().accent : colors().border, borderRadius: 10, borderWidth: 1, borderColor: p.checked ? colors().accent : (colorScheme() === "dark" ? tint(colors().faint, "4d") : tint(colors().faint, "2e")), boxShadow: p.checked ? [{ x: 0, y: 1, blur: 4, color: tint(colors().accent, "3d") }] : [{ x: 0, y: 1, blur: 1, color: "#0000000f", inset: true }], transitionProperty: "background-color, border-color, box-shadow", transitionDuration: duration, transitionTimingFunction: "ease-out" }}
      thumbStyle={{ width: 14, height: 14, borderRadius: 7, backgroundColor: "#ffffff", boxShadow: [{ x: 0, y: 1, blur: 2.5, color: "#1018283d" }], transform: switchThumbTransform(p.checked), transitionProperty: "transform", transitionDuration: duration, transitionTimingFunction: "ease-out" }} /></Row>;
}

/**
 * Segmented control: a recessed track with one raised selected pill. The
 * indicator moves natively; text and press targets stay fixed.
 */
export function Tabs(p: { items: { id: string; label: string }[]; value: string; onChange: (v: string) => void; disabled?: boolean; testIDPrefix?: string }) {
  const dark = colorScheme() === "dark";
  return <NativeTabs style={{ flexDirection: "row", alignSelf: "flex-start", gap: 2, padding: 3, borderRadius: 11, backgroundColor: dark ? colors().surface : colors().hover, borderWidth: 1, borderColor: colors().border }}
    indicatorStyle={{ top: 3, bottom: 3, borderRadius: 8, backgroundColor: dark ? colors().hover : colors().surface, boxShadow: dark ? [] : [{ x: 0, y: 1, blur: 2, color: "#18213a1a" }, { x: 0, y: 1, blur: 1, color: "#18213a0f" }] }}>
    <Each of={p.items} key={(item) => item.id}>{(item) => <Tab testID={`${p.testIDPrefix ?? "desktop-tab"}-${item().id}`} selected={p.value === item().id} disabled={p.disabled} onPress={() => p.onChange(item().id)}
      style={{ paddingHorizontal: 13, paddingVertical: 3, minHeight: 24, borderRadius: 8, backgroundColor: "transparent" }}
      hoverStyle={{ backgroundColor: p.value === item().id ? "transparent" : colors().hover }}
      focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}>
      <Text style={{ fontSize: 12, fontWeight: p.value === item().id ? "600" : "500", letterSpacing: p.value === item().id ? -0.1 : 0, color: p.value === item().id ? colors().accent : colors().muted }}>{item().label}</Text>
    </Tab>}</Each>
  </NativeTabs>;
}

export { Select } from "./select";

export function Feedback(p: { text: string; danger?: boolean }) {
  const tone = p.danger ? colors().danger : colors().accent;
  return <View role="alert" style={{ paddingHorizontal: 13, paddingVertical: 10, borderRadius: 9, backgroundColor: tint(tone, "10"), borderWidth: 1, borderColor: tint(tone, "26"), borderLeftWidth: 3, borderLeftColor: tone }}>
    <Text style={{ fontSize: 12, lineHeight: 17, color: p.danger ? colors().danger : colors().text }}>{p.text}</Text>
  </View>;
}

export function Empty(p: { title: string; description?: string; action?: () => JSX.Element; icon?: string }) {
  return <View style={{ flex: 1, minHeight: 190, padding: 26, gap: 10, alignItems: "center", justifyContent: "center" }}>
    <View style={{ width: 46, height: 46, alignItems: "center", justifyContent: "center", borderRadius: 15, backgroundColor: colors().hover, borderWidth: 1, borderColor: colors().border }}><Icon name={p.icon ?? "circle"} size={22} color={colors().faint} /></View>
    <Text style={{ fontSize: 14, fontWeight: "600", letterSpacing: -0.1, color: colors().text, textAlign: "center" }}>{p.title}</Text>
    <If when={!!p.description}><Text style={{ fontSize: 12, lineHeight: 18, color: colors().muted, maxWidth: 360, textAlign: "center" }}>{p.description}</Text></If>
    <If when={!!p.action}><View style={{ paddingTop: 6 }}>{p.action?.()}</View></If>
  </View>;
}

/**
 * The scrim behind a dialog. macOS renders the blur as a system material
 * (AppKit picks the radius); Windows paints it literally against the window's
 * back buffer, where a partial frame then keeps hard-edged boxes of the page
 * instead of a blurred one. Windows therefore keeps the plain scrim.
 */
function dialogBackdrop() {
  const backgroundColor = colorScheme() === "dark" ? "#05070c8c" : "#0b122470";
  return { backgroundColor, ...(platform.os === "windows" ? {} : { backdropFilter: "blur(7px)" }) };
}

// NSH 0.4.0's Win32 backend stops asking the window for frames once a dialog
// has finished fading out: the dismissed dialog stays in the back buffer and
// later updates go undrawn, which reads as a window that cannot be closed.
// The Windows host draws the whole window once more on
// `desktop.window.repaint`. Windows also dismisses the dialog through the app
// so that this repaint lands after the 160 ms exit fade instead of meeting
// its picture; elsewhere the platform may close it itself.
const closeOnEscape = platform.os !== "windows";
const closeOnBackdropPress = platform.os !== "windows";

/** Asks the Windows host for one whole-window frame. */
function requestWindowRepaint() {
  if (platform.os !== "windows") return;
  try { void Promise.resolve(host.call("desktop.window.repaint", {})).catch(() => {}); } catch {}
}

/**
 * Dialog: one surface, a title row, a scrolling body and a footer that stays
 * put. The close action keeps its label so keyboard and screen-reader users
 * always have a named way out.
 */
export function Modal(p: { title: string; open: boolean; onClose: () => void; children?: JSX.Element; footer?: () => JSX.Element; width?: number; height?: number; error?: string; testID?: string; closeTestID?: string }) {
  // Dialog recreates its native layer whenever its open dependency changes.
  // Deduplicate visibility so fresh snapshots update content without reopening.
  const open = computed(() => p.open);
  // A press on Close, Escape or the backdrop removes the dialog from the app;
  // a footer action that closes it does so itself. Both end in the dialog
  // going away: ask for the repaint once the 160 ms exit fade is over.
  const close = () => { p.onClose(); if (platform.os === "windows") setTimeout(requestWindowRepaint, 240); };
  return <Dialog testID={p.testID} open={open()} onOpenChange={({ open }) => { if (!open) close(); }} onCancel={close} onClose={requestWindowRepaint} closeOnEscape={closeOnEscape} closeOnBackdropPress={closeOnBackdropPress} animation="fade"
    style={{ marginHorizontal: "auto", marginVertical: "auto", width: Math.min(p.width ?? 560, windowSize().width - 48), maxHeight: windowSize().height - 48, padding: 0, gap: 0, backgroundColor: colors().surface, borderRadius: RADIUS.dialog, borderWidth: 1, borderColor: colors().border, boxShadow: elevation("dialog") }}
    backdropStyle={dialogBackdrop()}>
    <Row justify="space-between" gap={12} style={{ paddingHorizontal: 20, paddingTop: 16, paddingBottom: 13 }}>
      <Text style={{ fontSize: 16, fontWeight: "600", letterSpacing: -0.2, color: colors().text, numberOfLines: 1, textOverflow: "ellipsis" }}>{p.title}</Text>
      <Action quiet testID={p.closeTestID} label={tr("关闭", "Close")} onPress={close} />
    </Row>
    <Scroll style={{ maxHeight: p.height ?? windowSize().height - 210 }} contentStyle={{ paddingHorizontal: 20, paddingBottom: 18, gap: 13 }}>{p.children}</Scroll>
    <If when={!!p.error}><View style={{ paddingHorizontal: 20, paddingBottom: 13 }}><Feedback text={p.error ?? ""} danger /></View></If>
    <If when={!!p.footer}><View style={{ paddingHorizontal: 20, paddingVertical: 13, borderTopWidth: 1, borderColor: colors().border, backgroundColor: colorScheme() === "dark" ? tint(colors().surface, "00") : tint(colors().bg, "66") }}>{p.footer?.()}</View></If>
  </Dialog>;
}

export function Stat(p: { label: string; value: string; tooltip?: string }) {
  return <View style={{ flexDirection: "row", alignItems: "baseline", gap: 6, minWidth: 0 }}>
    <Label text={p.label} faint size={TYPE.micro} lines={1} />
    <Label text={p.value} size={12.5} weight={600} tabular tooltip={p.tooltip} />
  </View>;
}

export function fmtBytes(value: number): string { let n = Math.max(0, value || 0), i = 0; const units = ["B", "KB", "MB", "GB", "TB"]; while (n >= 1024 && i < 4) { n /= 1024; i++; } return `${n.toFixed(i ? 1 : 0)} ${units[i]}`; }
export function fmtRate(value: number): string { return `${fmtBytes(value)}/s`; }
export function fmtTime(value: string | number): string { if (!value) return "—"; const d = new Date(value); return Number.isNaN(d.getTime()) ? String(value) : d.toLocaleString(); }
export function groupTypeLabel(kind: string): string { switch (String(kind).toLowerCase()) { case "selector": case "select": return tr("手动选择", "Manual"); case "urltest": case "url-test": return tr("自动选择", "Auto"); case "fallback": return tr("故障转移", "Fallback"); case "loadbalance": case "load-balance": return tr("负载均衡", "Load balance"); case "relay": return tr("链式代理", "Relay"); default: return kind; } }
export function modeLabel(mode: string): string { switch (mode) { case "global": return tr("全局", "Global"); case "direct": return tr("直连", "Direct"); default: return tr("规则", "Rule"); } }
