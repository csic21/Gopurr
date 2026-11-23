import { cleanup, colorScheme, computed, createAnimatedValue, effect, Image, Overlay, reduceMotion, Scroll, Text, View, windowSize } from "nsh";
import type { BoxShadowLayer, ImageAsset, JSX, StyleProp, ViewStyle } from "nsh";
import { AlertDialog, Button, Dialog, Input, Sheet, Switch, Tab, Tabs as NativeTabs } from "nsh/base";
import { colors } from "../theme";
import { tr } from "../i18n";
import { mutationBusy, sheetDismissSeq, toast } from "./store";
import activityIcon from "../../assets/icons/activity.svg";
import arrowDownIcon from "../../assets/icons/arrow-down-left.svg";
import arrowUpIcon from "../../assets/icons/arrow-up-right.svg";
import swapIcon from "../../assets/icons/arrow-left-right.svg";
import cableIcon from "../../assets/icons/cable.svg";
import checkIcon from "../../assets/icons/check.svg";
import chevronDownIcon from "../../assets/icons/chevron-down.svg";
import chevronRightIcon from "../../assets/icons/chevron-right.svg";
import circleCheckIcon from "../../assets/icons/circle-check.svg";
import circleIcon from "../../assets/icons/circle.svg";
import fileTextIcon from "../../assets/icons/file-text.svg";
import globeIcon from "../../assets/icons/globe.svg";
import layersIcon from "../../assets/icons/layers-2.svg";
import dashboardIcon from "../../assets/icons/layout-dashboard.svg";
import filterIcon from "../../assets/icons/list-filter.svg";
import networkIcon from "../../assets/icons/network.svg";
import powerIcon from "../../assets/icons/power.svg";
import radarIcon from "../../assets/icons/radar.svg";
import refreshIcon from "../../assets/icons/refresh-cw.svg";
import routeIcon from "../../assets/icons/route.svg";
import searchIcon from "../../assets/icons/search.svg";
import serverIcon from "../../assets/icons/server.svg";
import settingsIcon from "../../assets/icons/settings-2.svg";
import shieldIcon from "../../assets/icons/shield-check.svg";
import terminalIcon from "../../assets/icons/square-terminal.svg";
import xIcon from "../../assets/icons/x.svg";

export type MobileIconName =
  | "activity" | "arrowDown" | "arrowUp" | "cable" | "check" | "chevronDown"
  | "chevronLeft" | "chevronRight" | "circleCheck" | "circle" | "filter"
  | "layers" | "logs" | "network" | "overview" | "power" | "profiles"
  | "proxies" | "radar" | "refresh" | "route" | "search" | "server"
  | "settings" | "shield" | "swap" | "x";

const ICONS: Record<Exclude<MobileIconName, "chevronLeft">, ImageAsset> = {
  activity: activityIcon,
  arrowDown: arrowDownIcon,
  arrowUp: arrowUpIcon,
  cable: cableIcon,
  check: checkIcon,
  chevronDown: chevronDownIcon,
  chevronRight: chevronRightIcon,
  circleCheck: circleCheckIcon,
  circle: circleIcon,
  filter: filterIcon,
  layers: layersIcon,
  logs: terminalIcon,
  network: networkIcon,
  overview: dashboardIcon,
  power: powerIcon,
  profiles: fileTextIcon,
  proxies: globeIcon,
  radar: radarIcon,
  refresh: refreshIcon,
  route: routeIcon,
  search: searchIcon,
  server: serverIcon,
  settings: settingsIcon,
  shield: shieldIcon,
  swap: swapIcon,
  x: xIcon,
};

export type Tone = "accent" | "success" | "warn" | "danger" | "muted";

export const compact = () => windowSize().width < 680;
export const RADIUS = { control: 10, surface: 16, sheet: 20, pill: 999 } as const;

export function tint(color: string, alpha: string): string { return color + alpha; }

export function accentFill(): { bg: string; hover: string; pressed: string; fg: string } {
  return colorScheme() === "dark"
    ? { bg: "#2e62e6", hover: "#386bf0", pressed: "#2452cc", fg: "#ffffff" }
    : { bg: colors().accent, hover: colors().accentHover, pressed: colors().accentPressed, fg: colors().accentText };
}

export function toneColor(tone: Tone = "muted"): string {
  switch (tone) {
    case "accent": return colors().accent;
    case "success": return colors().success;
    case "warn": return colors().warn;
    case "danger": return colors().danger;
    default: return colors().muted;
  }
}

export function elevation(kind: "surface" | "popover" = "surface"): BoxShadowLayer[] {
  const dark = colorScheme() === "dark";
  if (kind === "popover") {
    return dark
      ? [{ x: 0, y: 10, blur: 28, color: "#00000080" }, { x: 0, y: 0, blur: 0, spread: 1, color: "#ffffff0d", inset: true }]
      : [{ x: 0, y: 10, blur: 26, color: "#131c3024" }, { x: 0, y: 1, blur: 2, color: "#131c300f" }];
  }
  return dark
    ? [{ x: 0, y: 6, blur: 18, color: "#00000033" }, { x: 0, y: 1, blur: 0, color: "#ffffff08", inset: true }]
    : [{ x: 0, y: 1, blur: 2, color: "#0f172a0a" }, { x: 0, y: 6, blur: 18, color: "#0f172a0b" }];
}

export function bytes(value: number, rate = false): string {
  let size = Math.max(0, value || 0), unit = 0;
  const units = ["B", "KB", "MB", "GB", "TB"];
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++; }
  return `${size.toFixed(unit ? 1 : 0)} ${units[unit]}${rate ? "/s" : ""}`;
}

export function formatRateParts(value: number): { num: string; unit: string } {
  let size = Math.max(0, value || 0), unit = 0;
  const units = ["B/s", "KB/s", "MB/s", "GB/s", "TB/s"];
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++; }
  return { num: size.toFixed(unit ? 1 : 0), unit: units[unit] };
}

export function formatUpdated(iso?: string): string {
  if (!iso) return "";
  const ts = Date.parse(iso);
  if (Number.isNaN(ts)) return iso;
  const diffMin = Math.floor((Date.now() - ts) / 60000);
  if (diffMin >= 0 && diffMin < 1) return tr("刚刚更新", "Updated just now");
  if (diffMin >= 1 && diffMin < 60) return tr(`${diffMin} 分钟前更新`, `Updated ${diffMin}m ago`);
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr >= 1 && diffHr < 24) return tr(`${diffHr} 小时前更新`, `Updated ${diffHr}h ago`);
  const d = new Date(ts);
  const mm = String(d.getMonth() + 1).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  return tr(`更新于 ${mm}-${dd}`, `Updated ${mm}-${dd}`);
}

export function Icon(props: { source?: ImageAsset; name?: MobileIconName; selected?: boolean; size?: number; color?: string }) {
  const asset = props.source ?? (props.name === "chevronLeft" ? ICONS.chevronRight : props.name ? ICONS[props.name] : circleIcon);
  const rotate = props.name === "chevronLeft" ? [{ rotate: "180deg" }] : undefined;
  return <Image src={asset} style={{ width: props.size ?? 18, height: props.size ?? 18, objectFit: "contain", tintColor: props.color ?? (props.selected ? colors().accent : colors().muted), transform: rotate }} />;
}

export function Label(props: { text: string; muted?: boolean; faint?: boolean; size?: number; bold?: boolean; weight?: number | `${number}`; color?: string; mono?: boolean; lines?: number; tabular?: boolean }) {
  const size = props.size ?? 13;
  return <Text style={{
    color: props.color ?? (props.faint ? colors().faint : props.muted ? colors().muted : colors().text),
    fontSize: size,
    fontWeight: props.weight ?? (props.bold ? "600" : "400"),
    lineHeight: Math.round(size * 1.42),
    fontFamily: props.mono ? "monospace" : undefined,
    numberOfLines: props.lines,
    textOverflow: props.lines === 1 ? "ellipsis" : undefined,
    fontVariant: props.tabular ? "tabular-nums" : undefined,
  }}>{props.text}</Text>;
}

export function StatusDot(props: { on?: boolean; tone?: Tone; size?: number }) {
  const size = props.size ?? 7;
  const color = () => props.tone ? toneColor(props.tone) : (props.on ? colors().success : colors().faint);
  return <View style={{ width: size, height: size, borderRadius: size / 2, backgroundColor: color(), flexShrink: 0 }} />;
}

export function StatusPill(props: { label: string; tone?: Tone; testID?: string }) {
  const dark = () => colorScheme() === "dark";
  const tone = () => props.tone ?? "muted";
  const fg = () => tone() === "muted" ? colors().muted : toneColor(tone());
  return <View testID={props.testID} style={{
    flexDirection: "row", alignItems: "center", gap: 6, height: 26, paddingHorizontal: 10,
    borderRadius: RADIUS.pill,
    backgroundColor: tone() === "muted" ? colors().hover : tint(fg(), dark() ? "1a" : "12"),
    borderWidth: 1,
    borderColor: tone() === "muted" ? colors().border : tint(fg(), dark() ? "30" : "24"),
    flexShrink: 0,
  }}>
    <StatusDot tone={tone()} size={6} />
    <Text style={{ fontSize: 11.5, fontWeight: "600", color: tone() === "muted" ? colors().text : fg(), numberOfLines: 1 }}>{props.label}</Text>
  </View>;
}

export function Tag(props: { text: string; tone?: Tone; testID?: string }) {
  const dark = () => colorScheme() === "dark";
  const tone = () => props.tone ?? "accent";
  const fg = () => toneColor(tone());
  return <View testID={props.testID} style={{
    height: 22, paddingHorizontal: 8, alignItems: "center", justifyContent: "center",
    borderRadius: 6,
    backgroundColor: tone() === "muted" ? colors().hover : tint(fg(), dark() ? "1a" : "12"),
    borderWidth: tone() === "muted" ? 0 : 1,
    borderColor: tone() === "muted" ? "transparent" : tint(fg(), dark() ? "2c" : "22"),
    flexShrink: 0,
  }}>
    <Text style={{ fontSize: 11, fontWeight: "600", color: fg(), numberOfLines: 1 }}>{props.text}</Text>
  </View>;
}

export function Notice(props: { text: string; tone?: "info" | "warn" | "danger" | "success"; action?: () => JSX.Element; testID?: string }) {
  const dark = () => colorScheme() === "dark";
  const color = () => {
    switch (props.tone) {
      case "danger": return colors().danger;
      case "warn": return colors().warn;
      case "success": return colors().success;
      default: return colors().accent;
    }
  };
  return <View testID={props.testID} role={props.tone === "danger" ? "alert" : undefined} style={{
    flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 10,
    paddingHorizontal: 12, paddingVertical: 10, borderRadius: 10,
    backgroundColor: tint(color(), dark() ? "18" : "10"),
    borderWidth: 1, borderColor: tint(color(), dark() ? "30" : "26"),
    borderLeftWidth: 3, borderLeftColor: color(),
  }}>
    <Text style={{ flex: 1, fontSize: 12, lineHeight: 17, color: props.tone === "danger" ? colors().danger : colors().text }}>{props.text}</Text>
    <If when={!!props.action}><View style={{ flexShrink: 0 }}>{props.action?.()}</View></If>
  </View>;
}

export function Action(props: {
  label: string;
  onPress: () => void;
  primary?: boolean;
  quiet?: boolean;
  danger?: boolean;
  disabled?: boolean;
  full?: boolean;
  size?: "regular" | "large" | "compact";
  icon?: MobileIconName;
  testID?: string;
}) {
  const filled = () => !!props.primary;
  const plain = () => !filled() && !!props.quiet;
  const dark = () => colorScheme() === "dark";
  const fill = () => accentFill();
  const fg = () => filled() ? fill().fg : props.danger ? colors().danger : colors().text;
  const minHeight = props.size === "large" ? 50 : props.size === "compact" ? 36 : 44;
  const hitSlop = props.size === "compact" ? { top: 4, bottom: 4, left: 4, right: 4 } : undefined;
  return <Button
    testID={props.testID}
    onPress={props.onPress}
    disabled={props.disabled ?? mutationBusy()}
    hitSlop={hitSlop}
    style={{
      minHeight,
      width: props.full ? "100%" : undefined,
      paddingHorizontal: plain() ? 10 : props.size === "large" ? 18 : 14,
      paddingVertical: props.size === "compact" ? 6 : 8,
      borderRadius: RADIUS.control,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: filled() ? fill().bg : plain() ? "transparent" : (dark() ? "#1b1f2a" : colors().surface),
      borderWidth: filled() || plain() ? 0 : 1,
      borderColor: dark() ? "#2a3040" : colors().border,
      transitionProperty: "background-color, opacity",
      transitionDuration: reduceMotion() ? 0 : 140,
    }}
    hoverStyle={{ backgroundColor: filled() ? fill().hover : plain() ? colors().hover : (dark() ? "#232836" : colors().hover) }}
    activeStyle={{ backgroundColor: filled() ? fill().pressed : colors().selected, opacity: filled() ? 1 : 0.88 }}
    focusStyle={{ outlineWidth: 2, outlineColor: colors().accent, outlineOffset: 2 }}
    disabledStyle={{ opacity: 0.42 }}>
    <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 6 }}>
      <If when={!!props.icon}><Icon name={props.icon} size={props.size === "large" ? 16 : 15} color={fg()} /></If>
      <Text style={{ color: fg(), fontSize: props.size === "large" ? 15 : 13.5, fontWeight: filled() || props.size === "large" ? "600" : "500", numberOfLines: 1 }}>{props.label}</Text>
    </View>
  </Button>;
}

export function IconButton(props: { icon: MobileIconName; label: string; onPress: () => void; disabled?: boolean; color?: string; size?: number; testID?: string }) {
  return <Button
    testID={props.testID}
    accessibilityLabel={props.label}
    disabled={props.disabled}
    onPress={props.onPress}
    style={{
      width: 44, height: 44, borderRadius: RADIUS.control,
      alignItems: "center", justifyContent: "center",
      backgroundColor: "transparent",
      transitionProperty: "background-color, opacity",
      transitionDuration: reduceMotion() ? 0 : 140,
    }}
    hoverStyle={{ backgroundColor: colors().hover }}
    activeStyle={{ backgroundColor: colors().selected, opacity: 0.8 }}
    focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}
    disabledStyle={{ opacity: 0.38 }}>
    <Icon name={props.icon} size={props.size ?? 19} color={props.color ?? colors().muted} />
  </Button>;
}

export function MoreButton(props: { label: string; onPress: () => void; disabled?: boolean; testID?: string }) {
  return <Button
    testID={props.testID}
    accessibilityLabel={props.label}
    disabled={props.disabled}
    onPress={props.onPress}
    style={{
      width: 44, height: 44, borderRadius: RADIUS.control,
      alignItems: "center", justifyContent: "center",
      backgroundColor: "transparent",
    }}
    hoverStyle={{ backgroundColor: colors().hover }}
    activeStyle={{ backgroundColor: colors().selected, opacity: 0.8 }}
    focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}
    disabledStyle={{ opacity: 0.38 }}>
    <View style={{ flexDirection: "row", alignItems: "center", gap: 3 }}>
      <View style={{ width: 4, height: 4, borderRadius: 2, backgroundColor: colors().muted }} />
      <View style={{ width: 4, height: 4, borderRadius: 2, backgroundColor: colors().muted }} />
      <View style={{ width: 4, height: 4, borderRadius: 2, backgroundColor: colors().muted }} />
    </View>
  </Button>;
}

export function ToggleSwitch(props: { checked: boolean; onChange: (checked: boolean) => void; label: string; disabled?: boolean; testID?: string }) {
  const duration = () => reduceMotion() ? 0 : 240;
  const dark = () => colorScheme() === "dark";
  const checkedBg = () => accentFill().bg;
  return <Switch
    testID={props.testID}
    checked={props.checked}
    disabled={props.disabled}
    accessibilityLabel={props.label}
    hitSlop={{ top: 10, bottom: 10, left: 8, right: 8 }}
    onChange={({ checked }) => props.onChange(checked)}
    style={{ width: 44, height: 26, opacity: props.disabled ? 0.4 : 1 }}
    trackStyle={{
      width: 44, height: 26, padding: 3, borderRadius: 13,
      backgroundColor: props.checked ? checkedBg() : (dark() ? "#252b38" : "#cfd7e3"),
      borderWidth: 1,
      borderColor: props.checked ? checkedBg() : (dark() ? "#333b4d" : "#c1cad8"),
      transitionProperty: "background-color, border-color",
      transitionDuration: duration(),
      transitionTimingFunction: "ease-out",
    }}
    thumbStyle={{
      width: 18, height: 18, borderRadius: 9,
      backgroundColor: "#ffffff",
      boxShadow: [{ x: 0, y: 1, blur: 2, color: "#10182838" }],
      transform: props.checked ? [{ translateX: 18 }] : [{ translateX: 0 }],
      transitionProperty: "transform",
      transitionDuration: duration(),
      transitionTimingFunction: "ease-out",
    }} />;
}

export interface SegmentedItem {
  id: string;
  label: string;
  testID?: string;
}

export function Segmented(props: { items: SegmentedItem[]; value: string; onChange: (id: string) => void; disabled?: boolean; testIDPrefix?: string }) {
  const dark = () => colorScheme() === "dark";
  return <NativeTabs
    style={{
      flexDirection: "row", width: "100%", gap: 2, padding: 3, borderRadius: 12,
      backgroundColor: dark() ? "#0b0d12" : "#e6eaf2",
      borderWidth: 1, borderColor: dark() ? "#222733" : "#d6dce8",
      opacity: props.disabled ? 0.5 : 1,
    }}
    indicatorStyle={{
      top: 3, bottom: 3, borderRadius: 9,
      backgroundColor: dark() ? "#202532" : "#ffffff",
      borderWidth: 1, borderColor: dark() ? "#2e3546" : "#d8dfe9",
      boxShadow: dark() ? [{ x: 0, y: 1, blur: 2, color: "#00000040" }] : [{ x: 0, y: 1, blur: 2, color: "#131c3018" }],
    }}>
    <Each of={props.items} key={item => item.id}>{item => <Tab
      testID={item().testID ?? (props.testIDPrefix ? `${props.testIDPrefix}-${item().id}` : undefined)}
      selected={props.value === item().id}
      disabled={props.disabled}
      onPress={() => props.onChange(item().id)}
      style={{ flex: 1, minHeight: 38, paddingHorizontal: 8, paddingVertical: 6, borderRadius: 9, alignItems: "center", justifyContent: "center", backgroundColor: "transparent" }}
      hoverStyle={{ backgroundColor: props.value === item().id ? "transparent" : colors().hover }}
      focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}>
      <Text style={{ fontSize: 13, fontWeight: props.value === item().id ? "600" : "500", color: props.value === item().id ? colors().text : colors().muted, numberOfLines: 1, textOverflow: "ellipsis" }}>{item().label}</Text>
    </Tab>}</Each>
  </NativeTabs>;
}

export function Panel(props: { children?: JSX.Element; padding?: number; gap?: number; style?: StyleProp<ViewStyle>; accentLeft?: boolean; accentColor?: string }) {
  return <View style={[{
    padding: props.padding ?? 16,
    gap: props.gap ?? 12,
    borderRadius: RADIUS.surface,
    backgroundColor: colors().surface,
    borderWidth: 1,
    borderColor: colors().border,
    borderLeftWidth: props.accentLeft ? 3 : 1,
    borderLeftColor: props.accentLeft ? (props.accentColor ?? colors().accent) : colors().border,
    boxShadow: elevation("surface"),
  }, props.style]}>{props.children}</View>;
}

export function SectionLabel(props: { title: string; trailing?: string }) {
  return <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", paddingHorizontal: 4, paddingTop: 4, gap: 8 }}>
    <Text style={{ fontSize: 12, fontWeight: "600", color: colors().muted, letterSpacing: 0.2 }}>{props.title}</Text>
    <If when={!!props.trailing}><Label text={props.trailing ?? ""} faint size={11} tabular /></If>
  </View>;
}

export function Group(props: { children?: JSX.Element; style?: StyleProp<ViewStyle> }) {
  return <View style={[{
    borderRadius: RADIUS.surface,
    backgroundColor: colors().border,
    borderWidth: 1,
    borderColor: colors().border,
    overflow: "hidden",
    gap: 1,
    boxShadow: elevation("surface"),
  }, props.style]}>{props.children}</View>;
}

export function GroupRow(props: {
  icon?: MobileIconName;
  iconColor?: string;
  title: string;
  subtitle?: string;
  value?: string;
  valueTone?: Tone;
  chevron?: boolean;
  trailing?: () => JSX.Element;
  onPress?: () => void;
  disabled?: boolean;
  danger?: boolean;
  children?: JSX.Element;
  testID?: string;
}) {
  const dark = () => colorScheme() === "dark";
  const content = () => <View android={props.onPress ? { ripple: { color: tint(colors().accent, "1a") } } : undefined} style={{
    paddingHorizontal: 16, paddingVertical: 12, minHeight: 54, gap: props.children ? 10 : 0,
    backgroundColor: colors().surface, justifyContent: "center",
  }}>
    <View style={{ flexDirection: "row", alignItems: "center", gap: 12 }}>
      <If when={!!props.icon}>
        <View style={{
          width: 32, height: 32, borderRadius: 8, alignItems: "center", justifyContent: "center", flexShrink: 0,
          backgroundColor: dark() ? "#1c202c" : colors().bg,
        }}>
          <Icon name={props.icon} size={17} color={props.iconColor ?? (props.danger ? colors().danger : colors().muted)} />
        </View>
      </If>
      <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
        <Text style={{ fontSize: 14, fontWeight: "500", color: props.danger ? colors().danger : colors().text, numberOfLines: 2 }}>{props.title}</Text>
        <If when={!!props.subtitle}><Text style={{ fontSize: 12, lineHeight: 17, color: colors().muted, numberOfLines: 2 }}>{props.subtitle ?? ""}</Text></If>
      </View>
      <If when={!!props.value}><Text style={{ fontSize: 12.5, fontWeight: "500", color: toneColor(props.valueTone ?? "muted"), numberOfLines: 1, maxWidth: 140, textOverflow: "ellipsis", flexShrink: 0 }}>{props.value ?? ""}</Text></If>
      <If when={!!props.trailing}><View style={{ flexShrink: 0 }}>{props.trailing?.()}</View></If>
      <If when={!!props.chevron}><Icon name="chevronRight" size={16} color={colors().faint} /></If>
    </View>
    <If when={!!props.children}><View>{props.children}</View></If>
  </View>;
  if (!props.onPress) return content();
  return <Button testID={props.testID} disabled={props.disabled} onPress={props.onPress}
    style={{ width: "100%", backgroundColor: colors().surface }}
    hoverStyle={{ opacity: 0.96 }}
    activeStyle={{ opacity: 0.82 }}
    focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}
    disabledStyle={{ opacity: 0.45 }}>
    {content()}
  </Button>;
}

export function Heading(props: { title: string; description?: string; action?: () => JSX.Element }) {
  return <View style={{ gap: 4 }}>
    <View style={{ flexDirection: "row", gap: 12, alignItems: "center", justifyContent: "space-between", minHeight: 44 }}>
      <Text role="header" style={{ fontSize: 22, fontWeight: "600", letterSpacing: -0.4, color: colors().text, flexShrink: 1 }}>{props.title}</Text>
      <If when={!!props.action}><View style={{ flexShrink: 0 }}>{props.action?.()}</View></If>
    </View>
    <If when={!!props.description}><Label text={props.description ?? ""} muted size={12.5} /></If>
  </View>;
}

export function SubpageHeader(props: { title: string; onBack: () => void; backDisabled?: boolean; backTestID?: string; action?: () => JSX.Element }) {
  return <View style={{
    flexDirection: "row", alignItems: "center", gap: 6, minHeight: 52,
    paddingHorizontal: 8, paddingVertical: 4,
    backgroundColor: colors().bg, borderBottomWidth: 1, borderColor: colors().border,
  }}>
    <Button
      testID={props.backTestID}
      accessibilityLabel={tr("返回", "Back")}
      disabled={props.backDisabled}
      onPress={props.onBack}
      style={{ flexDirection: "row", alignItems: "center", gap: 2, minWidth: 44, minHeight: 44, paddingHorizontal: 8, borderRadius: RADIUS.control }}
      hoverStyle={{ backgroundColor: colors().hover }}
      activeStyle={{ backgroundColor: colors().selected, opacity: 0.8 }}
      focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}
      disabledStyle={{ opacity: 0.4 }}>
      <Icon name="chevronLeft" size={20} color={colors().text} />
      <Text style={{ fontSize: 14, fontWeight: "500", color: colors().text }}>{tr("返回", "Back")}</Text>
    </Button>
    <Text role="header" style={{ flex: 1, fontSize: 17, fontWeight: "600", color: colors().text, numberOfLines: 1, textOverflow: "ellipsis" }}>{props.title}</Text>
    <If when={!!props.action}><View style={{ flexShrink: 0, paddingRight: 4 }}>{props.action?.()}</View></If>
  </View>;
}

export function Empty(props: { title: string; description: string; icon?: MobileIconName; action?: () => JSX.Element }) {
  return <View style={{ alignItems: "center", justifyContent: "center", paddingVertical: 32, paddingHorizontal: 20, gap: 10 }}>
    <View style={{ width: 46, height: 46, borderRadius: 14, alignItems: "center", justifyContent: "center", backgroundColor: colors().hover, borderWidth: 1, borderColor: colors().border }}>
      <Icon name={props.icon ?? "circle"} size={22} color={colors().faint} />
    </View>
    <Text style={{ fontSize: 15, fontWeight: "600", color: colors().text, textAlign: "center" }}>{props.title}</Text>
    <Text style={{ fontSize: 12.5, lineHeight: 18, color: colors().muted, textAlign: "center", maxWidth: 340 }}>{props.description}</Text>
    <If when={!!props.action}><View style={{ paddingTop: 4 }}>{props.action?.()}</View></If>
  </View>;
}

export function Field(props: { label?: string; value: string; onChange: (value: string) => void; placeholder?: string; multiline?: boolean; disabled?: boolean; helper?: string; testID?: string; accessibilityLabel?: string }) {
  return <View style={{ gap: 6 }}>
    <If when={!!props.label}><Label text={props.label ?? ""} bold size={12} muted /></If>
    <Input
      testID={props.testID}
      accessibilityLabel={props.accessibilityLabel ?? props.label ?? props.placeholder}
      value={props.value}
      onChange={({ value }) => props.onChange(value)}
      placeholder={props.placeholder}
      multiline={props.multiline}
      autoCapitalize="none"
      autoCorrect={false}
      autoComplete="off"
      readonly={props.disabled}
      style={{
        minHeight: 44,
        height: props.multiline ? 150 : undefined,
        paddingHorizontal: 12, paddingVertical: 10,
        borderWidth: 1, borderColor: colors().border, borderRadius: RADIUS.control,
        color: colors().text,
        fontSize: props.multiline ? 12 : 14,
        fontFamily: props.multiline ? "monospace" : undefined,
        backgroundColor: colors().bg,
        placeholderColor: colors().faint,
      }}
      focusStyle={{ borderColor: colors().accent }} />
    <If when={!!props.helper}><Label text={props.helper ?? ""} muted size={11.5} /></If>
  </View>;
}

export function SearchField(props: { value: string; onChange: (value: string) => void; placeholder: string; testID?: string; accessibilityLabel?: string }) {
  return <View style={{
    flexDirection: "row", alignItems: "center", minHeight: 44, gap: 8,
    paddingLeft: 12, paddingRight: props.value ? 2 : 12,
    borderRadius: RADIUS.control, borderWidth: 1, borderColor: colors().border,
    backgroundColor: colors().surface,
  }} focusWithinStyle={{ borderColor: colors().accent }}>
    <Icon name="search" size={16} color={colors().faint} />
    <Input
      testID={props.testID}
      accessibilityLabel={props.accessibilityLabel ?? props.placeholder}
      value={props.value}
      onChange={({ value }) => props.onChange(value)}
      placeholder={props.placeholder}
      autoCorrect={false}
      autoCapitalize="none"
      autoComplete="off"
      style={{ flex: 1, minWidth: 0, minHeight: 42, paddingHorizontal: 0, paddingVertical: 8, borderWidth: 0, backgroundColor: "transparent", color: colors().text, placeholderColor: colors().faint, fontSize: 14 }} />
    <If when={!!props.value}>
      <Button
        testID={props.testID ? `${props.testID}-clear` : undefined}
        accessibilityLabel={tr("清空搜索", "Clear search")}
        onPress={() => props.onChange("")}
        style={{ width: 40, height: 40, borderRadius: 8, alignItems: "center", justifyContent: "center" }}
        hoverStyle={{ backgroundColor: colors().hover }}
        activeStyle={{ opacity: 0.7 }}>
        <Icon name="x" size={15} color={colors().muted} />
      </Button>
    </If>
  </View>;
}

export function BottomSheet(props: {
  open: boolean;
  onClose: () => void;
  title?: string;
  subtitle?: string;
  scrollable?: boolean;
  closeOnSwipe?: boolean;
  closeOnEscape?: boolean;
  closeOnBackdropPress?: boolean;
  onCancel?: () => void;
  error?: string;
  footer?: () => JSX.Element;
  children?: JSX.Element;
  testID?: string;
  closeTestID?: string;
}) {
  const open = computed(() => props.open);
  let seenDismiss = sheetDismissSeq();
  effect(() => {
    const seq = sheetDismissSeq();
    if (seq !== seenDismiss) {
      seenDismiss = seq;
      if (props.open) props.onClose();
    }
  });
  const backdrop = () => ({ backgroundColor: colorScheme() === "dark" ? "#05070ca0" : "#0b122466" });
  const innerContent = (showHandle: boolean) => <View style={{ paddingBottom: showHandle ? "env(safe-area-inset-bottom)" : 0, maxHeight: "100%" }}>
    <If when={showHandle}>
      <View style={{ alignItems: "center", paddingTop: 10, paddingBottom: 4 }}>
        <View style={{ width: 36, height: 4, borderRadius: 2, backgroundColor: colors().border }} />
      </View>
    </If>
    <If when={!!props.title}>
      <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 12, paddingLeft: 20, paddingRight: 8, paddingTop: showHandle ? 0 : 12, paddingBottom: 8 }}>
        <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
          <Text style={{ fontSize: 17, fontWeight: "600", color: colors().text, numberOfLines: 1, textOverflow: "ellipsis" }}>{props.title ?? ""}</Text>
          <If when={!!props.subtitle}><Label text={props.subtitle ?? ""} muted size={12} lines={2} /></If>
        </View>
        <IconButton testID={props.closeTestID} icon="x" label={tr("关闭", "Close")} onPress={props.onClose} />
      </View>
    </If>
    <If when={!!props.scrollable}>
      <Scroll keyboardShouldPersistTaps="handled" keyboardDismissMode="on-drag" style={{ maxHeight: Math.max(200, Math.floor(windowSize().height * (showHandle ? 0.62 : 0.66))) }} contentStyle={{ paddingHorizontal: 20, paddingBottom: 16, gap: 14 }}>
        {props.children}
      </Scroll>
    </If>
    <If when={!props.scrollable}>
      <View style={{ paddingHorizontal: 20, paddingBottom: 16, gap: 14 }}>{props.children}</View>
    </If>
    <If when={!!props.error}><View style={{ paddingHorizontal: 20, paddingBottom: 12 }}><Notice text={props.error ?? ""} tone="danger" /></View></If>
    <If when={!!props.footer}>
      <View style={{ paddingHorizontal: 20, paddingVertical: 12, borderTopWidth: 1, borderColor: colors().border }}>
        {props.footer?.()}
      </View>
    </If>
  </View>;

  return <>
    <If when={compact()}>
      <Sheet
        testID={props.testID}
        open={open()}
        side="bottom"
        closeOnSwipe={props.closeOnSwipe ?? !props.scrollable}
        closeOnEscape={props.closeOnEscape ?? true}
        closeOnBackdropPress={props.closeOnBackdropPress ?? true}
        onOpenChange={({ open: next }) => { if (!next && props.open) props.onClose(); }}
        onCancel={props.onCancel}
        backdropStyle={backdrop()}
        popupStyle={{
          width: "100%",
          maxHeight: "88%",
          backgroundColor: colors().surface,
          borderTopLeftRadius: RADIUS.sheet,
          borderTopRightRadius: RADIUS.sheet,
          borderWidth: 1,
          borderBottomWidth: 0,
          borderColor: colors().border,
          paddingBottom: "env(keyboard-inset-height)",
          boxShadow: elevation("popover"),
        }}>
        {innerContent(true)}
      </Sheet>
    </If>
    <If when={!compact()}>
      <Dialog
        testID={props.testID}
        open={open()}
        closeOnEscape={props.closeOnEscape ?? true}
        closeOnBackdropPress={props.closeOnBackdropPress ?? true}
        onOpenChange={({ open: next }) => { if (!next && props.open) props.onClose(); }}
        onCancel={props.onCancel}
        animation="fade"
        backdropStyle={backdrop()}
        style={{
          marginHorizontal: "auto",
          marginVertical: "auto",
          width: Math.min(520, windowSize().width - 48),
          maxHeight: windowSize().height - 48,
          padding: 0,
          gap: 0,
          backgroundColor: colors().surface,
          borderRadius: RADIUS.surface,
          borderWidth: 1,
          borderColor: colors().border,
          boxShadow: elevation("popover"),
        }}>
        {innerContent(false)}
      </Dialog>
    </If>
  </>;
}

export async function confirmDialog(options: {
  title: string;
  description?: string;
  confirmLabel: string;
  cancelLabel?: string;
  destructive?: boolean;
}): Promise<boolean> {
  const result = await AlertDialog.show({
    title: options.title,
    description: options.description,
    presentation: "alert",
    actions: [
      { id: "cancel", label: options.cancelLabel ?? tr("取消", "Cancel"), role: "cancel" },
      { id: "confirm", label: options.confirmLabel, role: options.destructive ? "destructive" : "default" },
    ],
  });
  return result.action === "confirm";
}

export function ToastHost() {
  const progress = createAnimatedValue(0);
  effect(() => {
    const visible = !!toast();
    void progress.stop();
    if (reduceMotion()) { progress.set(visible ? 1 : 0); return; }
    if (visible) {
      progress.set(0);
      void progress.timing(1, { duration: 180, easing: "easeOut" });
    } else {
      progress.set(0);
    }
  });
  cleanup(() => void progress.stop());
  return <If when={!!toast()}>
    <Overlay style={{ position: "fixed", left: 16, right: 16, bottom: compact() ? 78 : 24, marginBottom: "env(safe-area-inset-bottom)", alignItems: "center", pointerEvents: "none" }}>
      <View testID="toast-banner" style={{
        maxWidth: 420, paddingHorizontal: 16, paddingVertical: 10, borderRadius: RADIUS.pill,
        backgroundColor: colorScheme() === "dark" ? "#222736" : "#111622",
        borderWidth: 1, borderColor: colorScheme() === "dark" ? "#343b4f" : "#262e40",
        boxShadow: elevation("popover"),
        opacity: progress,
        transform: [{ translateY: progress.interpolate({ inputRange: [0, 1], outputRange: [8, 0] }) }],
      }}>
        <Text style={{ fontSize: 13, fontWeight: "500", color: "#ffffff", textAlign: "center" }}>{toast()}</Text>
      </View>
    </Overlay>
  </If>;
}
